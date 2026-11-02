package task

import (
	"context"
	"fmt"
	"runtime"
	"tg-sender/internal/safego"
	"time"
)

func (e *Engine) StartResourceMonitor(ctx context.Context) {
	safego.Go(func() {
		ticker := time.NewTicker(e.cfg.ResourceMonitorInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-e.stopCh:
				return
			case <-ticker.C:
				e.checkAndAlertResources(ctx)
			}
		}
	})
}

func (e *Engine) checkAndAlertResources(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			taskLog.Printf("resource monitor panic: %v\n", r)
		}
	}()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memMB := m.Alloc / 1024 / 1024

	// Memory alert: > threshold
	if int64(memMB) > int64(e.cfg.MemAlertThresholdMB) {
		e.sseBroker.Broadcast("resource_alert", map[string]any{
			"type":    "memory_high",
			"message": fmt.Sprintf("内存占用过高: %d MB，建议降低并发或重启服务", memMB),
			"value":   memMB,
			"time":    time.Now().UTC(),
		})
	}

	// Queue depth alert (bypass tenant filter for global monitoring)
	var queued int
	if err := e.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE status = 'queued'`).Scan(&queued); err != nil {
		taskLog.Printf("[task] failed to count queued tasks: %v", err)
	} else if queued > e.cfg.QueueAlertThreshold {
		e.sseBroker.BroadcastGlobal("queue_deep", map[string]any{
			"message": fmt.Sprintf("队列堆积: %d 个任务排队中", queued),
			"value":   queued,
			"time":    time.Now().UTC(),
		})
	}

	// Connection pool alert (via DB stats)
	if e.db != nil && e.db.Pool != nil {
		stats := e.db.Pool.Stat()
		openConns := stats.AcquiredConns()
		maxConns := stats.MaxConns()
		if maxConns > 0 && float64(openConns)/float64(maxConns) > 0.9 {
			e.sseBroker.Broadcast("resource_alert", map[string]any{
				"type":    "db_pool_high",
				"message": fmt.Sprintf("数据库连接池紧张: %d/%d", openConns, maxConns),
				"value":   openConns,
				"time":    time.Now().UTC(),
			})
		}
	}
}
