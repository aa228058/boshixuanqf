package account

import (
	"context"
	"log"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/tg"
	"tg-sender/internal/safego"
)

// R130 协议号手机号回读: 协议号(session 导入)此前 phone 字段从未写入——
// 但 TG 协议支持: UsersGetUsers(InputUserSelf) 返回的 self 对象自带 Phone。
// 挂在账号上线钩子(updateStatus)旁: phone 为空的号异步回读存库, 任何失败只记日志。
func (e *Engine) ensurePhoneAsync(tenantID, accountID uuid.UUID) {
	safego.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		acc, err := e.repo.GetByID(ctx, tenantID, accountID)
		if err != nil || acc.Phone != "" {
			return // 已有手机号或查询失败, 不打扰
		}
		client, err := e.GetClient(ctx, tenantID, accountID)
		if err != nil {
			return // 客户端不可用, 下次上线再试
		}
		defer e.ReleaseClient(tenantID, accountID, client)
		res, err := client.Raw().UsersGetUsers(ctx, &tg.UsersGetUsersRequest{
			ID: []tg.InputUserClass{&tg.InputUserSelf{}},
		})
		if err != nil {
			log.Printf("[account] 回读手机号失败 account=%s: %v", accountID, err)
			return
		}
		phone := extractPhoneViaReflection(res)
		if phone == "" {
			return
		}
		if err := e.repo.UpdatePhone(ctx, tenantID, accountID, phone); err != nil {
			log.Printf("[account] 存手机号失败 account=%s: %v", accountID, err)
		}
	})
}

// extractPhoneViaReflection 从 UsersGetUsers 的 TLObject 返回里按字段名提取 Phone。
// mtgo Raw() 返回通用接口, 具体包装类型名不猜(同 raffle.go 反射思路): 找 Users/User
// 等切片字段的第一个元素, 读其 Phone 字符串字段。
func extractPhoneViaReflection(res tg.TLObject) string {
	defer func() { _ = recover() }()
	v := reflect.ValueOf(res)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	findPhone := func(uv reflect.Value) string {
		if uv.Kind() == reflect.Interface || uv.Kind() == reflect.Ptr {
			if uv.IsNil() {
				return ""
			}
			uv = uv.Elem()
		}
		if uv.Kind() != reflect.Struct {
			return ""
		}
		for i := 0; i < uv.NumField(); i++ {
			f := uv.Field(i)
			if uv.Type().Field(i).Name == "Phone" && f.Kind() == reflect.String {
				return f.String()
			}
		}
		return ""
	}
	for _, name := range []string{"Users", "User"} {
		f := v.FieldByName(name)
		if !f.IsValid() {
			continue
		}
		if f.Kind() == reflect.Slice && f.Len() > 0 {
			if p := findPhone(f.Index(0)); p != "" {
				return p
			}
		}
	}
	// 兜底: 扁平结构直接找
	return findPhone(v)
}
