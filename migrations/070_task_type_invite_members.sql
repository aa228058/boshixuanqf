-- +goose Up
-- 070: tasks.type CHECK 补 'invite_members'(2026-10-22, 审计 P0-3)
-- domain.TaskTypeInviteMembers 的执行器(engine 分发 + invite.go)与创建链路
-- (通用 POST /tasks)早已存在, 唯一拦路的是 023 的 tasks_type_check 不含该值——
-- 创建邀请任务必报 CHECK 违例, 整条链路被 DB 约束锁死。
-- 注意: 枚举已对齐 domain.TaskType 全量(monitor_forward/monitor_reply 是死类型,
-- 已在 P2-1 删除, 不进 CHECK)。
ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_type_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_type_check CHECK (type IN (
    'broadcast',
    'scrape_members',
    'scrape_links',
    'join_groups',
    'scheduled_broadcast',
    'monitor_scrape',
    'invite_members'
));

-- +goose Down
ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_type_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_type_check CHECK (type IN (
    'broadcast',
    'scrape_members',
    'scrape_links',
    'join_groups',
    'scheduled_broadcast',
    'monitor_scrape'
));
