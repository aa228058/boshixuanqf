package adaptive

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"
	"tg-sender/internal/safego"
	"time"

	"tg-sender/internal/config"
	"tg-sender/internal/sse"
)

// Mode represents the system operating mode.
type Mode string

const (
	ModeConservative Mode = "conservative"
	ModeStandard     Mode = "standard"
	ModeAggressive   Mode = "aggressive"
)

// Profile holds the parameters for each mode.
type Profile struct {
	WorkerCount     int
	MaxConcurrent   int
	MaxPoolSize     int
	MinSendInterval time.Duration
	MaxSendInterval time.Duration
	PoolIdleTimeout time.Duration
}

// Predefined profiles optimized for 30 tenants + 70 TG accounts on 4C4G.
// Key insight: PoolSize must be >= 70 to avoid constant connect/disconnect overhead.
// Memory estimate: base(~500MB) + workers(~5MB each) + TG conns(~40MB each) + DB(~5MB each)
var (
	profilesOnce sync.Once
	profiles     map[Mode]Profile
)

func initProfiles() {
	profilesOnce.Do(func() {
		profiles = map[Mode]Profile{
			ModeConservative: {
				WorkerCount:     15,
				MaxConcurrent:   25,
				MaxPoolSize:     40,
				MinSendInterval: 4 * time.Second,
				MaxSendInterval: 12 * time.Second,
				PoolIdleTimeout: 2 * time.Minute,
			},
			ModeStandard: {
				WorkerCount:     22,
				MaxConcurrent:   40,
				MaxPoolSize:     55,
				MinSendInterval: 3 * time.Second,
				MaxSendInterval: 8 * time.Second,
				PoolIdleTimeout: 3 * time.Minute,
			},
			ModeAggressive: {
				WorkerCount:     30,
				MaxConcurrent:   60,
				MaxPoolSize:     70,
				MinSendInterval: 2 * time.Second,
				MaxSendInterval: 6 * time.Second,
				PoolIdleTimeout: 5 * time.Minute,
			},
		}
	})
}

// CurrentProfile 返回当前生效模式的参数(MaxConcurrent 等), 由任务引擎的全局并发上限消费
func (e *Engine) CurrentProfile() Profile {
	e.mu.RLock()
	defer e.mu.RUnlock()
	p, _ := GetProfile(e.current)
	return p
}

// GetProfile returns the profile for a given mode.
// Safe for concurrent use.
func GetProfile(mode Mode) (Profile, bool) {
	initProfiles()
	p, ok := profiles[mode]
	return p, ok
}

// Engine monitors system metrics and adjusts operating mode automatically.
type Engine struct {
	cfg       *config.Config
	sseBroker *sse.Broker
	current   Mode
	mu        sync.RWMutex

	// Metrics
	recentFailures []bool // sliding window of recent task outcomes
	recentMu       sync.Mutex
	writeCount     int // actual number of recorded outcomes (for accurate failRate during warmup)
}

// NewEngine creates an adaptive engine.
func NewEngine(cfg *config.Config, broker *sse.Broker) *Engine {
	mode := Mode(cfg.Mode)
	if mode != ModeConservative && mode != ModeStandard && mode != ModeAggressive {
		mode = ModeConservative // default to conservative for safety
	}
	return &Engine{
		cfg:            cfg,
		sseBroker:      broker,
		current:        mode,
		recentFailures: make([]bool, 100), // last 100 task outcomes
	}
}

// Current returns the current operating mode.
func (e *Engine) Current() Mode {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.current
}

// Profile returns the current profile parameters.
func (e *Engine) Profile() Profile {
	p, _ := GetProfile(e.Current())
	return p
}

// RecordOutcome records a task success (false) or failure (true).
func (e *Engine) RecordOutcome(failed bool) {
	e.recentMu.Lock()
	defer e.recentMu.Unlock()
	// Shift window
	copy(e.recentFailures, e.recentFailures[1:])
	e.recentFailures[len(e.recentFailures)-1] = failed
	if e.writeCount < len(e.recentFailures) {
		e.writeCount++
	}
}

