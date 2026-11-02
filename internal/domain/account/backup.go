package account

import (
	"fmt"
	"io"
	"log"
	"os"
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
	backedUp, err := backupSessionFilesFrom(".", e.backupDir)
	if err != nil {
		log.Printf("[BACKUP] 备份会话文件失败: %v", err)
		return
	}
	if backedUp > 0 {
		log.Printf("[BACKUP] 已备份 %d 个会话文件到 %s", backedUp, e.backupDir)
	}
}

// backupSessionFilesFrom copies regular .session files into a private backup
// directory. Both roots are scoped so filenames cannot escape either directory.
func backupSessionFilesFrom(sourceDir, backupDir string) (int, error) {
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return 0, fmt.Errorf("create backup directory: %w", err)
	}
	// MkdirAll does not change permissions on an existing directory. Session
	// backups contain Telegram authorization keys, so enforce private access.
	if err := os.Chmod(backupDir, 0o700); err != nil {
		return 0, fmt.Errorf("secure backup directory permissions: %w", err)
	}

	sourceRoot, err := os.OpenRoot(sourceDir)
	if err != nil {
		return 0, fmt.Errorf("open session source directory: %w", err)
	}
	defer sourceRoot.Close()

	backupRoot, err := os.OpenRoot(backupDir)
	if err != nil {
		return 0, fmt.Errorf("open session backup directory: %w", err)
	}
	defer backupRoot.Close()

	dir, err := sourceRoot.Open(".")
	if err != nil {
		return 0, fmt.Errorf("open session source directory entries: %w", err)
	}
	entries, readErr := dir.ReadDir(-1)
	closeErr := dir.Close()
	if readErr != nil {
		return 0, fmt.Errorf("read session source directory: %w", readErr)
	}
	if closeErr != nil {
		return 0, fmt.Errorf("close session source directory: %w", closeErr)
	}

	backedUp := 0
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 ||
			!strings.HasSuffix(entry.Name(), ".session") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}

		src, err := sourceRoot.Open(entry.Name())
		if err != nil {
			log.Printf("[BACKUP] 打开会话文件 %s 失败: %v", entry.Name(), err)
			continue
		}

		name := fmt.Sprintf("%s_%s", entry.Name(), time.Now().Format("20060102_150405.000000000"))
		dst, err := backupRoot.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = src.Close()
			log.Printf("[BACKUP] 创建备份文件 %s 失败: %v", name, err)
			continue
		}

		_, copyErr := io.Copy(dst, src)
		srcCloseErr := src.Close()
		dstCloseErr := dst.Close()
		if copyErr != nil || srcCloseErr != nil || dstCloseErr != nil {
			_ = backupRoot.Remove(name)
			log.Printf("[BACKUP] 写入会话备份 %s 失败: copy=%v source_close=%v backup_close=%v", name, copyErr, srcCloseErr, dstCloseErr)
			continue
		}
		backedUp++
	}
	return backedUp, nil
}
