package account

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"

	_ "modernc.org/sqlite"
	"github.com/mtgo-labs/storage"
)

// ParseTelethonSessionFile 从 Telethon 导出的 .session 文件（SQLite 格式）提取会话。
// 卡密商常见的交付物是这种文件，解析后得到 auth_key + DC 信息，转成 mtgo 存储格式。
// R182: 第三返回值为手机号(entities.phone 提取)。
func ParseTelethonSessionFile(ctx context.Context, path string) (*storage.Session, string, string, error) {
	// 先拷到临时文件——网络文件系统/上传临时文件直接 Open 可能被占用，SQLite 驱动需要可寻址文件
	tmp, err := os.CreateTemp("", "sess-*.sqlite")
	if err != nil {
		return nil, "", "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer os.Remove(tmp.Name())

	src, err := os.Open(path)
	if err != nil {
		return nil, "", "", fmt.Errorf("打开文件失败: %w", err)
	}
	if _, err := io.Copy(tmp, src); err != nil {
		src.Close()
		return nil, "", "", fmt.Errorf("读取文件失败: %w", err)
	}
	src.Close()
	tmp.Close()

	db, err := sql.Open("sqlite", tmp.Name())
	if err != nil {
		return nil, "", "", fmt.Errorf("打开 SQLite 失败（不是有效的 .session 文件？）: %w", err)
	}
	defer db.Close()

	// Telethon sessions 表：dc_id, server_address, port, auth_key
	var (
		dcID      int
		server    string
		port      int
		authKey   []byte
	)
	row := db.QueryRowContext(ctx,
		`SELECT dc_id, server_address, port, auth_key FROM sessions ORDER BY dc_id LIMIT 1`)
	if err := row.Scan(&dcID, &server, &port, &authKey); err != nil {
		return nil, "", "", fmt.Errorf("读取 sessions 表失败（文件可能不是 Telethon 格式）: %w", err)
	}
	if len(authKey) != 256 {
		return nil, "", "", fmt.Errorf("auth_key 长度异常: %d，期望 256 字节", len(authKey))
	}

	// R182: entities 表存着登录账号的手机号(Telethon 标准字段, 号商导出文件都有),
	// 提取用于账号显示。此前只取 server:port, 手机号全靠手工填——审计报告反馈的"导入后无手机号"。
	phone := ""
	var entPhone int64
	if err := db.QueryRowContext(ctx,
		`SELECT phone FROM entities WHERE phone IS NOT NULL AND phone != 0 LIMIT 1`).Scan(&entPhone); err == nil && entPhone > 0 {
		phone = fmt.Sprintf("+%d", entPhone)
	}

	return &storage.Session{
		DC:      dcID,
		AuthKey: authKey,
	}, fmt.Sprintf("%s:%d", server, port), phone, nil
}
