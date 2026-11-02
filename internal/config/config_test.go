package config

import (
	"strings"
	"testing"
)

// TestValidate_JWTSecretEntropy 钉死启动时的弱密钥拦截：
// 低熵 JWT_SECRET（如全是同一字符）必须被拒，防线上用 "aaaa..." 当密钥。
func TestValidate_JWTSecretEntropy(t *testing.T) {
	c := defaultTestConfig()
	c.JWTSecret = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "entropy") {
		t.Fatalf("low-entropy secret must be rejected, got: %v", err)
	}
}

func TestValidate_JWTSecretTooShort(t *testing.T) {
	c := defaultTestConfig()
	c.JWTSecret = "short"
	if err := c.Validate(); err == nil {
		t.Fatal("short secret must be rejected")
	}
}

func TestValidate_StrongSecretPasses(t *testing.T) {
	c := defaultTestConfig()
	c.JWTSecret = "kX9#mQ2$vL7&pR4!zN8@cT3%wB6sD5*fH2(jK8"
	if err := c.Validate(); err != nil {
		t.Fatalf("strong secret should pass, got: %v", err)
	}
}

// defaultTestConfig 返回仅含 Validate 必填字段的最小配置。
func defaultTestConfig() *Config {
	return &Config{
		JWTSecret:          "kX9#mQ2$vL7&pR4!zN8@cT3%wB6sD5*fH2(jK8",
		TGAPIID:            123456,
		TGAPIHash:          "test_hash_value_0123456789abcdef",
		DBPassword:         "testdbpass",
		DBName:             "testdb",
		MemoryLimitMB:      2800,
		TaskWorkerCount:    3,
		TaskClaimBatch:     10,
		TaskTimeoutClaim:   5,
		TaskLockTimeoutMin: 5,
	}
}
