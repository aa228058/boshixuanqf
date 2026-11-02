package crypto

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"sync"
	"testing"
)

// resetSessionKey 清空缓存的派生密钥。GetSessionKey 用 sync.Once 进程内缓存，
// 每个用例必须先重置再用 t.Setenv 换密钥，否则读到上一个用例的缓存值。
func resetSessionKey() {
	sessionKey = nil
	sessionKeyOnce = sync.Once{}
}

func TestGetSessionKey_PrefersSessionEncryptionKey(t *testing.T) {
	resetSessionKey()
	t.Setenv("SESSION_ENCRYPTION_KEY", "session-key-0123456789abcdef0123456789abcdef")
	t.Setenv("JWT_SECRET", "jwt-key-0123456789abcdef0123456789abcdef") // gitleaks:allow 测试夹具

	got := GetSessionKey()
	want := sha256.Sum256([]byte("session-key-0123456789abcdef0123456789abcdef"))
	if len(got) != 32 {
		t.Fatalf("expected 32-byte key, got %d bytes", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("key mismatch at byte %d: got %x, want %x", i, got, want[:])
		}
	}
}

func TestGetSessionKey_FallsBackToJWTSecret(t *testing.T) {
	resetSessionKey()
	// 确保 SESSION_ENCRYPTION_KEY 未设置
	os.Unsetenv("SESSION_ENCRYPTION_KEY")
	t.Setenv("JWT_SECRET", "only-jwt-key-0123456789abcdef0123456789abcdef")

	got := GetSessionKey()
	want := sha256.Sum256([]byte("only-jwt-key-0123456789abcdef0123456789abcdef"))
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fallback key mismatch at byte %d", i)
		}
	}
}

// TestGetSessionKey_NoKeyExits 验证两个 env 都未设置时进程必须退出（log.Fatal），
// 绝不允许用公开常量密钥静默加密所有会话。用子进程捕获 os.Exit(1)。
func TestGetSessionKey_NoKeyExits(t *testing.T) {
	if os.Getenv("TEST_SESSION_KEY_FATAL") == "1" {
		os.Unsetenv("SESSION_ENCRYPTION_KEY")
		os.Unsetenv("JWT_SECRET")
		GetSessionKey() // 应 log.Fatal → os.Exit(1)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestGetSessionKey_NoKeyExits")
	cmd.Env = append(os.Environ(), "TEST_SESSION_KEY_FATAL=1")
	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() == 0 {
		t.Fatalf("expected process to exit non-zero when no session key configured, got err=%v", err)
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	t.Setenv("SESSION_ENCRYPTION_KEY", "roundtrip-key-0123456789abcdef0123456789") // gitleaks:allow 测试夹具

	plaintext := []byte("telegram-auth-key-material")
	encrypted, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Fatalf("round-trip mismatch: got %q, want %q", decrypted, plaintext)
	}
}

func TestDecrypt_TamperedCiphertextFails(t *testing.T) {
	t.Setenv("SESSION_ENCRYPTION_KEY", "tamper-key-0123456789abcdef0123456789abcdef") // gitleaks:allow 测试夹具

	encrypted, err := Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	// 翻转密文中间的一个字节（string 不可寻址，先转 []byte）
	raw := []byte(encrypted)
	raw[len(raw)/2] ^= 0xFF

	if _, err := Decrypt(string(raw)); err == nil {
		t.Fatal("expected error for tampered ciphertext, got nil")
	}
}
