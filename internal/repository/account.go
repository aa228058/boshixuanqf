package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// AccountRepository handles account data access.
type AccountRepository struct {
	db *pgxpool.Pool
}

// NewAccountRepository creates a new account repository.
func NewAccountRepository(db *pgxpool.Pool) *AccountRepository {
	return &AccountRepository{db: db}
}

// List retrieves accounts for a tenant.
func (r *AccountRepository) List(ctx context.Context, tenantID uuid.UUID, source string) ([]*domain.Account, error) {
	q := `SELECT a.account_id, a.tenant_id, a.phone, a.source, a.session_name, a.status, a.level, a.health_score, a.device_profile, a.last_online, a.remark, a.blocked_until, a.created_at, a.avatar_path, a.proxy_url, a.raffle_enabled, a.pool_status,
		COALESCE(s.dc, 0)
		FROM accounts a LEFT JOIN sessions s ON s.session_id = a.session_name
		WHERE a.tenant_id = $1`
	args := []any{tenantID}
	if source == "phone" || source == "protocol" {
		q += ` AND source = $2`
		args = append(args, source)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()

	var accounts []*domain.Account
	for rows.Next() {
		var a domain.Account
		var lastOnline pgtype.Timestamptz
		var remark pgtype.Text
		var blockedUntil pgtype.Timestamptz
		// 精读修复: SELECT 18 列必须与 Scan 目标一一对应(此前漏 proxy_url/dc 会在运行时直接报错;
		// P0-1 又补了 source 列, 列数以磁盘实际为准)
		err := rows.Scan(&a.AccountID, &a.TenantID, &a.Phone, &a.Source, &a.SessionName, &a.Status, &a.Level,
			&a.HealthScore, &a.DeviceProfile, &lastOnline, &remark, &blockedUntil, &a.CreatedAt, &a.AvatarPath, &a.ProxyURL,
			&a.RaffleEnabled, &a.PoolStatus, &a.DC)
		if err != nil {
			return nil, err
		}
		if lastOnline.Valid {
			t := lastOnline.Time
			a.LastOnline = &t
		}
		if remark.Valid {
			a.Remark = remark.String
		}
		if blockedUntil.Valid {
			t := blockedUntil.Time
			a.BlockedUntil = &t
		}
		accounts = append(accounts, &a)
	}
	return accounts, rows.Err()
}

// ListAll retrieves all accounts across all tenants.
func (r *AccountRepository) ListAll(ctx context.Context) ([]*domain.Account, error) {
	// P0-10 审计修复: 原实现无 WHERE 把 standby/cooling 备用号一并返回——
	// 唯一业务调用方 LinkScraper 拿它跑采集, 备用号被拉去空耗配额。
	// 语义收紧为"可跑业务的激活号"(管理端展示走别的 repo 方法, 不受影响)。
	rows, err := r.db.Query(ctx, `
		SELECT account_id, tenant_id, phone, session_name, status, level, health_score, device_profile, last_online, remark, created_at
		FROM accounts WHERE pool_status = 'active' ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list all accounts: %w", err)
	}
	defer rows.Close()

	var accounts []*domain.Account
	for rows.Next() {
		var a domain.Account
		var lastOnline pgtype.Timestamptz
		var remark pgtype.Text
		err := rows.Scan(&a.AccountID, &a.TenantID, &a.Phone, &a.SessionName, &a.Status, &a.Level,
			&a.HealthScore, &a.DeviceProfile, &lastOnline, &remark, &a.CreatedAt)
		if err != nil {
			return nil, err
		}
		if lastOnline.Valid {
			t := lastOnline.Time
			a.LastOnline = &t
		}
		if remark.Valid {
			a.Remark = remark.String
		}
		accounts = append(accounts, &a)
	}
	return accounts, rows.Err()
}

// ListByIDs retrieves multiple accounts by their IDs for a tenant.
func (r *AccountRepository) ListByIDs(ctx context.Context, tenantID uuid.UUID, accountIDs []uuid.UUID) ([]*domain.Account, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT account_id, tenant_id, phone, session_name, status, level, health_score, device_profile, last_online, remark, created_at
		FROM accounts WHERE tenant_id = $1 AND account_id = ANY($2)`, tenantID, accountIDs)
	if err != nil {
		return nil, fmt.Errorf("list accounts by ids: %w", err)
	}
	defer rows.Close()

	var accounts []*domain.Account
	for rows.Next() {
		var a domain.Account
		var lastOnline pgtype.Timestamptz
		var remark pgtype.Text
		err := rows.Scan(&a.AccountID, &a.TenantID, &a.Phone, &a.SessionName, &a.Status, &a.Level,
			&a.HealthScore, &a.DeviceProfile, &lastOnline, &remark, &a.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan account: %w", err)
		}
		if lastOnline.Valid {
			t := lastOnline.Time
			a.LastOnline = &t
		}
		if remark.Valid {
			a.Remark = remark.String
		}
		accounts = append(accounts, &a)
	}
	return accounts, rows.Err()
}

// UpdatePhone R130: 回写协议号手机号(self.Phone 回读结果)
func (r *AccountRepository) UpdatePhone(ctx context.Context, tenantID, accountID uuid.UUID, phone string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE accounts SET phone = $3 WHERE tenant_id = $1 AND account_id = $2`,
		tenantID, accountID, phone)
	return err
}

// ProbeCooldownUntil R119: 读取账号探测冷却截止时间(无记录返回零值)
func (r *AccountRepository) ProbeCooldownUntil(ctx context.Context, tenantID, accountID uuid.UUID) (time.Time, error) {
	var t *time.Time
	err := r.db.QueryRow(ctx,
		`SELECT probe_checked_at FROM accounts WHERE tenant_id = $1 AND account_id = $2`,
		tenantID, accountID).Scan(&t)
	if err != nil {
		return time.Time{}, err
	}
	if t == nil {
		return time.Time{}, nil
	}
	return t.Add(24 * time.Hour), nil // 24h 冷却
}

// SaveProbeResult R119: 保存探测结果+刷新冷却时间
func (r *AccountRepository) SaveProbeResult(ctx context.Context, tenantID, accountID uuid.UUID, result json.RawMessage) error {
	_, err := r.db.Exec(ctx,
		`UPDATE accounts SET probe_result = $3, probe_checked_at = now() WHERE tenant_id = $1 AND account_id = $2`,
		tenantID, accountID, result)
	return err
}

// GetProbeResult R119: 读取最近一次探测结果(无则返回空)
func (r *AccountRepository) GetProbeResult(ctx context.Context, tenantID, accountID uuid.UUID) (json.RawMessage, error) {
	var raw json.RawMessage
	err := r.db.QueryRow(ctx,
		`SELECT probe_result FROM accounts WHERE tenant_id = $1 AND account_id = $2`,
		tenantID, accountID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// GetByID retrieves an account by ID.
func (r *AccountRepository) GetByID(ctx context.Context, tenantID, accountID uuid.UUID) (*domain.Account, error) {
	var a domain.Account
	var lastOnline pgtype.Timestamptz
	var remark pgtype.Text
	var blockedUntil pgtype.Timestamptz
	var capsRaw []byte
	err := r.db.QueryRow(ctx, `
		SELECT a.account_id, a.tenant_id, a.phone, a.session_name, a.status, a.source, a.level, a.health_score, a.device_profile, a.last_online, a.remark, a.blocked_until, a.created_at, a.avatar_path, a.capabilities, a.proxy_url, a.raffle_enabled, a.pool_status,
		COALESCE(s.dc, 0)
		FROM accounts a LEFT JOIN sessions s ON s.session_id = a.session_name
		WHERE a.tenant_id = $1 AND a.account_id = $2`, tenantID, accountID,
	).Scan(&a.AccountID, &a.TenantID, &a.Phone, &a.SessionName, &a.Status, &a.Source, &a.Level,
		&a.HealthScore, &a.DeviceProfile, &lastOnline, &remark, &blockedUntil, &a.CreatedAt, &a.AvatarPath, &capsRaw, &a.ProxyURL, &a.RaffleEnabled, &a.PoolStatus, &a.DC)
	if err == nil && len(capsRaw) > 0 {
		_ = json.Unmarshal(capsRaw, &a.Capabilities)
	}
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	if lastOnline.Valid {
		t := lastOnline.Time
		a.LastOnline = &t
	}
	if remark.Valid {
		a.Remark = remark.String
	}
	if blockedUntil.Valid {
		t := blockedUntil.Time
		a.BlockedUntil = &t
	}
	return &a, nil
}

// Create inserts a new account.
// source 必须入库：037 迁移后该列默认 'phone'，缺省写入导致协议号全被标成正常号（串世界根因）
func (r *AccountRepository) Create(ctx context.Context, a *domain.Account) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO accounts (account_id, tenant_id, phone, session_name, status, level, health_score, device_profile, remark, source)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		a.AccountID, a.TenantID, a.Phone, a.SessionName, a.Status, a.Level, a.HealthScore, a.DeviceProfile, a.Remark, a.Source)
	return err
}

// CountAndCreate atomically counts accounts for a tenant and creates a new one if under limit.
// Returns the new account and current count, or error if limit reached.
func (r *AccountRepository) CountAndCreate(ctx context.Context, a *domain.Account, maxAccounts int) (*domain.Account, int, error) {
	var count int
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		// 056: 备用池不占额度 —— 只统计"启用"状态的账号
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE tenant_id = $1 AND pool_status = 'active'`, a.TenantID).Scan(&count); err != nil {
			return err
		}
		if count >= maxAccounts {
			return fmt.Errorf("account limit reached: %d/%d", count, maxAccounts)
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO accounts (account_id, tenant_id, phone, session_name, status, level, health_score, device_profile, remark, source)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			a.AccountID, a.TenantID, a.Phone, a.SessionName, a.Status, a.Level, a.HealthScore, a.DeviceProfile, a.Remark, a.Source)
		return err
	})
	if err != nil {
		return nil, count, err
	}
	return a, count + 1, nil
}

