package task

import (
	"strings"
	"testing"
)

// TestValidateMediaPath 钉死 P1-1 路径穿越修复：mediaSource 来自任务 payload（用户可控），
// 必须拒绝绝对路径与任何 .. 逃逸，拼接后复核必须仍位于 MediaDir 内。
func TestValidateMediaPath(t *testing.T) {
	cases := []struct {
		name    string
		source  string
		wantErr string // 空串 = 期望通过
	}{
		{"正常相对路径", "photo.jpg", ""},
		{"子目录文件", "sub/dir/pic.png", ""},
		{"点开头", "./a.png", ""},
		{"一级上级", "../etc/passwd", "越界"},
		{"多级上级", "../../etc/passwd", "越界"},
		{"绝对路径", "/etc/passwd", "越界"},
		{"伪装绝对", "//etc/passwd", "越界"},
		{"前缀混淆 a/../b", "a/../b.png", ""},     // Clean 后是 b.png，合法
		{"前缀混淆 ..x/y", "..x/y.png", ""},       // 不是 .. 路径段，合法
		{"空路径放行（Clean 后为目录本身，后续 Open 会失败）", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateMediaPath("/srv/media", tc.source)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expect pass, got err: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expect error containing %q, got: %v", tc.wantErr, err)
				}
			}
		})
	}
}

// TestValidateMediaPath_SymlinkEscape 验证拼接后复核：即使 clean 后无 ..，
// Join 结果也必须仍在 MediaDir 内（防符号链接/异常拼接逃逸）。
func TestValidateMediaPath_EscapeVariants(t *testing.T) {
	// Clean 会折叠 a/../.. → ..，仍应被拒
	if _, err := validateMediaPath("/srv/media", "a/../../etc/passwd"); err == nil {
		t.Fatal("a/../../etc/passwd should be rejected")
	}
}
