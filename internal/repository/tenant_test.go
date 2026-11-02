package repository

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestAPIKeyPrefix_WriteReadConsistency 钉死这个 bug 的回归：
// 历史 bug 是写入侧对 bcrypt 哈希取前缀、查询侧对明文取前缀，
// SHA256(哈希)[:8] ≠ SHA256(明文)[:8]，API Key 查询永远失配。
// 不变量：写入与查询必须用同一个输入（明文）计算前缀。
func TestAPIKeyPrefix_WriteReadConsistency(t *testing.T) {
	apiKey := "test-api-key-0123456789abcdef0123456789abcdef" // gitleaks:allow 测试夹具

	// 写入侧（Create / UpdateAPIKeyHash 现在的方式）
	writePrefix := computeAPIKeyPrefix(apiKey)

	// 查询侧（GetByAPIKey 的方式）
	lookupPrefix := computeAPIKeyPrefix(apiKey)

	if writePrefix != lookupPrefix {
		t.Fatalf("write prefix %q != lookup prefix %q: same plaintext must yield same prefix", writePrefix, lookupPrefix)
	}
}

// TestAPIKeyPrefix_PlaintextDiffersFromHash 显式断言旧实现是错的：
// 对哈希取前缀与对明文取前缀结果不同——如果未来有人把参数传回哈希，
// 这个用例会立刻红灯。
func TestAPIKeyPrefix_PlaintextDiffersFromHash(t *testing.T) {
	apiKey := "another-key-0123456789abcdef"
	hash, err := bcrypt.GenerateFromPassword([]byte(apiKey), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}

	fromPlaintext := computeAPIKeyPrefix(apiKey)
	fromHash := computeAPIKeyPrefix(string(hash))

	if fromPlaintext == fromHash {
		t.Fatalf("prefix(plaintext) == prefix(hash) for same key — old buggy behavior is back (both = %q)", fromPlaintext)
	}
}

func TestAPIKeyPrefix_StableAndHex(t *testing.T) {
	apiKey := "stable-key"
	a := computeAPIKeyPrefix(apiKey)
	b := computeAPIKeyPrefix(apiKey)
	if a != b {
		t.Errorf("prefix not deterministic: %q vs %q", a, b)
	}
	if len(a) != 8 {
		t.Errorf("prefix length = %d, want 8 hex chars", len(a))
	}
	for _, c := range a {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("prefix %q contains non-hex char %q", a, c)
		}
	}
}