// UpdateStatus updates account status.
func (r *AccountRepository) UpdateStatus(ctx context.Context, tenantID, accountID uuid.UUID, status domain.AccountStatus) error {
	_, err := r.db.Exec(ctx, `
		UPDATE accounts SET status = $3, last_online = NOW()
		WHERE tenant_id = $1 AND account_id = $2`, tenantID, accountID, status)
	return err
}

// UpdateStatusAndBlockedUntil 原子更新状态+封禁截止时间(P1-20 审计修复):
// markAccountRestricted 原来分两次写(UpdateStatus 后 UpdateBlockedUntil),
// 第二步失败时账号已 restricted 但无 blocked_until——unblockExpiredLoop
// 依赖 blocked_until 恢复, 缺失即永久封禁。单条 UPDATE 消除中间态。
func (r *AccountRepository) UpdateStatusAndBlockedUntil(ctx context.Context, tenantID, accountID uuid.UUID, status domain.AccountStatus, blockedUntil time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE accounts SET status = $3, last_online = NOW(), blocked_until = $4
		WHERE tenant_id = $1 AND account_id = $2`, tenantID, accountID, status, blockedUntil)
	return err
}

// UpdateHealthScore updates account health score.
func (r *AccountRepository) UpdateHealthScore(ctx context.Context, tenantID, accountID uuid.UUID, score int) error {
	_, err := r.db.Exec(ctx, `
		UPDATE accounts SET health_score = $3
		WHERE tenant_id = $1 AND account_id = $2`, tenantID, accountID, score)
	return err
}

// Delete removes an account and its session file.
func (r *AccountRepository) Delete(ctx context.Context, tenantID, accountID uuid.UUID) error {
	var sessionName string

	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		// Read session_name inside transaction to avoid race condition
		var sn sql.NullString
		if err := tx.QueryRow(ctx,
			`SELECT session_name FROM accounts WHERE tenant_id = $1 AND account_id = $2`,
			tenantID, accountID,
		).Scan(&sn); err != nil {
			return fmt.Errorf("get session name: %w", err)
		}
		if sn.Valid {
			sessionName = sn.String
		}

		// Cascade delete related records first (order matters for FK constraints)
		tables := []struct {
			name string
			sql  string
			args []any
		}{
			{"account_quotas", `DELETE FROM account_quotas WHERE account_id = $1`, []any{accountID}},
			{"risk_events", `DELETE FROM risk_events WHERE tenant_id = $1 AND account_id = $2`, []any{tenantID, accountID}},
			{"task_logs", `DELETE FROM task_logs WHERE tenant_id = $1 AND account_id = $2`, []any{tenantID, accountID}},
			{"pending_approvals", `DELETE FROM pending_approvals WHERE tenant_id = $1 AND account_id = $2`, []any{tenantID, accountID}},
			{"scraped_links", `DELETE FROM scraped_links WHERE tenant_id = $1 AND account_id = $2`, []any{tenantID, accountID}},
			{"account_groups", `DELETE FROM account_groups WHERE account_id = $1`, []any{accountID}},
			// 039 迁移新增的 NOT VALID 外键（删除时仍强制）：039 之前账号删除依赖它们为空，
			// 参与过活动/编队的账号会残留引用行，必须先清——这是日志里 fk_campaign_accounts_account 报错的根因
			{"campaign_accounts", `DELETE FROM campaign_accounts WHERE account_id = $1`, []any{accountID}},
			{"squad_members", `DELETE FROM squad_members WHERE account_id = $1`, []any{accountID}},
		}
		for _, t := range tables {
			_, err := tx.Exec(ctx, t.sql, t.args...)
			if err != nil {
				return fmt.Errorf("delete %s: %w", t.name, err)
			}
		}

		// sessions table has no tenant_id/account_id columns; delete by session_name only if non-empty
		if sessionName != "" {
			_, err := tx.Exec(ctx, `DELETE FROM sessions WHERE session_id = $1`, sessionName)
			if err != nil {
				return fmt.Errorf("delete sessions: %w", err)
			}
		}

		// Finally delete the account (FK CASCADE handles any remaining references)
		_, err := tx.Exec(ctx, `DELETE FROM accounts WHERE tenant_id = $1 AND account_id = $2`, tenantID, accountID)
		if err != nil {
			return fmt.Errorf("delete account: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Delete session file after successful DB deletion
	if sessionName != "" {
		sessionPath := "sessions/" + sessionName + ".session"
		if removeErr := os.Remove(sessionPath); removeErr != nil {
			log.Printf("[account] 删除会话文件 %s 失败: %v", sessionPath, removeErr)
		}
	}
	return nil
}

// CountByTenant counts ACTIVE accounts for a tenant (备用池不占额度, 只数启用).
func (r *AccountRepository) CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE tenant_id = $1 AND pool_status = 'active'`, tenantID).Scan(&count)
	return count, err
}

