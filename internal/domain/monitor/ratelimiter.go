package monitor

import (
	"sync"
	"time"
)

// RateLimiter limits message processing rate per account to prevent CPU overload.
type RateLimiter struct {
	mu            sync.RWMutex
	counters      map[string]int
	lastReset     time.Time
	resetInterval time.Duration
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		counters:      make(map[string]int),
		lastReset:     time.Now(),
		resetInterval: time.Minute,
	}
}

// Allow checks if the account is within the rate limit.
// 实现为每分钟 100 条（固定窗口计数，每分钟重置），不是注释原来写的"每秒 50"。
func (r *RateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if now.Sub(r.lastReset) > r.resetInterval {
		r.counters = make(map[string]int)
		r.lastReset = now
	}

	if r.counters[key] >= 100 {
		return false
	}
	r.counters[key]++
	return true
}

// Reset clears all counters. Should be called by a background ticker.
func (r *RateLimiter) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.counters {
		r.counters[k] = 0
	}
	r.lastReset = time.Now()
}
