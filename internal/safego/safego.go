// Package safego 提供带 panic 恢复的 goroutine 启动函数.
//
// 项目为单进程承载多租户: 任何一个后台 goroutine panic(空指针/切片越界/TG 数据异常)
// 都会让整个进程崩溃, 导致全部租户的 TG 连接与任务中断。
// 规则: 所有后台 goroutine 必须用 safego.Go 启动, 禁止裸 go.
package safego

import (
	"fmt"
	"log"
	"runtime/debug"
)

var panicHook func(string)

// SetPanicHook 注册 panic 告警回调（如 TG 推送）。进程级设一次。
// 回调自身 panic 会被再次捕获, 不会连锁崩溃。
func SetPanicHook(f func(string)) { panicHook = f }

// Go 在新 goroutine 中运行 fn, panic 时记录日志而不崩溃进程.
// 注意: 本函数内部使用裸 go —— 它自己就是 recover 包装层, 自引用会造成 import cycle.
func Go(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				msg := fmt.Sprintf("[safego] goroutine panic 已捕获: %v\n%s", r, debug.Stack())
				log.Print(msg)
				if panicHook != nil {
					hook := panicHook
					snippet := msg
					if len(snippet) > 3500 {
						snippet = snippet[:3500] // TG 消息长度限制
					}
					go func() {
						defer func() { _ = recover() }()
						hook(snippet)
					}()
				}
			}
		}()
		fn()
	}()
}