// CountStats returns total and online account counts for a tenant.
func (r *AccountRepository) CountStats(ctx context.Context, tenantID uuid.UUID) (total, online int, err error) {
	err = r.db.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'online')
		FROM accounts WHERE tenant_id = $1`, tenantID).Scan(&total, &online)
	return
}

// CountOnlineYesterday counts online accounts at yesterday's end for a tenant.
func (r *AccountRepository) CountOnlineYesterday(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM accounts
		WHERE tenant_id = $1 AND status = 'online'
		AND last_online >= CURRENT_DATE - INTERVAL '1 day'
		AND last_online < CURRENT_DATE`, tenantID).Scan(&count)
	return count, err
}

// Update updates account fields (level, health_score, device_profile).
func (r *AccountRepository) Update(ctx context.Context, a *domain.Account) error {
	_, err := r.db.Exec(ctx, `
		UPDATE accounts SET level = $3, health_score = $4, device_profile = $5, remark = $6
		WHERE tenant_id = $1 AND account_id = $2`,
		a.TenantID, a.AccountID, a.Level, a.HealthScore, a.DeviceProfile, a.Remark)
	return err
}

// UpdateProxyURL 设置/清除账号代理(socks5://user:pass@host:port, 空串=直连)
func (r *AccountRepository) UpdateProxyURL(ctx context.Context, tenantID, accountID uuid.UUID, proxyURL string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE accounts SET proxy_url = $3 WHERE tenant_id = $1 AND account_id = $2`,
		tenantID, accountID, proxyURL)
	return err
}

// UpdateCapabilities 回写能力体检结果(jsonb)。
func (r *AccountRepository) UpdateCapabilities(ctx context.Context, tenantID, accountID uuid.UUID, caps map[string]interface{}) error {
	raw, err := json.Marshal(caps)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx,
		`UPDATE accounts SET capabilities = $3 WHERE tenant_id = $1 AND account_id = $2`,
		tenantID, accountID, raw)
	return err
}

// UpdateLoginCode R153: 登录验证码捕获落库(该账号最近一条)
func (r *AccountRepository) UpdateLoginCode(ctx context.Context, tenantID, accountID uuid.UUID, code string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE accounts SET last_login_code=$3, login_code_at=now()
		WHERE tenant_id=$1 AND account_id=$2`,
		tenantID, accountID, code)
	return err
}

