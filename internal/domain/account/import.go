package account

import (
	"bytes"
	"context"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/tg"
	"github.com/mtgo-labs/storage"
	sessionconverter "github.com/mtgo-labs/session-converter"

	"strings"
)

// DecodeImportedSession 解析外部协议号字符串（Telethon/Pyrogram/GramJS 等格式自动识别）
// 为 mtgo 存储格式。协议号 = 已授权的 auth_key，无需再走短信验证码登录。
func DecodeImportedSession(sessionStr string) (*storage.Session, string, error) {
	sess, format, err := sessionconverter.Decode(sessionStr)
	if err != nil {
		return nil, "", fmt.Errorf("解析会话失败（格式不支持或已损坏）: %w", err)
	}
	if len(sess.AuthKey) != 256 {
		return nil, "", fmt.Errorf("auth_key 长度异常: %d，期望 256 字节（识别为 %v）", len(sess.AuthKey), format)
	}
	return &storage.Session{
		DC:       sess.DCID,
		APIID:    sess.AppID,
		TestMode: sess.TestMode,
		AuthKey:  sess.AuthKey,
		UserID:   sess.UserID,
		IsBot:    sess.IsBot,
	}, sess.ServerAddress, nil
}

// StoreImportedSession 把导入的会话加密落库到指定 session_name，
// 之后 GetClient 走标准池化连接路径（复用既有生命周期管理）。
func (e *Engine) StoreImportedSession(ctx context.Context, sessionName string, serverAddr string, sess *storage.Session) error {
	// SessionID 即数据库主键（LoadSession 按 session_id=sessionName 查询）；
	// 不设会导致所有导入号挤在同一个空串主键上互相覆盖。
	sess.SessionID = sessionName
	adapter := &PgxPoolAdapter{pool: e.pool, sessionName: sessionName, serverAddress: serverAddr}
	return adapter.SaveSession(sess)
}

// UpdateProfile 调用 TG 接口修改账号资料：名字/姓氏/签名 + 用户名。
// username 传空字符串表示清除用户名；about 同理。
func (e *Engine) UpdateProfile(ctx context.Context, tenantID, accountID uuid.UUID, firstName, lastName, about, username string) error {
	client, err := e.GetClient(ctx, tenantID, accountID)
	if err != nil {
		return fmt.Errorf("获取连接失败: %w", err)
	}
	defer e.ReleaseClient(tenantID, accountID, client)
	raw := client.Raw()
	if _, err := raw.AccountUpdateProfile(ctx, &tg.AccountUpdateProfileRequest{
		FirstName: firstName,
		LastName:  lastName,
		About:     about,
	}); err != nil {
		return fmt.Errorf("修改名字/签名失败: %w", err)
	}
	if _, err := raw.AccountUpdateUsername(ctx, &tg.AccountUpdateUsernameRequest{
		Username: username,
	}); err != nil && !strings.Contains(err.Error(), "USERNAME_NOT_MODIFIED") {
		// R119: 错误细分 —— 占用/格式/频繁分别明确提示, 不再笼统"可能已被占用"
		es := err.Error()
		switch {
		case strings.Contains(es, "USERNAME_OCCUPIED"):
			return fmt.Errorf("用户名 @%s 已被他人占用, 请换一个再试", username)
		case strings.Contains(es, "USERNAME_INVALID"):
			return fmt.Errorf("用户名 @%s 格式非法(仅字母/数字/下划线, 5-32位)", username)
		case strings.Contains(es, "FLOOD_WAIT"):
			return fmt.Errorf("修改用户名太频繁, 请等待几分钟后重试")
		default:
			return fmt.Errorf("修改用户名失败: %w", err)
		}
	}
	// USERNAME_NOT_MODIFIED = 用户名和当前一致，TG 视为"无需修改"，不算失败
	// 关键: 同步本地 sessions 表。列表的名字从 sessions 表读，
	// 不同步的话 TG 改成功了但前端刷新看到的还是旧名字。
	acc, err := e.repo.GetByID(ctx, tenantID, accountID)
	if err == nil && acc != nil && acc.SessionName != "" {
		if _, err := e.pool.Exec(ctx,
			`UPDATE sessions SET first_name=$1, last_name=$2, username=$3 WHERE session_id=$4`,
			firstName, lastName, username, acc.SessionName); err != nil {
			log.Printf("[profile] 同步本地资料失败: %v\n", err)
		}
	}
	return nil
}

// UploadAvatar 上传头像：先把字节走 TG 上传通道拿句柄，再设为头像。
func (e *Engine) UploadAvatar(ctx context.Context, tenantID, accountID uuid.UUID, photo []byte) error {
	client, err := e.GetClient(ctx, tenantID, accountID)
	if err != nil {
		return fmt.Errorf("获取连接失败: %w", err)
	}
	defer e.ReleaseClient(tenantID, accountID, client)
	up, err := client.UploadFile(ctx, bytes.NewReader(photo), "avatar.jpg", int64(len(photo)), nil)
	if err != nil {
		return fmt.Errorf("上传照片失败: %w", err)
	}
	if _, err := client.Raw().PhotosUploadProfilePhoto(ctx, &tg.PhotosUploadProfilePhotoRequest{
		File: up.File,
	}); err != nil {
		return fmt.Errorf("设置头像失败: %w", err)
	}
	return nil
}
