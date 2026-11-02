package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	// R117 修复: domain import 已删(写方法移除后 WordsByNames 只返 []string, 零引用)
)

// TextDictRepository 文案变量(数据字典), 租户级。
type TextDictRepository struct {
	db *pgxpool.Pool
}

func NewTextDictRepository(db *pgxpool.Pool) *TextDictRepository {
	return &TextDictRepository{db: db}
}

// R117: 词典写侧五方法(Create/Delete/List/AddEntries/DeleteEntry/ListEntries)已删
// ——词典管理 HTTP 层(r116)删除后零调用; 引擎变量替换只读 WordsByNames。
// 若将来词典管理界面回归, 从版本历史恢复本段即可。

// WordsByNames 一次取出多个词典的全部词条(群发前构建替换池, 避免逐条查库)
func (r *TextDictRepository) WordsByNames(ctx context.Context, tenantID uuid.UUID, names []string) (map[string][]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT d.name, e.word FROM text_dicts d
		JOIN text_dict_entries e ON e.dict_id = d.dict_id
		WHERE d.tenant_id = $1 AND d.name = ANY($2)`, tenantID, names)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var n, w string
		if err := rows.Scan(&n, &w); err != nil {
			return nil, err
		}
		out[n] = append(out[n], w)
	}
	return out, rows.Err()
}

// PickWord 随机取一个词(按权重, 发送时用)。无词条返回空串。
func (r *TextDictRepository) PickWord(ctx context.Context, tenantID uuid.UUID, name string) (string, error) {
	var w string
	err := r.db.QueryRow(ctx, `
		SELECT e.word FROM text_dict_entries e
		JOIN text_dicts d ON d.dict_id = e.dict_id
		-- V22 审计修复: weight<=0 时 -ln(random())/weight = Inf/NaN 排序未定义——排除
		WHERE d.tenant_id = $1 AND d.name = $2 AND e.weight > 0
		ORDER BY -ln(random()) / e.weight  -- 指数分布: weight 越大越容易被选中
		LIMIT 1`, tenantID, name).Scan(&w)
	return w, err
}