// CleanupLoginSessions R164: 清理所有登录临时会话(acc_*_login_*), 防表膨胀 + 旧密钥残留
func (r *AccountRepository) CleanupLoginSessions(ctx context.Context) (int64, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM sessions WHERE strpos(session_id, '_login_') > 0`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CleanupLoginSessionsFor R164: 清理指定账号的兄弟临时会话(改名接管成功后调用)
func (r *AccountRepository) CleanupLoginSessionsFor(ctx context.Context, base string) (int64, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM sessions WHERE strpos(session_id, $1 || '_login_') = 1`, base)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// RenameSessionStorage R160: 登录成功后把临时会话改名为正式名(先删旧正式记录防唯一键冲突)
// SessionExists R182: 检测会话存档是否已存在(读档优先判定的依据)
func (r *AccountRepository) SessionExists(ctx context.Context, sessionID string) (bool, error) {
	var cnt int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE session_id=$1`, sessionID).Scan(&cnt)
	return cnt > 0, err
}

// RenameSessionStorage 登录会话接管: 把源(login_xxx)整行内容搬到目标(正式名), 原子完成。
//
// R182.2 关键修复: 旧实现只 UPDATE session_id(改行名), 但 gotd 在 rename 之后仍会向
// login_xxx 名 flush 最终授权密钥(授权迁移后的新 key)——正式名永远停在中间态,
// 池读档拿到 transfer 前的旧 key → 连归属 DC 被 -404 拒绝 → "正常号登录后必死"。
// CTE 语义: 源行存在 → 内容覆盖到目标(含未来 flush 前已写入的最新内容)并删源;
// 源不存在 → 无操作(幂等, 读档直连场景 rename 自然跳过)。
func (r *AccountRepository) RenameSessionStorage(ctx context.Context, from, to string) error {
	_, err := r.db.Exec(ctx, `
		WITH src AS (
			DELETE FROM sessions WHERE session_id = $1
			RETURNING dc, api_id, api_hash, test_mode, auth_key, state,
			          user_id, is_bot, first_name, last_name, username, date, server_address
		)
		INSERT INTO sessions (
			session_id, dc, api_id, api_hash, test_mode, auth_key, state,
			user_id, is_bot, first_name, last_name, username, date, server_address
		)
		SELECT $2, dc, api_id, api_hash, test_mode, auth_key, state,
		       user_id, is_bot, first_name, last_name, username, date, server_address
		FROM src
		ON CONFLICT (session_id) DO UPDATE SET
			dc = EXCLUDED.dc,
			api_id = EXCLUDED.api_id,
			api_hash = EXCLUDED.api_hash,
			test_mode = EXCLUDED.test_mode,
			auth_key = EXCLUDED.auth_key,
			state = EXCLUDED.state,
			user_id = EXCLUDED.user_id,
			is_bot = EXCLUDED.is_bot,
			first_name = EXCLUDED.first_name,
			last_name = EXCLUDED.last_name,
			username = EXCLUDED.username,
			date = EXCLUDED.date,
			server_address = EXCLUDED.server_address`,
		from, to)
	return err
}

// UpdateAvatarPath 回写账号头像文件相对路径(相对 AvatarDir), 空串表示清除。
func (r *AccountRepository) UpdateAvatarPath(ctx context.Context, tenantID, accountID uuid.UUID, avatarPath string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE accounts SET avatar_path = $3 WHERE tenant_id = $1 AND account_id = $2`,
		tenantID, accountID, avatarPath)
	return err
}

