package account

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupSessionFilesFromUsesPrivateFilesAndSkipsSymlinks(t *testing.T) {
	sourceDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "session-backups")
	sessionData := []byte("telegram authorization key")
	if err := os.WriteFile(filepath.Join(sourceDir, "account.session"), sessionData, 0o600); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(t.TempDir(), "outside.session")
	if err := os.WriteFile(outsideFile, []byte("must not be copied"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(sourceDir, "linked.session")); err != nil {
		t.Fatal(err)
	}

	count, err := backupSessionFilesFrom(sourceDir, backupDir)
	if err != nil {
		t.Fatalf("backupSessionFilesFrom() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("backup count = %d, want 1", count)
	}

	dirInfo, err := os.Stat(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("backup directory permissions = %04o, want 0700", got)
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("backup files = %d, want 1", len(entries))
	}
	backupPath := filepath.Join(backupDir, entries[0].Name())
	fileInfo, err := os.Stat(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("backup file permissions = %04o, want 0600", got)
	}
	got, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(sessionData) {
		t.Fatalf("backup contents = %q, want %q", got, sessionData)
	}
}
