-- +goose Up
-- 061: 抽奖私聊机器人激活(2026-10-21)
-- 参与成功后自动私聊抽奖机器人发激活语(默认 /start), 建立私聊通道——
-- 中奖时机器人会私聊通知(恭喜中奖啦), 这是最可靠的中奖判定信号(无同名误判)
ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS raffle_bot_ping_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS raffle_bot_ping_text TEXT NOT NULL DEFAULT '/start';

-- +goose Down
ALTER TABLE tenants
    DROP COLUMN IF EXISTS raffle_bot_ping_enabled,
    DROP COLUMN IF EXISTS raffle_bot_ping_text;
