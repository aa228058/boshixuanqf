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
	if err := os.MkdirAll(e.backupDir, 0o755); err != nil {
		log.Printf("[BACKUP] 创建备份目录失败: %v", err)
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
		if err := os.WriteFile(dst, data, 0644); err != nil {
			log.Printf("[BACKUP] 写入 %s 失败: %v", dst, err)
			continue
		}
		backedUp++
	}
	if backedUp > 0 {
		log.Printf("[BACKUP] 已备份 %d 个会话文件到 %s\n", backedUp, e.backupDir)
	}
}
