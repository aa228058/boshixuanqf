#!/usr/bin/env bash
# F-30 密钥轮换脚本——在部署目录执行（需已安装 docker compose + sudo postgres 权限）
# 用法：./scripts/rotate-secrets.sh
# 注意：SESSION_ENCRYPTION_KEY 轮换会使存量加密 TG 会话失效（需选手册 0.3 方案 A/B）
set -euo pipefail

ENV_FILE="${1:-.env}"
cd "$(dirname "$ENV_FILE")"
cp "$ENV_FILE" "$ENV_FILE.bak-$(date +%Y%m%d%H%M)"

sed -i "s|^JWT_SECRET=.*|JWT_SECRET=$(openssl rand -hex 32)|" "$ENV_FILE"
sed -i "s|^ADMIN_JWT_SECRET=.*|ADMIN_JWT_SECRET=$(openssl rand -hex 32)|" "$ENV_FILE"
sed -i "s|^SESSION_ENCRYPTION_KEY=.*|SESSION_ENCRYPTION_KEY=$(openssl rand -hex 32)|" "$ENV_FILE"
sed -i "s|^DB_PASSWORD=.*|DB_PASSWORD=$(openssl rand -base64 24 | tr -dc 'a-zA-Z0-9' | head -c 24)|" "$ENV_FILE"
sed -i "s|^ADMIN_PASSWORD=.*|ADMIN_PASSWORD=$(openssl rand -base64 18 | tr -dc 'a-zA-Z0-9' | head -c 20)|" "$ENV_FILE"

NEW_DB_PASS=$(grep '^DB_PASSWORD=' "$ENV_FILE" | cut -d= -f2)
sudo -u postgres psql -c "ALTER USER tg_sender WITH PASSWORD '$NEW_DB_PASS';"

docker compose up -d --build
sleep 10
curl -sf https://localhost/health || curl -sf http://localhost:8080/health

echo "轮换完成。旧值在 $ENV_FILE.bak-*（验证无误后 shred -u 销毁）。"
echo "⚠️ 全部用户需重新登录；SESSION_ENCRYPTION_KEY 已换，存量 TG 会话按手册 0.3 方案处理。"
