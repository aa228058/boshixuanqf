// Package notify R119: 租户通知中心 —— SMTP 邮件通知。
// 租户在「设置 → 通知」配置接收邮箱并勾选订阅事件,
// 后端各事件点统一调用 Notify() 完成 订阅检查→邮件发送。
package notify

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"tg-sender/internal/domain"
)

// 事件键 —— 前端设置页/后端订阅检查共用
const (
	EventRaffleActivity = "raffle_activity" // 新抽奖活动(发现活动并自动参与)
	EventRaffleWinner   = "raffle_winner"   // 抽奖中奖
	EventTaskFinished   = "task_finished"   // 任务完成
	EventTaskFailed     = "task_failed"     // 任务失败/异常停止
	EventAccountLimited = "account_limited" // 账号被限制/风控
	EventAccountOffline = "account_offline" // 账号掉线
	EventNeedVerify     = "need_verify"     // 账号需要验证码
	EventVerifySolved   = "verify_solved"   // 验证码处理成功
	EventJoinFailed     = "join_failed"     // 加群/邀请失败率过高
)

// EventLabels 事件中文名(前端渲染开关列表用, 与后端事件键一一对应)
var EventLabels = map[string]string{
	EventRaffleActivity: "新抽奖活动",
	EventRaffleWinner:   "抽奖中奖",
	EventTaskFinished:   "任务完成",
	EventTaskFailed:     "任务失败",
	EventAccountLimited: "账号被限制",
	EventAccountOffline: "账号掉线",
	EventNeedVerify:     "账号需要验证码",
	EventVerifySolved:   "验证码处理成功",
	EventJoinFailed:     "加群/邀请失败",
}

// R120: 租户级 SMTP —— 每个租户配置自己的发件邮箱(管理端只控套餐权限),
// 通知用租户自己的邮箱发出。Mailer 退化为无状态发送工具(仅保留并发锁)。
type Mailer struct {
	mu sync.Mutex // smtp 并发保护(PlainAuth 非并发安全)
}

func NewMailer() *Mailer {
	return &Mailer{}
}

// Configured SMTP 配置是否齐全
func Configured(cfg *domain.NotifySMTPConfig) bool {
	return cfg != nil && cfg.Host != "" && cfg.From != ""
}

// Send 同步发送(465 SMTPS 直连 / 587 STARTTLS 自动升级)
func (m *Mailer) Send(cfg *domain.NotifySMTPConfig, to, subject, body string) (err error) {
	// R147: 失败统一打日志(此前 Send 内所有错误只 return 不打日志, 测试接口失败时完全静默无从排查)
	defer func() {
		if err != nil {
			log.Printf("[notify] 邮件发送失败 smtp=%s:%d to=%s err=%v", cfg.Host, cfg.Port, to, err)
		}
	}()
	if !Configured(cfg) {
		return fmt.Errorf("SMTP 未配置")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s", cfg.From, to, subject, body))

	if cfg.Port == 465 {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 15 * time.Second}, "tcp", addr, &tls.Config{ServerName: cfg.Host})
		if err != nil && (strings.Contains(err.Error(), "x509") || strings.Contains(err.Error(), "certificate")) {
			// R147: 小众 SMTP 服务(证书域名不匹配/自签)回退跳过校验重试; QQ/163/Gmail 证书有效, 严格校验路径不受影响
			log.Printf("[notify] TLS 证书校验失败(%v), 回退 InsecureSkipVerify 重试", err)
			conn, err = tls.DialWithDialer(&net.Dialer{Timeout: 15 * time.Second}, "tcp", addr, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec
		}
		if err != nil {
			return fmt.Errorf("TLS 连接失败: %w", err)
		}
		client, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			return err
		}
		defer client.Close()
		if cfg.User != "" && cfg.Pass != "" {
			if err := client.Auth(smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)); err != nil {
				return err
			}
		}
		if err := client.Mail(cfg.From); err != nil {
			return err
		}
		if err := client.Rcpt(to); err != nil {
			return err
		}
		w, err := client.Data()
		if err != nil {
			return err
		}
		if _, err := w.Write(msg); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		return client.Quit()
	}

	var auth smtp.Auth
	if cfg.User != "" && cfg.Pass != "" {
		auth = smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)
	}
	// R147: 原 smtp.SendMail 无超时(SMTP 服务挂了会无限阻塞), 换 15s 超时 + 显式 STARTTLS 流程
	conn, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		return fmt.Errorf("TCP 连接失败: %w", err)
	}
	defer conn.Close()
	cli, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return err
	}
	defer cli.Close()
	if cfg.Port == 587 {
		if ok, _ := cli.Extension("STARTTLS"); ok {
			if err := cli.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
				return fmt.Errorf("STARTTLS 失败: %w", err)
			}
		}
	}
	if auth != nil {
		if err := cli.Auth(auth); err != nil {
			return err
		}
	}
	if err := cli.Mail(cfg.From); err != nil {
		return err
	}
	if err := cli.Rcpt(to); err != nil {
		return err
	}
	w, err := cli.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cli.Quit()
}

// Notify 事件通知入口: 校验租户订阅+邮箱+SMTP 配置 → 异步发送。
// 未订阅/无邮箱/未配置 SMTP 均静默返回。
func (m *Mailer) Notify(t *domain.Tenant, event, subject, body string) {
	if t == nil || t.NotifyEmail == "" {
		return
	}
	var subs map[string]bool
	if len(t.NotifySettings) > 0 {
		if err := json.Unmarshal(t.NotifySettings, &subs); err != nil {
			log.Printf("[notify] 订阅设置解析失败 tenant=%s event=%s err=%v", t.TenantID, event, err)
			return
		}
	}
	if !subs[event] {
		return
	}
	cfg := t.SMTPConfig()
	if !Configured(cfg) {
		return
	}
	email := t.NotifyEmail
	go func() {
		if err := m.Send(cfg, email, subject, body); err != nil {
			log.Printf("[notify] 发送失败 tenant=%s event=%s err=%v", t.TenantID, event, err)
			return
		}
		log.Printf("[notify] 已发送 tenant=%s event=%s subject=%q", t.TenantID, event, subject)
	}()
}
