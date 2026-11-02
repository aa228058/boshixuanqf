-- +goose Up
-- 扩展 tasks.type 允许的任务类型: 代码里早已加入 scheduled_broadcast / monitor_scrape,
-- 但 001_init 的 CHECK 约束从未同步, 创建定时任务时报 tasks_type_check 违规。
ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_type_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_type_check CHECK (type IN (
    'broadcast',
    'scrape_members',
    'scrape_links',
    'join_groups',
    'monitor_forward',
    'monitor_reply',
    'scheduled_broadcast',
    'monitor_scrape'
));

-- +goose Down
ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_type_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_type_check CHECK (type IN (
    'broadcast',
    'scrape_members',
    'scrape_links',
    'join_groups',
    'monitor_forward',
    'monitor_reply'
));
