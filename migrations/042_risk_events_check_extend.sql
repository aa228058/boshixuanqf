-- +goose Up
-- +goose StatementBegin
-- 042: 扩展 risk_events.event_type 的 CHECK 约束
-- 代码侧实际使用但约束白名单缺失的两个值：broadcast_group_restricted / join_fail
-- 不扩展会导致 recordRiskEvent 每次插入都 23514 报错（风险事件丢失）。
ALTER TABLE risk_events DROP CONSTRAINT IF EXISTS risk_events_event_type_check;
ALTER TABLE risk_events ADD CONSTRAINT risk_events_event_type_check
  CHECK (event_type IN ('flood_wait','phone_banned','restriction','rate_limit','peer_flood',
    'account_banned','session_revoked','broadcast_flood_wait','broadcast_fail',
    'account_restricted','session_restore_failed',
    'broadcast_group_restricted','join_fail'));
-- +goose StatementEnd
