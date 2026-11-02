-- +goose Up
-- 修复战役定时/循环(BUG#2): ResetCampaign/ActivateDue 引用 campaigns.run_at,
-- 但 034 建表时漏了该列, 导致:
--   1) 定时战役(run_at 未来)到点永不激活, 永远卡在 queued
--   2) 循环战役(LoopMinutes)跑完一轮 ResetCampaign 报错, 静默变 completed
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS run_at TIMESTAMPTZ;

-- ActivateDue 每 2 秒扫 queued 且到点的战役, 部分索引加速
CREATE INDEX IF NOT EXISTS idx_campaigns_run ON campaigns(status, run_at) WHERE status = 'queued';

-- +goose Down
DROP INDEX IF EXISTS idx_campaigns_run;
ALTER TABLE campaigns DROP COLUMN IF EXISTS run_at;
