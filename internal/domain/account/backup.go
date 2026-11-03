package account

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"tg-sender/internal/safego"
	"time"
)

func (e *Engine) StartSessionBackup() {
	e.wg.Add(1)
	safego.Go(func() {
		defer e.wg.Done()
		ticker := time.NewTicker(e.cfg.SessionBackupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-e.stopCh:
				return
			case <-ticker.C:
				e.backupSessionFiles()
			}
		}
	})
}

func (e *Engine) backupSessionFiles() {
	if err := os.MkdirAll(e.backupDir, 0o700); err != nil {
		log.Printf("[BACKUP] 创建备份目录失败: %v", err)
		return
	}
	// Session 备份包含可直接登录的 auth_key，目录必须仅允许当前服务用户访问；
	// MkdirAll 不会收紧已存在目录的权限，因此显式 Chmod。
	if err := os.Chmod(e.backupDir, 0o700); err != nil {
		log.Printf("[BACKUP] 收紧备份目录权限失败: %v", err)
		return
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		log.Printf("[BACKUP] 读取目录失败: %v", err)
		return
	}
	backedUp := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".session") {
			continue
		}
		src := filepath.Join(".", entry.Name())
		dst := fmt.Sprintf("%s/%s_%s", e.backupDir, entry.Name(), time.Now().Format("20060102_150405.000"))
		data, err := os.ReadFile(src)
		if err != nil {
			log.Printf("[BACKUP] 读取 %s 失败: %v", src, err)
			continue
		}
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			log.Printf("[BACKUP] 写入 %s 失败: %v", dst, err)
			continue
		}
		backedUp++
	}
	if backedUp > 0 {
		log.Printf("[BACKUP] 已备份 %d 个会话文件到 %s\n", backedUp, e.backupDir)
	}
}
 
// DeleteSessionBackups removes timestamped local backups for a deleted account.
// Backup files contain a reusable Telegram auth_key and must not outlive account deletion.
func (e *Engine) DeleteSessionBackups(sessionName string) error {
	base := filepath.Base(sessionName) + ".session_"
	entries, err := os.ReadDir(e.backupDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read session backup directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), base) {
			continue
		}
		if err := os.Remove(filepath.Join(e.backupDir, entry.Name())); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove session backup %s: %w", entry.Name(), err)
		}
	}
	return nil
}
