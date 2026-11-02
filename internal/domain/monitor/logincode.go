package monitor

// R153: 登录验证码捕获 —— 协议号登录手机时, TG 官方消息(777000)或私聊里的
// "Login code: 18726" / "登录验证码" 文本自动提取, 供面板/邮箱展示。

import (
	"regexp"

	"github.com/mtgo-labs/mtgo/tg"
)

// 官方多语言话术兜底: login code / 登录验证码 / 验证码 / verification code + 5-6 位数字
// R158: 覆盖 TG 多语言登录验证码话术(西/葡/俄/阿等 locale 的号常见)
var loginCodeRe = regexp.MustCompile(`(?i)(?:login code|登录验证码|登入驗證碼|verification code|验证码|确认码|c[oó]digo(?: de (?:inicio de sesi[oó]n|acesso|confirmaci[oó]n|verificaci[oó]n))?|код(?:\s*подтверждения|(?:\s*для)?\s*входа)?|رمز(?:\s*الدخول)?)[^\d]{0,20}(\d{5,6})`)

// extractLoginCode 只采信两类来源, 防群里的伪造消息:
//   1) TG 官方服务号 777000
//   2) 私聊(chatID > 0)里的验证码文本
func extractLoginCode(text string, fromID any, chatID int64) string {
	if text == "" {
		return ""
	}
	var senderID int64
	switch v := fromID.(type) {
	case *tg.PeerUser:
		senderID = v.UserID
	case tg.PeerUser:
		senderID = v.UserID
	}
	if senderID != 777000 && chatID < 0 {
		return ""
	}
	m := loginCodeRe.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return m[1]
}
