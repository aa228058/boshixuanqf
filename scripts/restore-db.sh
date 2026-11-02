#!/bin/bash
# ============================================================
# 数据库恢复脚本 (对应 internal/backup/db.go 的每小时自动备份)
# 备份文件: backups/db/db_YYYYMMDD_HHMMSS.sql.gz (pg_dump -Fc 自定义格式)
#
# 用法:
#   ./scripts/restore-db.sh backups/db/db_20260916_092649.sql.gz
#
# 恢复前必读:
#   1. 会清空并重建 tg_sender 库, 当前数据全部丢失——先备份现状!
#      pg_dump -Fc -h 127.0.0.1 -U postgres -d tg_sender -f /tmp/before_restore.sql.gz
#   2. 恢复期间停止 tg-sender 进程
#   3. 恢复后重启 tg-sender (migration 会自动补齐备份后新增的迁移)
# ============================================================
set -euo pipefail

BACKUP_FILE="${1:?用法: $0 <备份文件.sql.gz>}"

if [ ! -f "$BACKUP_FILE" ]; then
  echo "❌ 备份文件不存在: $BACKUP_FILE"
  echo "可用备份:"; ls -lh backups/db/*.sql.gz 2>/dev/null | tail -5
  exit 1
fi

echo "⚠️  警告: 即将清空 tg_sender 库并恢复 $BACKUP_FILE"
echo "    当前数据将丢失! 按 Ctrl+C 取消, 或输入 YES 继续:"
read -r CONFIRM
[ "$CONFIRM" = "YES" ] || { echo "已取消"; exit 0; }

echo "→ 停止 tg-sender..."
pkill -x tg-sender 2>/dev/null || true
sleep 1

echo "→ 重建空库..."
psql -h 127.0.0.1 -U postgres -c "DROP DATABASE IF EXISTS tg_sender;" -c "CREATE DATABASE tg_sender OWNER postgres;"

echo "→ 恢复数据 ($BACKUP_FILE)..."
# Z4 修复: -c(clean) 在空库/对象缺失时会刷大量 NOTICE; --if-exists 让 DROP 语句
# 带 IF EXISTS 前缀, NOTICE 消除, 日志干净
gunzip -c "$BACKUP_FILE" | pg_restore -h 127.0.0.1 -U postgres -d tg_sender --no-owner --no-privileges -c --if-exists || true
# 注: -c 清理已存在对象, 目标库已为空所以冲突告警可忽略; || true 容忍非致命告警

echo "→ 重启 tg-sender (migration 自动补齐)..."
nohup ./tg-sender > tg-sender.log 2>&1 &

echo "✅ 恢复完成, 进程已启动。观察日志: tail -f tg-sender.log"
