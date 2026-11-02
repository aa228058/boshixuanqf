package account

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"tg-sender/internal/domain"
)

// R181: 死会话密钥(AUTH_KEY_UNREGISTERED)的判定/熔断/清除。
// 背景: 会话密钥被 TG 永久吊销后, 池化路径每次 GetClient 都会拨号成功(TCP 层面)、
// 首次 RPC 才 401, 旧逻辑无差别重试 → 8次/秒重拨风暴 + 保活循环无限重连。
// 现在: 任何路径发现死密钥 → markDeadKey(熔断30min+状态置"需重新登录"+风险事件),
// GetClient 入口对熔断账号快速失败; 重新登录时 clearDeadKey。

const deadKeyTTL = 30 * time.Minute

// isAuthKeyDead 判定错误是否"密钥被 TG 永久吊销"(永久死亡, 重试无意义)。
func isAuthKeyDead(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "auth_key_unregistered") ||
		strings.Contains(msg, "auth key invalidated")
}

// deadKeyBlocked 熔断查询: TTL 内的死密钥账号直接拒。
func (e *Engine) deadKeyBlocked(accountID uuid.UUID) bool {
	e.deadKeyMu.Lock()
	defer e.deadKeyMu.Unlock()
	t, ok := e.deadKey[accountID]
	return ok && time.Since(t) < deadKeyTTL
}

// markDeadKey 熔断 + 状态置"需重新登录"(SessionRevoked) + 风险事件。
// R182.1 宽限: 刚登录/读档成功 5 分钟内的号, 遇到 -404 是"gotd 异步 flush 未完成"的时序竞态
// (会话接管/rename 先于最终 key 落盘, 池立刻读档拿到中间态), 档随后会自动变正确。
// 此时熔断 30 分钟是误杀——宽限期内不熔断, 标 offline 交给下一轮重试/用户点连接。
func (e *Engine) markDeadKey(ctx context.Context, tenantID, accountID uuid.UUID) {
	e.deadKeyMu.Lock()
	if t, ok := e.authGrace[accountID]; ok && time.Since(t) < 5*time.Minute {
		e.deadKeyMu.Unlock()
		log.Printf("[client] R182 账号 %s 登录成功仅 %v, -404 判定为存储时序竞态, 宽限不熔断, 标离线待重试", accountID, time.Since(t).Round(time.Second))
		if err := e.updateStatus(ctx, tenantID, accountID, domain.AccountStatusOffline); err != nil {
			log.Printf("[client] R182 宽限标离线失败: %v", err)
		}
		return
	}
	if e.deadKey == nil {
		e.deadKey = make(map[uuid.UUID]time.Time)
	}
	e.deadKey[accountID] = time.Now()
	e.deadKeyMu.Unlock()

	log.Printf("[client] R181 账号 %s 会话密钥已被 TG 吊销(AUTH_KEY_UNREGISTERED), 熔断 %v, 状态置为需重新登录", accountID, deadKeyTTL)
	if err := e.updateStatus(ctx, tenantID, accountID, domain.AccountStatusSessionRevoked); err != nil {
		log.Printf("[client] R181 标记需重新登录失败: %v", err)
	}
	// 事件类型必须落在 risk_events_event_type_check 允许列表内(已有 'session_revoked' 可用,
	// 此前写 'session_key_revoked' 被 CHECK 约束拒绝, 事件从未落库成功)
	e.recordRiskEvent(ctx, tenantID, accountID, "session_revoked", "high", "AUTH_KEY_UNREGISTERED")
}

// noteAuthOK 记录"该账号刚登录/读档成功"(认证成功、会话接管时调用), markDeadKey 据此宽限。
func (e *Engine) noteAuthOK(accountID uuid.UUID) {
	e.deadKeyMu.Lock()
	if e.authGrace == nil {
		e.authGrace = make(map[uuid.UUID]time.Time)
	}
	e.authGrace[accountID] = time.Now()
	e.deadKeyMu.Unlock()
}

// clearDeadKey 重新登录/导入新会话时清除熔断。
func (e *Engine) clearDeadKey(accountID uuid.UUID) {
	e.deadKeyMu.Lock()
	delete(e.deadKey, accountID)
	e.deadKeyMu.Unlock()
}
