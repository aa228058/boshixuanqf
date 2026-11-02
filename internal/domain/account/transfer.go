package account

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/tg"
)

// R181 群转移: 把源账号已加入的群导出成邀请链接, 交给目标账号走标准 join_groups 任务。
// 执行器(间隔/抖动/Flood熔断/任务面板)全部复用, 这里只做"采集+转链"。
//
// 转链规则:
//   - 公开群(username≠"") → https://t.me/username, 目标号 join 任务自行解析
//   - 私群 → MessagesExportChatInvite 试导(只有管理员能导成功), 失败记入跳过清单
//   - 已退出的群(is_member=false)直接不统计

// TransferSkippedGroup 无法转移的群 + 人话原因(前端弹窗展示)
type TransferSkippedGroup struct {
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

// TransferGroupsExport 群转移采集结果
type TransferGroupsExport struct {
	Links   []string               `json:"links"`
	Skipped []TransferSkippedGroup `json:"skipped"`
	Total   int                    `json:"total"`
}

// maxInviteExportsPerRun 单次最多导出多少个私群邀请链接。
// 每个私群一次 RPC + 1.1s 节流, 不设上限的话几百个私群要挂十几分钟。
const maxInviteExportsPerRun = 300

// ExportJoinedGroupLinks 列出源账号已加入的全部群组并转成邀请链接
func (e *Engine) ExportJoinedGroupLinks(ctx context.Context, tenantID, accountID uuid.UUID) (*TransferGroupsExport, error) {
	client, err := e.GetClient(ctx, tenantID, accountID)
	if err != nil {
		return nil, fmt.Errorf("get client: %w", err)
	}
	defer e.ReleaseClient(tenantID, accountID, client)

	dialogs, err := e.getAllDialogs(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("list dialogs: %w", err)
	}

	export := &TransferGroupsExport{Links: []string{}, Skipped: []TransferSkippedGroup{}}
	exported := 0

	for _, d := range dialogs {
		chatType, _ := d["type"].(string)
		if chatType != "group" && chatType != "supergroup" && chatType != "channel" {
			continue
		}
		// 已退出的群不转移(join 也必然失败)
		if isMember, _ := d["is_member"].(bool); !isMember {
			continue
		}
		title, _ := d["title"].(string)
		export.Total++

		// 公开群: t.me/username 直接可用
		if username, _ := d["username"].(string); username != "" {
			export.Links = append(export.Links, "https://t.me/"+username)
			continue
		}

		// 私群: 试导邀请链接
		if exported >= maxInviteExportsPerRun {
			export.Skipped = append(export.Skipped, TransferSkippedGroup{
				Title:  title,
				Reason: fmt.Sprintf("超出单次导出上限(%d个私群), 分批再转", maxInviteExportsPerRun),
			})
			continue
		}

		link, err := e.exportChatInvite(ctx, client, d)
		if err != nil {
			export.Skipped = append(export.Skipped, TransferSkippedGroup{
				Title:  title,
				Reason: classifyInviteExportError(err),
			})
		} else {
			export.Links = append(export.Links, link)
			exported++
		}

		// 节流: exportChatInvite 配额敏感, 私群之间至少间隔 1.1s, 防 FLOOD_WAIT
		select {
		case <-ctx.Done():
			return export, nil
		case <-time.After(1100 * time.Millisecond):
		}
	}

	log.Printf("[transfer] 账号 %s 群转移采集完成: 可转移 %d 个群, 跳过 %d 个", accountID, len(export.Links), len(export.Skipped))
	return export, nil
}

// exportChatInvite 私群导出邀请链接。
// 普通群 InputPeerChat 不需要 AccessHash; 超级群/频道 GetDialogs 不提供 AccessHash,
// 用 ResolvePeer 补(与 join.go 同一惯例)。
func (e *Engine) exportChatInvite(ctx context.Context, client *telegram.Client, d map[string]any) (string, error) {
	chatID, _ := d["tg_chat_id"].(int64)
	chatType, _ := d["type"].(string)

	var peer tg.InputPeerClass
	if chatType == "group" {
		peer = &tg.InputPeerChat{ChatID: chatID}
	} else {
		resolved, err := client.ResolvePeer(ctx, strconv.FormatInt(chatID, 10))
		if err != nil {
			return "", fmt.Errorf("解析群信息失败: %w", err)
		}
		p, ok := resolved.(*tg.InputPeerChannel)
		if !ok {
			return "", fmt.Errorf("群类型不支持导出链接")
		}
		peer = p
	}

	raw, err := client.Raw().MessagesExportChatInvite(ctx, &tg.MessagesExportChatInviteRequest{
		Peer:          peer,
		ExpireDate:    0,
		UsageLimit:    0,
		RequestNeeded: false,
		Title:         "",
	})
	if err != nil {
		return "", err
	}
	inv, ok := raw.(*tg.ChatInviteExported)
	if !ok {
		return "", fmt.Errorf("unexpected invite type %T", raw)
	}
	if inv.Link == "" {
		return "", fmt.Errorf("TG 未返回链接")
	}
	return inv.Link, nil
}

// classifyInviteExportError 把 TG 拒绝原因翻成人话(用户看跳过清单时知道该怎么办)
func classifyInviteExportError(err error) string {
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "chat_admin_required"), strings.Contains(s, "admin_required"):
		return "私有群且源号不是管理员, 无法导出邀请链接"
	case strings.Contains(s, "flood"):
		return "触发 TG 限速, 请稍后重试"
	case strings.Contains(s, "chat_invalid"), strings.Contains(s, "peer_id_invalid"):
		return "群已不存在或源号已退出"
	default:
		return "导出失败: " + err.Error()
	}
}
