package service

import (
	"testing"

	"github.com/google/uuid"
	"tg-sender/internal/domain"
)

func TestValidateMonitorRule(t *testing.T) {
	tests := []struct {
		caseName string
		ruleName string
		ruleType string
		sources, targets, keywords []string
		matchType string
		wantErr bool
	}{
		{caseName: "valid reply", ruleName: "reply rule", ruleType: "reply", sources: []string{"-1001"}, keywords: []string{"hello"}, matchType: "contains"},
		{caseName: "valid forward", ruleName: "forward rule", ruleType: "forward", sources: []string{"-1001"}, targets: []string{"-1002"}, keywords: []string{"hello"}, matchType: "exact"},
		{caseName: "valid from user without keywords", ruleName: "from user", ruleType: "reply", sources: []string{"-1001"}, keywords: nil, matchType: "from_user"},
		{caseName: "empty name", ruleType: "reply", sources: []string{"-1001"}, keywords: []string{"hello"}, matchType: "contains", wantErr: true},
		{caseName: "missing source chats", ruleName: "r", ruleType: "reply", keywords: []string{"hello"}, matchType: "contains", wantErr: true},
		{caseName: "invalid rule type", ruleName: "r", ruleType: "invalid", sources: []string{"-1001"}, keywords: []string{"hello"}, matchType: "contains", wantErr: true},
		{caseName: "invalid match type", ruleName: "r", ruleType: "reply", sources: []string{"-1001"}, keywords: []string{"hello"}, matchType: "invalid", wantErr: true},
		{caseName: "empty keywords", ruleName: "r", ruleType: "reply", sources: []string{"-1001"}, matchType: "contains", wantErr: true},
		{caseName: "empty keyword entry", ruleName: "r", ruleType: "reply", sources: []string{"-1001"}, keywords: []string{" "}, matchType: "contains", wantErr: true},
		{caseName: "invalid regex", ruleName: "r", ruleType: "reply", sources: []string{"-1001"}, keywords: []string{"("}, matchType: "regex", wantErr: true},
		{caseName: "forward without target", ruleName: "r", ruleType: "forward", sources: []string{"-1001"}, keywords: []string{"hello"}, matchType: "contains", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.caseName, func(t *testing.T) {
			err := validateMonitorRule(tt.ruleName, tt.ruleType, tt.sources, tt.targets, tt.keywords, tt.matchType)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateMonitorRule() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMonitorServiceRejectsInvalidRuleBeforeRepositoryAccess(t *testing.T) {
	svc := NewMonitorService(nil, nil)
	_, err := svc.Create(t.Context(), uuid.New(), "bad", "reply", nil, nil, []string{"("}, "regex", "", nil)
	if err == nil {
		t.Fatal("invalid regex must be rejected before repository access")
	}
	err = svc.Update(t.Context(), &domain.MonitorRule{
		Name: "bad", Type: "reply", MatchType: "regex", Keywords: []string{"("},
	})
	if err == nil {
		t.Fatal("invalid regex update must be rejected before repository access")
	}
}
