package monitor

import (
	"context"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/tg"
	"github.com/mtgo-labs/mtgo/telegram/types"
)

// tapVerification 消息分发处的验证入口(挂在 monitorLoop 的 OnMessage 回调里)。
// mtgo 的 types.Message 是蒸馏类型, 各 fork 字段不同:
//   - 已确认通用: ChatID / Text
//   - 内联键盘/原生投票/图片: 见 rawTG() 适配点
// 即使拿不到原始消息, 「文字算式类」验证(回复答案即可)也能直接处理。
func (e *Engine) tapVerification(ctx context.Context, cl *telegram.Client, tenantID, accountID uuid.UUID, chatID int64, chatTitle, fromName, text string, raw *tg.Message) {
	if e.vstates == nil || e.aiKeyProvider == nil {
		return
	}
	_, _, _, enabled := e.aiKeyProvider(tenantID)
	if !enabled {
		return
	}
	if raw != nil {
		e.HandleVerification(ctx, cl, tenantID, accountID, raw, chatID, chatTitle)
		return
	}
	// 无原始消息兜底: 仅处理"文字算式+关键词"型(直接回复答案, 无需按钮)
	if text == "" || !verifyKeywordRe.MatchString(text) {
		return
	}
	if _, ok := solveArithmetic(text); !ok {
		return
	}
	if st := e.vstates.get(accountID, chatID); st != nil {
		return // 已有进行中验证
	}
	// 构造极简候选(仅文本), 走标准状态机
	// P1-25 审计修复: 原兜底消息无 PeerID——resolvePeer 的 PeerChat 分支只需
	// rawPeer 非空即可本地构造 peer(零网络), 缺了它被 HandleVerification 的
	// cand.peer==nil 检查直接丢弃。此处用已知 chatID 补上。
	peerChatID := chatID
	if peerChatID < 0 {
		peerChatID = -peerChatID
	}
	e.HandleVerification(ctx, cl, tenantID, accountID, &tg.Message{
		ID:      0,
		Message: text,
		PeerID:  &tg.PeerChat{ChatID: peerChatID},
	}, chatID, chatTitle)
}

// rawTG 从蒸馏消息取原始 tg.Message。
// mtgo 官方 types.Message 结构含 Raw tg.MessageClass 字段(pkg.go.dev 可查),
// 此处直接类型断言取回完整原始消息(含内联键盘/Poll/图片/Peer)。
func rawTG(m *types.Message) *tg.Message {
	if m == nil || m.Raw == nil {
		return nil
	}
	if tm, ok := m.Raw.(*tg.Message); ok {
		return tm
	}
	return nil
}
