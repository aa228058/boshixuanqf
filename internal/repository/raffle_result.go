package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RaffleResultRepository 抽奖结果(参与回执/中奖)持久化
type RaffleResultRepository struct {
	db *pgxpool.Pool
}

func NewRaffleResultRepository(db *pgxpool.Pool) *RaffleResultRepository {
	return &RaffleResultRepository{db: db}
}

// HasJoined 持久化防重: 该账号在该群对该关键词已有参与记录(joined/winner)即视为已参与,
// 重启服务也不丢——"一个号一个活动一生只参与一次"的兜底(内存 10 分钟窗口之外的保险)
func (r *RaffleResultRepository) HasJoined(ctx context.Context, tenantID, accountID uuid.UUID, chatID int64, keyword string) (bool, error) {
	var n int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM raffle_results
		WHERE tenant_id=$1 AND account_id=$2 AND tg_chat_id=$3 AND keyword=$4
		  AND kind IN ('joined','winner')`,
		tenantID, accountID, chatID, keyword).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("raffle has joined: %w", err)
	}
	return n > 0, nil
}

// Insert R121: 活动聚合入库(签名不变, 调用方零改动) —— 同一活动(账号+群+关键词)只保留一行,
// 状态就地流转: joined 插入不重复 / winner 就地升级记奖品(最高优先级) / mention 仅从 joined 升级。
func (r *RaffleResultRepository) Insert(ctx context.Context, tenantID, accountID uuid.UUID, chatID int64, kind, keyword, prize, detail string) error {
	key := accountID.String() + ":" + fmt.Sprintf("%d", chatID) + ":" + keyword
	switch kind {
	case "joined":
		// R143 修复: 原 VALUES 里 $4/$6 未使用(kind/prize 已内联字面量), pgx 推断不出未使用参数的类型
		// 报 "could not determine data type of parameter $4"(42P18) —— 参数编号与实参一一对齐
		_, err := r.db.Exec(ctx, `
			INSERT INTO raffle_results (tenant_id, account_id, tg_chat_id, kind, keyword, prize, detail, activity_key)
			VALUES ($1,$2,$3,'joined',$4,'',$5,$6)
			ON CONFLICT (tenant_id, activity_key) DO NOTHING`,
			tenantID, accountID, chatID, keyword, detail, key)
		if err != nil {
			return fmt.Errorf("raffle result upsert: %w", err)
		}
		return nil
	case "winner":
		// R126 加固: UPDATE 影响 0 行(开奖先于参与记录落库的边界序)时回退 INSERT,
		// 保证中奖信息绝不丢——UPSERT 语义。
		// R143 修复: 原语句引用 $5 但只传了 4 个实参, 且 $2/$3 未使用 —— 重新对齐
		res, err := r.db.Exec(ctx, `
			UPDATE raffle_results SET kind='winner', prize=$2, created_at=now()
			WHERE tenant_id=$1 AND activity_key=$3`,
			tenantID, prize, key)
		if err != nil {
			return fmt.Errorf("raffle result winner update: %w", err)
		}
		if n := res.RowsAffected(); n == 0 {
			_, err = r.db.Exec(ctx, `
				INSERT INTO raffle_results (tenant_id, account_id, tg_chat_id, kind, keyword, prize, detail, activity_key)
				VALUES ($1,$2,$3,'winner',$4,$5,$6,$7)
				ON CONFLICT (tenant_id, activity_key) DO UPDATE SET kind='winner', prize=EXCLUDED.prize, created_at=now()`,
				tenantID, accountID, chatID, keyword, prize, detail, key)
			if err != nil {
				return fmt.Errorf("raffle result winner insert: %w", err)
			}
		}
		return nil
	default: // mention: 已开奖未中奖, 不覆盖已中奖; UPDATE 0 行同样回退 INSERT
		// R143 修复: 原语句引用 $4 但只传 3 个实参(会报参数个数不匹配), 重新对齐
		res, err := r.db.Exec(ctx, `
			UPDATE raffle_results SET kind='mention', created_at=now()
			WHERE tenant_id=$1 AND activity_key=$2 AND kind != 'winner'`,
			tenantID, key)
		if err != nil {
			return fmt.Errorf("raffle result mention update: %w", err)
		}
		if n := res.RowsAffected(); n == 0 {
			_, err = r.db.Exec(ctx, `
				INSERT INTO raffle_results (tenant_id, account_id, tg_chat_id, kind, keyword, prize, detail, activity_key)
				VALUES ($1,$2,$3,'mention',$4,'',$5,$6)
				ON CONFLICT (tenant_id, activity_key) DO NOTHING`,
				tenantID, accountID, chatID, keyword, detail, key)
			if err != nil {
				return fmt.Errorf("raffle result mention insert: %w", err)
			}
		}
		return nil
	}
}

// DeleteAll R128: 清空租户全部抽奖记录(参与记录卡的重置按钮)
func (r *RaffleResultRepository) DeleteAll(ctx context.Context, tenantID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM raffle_results WHERE tenant_id = $1`, tenantID)
	return err
}

// List 查询租户最近记录(前端参与记录列表)
func (r *RaffleResultRepository) List(ctx context.Context, tenantID uuid.UUID, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT r.id, r.account_id, r.tg_chat_id, r.kind, r.keyword, r.prize, r.detail, r.created_at, a.phone, a.remark, s.first_name, s.last_name, g.title
		FROM raffle_results r
		LEFT JOIN accounts a ON a.account_id = r.account_id
		LEFT JOIN sessions s ON s.session_id = a.session_name
		LEFT JOIN LATERAL (
			SELECT g2.title FROM groups g2 WHERE g2.tenant_id = r.tenant_id AND g2.tg_chat_id = r.tg_chat_id LIMIT 1
		) g ON true
		WHERE r.tenant_id=$1
		ORDER BY r.created_at DESC LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("raffle result list: %w", err)
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id int64
		var accID uuid.UUID
		var chatID int64
		var kind, keyword, prize, detail string
		var createdAt interface{}
		var phone, remark, firstName, lastName, groupTitle *string
		if err := rows.Scan(&id, &accID, &chatID, &kind, &keyword, &prize, &detail, &createdAt, &phone, &remark, &firstName, &lastName, &groupTitle); err != nil {
			continue
		}
		strOr := func(p *string) string { if p != nil { return *p }; return "" }
		tgName := strings.TrimSpace(strOr(firstName) + " " + strOr(lastName))
		out = append(out, map[string]interface{}{
			"id": id, "account_id": accID, "tg_chat_id": chatID,
			"kind": kind, "keyword": keyword, "prize": prize, "detail": detail,
			"created_at": createdAt,
			"phone":    strOr(phone),
			"remark":   strOr(remark),
			"tg_name":  tgName, // 061+: 前端显示真实 TG 名, 不再靠手机号/备注
			"chat_title": strOr(groupTitle), // R121: 来源群名(LATERAL join, 一群多账号只取一条)
		})
	}
	return out, nil
}
