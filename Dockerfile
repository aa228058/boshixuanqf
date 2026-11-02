# syntax=docker/dockerfile:1
# R87: BuildKit 构建语法声明 + 缓存挂载(npm/go 依赖缓存持久化, 增量构建不再重复下载)
FROM node:20-alpine AS webbuild
WORKDIR /build
COPY web/package*.json ./
# BUG#6 修复: 包内 lock 文件过期(缺 esbuild@0.28.2), npm ci 一致性校验失败; install 按 package.json 重新解析
# R87: npm 缓存挂载到 BuildKit 缓存目录, 二次构建跳过下载
RUN --mount=type=cache,target=/root/.npm \
    npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build

# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY go.mod go.sum ./
# R87: go module 缓存挂载
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source
COPY . .
COPY --from=webbuild /build/dist ./web/dist

# Build
# R87: go build 缓存挂载, 增量编译秒级完成
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/server ./cmd/server

# Runtime stage
# BUG#5 修复: alpine:3.26 标签不存在, 构建第一步即失败
FROM alpine:3.21

# pg_dump 供 db-backup 使用, 否则每分钟刷重试日志
# R182: unrar/p7zip 用于协议号 rar/7z 压缩包导入(handler extractArchiveCLI)
# R182: p7zip 用于协议号 rar/7z 压缩包导入(7z 命令内置 RAR 解码, rar 也走它, 无需 unrar 包)
RUN apk --no-cache add ca-certificates tzdata postgresql16-client p7zip

WORKDIR /app

COPY --from=builder /app/server /app/server
COPY --from=builder /app/migrations /app/migrations
# P1-36 审计修复: 补拷 docs/——/docs/ 路由引用 web/dist 的 ../docs,
# 原镜像没拷导致 /docs/ 404(swagger 文档页打不开)
COPY --from=builder /app/docs /app/docs
# BUG#8 修复: runtime 阶段必须拷入前端构建产物, 否则 SPA 路由 404
COPY --from=builder /app/web/dist /app/web/dist

EXPOSE 8080

# D-03: 非 root 运行
RUN adduser -D -u 10001 appuser && chown -R appuser /app
USER appuser

# D-02: 容器健康检查
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
	CMD wget -qO- http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/app/server"]
