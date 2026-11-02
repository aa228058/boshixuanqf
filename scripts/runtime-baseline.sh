#!/usr/bin/env bash
# U 工作流：运行时行为基线采集（staging，约 35 分钟）
# 前置：ENABLE_PPROF=1；10 个测试租户并发任务负载正在运行
set -euo pipefail
OUT="baseline-$(date +%Y%m%d-%H%M)"
mkdir -p "$OUT"

echo "[1/5] goroutine 采样（3 次，间隔 5 分钟）"
for i in 1 2 3; do
  curl -s 127.0.0.1:6060/debug/pprof/goroutine?debug=1 > "$OUT/goroutine-$i.txt" || true
  sleep 300
done

echo "[2/5] heap/allocs 快照"
curl -s 127.0.0.1:6060/debug/pprof/heap > "$OUT/heap.pb.gz" || true

echo "[3/5] 慢查询（需 log_min_duration_statement=500）"
docker compose logs postgres 2>/dev/null | grep -i "duration" | tail -100 > "$OUT/slow-queries.log" || true

echo "[4/5] suspend 恢复时延验证"
TENANT=$(docker compose exec -T postgres psql -U postgres -d tg_sender -tAc "SELECT tenant_id FROM tenants WHERE status='active' LIMIT 1")
docker compose exec -T postgres psql -U postgres -d tg_sender -c "UPDATE tenants SET status='suspended' WHERE tenant_id='$TENANT'" >/dev/null
sleep 35   # G-03 后后台引擎应在下一个周期（≤30s+tick）跳过该租户
docker compose exec -T postgres psql -U postgres -d tg_sender -c "UPDATE tenants SET status='active' WHERE tenant_id='$TENANT'" >/dev/null
echo "  恢复完成，观察引擎日志确认该租户监控/活动重新纳入"

echo "[5/5] SSE 断线重连抽样"
curl -sN "https://localhost/api/v1/events?token=无效" -m 5 -o /dev/null -w "无效ticket HTTP %{http_code}\n" | tee "$OUT/sse-invalid.txt"

echo "基线输出目录：$OUT"
