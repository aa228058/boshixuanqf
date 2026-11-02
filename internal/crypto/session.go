package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

// sessionKey caches the derived key for process lifetime. Key is read from
// env on first use; caching avoids re-hashing on every encrypt/decrypt call.
var (
	sessionKey     []byte
	sessionKeyOnce sync.Once
)

// GetSessionKey returns the encryption key from environment.
// It requires SESSION_ENCRYPTION_KEY or JWT_SECRET to be set.
// Falls back to JWT_SECRET only if SESSION_ENCRYPTION_KEY is empty.
// Refuses to run with a hardcoded fallback key: a publicly known key would
// let anyone decrypt all stored Telegram sessions if the database leaks.
func GetSessionKey() []byte {
	sessionKeyOnce.Do(func() {
		key := os.Getenv("SESSION_ENCRYPTION_KEY")
		if key == "" {
			key = os.Getenv("JWT_SECRET")
			// F-22: JWT_SECRET 泄露将同时暴露全部加密存储的 Telegram 会话，
			// 这里显式告警引导配置独立密钥。
			log.Print("[SECURITY] SESSION_ENCRYPTION_KEY not set; falling back to JWT_SECRET. " +
				"Set an independent SESSION_ENCRYPTION_KEY (>= 32 chars) so a JWT compromise " +
				"does not expose stored sessions.")
		}
		if key == "" {
			log.Fatal("[SECURITY] SESSION_ENCRYPTION_KEY (or JWT_SECRET) is not set; " +
				"refusing to encrypt sessions with a publicly known fallback key. " +
				"Set SESSION_ENCRYPTION_KEY to a random value (>= 32 chars).")
		}
		hash := sha256.Sum256([]byte(key))
		sessionKey = hash[:]
	})
	return sessionKey
}

// Encrypt encrypts plaintext using AES-GCM.
func Encrypt(plaintext []byte) (string, error) {
	key := GetSessionKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a base64-encoded ciphertext using AES-GCM.
func Decrypt(ciphertext string) ([]byte, error) {
	key := GetSessionKey()
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	if len(data) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertextBytes := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertextBytes, nil)
}
