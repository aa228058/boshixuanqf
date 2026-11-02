package domain

import "time"

// ProbeConfig R119: 账号能力探测配置(管理端全局, 单行 id=1)
// 三个探测目标: 主动私聊小号 / 私有群永久邀请链接 / 公开群链接
type ProbeConfig struct {
	ID            int       `json:"id" db:"id"`
	TestUsername  string    `json:"test_username" db:"test_username"`
	PrivateInvite string    `json:"private_invite" db:"private_invite"`
	PublicLink    string    `json:"public_link" db:"public_link"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

// Ready 三项配置齐全才算就绪(缺一, 对应检测项无法进行)
func (c *ProbeConfig) Ready() bool {
	return c != nil && c.TestUsername != "" && c.PrivateInvite != "" && c.PublicLink != ""
}
