-- +goose Up
-- 数据修复: 059 前误把 features 存成 map {"raffle":true,...}, 而 Plan.Features 结构体是 []string,
-- 导致 Unmarshal 失败/管理端勾选全丢/功能校验瘫痪。统一转回数组(全链路原生格式)。
UPDATE plans
SET features = COALESCE((
    SELECT jsonb_agg(k ORDER BY k)
    FROM jsonb_object_keys(features) AS k
), '[]'::jsonb)
WHERE jsonb_typeof(features) = 'object';

-- +goose Down
SELECT 1;
