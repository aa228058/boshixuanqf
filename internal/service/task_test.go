package service

import (
	"testing"

	"github.com/google/uuid"
	"tg-sender/internal/domain"
	"tg-sender/internal/repository"
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

func TestFailedTargetsToMappingsPreservesAccountAssignments(t *testing.T) {
	accountA := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	accountB := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	got := failedTargetsToMappings([]repository.FailedTarget{
		{AccountID: &accountA, TargetID: "group-1"},
		{AccountID: &accountB, TargetID: "group-1"},
		{AccountID: &accountA, TargetID: "group-2"},
		{AccountID: &accountA, TargetID: "group-1"},
	})
	if len(got) != 2 {
		t.Fatalf("expected 2 account mappings, got %d", len(got))
	}
	if got[0].AccountID != accountA || len(got[0].Targets) != 2 || got[0].Targets[0] != "group-1" || got[0].Targets[1] != "group-2" {
		t.Fatalf("unexpected account A mapping: %#v", got[0])
	}
	if got[1].AccountID != accountB || len(got[1].Targets) != 1 || got[1].Targets[0] != "group-1" {
		t.Fatalf("unexpected account B mapping: %#v", got[1])
	}
}

func TestSplitMappingsSplitsLargeSingleAccountMapping(t *testing.T) {
	accountID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	targets := []string{"g1", "g2", "g3", "g4", "g5", "g6", "g7"}
	chunks := splitMappings([]domain.AccountTargetMapping{{AccountID: accountID, Targets: targets}}, 3)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	var rebuilt []string
	for i, chunk := range chunks {
		count := 0
		for _, mapping := range chunk {
			if mapping.AccountID != accountID {
				t.Fatalf("chunk %d changed account assignment: %s", i, mapping.AccountID)
			}
			count += len(mapping.Targets)
			rebuilt = append(rebuilt, mapping.Targets...)
		}
		if count > 3 {
			t.Fatalf("chunk %d exceeds limit: %d", i, count)
		}
	}
	if len(rebuilt) != len(targets) {
		t.Fatalf("lost targets: got %d, want %d", len(rebuilt), len(targets))
	}
	for i := range targets {
		if rebuilt[i] != targets[i] {
			t.Fatalf("target order/content changed at %d: got %q want %q", i, rebuilt[i], targets[i])
		}
	}
}
