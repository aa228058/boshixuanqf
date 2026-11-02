package task

import (
	"fmt"
	"strings"

	"tg-sender/internal/domain"
)

// action 中文映射: 后端生成可读日志文案用。
var logActionLabels = map[string]string{
	"send_message":        "发送消息",
	"forward_message":     "转发消息",
	"reply_message":       "自动回复",
	"join_group":          "加入群聊",
	"scrape_members":      "采集成员",
	"scrape_links":        "链接采集",
	"monitor_start":       "监控启动",
	"monitor_complete":    "监控完成",
	"broadcast":           "群发",
	"scrape":              "采集",
	"scrape_private_link": "私密链接解析",
	"scrape_public_group": "公开群解析",
}

// status 中文映射。
var logStatusLabels = map[string]string{
	"success": "成功",
	"failed":  "失败",
	"skipped": "跳过",
}

// FormatLogMessage 生成可直接展示的中文文案。
// 规则: [动作] [目标] [结果]。历史数据(无 message)和实时推送统一走这里。
func FormatLogMessage(l *domain.TaskLog) string {
	if l.Message != "" {
		return l.Message
	}
	action, ok := logActionLabels[l.Action]
	if !ok {
		action = l.Action
	}
	var b strings.Builder
	b.WriteString(action)
	if l.TargetName != "" {
		b.WriteString(" ")
		b.WriteString(l.TargetName)
	} else if l.TargetID != "" {
		b.WriteString(" ")
		b.WriteString(l.TargetID)
	}
	if s, ok := logStatusLabels[l.Status]; ok {
		b.WriteString(" ")
		b.WriteString(s)
	}
	if l.ErrorCode != "" {
		b.WriteString(fmt.Sprintf(" (%s)", l.ErrorCode))
	}
	return b.String()
}