// UpdateSessionName updates the session name for an account.
func (r *AccountRepository) UpdateSessionName(ctx context.Context, tenantID, accountID uuid.UUID, sessionName string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE accounts SET session_name = $3
		WHERE tenant_id = $1 AND account_id = $2`,
		tenantID, accountID, sessionName)
	return err
}

// UpdateBlockedUntil updates the account blocked_until field.
// UpdateRemark 更新账号备注(导入自动标签等场景)
func (r *AccountRepository) UpdateRemark(ctx context.Context, tenantID, accountID uuid.UUID, remark string) error {
	_, err := r.db.Exec(ctx, `UPDATE accounts SET remark = $3 WHERE tenant_id = $1 AND account_id = $2`, tenantID, accountID, remark)
	return err
}

// UpdatePoolStatus 切换账号 启用/备用 状态(056)
// 转备用时业务由上层停止; 转正后占套餐额度(由 service 校验额度余量)
func (r *AccountRepository) UpdatePoolStatus(ctx context.Context, tenantID, accountID uuid.UUID, status string) error {
	_, err := r.db.Exec(ctx, `UPDATE accounts SET pool_status = $3 WHERE tenant_id = $1 AND account_id = $2`, tenantID, accountID, status)
	return err
}

// CountActive 统计租户"启用"状态的账号数(备用池不占额度, 只数 active)
func (r *AccountRepository) CountActive(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE tenant_id = $1 AND pool_status = 'active'`, tenantID).Scan(&n)
	return n, err
}

