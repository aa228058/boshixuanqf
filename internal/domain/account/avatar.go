package account

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/tg"
	"tg-sender/internal/safego"
)

// R181: 头像错误日志限频——mkdir permission denied / 拉取失败每次上线都刷，
// 同一个错一小时只打一条，避免日志风暴。
var (
	avatarErrLogMu sync.Mutex
	avatarErrLogAt = map[string]time.Time{}
)

func logAvatarErrf(key, format string, args ...any) {
	avatarErrLogMu.Lock()
	last, ok := avatarErrLogAt[key]
	if ok && time.Since(last) < time.Hour {
		avatarErrLogMu.Unlock()
		return
	}
	avatarErrLogAt[key] = time.Now()
	avatarErrLogMu.Unlock()
	msg := fmt.Sprintf(format, args...)
	// 目录权限类错误附可操作修复提示，不再让用户对着 errno 猜
	if strings.Contains(strings.ToLower(msg), "permission denied") {
		msg += " | 修复: chown -R 10001:10001 ./data && chmod -R u+rwX ./data"
	}
	log.Printf("[avatar] %s(每小时限频一条)", msg)
}

// refreshSelfAvatar 登录成功后异步拉取账号自己的 TG 头像存本地并回写库。
// ensureAvatarAsync 账号上线时的头像自愈: 库中无头像则取池化客户端异步补拉。
// updateStatus 钩子调用——存量号(早期登录/拉取失败)上线后自动补齐, 有头像立即返回。
// KickAvatarRefresh R176: "刷新头像"专用入口 —— 内部自取客户端(优先现有连接/存档会话恢复),
// 绝不像旧实现那样调 ConnectAccount 走完整登录(那把在线号踢进验证码流程, 用户以为号死了)。
func (e *Engine) KickAvatarRefresh(tenantID, accountID uuid.UUID) {
	e.ensureAvatarAsync(tenantID, accountID)
}

func (e *Engine) ensureAvatarAsync(tenantID, accountID uuid.UUID) {
	safego.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		acc, err := e.repo.GetByID(ctx, tenantID, accountID)
		if err != nil || acc.AvatarPath != "" {
			return // 已有头像或查询失败, 不打扰
		}
		client, err := e.GetClient(ctx, tenantID, accountID)
		if err != nil {
			return // 客户端不可用, 下次上线再试
		}
		// P0-5 审计修复: GetClient 后必须配对 ReleaseClient——原实现每次上线泄漏
		// 一次连接/锁, 累积后后续任务永久阻塞
		defer e.ReleaseClient(tenantID, accountID, client)
		e.refreshSelfAvatar(tenantID, accountID)
	})
}

// 纯增强功能: 任何失败只记日志, 绝不影响认证主流程。协议号(常驻连接)走这里。
// P1-8 审计修复: 不再接收外部传入的 client。原实现用调用方 goroutine 的 client
// 跑异步任务——调用方一结束(45 秒窗口内很常见)连接就被归还池中或被 Stop,
// 子 goroutine 裸用已归还的连接(生命周期越界)。改为 goroutine 内自取自用自还。
func (e *Engine) refreshSelfAvatar(tenantID, accountID uuid.UUID) {
	safego.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		client, err := e.GetClient(ctx, tenantID, accountID)
		if err != nil {
			logAvatarErrf("get-client:"+accountID.String(), "异步拉取头像获取客户端失败(下次上线再试): %v", err)
			return
		}
		defer e.ReleaseClient(tenantID, accountID, client)
		if err := e.downloadSelfAvatar(ctx, tenantID, accountID, client); err != nil {
			logAvatarErrf("download:"+accountID.String(), "账号 %s 头像拉取失败(不影响使用): %v", accountID, err)
		}
	})
}

