-- +goose Up
-- R121: 存量套餐补 notify 特性键 —— r120 新增"邮件通知"特性,
-- 但判定是 features 数组含此键, 存量套餐缺键会被永久锁在通知面板外。
-- 默认全部存量套餐获得权限, 管理端可在套餐编辑页取消勾选收回。
UPDATE plans SET features = features || '["notify"]'::jsonb
WHERE features IS NOT NULL AND NOT (features ? 'notify');
-- features 为 NULL 的老数据兜底置空数组(后端另有 can_* 布尔列兜底逻辑, 不强行补)
-- +goose Down
SELECT 1;