// ClearAvatar 清空头像路径(手动刷新头像时先清, 上线钩子会自动重拉覆盖)
func (r *AccountRepository) ClearAvatar(ctx context.Context, tenantID, accountID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE accounts SET avatar_path = '' WHERE tenant_id = $1 AND account_id = $2`, tenantID, accountID)
	return err
}

// UpdateRaffleEnabled 切换单账号抽奖参与开关(055 迁移)
func (r *AccountRepository) UpdateRaffleEnabled(ctx context.Context, tenantID, accountID uuid.UUID, enabled bool) error {
	_, err := r.db.Exec(ctx, `UPDATE accounts SET raffle_enabled = $3 WHERE tenant_id = $1 AND account_id = $2`, tenantID, accountID, enabled)
	return err
}

func (r *AccountRepository) UpdateBlockedUntil(ctx context.Context, tenantID, accountID uuid.UUID, blockedUntil time.Time) error {
	_, err := r.db.Exec(ctx,
		`UPDATE accounts SET blocked_until = $1 WHERE tenant_id = $2 AND account_id = $3`,
		blockedUntil, tenantID, accountID)
	return err
}

// CountAll counts all accounts across tenants (global ban-rate denominator).
func (r *AccountRepository) CountAll(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM accounts`).Scan(&count)
	return count, err
}

// SessionProfile 是会话表中的展示资料
type SessionProfile struct {
	FirstName string `db:"first_name"`
	LastName  string `db:"last_name"`
	Username  string `db:"username"`
}

// GetSessionProfiles 批量取账号的 TG 资料（first_name/username），键为 session_name
func (r *AccountRepository) GetSessionProfiles(ctx context.Context, sessionNames []string) (map[string]*SessionProfile, error) {
	if len(sessionNames) == 0 {
		return map[string]*SessionProfile{}, nil
	}
	rows, err := r.db.Query(ctx, `SELECT session_id, first_name, last_name, username FROM sessions WHERE session_id = ANY($1)`, sessionNames)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*SessionProfile{}
	for rows.Next() {
		var id string
		var p SessionProfile
		if err := rows.Scan(&id, &p.FirstName, &p.LastName, &p.Username); err == nil {
			out[id] = &p
		}
	}
	return out, rows.Err()
}
