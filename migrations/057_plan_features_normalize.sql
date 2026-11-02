-- +goose Up
-- 数据修复: 管理端曾把套餐 features 存成数组 ["raffle",...],
-- 后端按 map key 校验查不到导致全部 403。统一转回 map 格式。
UPDATE plans
SET features = (
    SELECT COALESCE(jsonb_object_agg(x, 'true'::jsonb), '{}'::jsonb)
    FROM jsonb_array_elements_text(features) AS x
)
WHERE jsonb_typeof(features) = 'array';

-- +goose Down
-- 不可逆(数组已含全部 key 信息), Down 留空
SELECT 1;
