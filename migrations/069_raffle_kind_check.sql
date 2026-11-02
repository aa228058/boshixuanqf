-- +goose Up
-- 069: raffle_results.kind 补 CHECK(2026-10-22, 审计 P2-31)
-- kind 语义见 058 定义: joined(参与成功)/winner(中奖)/mentioned(开奖提及未中)。
-- 用 NOT VALID 模式: 不扫描存量表(锁时间短), 之后后台 VALIDATE 验证,
-- 若存量有非法值, VALIDATE 报错时可人工清理(见 Down 之外的手动步骤)。
ALTER TABLE raffle_results ADD CONSTRAINT raffle_results_kind_check
    CHECK (kind IN ('joined', 'winner', 'mentioned')) NOT VALID;

-- 存量验证(数据量大时可改为手动执行)
ALTER TABLE raffle_results VALIDATE CONSTRAINT raffle_results_kind_check;

-- +goose Down
ALTER TABLE raffle_results DROP CONSTRAINT IF EXISTS raffle_results_kind_check;
