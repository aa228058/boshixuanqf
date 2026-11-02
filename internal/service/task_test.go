package service

import (
	"testing"

	"tg-sender/internal/domain"
)

// TestValidTransitions_DAG 钉死任务状态机的合法迁移集合。
// 注意：Queued→Running 不在此表——那是引擎 ClaimTasks 的内部路径（直写 SQL），
// 此表只管用户主动操作（暂停/取消/恢复/重试），两者分层有意为之。
func TestValidTransitions_DAG(t *testing.T) {
	mustAllow := []struct{ from, to domain.TaskStatus }{
		{domain.TaskStatusQueued, domain.TaskStatusPaused},
		{domain.TaskStatusQueued, domain.TaskStatusCancelled},
		{domain.TaskStatusRunning, domain.TaskStatusPaused},
		{domain.TaskStatusRunning, domain.TaskStatusCancelled},
		{domain.TaskStatusPaused, domain.TaskStatusQueued}, // 恢复 = 重新入队等认领
		{domain.TaskStatusPaused, domain.TaskStatusCancelled},
		{domain.TaskStatusFailed, domain.TaskStatusQueued}, // 重试
		{domain.TaskStatusPending, domain.TaskStatusQueued},
		{domain.TaskStatusScheduled, domain.TaskStatusQueued},
	}
	for _, tr := range mustAllow {
		if !validTransitions[tr.from][tr.to] {
			t.Errorf("transition %s -> %s must be allowed", tr.from, tr.to)
		}
	}
}

func TestValidTransitions_RejectsIllegal(t *testing.T) {
	mustDeny := []struct{ from, to domain.TaskStatus }{
		{domain.TaskStatusCompleted, domain.TaskStatusRunning}, // 终态不可逆
		{domain.TaskStatusCompleted, domain.TaskStatusQueued},
		{domain.TaskStatusCancelled, domain.TaskStatusRunning},
		{domain.TaskStatusFailed, domain.TaskStatusCompleted}, // 必须先回 queued 重试
		{domain.TaskStatusQueued, domain.TaskStatusCompleted}, // 不能跳步
	}
	for _, tr := range mustDeny {
		if validTransitions[tr.from][tr.to] {
			t.Errorf("transition %s -> %s must be denied", tr.from, tr.to)
		}
	}
}
