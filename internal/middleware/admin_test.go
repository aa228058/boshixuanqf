package middleware

import (
	"os"
	"testing"

	"tg-sender/internal/config"
)

func resetAdminSecretWarned() {
	adminSecretWarned = false
}

func TestAdminSecret_PrefersEnv(t *testing.T) {
	resetAdminSecretWarned()
	t.Setenv("ADMIN_JWT_SECRET", "independent-admin-secret-key-0123456789abcdef")

	cfg := &config.Config{JWTSecret: "tenant-jwt-secret-0123456789abcdef"}
	got := adminSecret(cfg)
	want := "independent-admin-secret-key-0123456789abcdef"
	if string(got) != want {
		t.Errorf("adminSecret = %q, want %q (env must take priority over derivation)", got, want)
	}
}

func TestAdminSecret_FallbackDerivation(t *testing.T) {
	resetAdminSecretWarned()
	// 确保 env 未设置（t.Setenv 不能设空值来表达"未设置"，用 Unsetenv）
	os.Unsetenv("ADMIN_JWT_SECRET")

	cfg := &config.Config{JWTSecret: "tenant-secret"}
	got := adminSecret(cfg)
	want := "tenant-secret_admin"
	if string(got) != want {
		t.Errorf("fallback derivation = %q, want %q", got, want)
	}
	if !adminSecretWarned {
		t.Error("expected a security warning log when falling back to derived key")
	}
}