// downloadSelfAvatar 从 TG 下载自己的头像并落盘回写。
// 【适配点】mtgo 是 gotd/td 的 fork, 以下调用按 gotd 命名; 若 fork 改名导致
// 编译错误, 只需对照调整本函数内的 tg.* 请求/响应类型, 其余链路无需改动。
func (e *Engine) downloadSelfAvatar(ctx context.Context, tenantID, accountID uuid.UUID, client *telegram.Client) error {
	// 1. 自己的完整资料(确认是否有头像)
	// 返回类型为接口 tg.UsersFullClass, 断言取具体结构(对齐项目内 MessagesClass 的用法)
	fullRaw, err := client.Raw().UsersGetFullUser(ctx, &tg.UsersGetFullUserRequest{ID: &tg.InputUserSelf{}})
	if err != nil {
		return fmt.Errorf("get full user: %w", err)
	}
	full, ok := fullRaw.(*tg.UsersUserFull)
	if !ok {
		return fmt.Errorf("unexpected full user type %T", fullRaw)
	}
	if len(full.Users) == 0 {
		return fmt.Errorf("empty full user")
	}
	self, ok := full.Users[0].(*tg.User)
	if !ok || self.Photo == nil {
		return fmt.Errorf("no avatar") // TG 账号本来就没头像, 正常情况
	}

	// 2. 相册第一张(即当前头像), 取最小可用尺寸(头像缩略图单 part 概率最高)
	// 返回类型为接口 tg.PhotosPhotosClass(Photos / PhotosSlice), 需断言取 .Photos
	photosRaw, err := client.Raw().PhotosGetUserPhotos(ctx, &tg.PhotosGetUserPhotosRequest{
		UserID: &tg.InputUserSelf{},
		Offset: 0,
		MaxID:  0,
		Limit:  1,
	})
	if err != nil {
		return fmt.Errorf("get user photos: %w", err)
	}
	var photoList []tg.PhotoClass
	switch pv := photosRaw.(type) {
	case *tg.PhotosPhotos:
		photoList = pv.Photos
	case *tg.PhotosPhotosSlice:
		photoList = pv.Photos
	default:
		return fmt.Errorf("unexpected photos type %T", photosRaw)
	}
	if len(photoList) == 0 {
		return fmt.Errorf("no user photos")
	}
	photo, ok := photoList[0].(*tg.Photo)
	if !ok {
		return fmt.Errorf("unexpected photo type %T", photoList[0])
	}
	var thumbType string
	for _, s := range photo.Sizes {
		if ps, ok := s.(*tg.PhotoSize); ok && ps.Type != "" {
			thumbType = ps.Type
		}
	}
	if thumbType == "" {
		return fmt.Errorf("no photo size")
	}

	// 3. upload.getFile 分块下载(头像缩略图通常一块就够)
	loc := &tg.InputPhotoFileLocation{
		ID:            photo.ID,
		AccessHash:    photo.AccessHash,
		FileReference: photo.FileReference,
		ThumbSize:     thumbType,
	}
	var data []byte
	var offset int64
	const chunk = 512 * 1024
	for {
		res, err := client.Raw().UploadGetFile(ctx, &tg.UploadGetFileRequest{
			Location: loc,
			Offset:   offset,
			Limit:    chunk,
		})
		if err != nil {
			// DC 不一致会在这报 FILE_MIGRATE_X(头像在别的 DC 的罕见场景),
			// 不为此引入迁移链路, 跳过即可——下次登录/重连会再试。
			return fmt.Errorf("upload.getFile: %w", err)
		}
		f, ok := res.(*tg.UploadFile)
		if !ok {
			return fmt.Errorf("unexpected upload result %T", res)
		}
		data = append(data, f.Bytes...)
		if len(f.Bytes) < chunk {
			break
		}
		offset += int64(len(f.Bytes))
	}
	return e.storeAvatar(ctx, tenantID, accountID, data)
}

// storeAvatar 落盘 + 回写 avatar_path。文件名固定为账号ID, 新头像覆盖旧文件。
func (e *Engine) storeAvatar(ctx context.Context, tenantID, accountID uuid.UUID, data []byte) error {
	if len(data) < 100 { // 异常小图( stripped/占位 ), 丢弃
		return fmt.Errorf("avatar too small: %d bytes", len(data))
	}
	if err := os.MkdirAll(e.cfg.AvatarDir, 0755); err != nil {
		logAvatarErrf("mkdir:"+e.cfg.AvatarDir, "创建头像目录 %s 失败: %v", e.cfg.AvatarDir, err)
		return fmt.Errorf("mkdir avatar dir: %w", err)
	}
	name := accountID.String() + ".jpg"
	if err := os.WriteFile(filepath.Join(e.cfg.AvatarDir, name), data, 0644); err != nil {
		logAvatarErrf("write:"+accountID.String(), "账号 %s 头像写入失败: %v", accountID, err)
		return fmt.Errorf("write avatar: %w", err)
	}
	if err := e.repo.UpdateAvatarPath(ctx, tenantID, accountID, name); err != nil {
		return fmt.Errorf("update avatar_path: %w", err)
	}
	log.Printf("[avatar] 账号 %s 头像已更新 (%d bytes)", accountID, len(data))
	return nil
}

// AvatarFilePath 校验租户归属后返回头像绝对路径(供 handler 鉴权后读取)。
// 未设置/文件丢失返回 error, handler 回 404。
func (e *Engine) AvatarFilePath(ctx context.Context, tenantID, accountID uuid.UUID) (string, error) {
	acc, err := e.repo.GetByID(ctx, tenantID, accountID)
	if err != nil {
		return "", err
	}
	if acc.AvatarPath == "" {
		return "", os.ErrNotExist
	}
	// filepath.Base 防路径穿越: avatar_path 只应是纯文件名
	p := filepath.Join(e.cfg.AvatarDir, filepath.Base(acc.AvatarPath))
	if _, err := os.Stat(p); err != nil {
		return "", err
	}
	return p, nil
}
