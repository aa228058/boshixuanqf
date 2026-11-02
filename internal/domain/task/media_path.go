package task

import (
	"fmt"
	"path/filepath"
	"strings"
)

// validateMediaPath 校验任务 payload 中的媒体路径，防路径穿越（P1-1 修复的纯函数化）。
// 拒绝绝对路径与任何 .. 逃逸；拼接后复核必须仍位于 mediaDir 内。
// 返回安全拼接后的绝对路径。
func validateMediaPath(mediaDir, mediaSource string) (string, error) {
	clean := filepath.Clean(mediaSource)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("非法媒体路径（拒绝越界访问）: %q", mediaSource)
	}
	base := filepath.Clean(mediaDir)
	path := filepath.Join(base, clean)
	if path != base && !strings.HasPrefix(path, base+string(filepath.Separator)) {
		return "", fmt.Errorf("非法媒体路径（越出媒体目录）: %q", mediaSource)
	}
	return path, nil
}
