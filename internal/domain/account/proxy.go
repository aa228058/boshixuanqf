package account

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/proxy"
)

// ParseProxyURL 解析 socks5://[user:pass@]host:port
func ParseProxyURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("代理地址格式错误: %w", err)
	}
	if u.Scheme != "socks5" {
		return nil, fmt.Errorf("仅支持 socks5 代理, 收到: %s", u.Scheme)
	}
	if u.Hostname() == "" || u.Port() == "" {
		return nil, fmt.Errorf("代理缺少主机或端口")
	}
	return u, nil
}

// ProxyDialer 按代理地址构建 Dialer(空串=直连)
func ProxyDialer(proxyURL string) (proxy.Dialer, error) {
	if strings.TrimSpace(proxyURL) == "" {
		return proxy.Direct, nil
	}
	u, err := ParseProxyURL(proxyURL)
	if err != nil {
		return nil, err
	}
	var auth *proxy.Auth
	if u.User != nil {
		auth = &proxy.Auth{User: u.User.Username()}
		auth.Password, _ = u.User.Password()
	}
	d, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("创建代理 Dialer: %w", err)
	}
	return d, nil
}

// TestProxy 连通性检测: 经代理 TCP 连接 TG 服务器(默认 DC2), 返回耗时。
// 不创建 MTProto 会话、不做 TLS 校验(避免证书 SAN 不匹配误报), 纯链路探测, 安全无副作用。
func (e *Engine) TestProxy(ctx context.Context, tenantID, accountID uuid.UUID) (map[string]interface{}, error) {
	acc, err := e.repo.GetByID(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(acc.ProxyURL) == "" {
		return map[string]interface{}{"ok": false, "error": "该账号未设置代理"}, nil
	}
	start := time.Now()
	dialer, err := ProxyDialer(acc.ProxyURL)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}, nil
	}
	// 注: proxy.Dialer.Dial 不接受 ctx, 超时由系统 TCP 连接超时兜底(通常 <75s)
	conn, err := dialer.Dial("tcp", "149.154.167.51:443") // TG DC2 边缘
	if err != nil {
		return map[string]interface{}{"ok": false, "error": "经代理连接 Telegram 失败: " + err.Error()}, nil
	}
	conn.Close()
	return map[string]interface{}{"ok": true, "latency_ms": time.Since(start).Milliseconds(), "proxy": maskProxy(acc.ProxyURL)}, nil
}

// maskProxy 隐藏代理密码, 用于日志/返回值展示
func maskProxy(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	if _, has := u.User.Password(); has {
		u.User = url.UserPassword(u.User.Username(), "****")
	}
	return u.String()
}
