package account

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mtgo-labs/storage"
	"tg-sender/internal/crypto"
)

// PgxPoolAdapter wraps *pgxpool.Pool as storage.Adapter with custom pool config.
// Implements SessionStore + PeerStore + Close (storage.Adapter interface).
type PgxPoolAdapter struct {
	pool          *pgxpool.Pool
	sessionName   string
	serverAddress string // 协议号原始 DC 地址(host:port), 恢复会话时优先使用
}

// NewPgxPoolAdapter creates a custom adapter from pgxpool.
// Returns storage.Adapter, wrap with storage.NewAdapter() for use as Config.Storage.
func NewPgxPoolAdapter(pool *pgxpool.Pool) storage.Adapter {
	return &PgxPoolAdapter{pool: pool}
}

// opCtx 给裸 context.Background() 的 DB 操作统一加 5s 超时。
// S修复: pgxpool 连接耗尽时无超时会永久挂起调用方(若发生在任务持锁路径上 = 级联死锁)。
func (a *PgxPoolAdapter) opCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// SetSessionName implements storage.SessionIDAware (optional but recommended).
// ServerAddress 返回该会话存储的原始 DC 地址(协议号导入时从 Telethon session 记录)。
// 空串表示未记录(老数据/手机验证码号), 调用方应退回内置地址表。
func (a *PgxPoolAdapter) ServerAddress() string {
	if a.serverAddress != "" {
		return a.serverAddress
	}
	// LoadSession 前可能还没读库,主动查一次
	ctx, cancel := a.opCtx()
	defer cancel()
	_ = a.pool.QueryRow(ctx,
		`SELECT server_address FROM sessions WHERE session_id = $1`, a.sessionName).Scan(&a.serverAddress)
	return a.serverAddress
}

func (a *PgxPoolAdapter) SetSessionName(name string) {
	a.sessionName = name
}

// ==================== SessionStore (2 methods) ====================

func (a *PgxPoolAdapter) LoadSession() (*storage.Session, error) {
	// sessionName must be set to prevent cross-session contamination in multi-tenant environments
	if a.sessionName == "" {
		return nil, fmt.Errorf("load session: sessionName not set")
	}

	var sess storage.Session
	var authKeyEnc, stateEnc []byte

	query := `
		SELECT session_id, dc, api_id, api_hash, test_mode, auth_key, state,
		       user_id, is_bot, first_name, last_name, username, date, server_address
		FROM sessions WHERE session_id = $1
	`

	qCtx, qCancel := a.opCtx()
	defer qCancel()
	err := a.pool.QueryRow(qCtx, query, a.sessionName).Scan(
		&sess.SessionID, &sess.DC, &sess.APIID, &sess.APIHash,
		&sess.TestMode, &authKeyEnc, &stateEnc,
		&sess.UserID, &sess.IsBot, &sess.FirstName,
		&sess.LastName, &sess.Username, &sess.Date, &a.serverAddress,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load session: %w", err)
	}

	// Decrypt auth_key: try encrypted first, fallback to plaintext for backward compatibility
	if len(authKeyEnc) > 0 {
		if decrypted, decErr := crypto.Decrypt(string(authKeyEnc)); decErr == nil {
			sess.AuthKey = decrypted
		} else if strings.Contains(decErr.Error(), "decode base64") || strings.Contains(decErr.Error(), "ciphertext too short") {
			// Plaintext fallback (legacy data before encryption migration)
			sess.AuthKey = authKeyEnc
		} else {
			return nil, fmt.Errorf("load session: decrypt auth_key failed: %w", decErr)
		}
	}

	// Decrypt state: try encrypted first, fallback to plaintext
	if len(stateEnc) > 0 {
		if decrypted, decErr := crypto.Decrypt(string(stateEnc)); decErr == nil {
			sess.State = decrypted
		} else if strings.Contains(decErr.Error(), "decode base64") || strings.Contains(decErr.Error(), "ciphertext too short") {
			sess.State = stateEnc
		} else {
			return nil, fmt.Errorf("load session: decrypt state failed: %w", decErr)
		}
	}

	return &sess, nil
}

