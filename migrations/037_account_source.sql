-- +goose Up
-- 账号来源: phone=手机验证码登录(正常号) | protocol=.session导入(协议号)
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS source VARCHAR(10) NOT NULL DEFAULT 'phone';
-- 存量数据回填: 无手机号的 = 之前导入的协议号
UPDATE accounts SET source='protocol' WHERE phone IS NULL OR phone = '';
