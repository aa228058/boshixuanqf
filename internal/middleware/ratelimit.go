package middleware

import (
	"net/http"
	"sync"
	"tg-sender/internal/safego"
	"time"

	"github.com/labstack/echo/v4"
)

// RateLimiter implements a simple in-memory rate limiter with periodic cleanup.
type RateLimiter struct {
	mu       sync.RWMutex
	clients  map[string]*clientLimit
	limit    int
	window   time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
}

type clientLimit struct {
	count   int
	resetAt time.Time
}

// NewRateLimiter creates a rate limiter and starts a cleanup goroutine.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		clients: make(map[string]*clientLimit),
		limit:   limit,
		window:  window,
		stopCh:  make(chan struct{}),
	}
	safego.Go(func() { rl.cleanup() })
	return rl
}

// Stop stops the cleanup goroutine safely.
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() { close(rl.stopCh) })
}

// Middleware returns the rate limiting middleware keyed by tenant ID.
func (rl *RateLimiter) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Prefer tenant ID as rate limit key; fall back to IP+Path for
			// routes that run before JWTAuth/TenantIsolation (e.g. admin login).
			key := ""
			if tenantID, err := GetTenantID(c); err == nil {
				key = tenantID.String() + ":" + c.Request().URL.Path
			} else {
				key = c.RealIP() + ":" + c.Request().URL.Path
			}

			rl.mu.Lock()
			cl, exists := rl.clients[key]
			now := time.Now()
			if !exists || now.After(cl.resetAt) {
				rl.clients[key] = &clientLimit{count: 1, resetAt: now.Add(rl.window)}
				rl.mu.Unlock()
				return next(c)
			}

			if cl.count >= rl.limit {
				rl.mu.Unlock()
				return echo.NewHTTPError(http.StatusTooManyRequests, "rate limit exceeded")
			}

			cl.count++
			rl.mu.Unlock()
			return next(c)
		}
	}
}

// cleanup periodically removes expired entries to prevent memory leaks.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.window)
	defer ticker.Stop()
	for {
		select {
		case <-rl.stopCh:
			return
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for k, v := range rl.clients {
				if now.After(v.resetAt) {
					delete(rl.clients, k)
				}
			}
			rl.mu.Unlock()
		}
	}
}

// P2-70 审计修复: RateLimit/AuthRateLimit 原来每次调用都 NewRateLimiter——
// 每个新 limiter 都起一个永不退出的 cleanup goroutine(goroutine 泄漏)且计数器
// 互不相通(限流形同虚设)。改包级单例, 两个 limiter 全局各一个。
var (
	globalRateLimiter     = NewRateLimiter(100, time.Minute)
	globalAuthRateLimiter = NewRateLimiter(10, time.Minute)
)

// RateLimit returns the shared global rate limiter middleware (100 req/min per IP).
func RateLimit() echo.MiddlewareFunc {
	return globalRateLimiter.Middleware()
}

// AuthRateLimit returns the shared strict auth limiter middleware (10 req/min per IP).
func AuthRateLimit() echo.MiddlewareFunc {
	return globalAuthRateLimiter.Middleware()
}