func (a *PgxPoolAdapter) SaveSession(sess *storage.Session) error {
	// Encrypt sensitive fields before saving
	authKeyEnc := ""
	if len(sess.AuthKey) > 0 {
		enc, err := crypto.Encrypt(sess.AuthKey)
		if err != nil {
			return fmt.Errorf("save session: encrypt auth_key failed: %w", err)
		}
		authKeyEnc = enc
	}
	stateEnc := ""
	if len(sess.State) > 0 {
		enc, err := crypto.Encrypt(sess.State)
		if err != nil {
			return fmt.Errorf("save session: encrypt state failed: %w", err)
		}
		stateEnc = enc
	}

	_, err := a.pool.Exec(context.Background(), `
		INSERT INTO sessions (
			session_id, dc, api_id, api_hash, test_mode,
			auth_key, state, user_id, is_bot,
			first_name, last_name, username, date, server_address
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
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
			-- P1-19 审计修复: 原 ON CONFLICT 列表漏 server_address——账号换 DC 后
			-- 登录态更新永远写不进新服务器地址, 持续连旧服务器直至被封。
			server_address = EXCLUDED.server_address
	`,
		sess.SessionID, sess.DC, sess.APIID, sess.APIHash, sess.TestMode,
		authKeyEnc, stateEnc, sess.UserID, sess.IsBot,
		sess.FirstName, sess.LastName, sess.Username, sess.Date, a.serverAddress,
	)
	if err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

// ==================== PeerStore (5 methods) ====================

func (a *PgxPoolAdapter) SavePeer(p *storage.Peer) error {
	_, err := a.pool.Exec(context.Background(), `
		INSERT INTO peers (
			id, type, access_hash, username, usernames,
			first_name, last_name, phone_number, is_bot,
			photo_id, language, last_updated
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO UPDATE SET
			type = EXCLUDED.type,
			access_hash = EXCLUDED.access_hash,
			username = EXCLUDED.username,
			usernames = EXCLUDED.usernames,
			first_name = EXCLUDED.first_name,
			last_name = EXCLUDED.last_name,
			phone_number = EXCLUDED.phone_number,
			is_bot = EXCLUDED.is_bot,
			photo_id = EXCLUDED.photo_id,
			language = EXCLUDED.language,
			last_updated = EXCLUDED.last_updated
	`, p.ID, int(p.Type), p.AccessHash, p.Username, p.Usernames,
		p.FirstName, p.LastName, p.PhoneNumber, p.IsBot,
		p.PhotoID, p.Language, p.LastUpdated)
	if err != nil {
		return fmt.Errorf("save peer: %w", err)
	}
	return nil
}

func (a *PgxPoolAdapter) GetPeer(id int64) (*storage.Peer, error) {
	var p storage.Peer
	var peerType int
	gCtx, gCancel := a.opCtx()
	defer gCancel()
	err := a.pool.QueryRow(gCtx, `
		SELECT id, type, access_hash, username, usernames,
		       first_name, last_name, phone_number, is_bot,
		       photo_id, language, last_updated
		FROM peers WHERE id = $1
	`, id).Scan(
		&p.ID, &peerType, &p.AccessHash, &p.Username, &p.Usernames,
		&p.FirstName, &p.LastName, &p.PhoneNumber, &p.IsBot,
		&p.PhotoID, &p.Language, &p.LastUpdated,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get peer: %w", err)
	}
	p.Type = storage.PeerType(peerType)
	return &p, nil
}

func (a *PgxPoolAdapter) GetPeerByUsername(username string) (*storage.Peer, error) {
	var p storage.Peer
	var peerType int
	err := a.pool.QueryRow(context.Background(), `
		SELECT id, type, access_hash, username, usernames,
		       first_name, last_name, phone_number, is_bot,
		       photo_id, language, last_updated
		FROM peers WHERE username = $1
	`, username).Scan(
		&p.ID, &peerType, &p.AccessHash, &p.Username, &p.Usernames,
		&p.FirstName, &p.LastName, &p.PhoneNumber, &p.IsBot,
		&p.PhotoID, &p.Language, &p.LastUpdated,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get peer by username: %w", err)
	}
	p.Type = storage.PeerType(peerType)
	return &p, nil
}

// peerLoadRecency: 只把最近活跃过的 peers 载入内存。此前全表加载，peers 表
// 随账号/群组数量无界增长，每个 client 启动都要把整表读进内存。
// 冷 peer（90 天未更新）大概率是死对话；若真需要，ResolvePeer 会走网络
// 重新拉取并经 SavePeer 写回，行为无损。NULL 行（旧数据无时间戳）保留加载。
const peerLoadRecency = "90 days"

func (a *PgxPoolAdapter) LoadPeers() ([]*storage.Peer, error) {
	lCtx, lCancel := a.opCtx()
	defer lCancel()
	rows, err := a.pool.Query(lCtx, `
		SELECT id, type, access_hash, username, usernames,
		       first_name, last_name, phone_number, is_bot,
		       photo_id, language, last_updated
		FROM peers
		WHERE last_updated IS NULL OR last_updated > NOW() - INTERVAL '`+peerLoadRecency+`'
	`)
	if err != nil {
		return nil, fmt.Errorf("load peers: %w", err)
	}
	defer rows.Close()

	var peers []*storage.Peer
	for rows.Next() {
		var p storage.Peer
		var peerType int
		if err := rows.Scan(
			&p.ID, &peerType, &p.AccessHash, &p.Username, &p.Usernames,
			&p.FirstName, &p.LastName, &p.PhoneNumber, &p.IsBot,
			&p.PhotoID, &p.Language, &p.LastUpdated,
		); err != nil {
			return nil, fmt.Errorf("scan peer: %w", err)
		}
		p.Type = storage.PeerType(peerType)
		peers = append(peers, &p)
	}
	return peers, rows.Err()
}

func (a *PgxPoolAdapter) DeletePeer(id int64) error {
	_, err := a.pool.Exec(context.Background(), `DELETE FROM peers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete peer: %w", err)
	}
	return nil
}

// ==================== Close (1 method) ====================

func (a *PgxPoolAdapter) Close() error {
	// Pool is shared and managed by Engine lifecycle.
	// Do NOT close here — client.Stop() calls this, but pool must stay open.
	return nil
}
