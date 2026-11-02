package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SSE 短期 ticket(审计P0-03): JWT 不再出现在 URL/日志里。
// 流程: 前端 POST /api/v1/events/ticket (带JWT) → 换 60 秒 ticket → EventSource 用 ticket 连接。
// ticket 绑定签发时的租户ID；校验通过后由 JWTAuth 注入 TenantContextKey，
// 下游 SSE Handler 才能订阅正确的租户频道。
// （修复：ticket 未绑租户 + /events 路由在鉴权组外，导致事件流全链路 401。）
type eventTicket struct {
	tenantID  uuid.UUID
	expiresAt time.Time
}

var (
	ticketMu     sync.Mutex
	eventTickets = map[string]eventTicket{}
)

// IssueEventTicket 生成 60 秒有效、绑定租户的 SSE 连接 ticket。
func IssueEventTicket(tenantID uuid.UUID) string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	t := hex.EncodeToString(b)
	ticketMu.Lock()
	// P1-6 审计修复: 原来领了 ticket 却从不连接 SSE 的票永远留在 map(内存泄漏,
	// 可被刷接口灌满)。签发时顺手清理过期票——签发频率低, 每次 O(n) 遍历可忽略。
	now := time.Now()
	for k, v := range eventTickets {
		if now.After(v.expiresAt) {
			delete(eventTickets, k)
		}
	}
	eventTickets[t] = eventTicket{tenantID: tenantID, expiresAt: now.Add(60 * time.Second)}
	ticketMu.Unlock()
	return t
}

// ValidateEventTicket 校验并立即作废(一次性, 防重放)，返回 ticket 绑定的租户ID。
func ValidateEventTicket(t string) (uuid.UUID, bool) {
	ticketMu.Lock()
	defer ticketMu.Unlock()
	entry, ok := eventTickets[t]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(eventTickets, t)
		return uuid.Nil, false
	}
	delete(eventTickets, t)
	return entry.tenantID, true
}
