// 批量克隆他人资料(2026-10-21)。
// 注意: Engine.GetClient / Engine.UpdateProfile 在 client.go / import.go 已有实现,
// 本文件只做"克隆他人"这一层(解析 @username → 套用到目标号), 不重复造轮子。
package account

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/tg"
)

// CloneProfile 批量克隆【他人】资料(2026-10-21 按租户澄清: 源是任意 TG 用户, 不是自己号):
// 输入对方 @username → 任一在线目标号解析出 名/姓/签名 → 套用到所有勾选号。
// 用户名不克隆(全局唯一, 复制无意义只会报错); 头像需下载+上传双通道, v1 不含。
// 复用现有 GetClient(带重试)/UpdateProfile(自带 ReleaseClient), 零新增连接管理逻辑。
func (e *Engine) CloneProfile(ctx context.Context, tenantID uuid.UUID, sourceUsername string, targetIDs []uuid.UUID) (ok int, failed int, err error) {
	un := strings.TrimPrefix(strings.TrimSpace(sourceUsername), "@")
	if un == "" {
		return 0, 0, fmt.Errorf("源用户名不能为空")
	}
	if len(targetIDs) == 0 {
		return 0, 0, fmt.Errorf("未选择目标账号")
	}
	// 用第一个在线目标号的客户端解析源用户(资料对任意客户端可见); 探测到立即释放连接
	var cl *telegram.Client
	for _, tid := range targetIDs {
		if c, err := e.GetClient(ctx, tenantID, tid); err == nil {
			cl = c
			defer e.ReleaseClient(tenantID, tid, c)
			break
		}
	}
	if cl == nil {
		return 0, 0, fmt.Errorf("没有在线的目标账号可用来解析")
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := cl.Raw().ContactsResolveUsername(cctx, &tg.ContactsResolveUsernameRequest{Username: un})
	if err != nil {
		return 0, 0, fmt.Errorf("解析用户名 @%s 失败: %w", un, err)
	}
	srcFirst, srcLast, srcAbout, okk := extractUserViaReflection(res)
	if !okk {
		return 0, 0, fmt.Errorf("未能从解析结果提取用户资料")
	}
	// 复用现有 UpdateProfile(ctx, tenant, acc, firstName, lastName, about, username) —— 末位 username 传空=不改
	for _, tid := range targetIDs {
		if err := e.UpdateProfile(ctx, tenantID, tid, srcFirst, srcLast, srcAbout, ""); err != nil {
			failed++
			continue
		}
		ok++
	}
	return ok, failed, nil
}

// extractUserViaReflection 从 ContactsResolveUsername 的 TLObject 返回按字段名提取
// 第一个用户的 FirstName/LastName/About —— 反射按字段名取值, 不猜壳类型(2026-10-21 实证手法;
// 上轮实测 fields=[] 未命中, 候选字段扩到 5 个 + 扁平兜底)。
func extractUserViaReflection(res tg.TLObject) (first, last, about string, ok bool) {
	defer func() { recover() }()
	rv := reflect.ValueOf(res)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return "", "", "", false
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return "", "", "", false
	}
	find := func(v reflect.Value) (string, string, string, bool) {
		if v.Kind() == reflect.Interface {
			v = v.Elem()
		}
		if v.Kind() == reflect.Ptr {
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			return "", "", "", false
		}
		var f, l, a string
		for i := 0; i < v.NumField(); i++ {
			switch v.Type().Field(i).Name {
			case "FirstName":
				f = v.Field(i).String()
			case "LastName":
				l = v.Field(i).String()
			case "About":
				a = v.Field(i).String()
			}
		}
		return f, l, a, f != ""
	}
	for _, name := range []string{"Users", "User", "Contacts", "Chats", "Peer"} {
		f := rv.FieldByName(name)
		if !f.IsValid() {
			continue
		}
		if f.Kind() == reflect.Slice && f.Len() > 0 {
			if fn, l2, a2, ok2 := find(f.Index(0)); ok2 {
				return fn, l2, a2, true
			}
		}
	}
	if fn, l3, a3, ok3 := find(rv); ok3 {
		return fn, l3, a3, true
	}
	return "", "", "", false
}