// failRate returns the percentage of recent failures.
func (e *Engine) failRate() float64 {
	e.recentMu.Lock()
	defer e.recentMu.Unlock()
	total := e.writeCount
	if total == 0 {
		return 0
	}
	failCount := 0
	// Only count actual recorded outcomes
	for i := 0; i < total && i < len(e.recentFailures); i++ {
		idx := len(e.recentFailures) - total + i
		if idx >= 0 && e.recentFailures[idx] {
			failCount++
		}
	}
	return float64(failCount) / float64(total) * 100
}

// memUsage returns current memory usage in MB and percentage.
func (e *Engine) memUsage() (mb int, percent float64) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	mb = int(m.Alloc / 1024 / 1024)
	// 与 main.go 的 debug.SetMemoryLimit 共用同一配置（MEMORY_LIMIT_MB），
	// 此前硬编码 4096 与 main 的 2800MiB 不一致：容器内存按 4GB 算档位，
	// 实际 GC 限制在 2.8GB，压力判断整体偏松。
	percent = float64(mb) / float64(e.cfg.MemoryLimitMB) * 100
	return
}

// Start begins the adaptive monitoring loop.
func (e *Engine) Start(ctx context.Context, getQueueDepth func() int) {
	if e.cfg.Mode != "auto" {
		log.Printf("[adaptive] mode=%s (fixed), skipping auto-adaptation\n", e.cfg.Mode)
		return
	}
	safego.Go(func() {
		ticker := time.NewTicker(e.cfg.AdaptiveCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.evaluate(getQueueDepth)
			}
		}
	})
}

// evaluate decides whether to shift mode based on current metrics.
func (e *Engine) evaluate(getQueueDepth func() int) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[adaptive] evaluate panic: %v\n", r)
		}
	}()

	memMB, memPct := e.memUsage()
	failRate := e.failRate()
	queueDepth := 0
	if getQueueDepth != nil {
		queueDepth = getQueueDepth()
	}

	e.mu.RLock()
	current := e.current
	e.mu.RUnlock()

	// Decision logic
	var target Mode
	switch current {
	case ModeAggressive:
		// Downshift triggers
		if memPct > e.cfg.MemHighThreshold || failRate > e.cfg.FailRateThreshold || queueDepth > e.cfg.QueueDeepThreshold {
			target = ModeStandard
		} else {
			target = ModeAggressive
		}
	case ModeStandard:
		// Upshift: safe memory, low fail rate, some queue pressure
		if memPct < e.cfg.MemSafeThreshold && failRate < e.cfg.FailRateSafe && queueDepth > e.cfg.QueueLightThreshold {
			target = ModeAggressive
		} else if memPct > e.cfg.MemHighThreshold || failRate > e.cfg.FailRateThreshold {
			target = ModeConservative
		} else {
			target = ModeStandard
		}
	case ModeConservative:
		// Upshift: memory OK, fail rate low, queue building
		if memPct < e.cfg.MemLowThreshold && failRate < e.cfg.FailRateSafe && queueDepth > e.cfg.QueueLightThreshold {
			target = ModeStandard
		} else {
			target = ModeConservative
		}
	}

	if target != current {
		e.mu.Lock()
		e.current = target
		e.mu.Unlock()

		profile, _ := GetProfile(target)
		msg := fmt.Sprintf(
			"[adaptive] mode switched: %s -> %s (mem=%dMB/%.1f%%, failRate=%.1f%%, queue=%d) | workers=%d concurrent=%d pool=%d send=%v~%v",
			current, target, memMB, memPct, failRate, queueDepth,
			profile.WorkerCount, profile.MaxConcurrent, profile.MaxPoolSize,
			profile.MinSendInterval, profile.MaxSendInterval,
		)
		fmt.Println(msg)

		if e.sseBroker != nil {
			e.sseBroker.BroadcastGlobal("adaptive_mode", map[string]any{
				"mode":           string(target),
				"previous":       string(current),
				"memory_mb":      memMB,
				"memory_pct":     memPct,
				"fail_rate":      failRate,
				"queue_depth":    queueDepth,
				"workers":        profile.WorkerCount,
				"max_concurrent": profile.MaxConcurrent,
				"max_pool_size":  profile.MaxPoolSize,
				"min_interval":   profile.MinSendInterval.Seconds(),
				"max_interval":   profile.MaxSendInterval.Seconds(),
				"time":           time.Now().UTC(),
			})
		}
	}
}
