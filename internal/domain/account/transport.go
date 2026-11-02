package account

import (
	"fmt"
	"log"
	"sync"
	"net"
	"strings"
	"time"

	"github.com/mtgo-labs/mtgo/telegram"
)

// dcFrontendIPs TG 各 DC 的公网前端 IP。某 IP 拒收(444)时传输层自动轮转到下一个。
// 传输回退日志节流: key → 上次打印时间(10分钟内不重复刷, 状态变化由成功路径体现)
var fallbackLogMu sync.Mutex
var fallbackLogAt = map[string]time.Time{}

func logFallbackThrottled(key, format string, args ...any) {
	fallbackLogMu.Lock()
	defer fallbackLogMu.Unlock()
	if last, ok := fallbackLogAt[key]; ok && time.Since(last) < 10*time.Minute {
		return
	}
	fallbackLogAt[key] = time.Now()
	log.Printf(format, args...)
}

var dcFrontendIPs = map[int32][]string{
	1: {"149.154.175.50", "149.154.175.51", "149.154.175.52", "149.154.175.53", "149.154.175.54", "149.154.175.55", "149.154.175.56"},
	2: {"149.154.167.50", "149.154.167.51"},
	3: {"149.154.175.100", "149.154.175.101"},
	4: {"149.154.167.91", "149.154.167.92"},
	5: {"91.108.56.130", "91.108.56.131"},
}

// dcIPBelongs 判断 host 是否属于指定 DC 的地址段(R182.2 存档地址与 dc 列错位校验用)。
func dcIPBelongs(dc int32, host string) bool {
	ips, ok := dcFrontendIPs[dc]
	if !ok {
		return true // 未知 DC 不拦截, 放行原样
	}
	h := strings.TrimSpace(host)
	for _, ip := range ips {
		if ip == h {
			return true
		}
	}
	return false
}

// dcHTTPURLs 生成某 DC 的 HTTP 传输端点（5222 优先 + 80 兜底）。
// serverAddr(host:port) 的原始 IP 排最前。返回 nil = 退回库自动生成的 80 端点。
func dcHTTPURLs(dc int32, serverAddr string) []string {
	ips := dcFrontendIPs[dc]
	if len(ips) == 0 && serverAddr != "" {
		host, _, err := net.SplitHostPort(serverAddr)
		if err != nil {
			host = serverAddr
		}
		for _, list := range dcFrontendIPs {
			for _, ip := range list {
				if ip == host {
					ips = list
					break
				}
			}
		}
	}
	if len(ips) == 0 {
		return nil
	}
	if host, _, err := net.SplitHostPort(serverAddr); err == nil {
		for i, ip := range ips {
			if ip == host && i > 0 {
				ips = append([]string{ip}, append(ips[:i:i], ips[i+1:]...)...)
				break
			}
		}
	}
	var urls []string
	for _, ip := range ips {
		urls = append(urls,
			"http://"+net.JoinHostPort(ip, "5222")+"/api",
			"http://"+net.JoinHostPort(ip, "80")+"/api",
		)
	}
	return urls
}

// httpTransportCfg 构造 HTTP 传输配置；urls 为空时库按 DC 自动生成 80 端点。
// applyProxy 把账号级 socks5/http 代理应用到客户端配置。
// 已对照 mtgo v0.19.0 源码(github.com/mtgo-labs/mtgo, telegram/config.go):
//   Config.Proxy *Proxy — "routes all MTProto traffic through the specified proxy"
//   telegram.ProxyFromURL(raw) 解析 socks5/socks4/http/https/mtproxy 返回 *Proxy 或 *MTProxyConfig。
func applyProxy(cfg *telegram.Config, proxyURL string) {
	raw := strings.TrimSpace(proxyURL)
	if raw == "" {
		return
	}
	parsed, err := telegram.ProxyFromURL(raw)
	if err != nil {
		return // 格式非法忽略, 不阻断建连(前端保存时会校验)
	}
	// 仅消费 *Proxy(socks/http); mtproxy/tg:// 类型由 TG_TRANSPORT 链路处理
	if p, ok := parsed.(*telegram.Proxy); ok {
		cfg.Proxy = p
	}
}

