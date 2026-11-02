package grouplib

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/tg"
	"tg-sender/internal/domain"
	"tg-sender/internal/safego"
)

// accountEngine 抽象: grouplib 需要借一个在线账号调用 TG 搜索(避免反向依赖)
type accountEngine interface {
	GetClient(ctx context.Context, tenantID, accountID uuid.UUID) (*telegram.Client, error)
	ReleaseClient(tenantID, accountID uuid.UUID, c *telegram.Client)
}

// SearchAndIngest 关键词搜群: 用指定账号调 TG 全局搜索, 把搜到的公开群/频道归并进群组库。
// 一次搜索最多 20 条, TG 侧有搜索频率限制, 前端需防抖。
func (e *Engine) SearchAndIngest(ctx context.Context, tenantID, accountID uuid.UUID, keyword string) ([]domain.SystemGroup, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("关键词不能为空")
	}

	client, err := e.acc.GetClient(ctx, tenantID, accountID)
	if err != nil {
		return nil, fmt.Errorf("取账号连接失败: %w", err)
	}
	defer e.acc.ReleaseClient(tenantID, accountID, client)

	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := client.Raw().ContactsSearch(sctx, &tg.ContactsSearchRequest{
		Q:     keyword,
		Limit: 20,
	})
	if err != nil {
		return nil, fmt.Errorf("TG 搜索失败: %w", err)
	}

	var out []domain.SystemGroup
	for _, ch := range res.Chats {
		var sg *domain.SystemGroup
		switch c := ch.(type) {
		case *tg.Channel:
			if c.Username == "" {
				continue // 搜到的非公开频道, 无法加入, 不入库
			}
			gt := "channel"
			if c.Megagroup {
				gt = "public"
			}
			sg = &domain.SystemGroup{
				Source: "scraped", Title: c.Title, Username: c.Username,
				TmeLink: "https://t.me/" + c.Username, GType: gt,
				MemberCount: int(c.ParticipantsCount), Status: "active",
			}
		case *tg.Chat:
			// 普通小群(无 username), 无法通过链接加入, 跳过
			continue
		default:
			continue
		}
		if err := e.repo.Upsert(ctx, sg); err != nil {
			log.Printf("[grouplib] 搜索入库失败 %s: %v", sg.TmeLink, err)
			continue
		}
		out = append(out, *sg)
	}
	if len(out) > 0 {
		log.Printf("[grouplib] 关键词「%s」搜到并入库 %d 个群", keyword, len(out))
	}
	return out, nil
}

// SearchAsync 后台执行搜索(前端点了就跑, 结果轮询列表即可看到)
func (e *Engine) SearchAsync(tenantID, accountID uuid.UUID, keyword string) {
	safego.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if _, err := e.SearchAndIngest(ctx, tenantID, accountID, keyword); err != nil {
			log.Printf("[grouplib] 关键词搜索失败: %v", err)
		}
	})
}
