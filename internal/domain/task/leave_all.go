package task

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/tg"

	"tg-sender/internal/safego"
)

// LeaveAllResult 一键退群结果统计
type LeaveAllResult struct {
	Left    int      `json:"left"`
	Skipped int      `json:"skipped"`
	Failed  int      `json:"failed"`
	Errors  []string `json:"errors,omitempty"`
}

// LeaveAllGroups 一键退出账号加入的全部群组(频道/超级群).
// 逐个 LeaveChannel 并标记 account_groups.is_joined=false; 间隔 800ms 防风控。
// 注: ListByAccount 带分页参数(大账号群列表分批取); UpsertAccountGroup 签名无 tenantID。
func (e *Engine) LeaveAllGroups(ctx context.Context, tenantID, accountID uuid.UUID) (*LeaveAllResult, error) {
	client, err := e.accEngine.GetClient(ctx, tenantID, accountID)
	if err != nil {
		return nil, fmt.Errorf("获取客户端失败: %w", err)
	}
	defer e.accEngine.ReleaseClient(tenantID, accountID, client)

	res := &LeaveAllResult{}
	// 分页拉全部已加入群(单批 200, 循环直到取完)
	const pageSize = 200
	for offset := 0; ; offset += pageSize {
		groups, err := e.groupRepo.ListByAccount(ctx, tenantID, accountID, pageSize, offset)
		if err != nil {
			return res, fmt.Errorf("查询已加入群组失败: %w", err)
		}
		if len(groups) == 0 {
			break
		}
		for _, g := range groups {
			if g.Type != "channel" && g.Type != "supergroup" {
				res.Skipped++
				continue
			}
			// P0-4 审计修复: TGChatID 是 -100xxx 全格式, InputChannel.ChannelID 要裸 ID——
			// 原样传负数会导致一键退群对超级群 100% 失败
			chID := g.TGChatID
			if chID <= -1000000000000 {
				chID = -chID - 1000000000000
			}
			peer := &tg.InputChannel{ChannelID: chID, AccessHash: g.AccessHash}
			_, err := client.Invoke(ctx, &tg.ChannelsLeaveChannelRequest{Channel: peer})
			if err != nil {
				res.Failed++
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", g.Title, err))
			} else {
				res.Left++
				gid := g.GroupID
				safego.Go(func() {
					c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					_ = e.groupRepo.UpsertAccountGroup(c, accountID, gid, false)
				})
			}
			select {
			case <-ctx.Done():
				return res, ctx.Err()
			case <-time.After(800 * time.Millisecond):
			}
		}
		if len(groups) < pageSize {
			break
		}
	}
	return res, nil
}