func httpTransportCfg(urls []string) *telegram.HTTPTransportConfig {
	cfg := &telegram.HTTPTransportConfig{
		MaxInFlight: 16,
		MaxWait:     25 * time.Second,
	}
	if len(urls) > 0 {
		cfg.URLs = urls
	}
	return cfg
}

// cachedTransport 读该 DC 上次成功的传输方式（空 = 无缓存）。
func (e *Engine) cachedTransport(dc int32) string {
	if dc <= 0 {
		return ""
	}
	e.transportCacheMu.RLock()
	defer e.transportCacheMu.RUnlock()
	return e.transportCache[dc]
}

// rememberTransport 记录该 DC 好用的传输方式，下次直接命中省掉死亡尝试。
func (e *Engine) rememberTransport(dc int32, kind string) {
	if dc <= 0 {
		return
	}
	e.transportCacheMu.Lock()
	defer e.transportCacheMu.Unlock()
	if e.transportCache == nil {
		e.transportCache = make(map[int32]string)
	}
	e.transportCache[dc] = kind
}

// dialClient 按 TG_TRANSPORT 策略创建并连接客户端：
//   - tcp    : 只用原生 TCP
//   - http   : 只用 HTTP 传输（80/5222，多 IP 轮转）
//   - mtproxy: 只走 MTProxy
//   - auto(默认): 传输缓存命中 → 直接用；未命中 → TCP(8s) → HTTP → MTProxy 逐级回退
//
// dcID 用于传输缓存（每个 DC 的网络情况不同，分开记忆）。传 0 = 不缓存。
func (e *Engine) dialClient(clientCfg *telegram.Config, httpURLs []string, dcID int32) (*telegram.Client, error) {
	mode := strings.ToLower(strings.TrimSpace(e.cfg.TGTransport))
	if mode == "" {
		mode = "auto"
	}

	try := func(kind string) (*telegram.Client, error) {
		cfg := *clientCfg // 浅拷贝，不动调用方的配置
		cfg.HTTPTransport = nil
		cfg.MTProxy = nil
		switch kind {
		case "http":
			cfg.ServerAddr = "" // ServerAddr 仅作用于 TCP 拨号
			cfg.HTTPTransport = httpTransportCfg(httpURLs)
		case "mtproxy":
			cfg.ServerAddr = ""
			cfg.MTProxy = &telegram.MTProxyConfig{
				Addr:   e.cfg.TGMTPProxyAddr,
				Secret: e.cfg.TGMTPProxySecret,
			}
		}
		c, err := telegram.NewClient(cfg.APIID, cfg.APIHash, &cfg)
		if err != nil {
			return nil, err
		}
		// TCP 单次尝试 8 秒封顶：死了就快点换，别白等 30 秒
		if err := c.Connect(8 * time.Second); err != nil {
			c.Stop()
			return nil, err
		}
		e.rememberTransport(dcID, kind)
		return c, nil
	}

	switch mode {
	case "tcp":
		return try("tcp")
	case "http":
		return try("http")
	case "mtproxy":
		return try("mtproxy")
	default: // auto：缓存优先，其次完整回退链
		if cached := e.cachedTransport(dcID); cached != "" {
			if c, err := try(cached); err == nil {
				return c, nil
			}
			logFallbackThrottled(fmt.Sprintf("cache-fail-dc%d", dcID), "[client] DC%d 缓存传输(%s)失败，走完整回退链…", dcID, cached)
		}
		c, err := try("tcp")
		if err == nil {
			return c, nil
		}
		logFallbackThrottled(fmt.Sprintf("tcp-fail-dc%d", dcID), "[client] TCP 连接失败(%v)，回退 HTTP 传输…", err)
		c, err = try("http")
		if err == nil {
			return c, nil
		}
		if e.cfg.TGMTPProxyAddr == "" {
			return nil, err
		}
		logFallbackThrottled("http-fail", "[client] HTTP 传输也失败(%v)，回退 MTProxy…", err)
		return try("mtproxy")
	}
}
