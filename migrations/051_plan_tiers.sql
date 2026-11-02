-- +goose Up
-- 套餐重定义(2026-10): 30/70/140 三档付费 + 免费体验版全功能开放(用额度/规模差异化)。
-- 注意: 群发日限用既有列 max_daily_broadcasts, 本迁移只新增搜群日限列。

ALTER TABLE plans ADD COLUMN IF NOT EXISTS daily_search_limit INT NOT NULL DEFAULT 0;

-- 日额度计数器(原子 upsert, 防并发超发)
CREATE TABLE IF NOT EXISTS plan_daily_counters (
    tenant_id UUID NOT NULL,
    day DATE NOT NULL DEFAULT CURRENT_DATE,
    kind VARCHAR(32) NOT NULL,          -- broadcast_msgs / group_search
    used INT NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, day, kind)
);

-- ============ 各档 features 按功能阶梯分配(后台可再调) ============
-- 基础版 = 手动核心 7 key; 专业版 = +自动化(无 ai_verify); 企业版 = 全量。
UPDATE plans SET name='免费体验版', price_monthly=0, max_accounts=2, max_concurrent=1,
    max_daily_broadcasts=50, daily_search_limit=3,
    can_broadcast=true, can_scrape_members=true, can_scrape_links=true, can_join_groups=true,
    can_monitor=true, can_export=true, can_use_templates=true,
    features='["broadcast","scrape_members","scrape_links","join_groups","monitor","templates","export","scheduled_tasks","discover","search","proxy","invite","verifier","ai_verify","checkup","dict","api"]'::jsonb,
    description='全功能开放体验: 2账号/1并发, 群发限50条/天, 采集限1次/天, 搜群限3次/天'
WHERE plan_id='free';

UPDATE plans SET name='基础版', price_monthly=30, max_accounts=10, max_concurrent=5,
    max_daily_broadcasts=0, daily_search_limit=5,
    can_broadcast=true, can_scrape_members=true, can_scrape_links=false, can_join_groups=true,
    can_monitor=false, can_export=true, can_use_templates=false,
    features='["broadcast","scrape_members","join_groups","checkup","discover","search","export"]'::jsonb,
    description='10账号/5并发 手动核心不限量: 群发/加群/采集/检测/群库(看100条)/搜群5次/导出'
WHERE plan_id='basic';

UPDATE plans SET name='专业版', price_monthly=70, max_accounts=30, max_concurrent=15,
    max_daily_broadcasts=0, daily_search_limit=0,
    can_broadcast=true, can_scrape_members=true, can_scrape_links=true, can_join_groups=true,
    can_monitor=true, can_export=true, can_use_templates=true,
    features='["broadcast","scrape_members","scrape_links","join_groups","monitor","templates","export","scheduled_tasks","discover","search","proxy","invite","verifier","checkup","dict"]'::jsonb,
    description='30账号/15并发 自动化全家桶: 定时/批量/邀请/简介挖掘/文案变量/入群验证/代理10条'
WHERE plan_id='pro';

UPDATE plans SET name='企业版', price_monthly=140, max_accounts=100, max_concurrent=40,
    max_daily_broadcasts=0, daily_search_limit=0,
    can_broadcast=true, can_scrape_members=true, can_scrape_links=true, can_join_groups=true,
    can_monitor=true, can_export=true, can_use_templates=true,
    features='["broadcast","scrape_members","scrape_links","join_groups","monitor","templates","export","scheduled_tasks","discover","search","proxy","invite","verifier","ai_verify","checkup","dict","api"]'::jsonb,
    description='100账号/40并发 全功能不限量: AI图片验证/API接入/代理不限/优先支持'
WHERE plan_id='enterprise';

-- +goose Down
ALTER TABLE plans DROP COLUMN IF EXISTS daily_search_limit;
DROP TABLE IF EXISTS plan_daily_counters;
