package backup

import (
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"tg-sender/internal/safego"
	"time"

	"tg-sender/internal/config"
)

const (
	// dbBackupRetention 保留最近 N 份数据库备份
	dbBackupRetention = 7
	// dbBackupInterval 备份间隔(每小时)
	dbBackupInterval = time.Hour
)

var dbBackupOnce sync.Once

// StartDBBackup 启动数据库定时备份(pg_dump + gzip, 保留最近 7 份).
// 服务器需安装 pg_dump; 未安装时每分钟重试探测一次, 不阻塞启动.
func StartDBBackup(cfg *config.Config) {
	dbBackupOnce.Do(func() {
		dir := os.Getenv("DB_BACKUP_DIR")
		if dir == "" {
			dir = "backups/db"
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			log.Printf("[db-backup] 创建备份目录失败: %v", err)
			return
		}
		safego.Go(func() { dbBackupLoop(cfg, dir, stopCh) })
	})
}

var stopCh = make(chan struct{})

// Stop 协调终止备份循环(服务关闭时调用, 审计P2-04)
// P2-69 审计修复: 原来裸 close(stopCh)——双调(优雅停机+信号处理各一次)必 panic。
var stopOnce sync.Once

func Stop() {
	stopOnce.Do(func() { close(stopCh) })
}

func dbBackupLoop(cfg *config.Config, dir string, stop <-chan struct{}) {
	for {
		if _, err := exec.LookPath("pg_dump"); err != nil {
			log.Printf("[db-backup] 未找到 pg_dump, 60s 后重试(安装: apt install postgresql-client)")
			select { case <-stop: return; case <-time.After(time.Minute): }
			continue
		}
		if err := runDBDump(cfg, dir); err != nil {
			log.Printf("[db-backup] 备份失败: %v", err)
		}
		pruneOldDBBackups(dir, dbBackupRetention)
		// 顺带清理过期媒体文件(群发上传的附件, 默认保留 30 天, 防止 media 目录无限膨胀)
		pruneOldMediaFiles(30 * 24 * time.Hour)
		select { case <-stop: return; case <-time.After(dbBackupInterval): }
	}
}

func runDBDump(cfg *config.Config, dir string) error {
	name := fmt.Sprintf("db_%s.sql.gz", time.Now().Format("20060102_150405"))
	path := filepath.Join(dir, name)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create backup file: %w", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	// 密码经环境变量传递, 不出现在命令行参数/日志里
	env := append(os.Environ(), "PGPASSWORD="+cfg.DBPassword)
	cmd := exec.Command("pg_dump",
		"-h", cfg.DBHost,
		"-p", fmt.Sprintf("%d", cfg.DBPort),
		"-U", cfg.DBUser,
		"-d", cfg.DBName,
		"--no-owner", "--no-privileges",
		"-Fc", // 自定义格式, 支持 pg_restore 并行恢复
	)
	cmd.Env = env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, _ := os.Create(path + ".err")
	defer stderr.Close()
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("pg_dump start: %w", err)
	}
	if _, err := io.Copy(gz, stdout); err != nil {
		_ = cmd.Wait()
		return fmt.Errorf("copy dump: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("pg_dump exit: %w (详见 %s)", err, path+".err")
	}
	log.Printf("[db-backup] 备份完成: %s", path)
	return nil
}

// pruneOldMediaFiles 删除 media 目录中超过 maxAge 的文件(上传的群发附件).
// 附件发送完成后即完成任务, 文件无需长期保留; 默认 30 天兜底防误删近期文件.
func pruneOldMediaFiles(maxAge time.Duration) {
	dir := os.Getenv("MEDIA_DIR")
	if dir == "" {
		dir = "media"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // 目录不存在或不可读: 静默跳过
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || info.IsDir() {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(dir, e.Name())); err == nil {
				removed++
			}
		}
	}
	if removed > 0 {
		log.Printf("[db-backup] 清理过期媒体文件: %d 个(>%v)", removed, maxAge)
	}
}

// pruneOldDBBackups 只保留最近 N 份, 按文件名排序(时间戳在文件名里).
func pruneOldDBBackups(dir string, keep int) {
	matches, _ := filepath.Glob(filepath.Join(dir, "db_*.sql.gz"))
	if len(matches) <= keep {
		return
	}
	sort.Strings(matches)
	for _, old := range matches[:len(matches)-keep] {
		if err := os.Remove(old); err == nil {
			log.Printf("[db-backup] 清理旧备份: %s", old)
		}
		_ = os.Remove(old + ".err")
	}
}
