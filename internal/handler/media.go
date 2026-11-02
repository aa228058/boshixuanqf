package handler

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"tg-sender/internal/config"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// MediaHandler handles media uploads for rich broadcast messages.
type MediaHandler struct {
	cfg *config.Config
}

func NewMediaHandler(cfg *config.Config) *MediaHandler {
	return &MediaHandler{cfg: cfg}
}

var mediaExtWhitelist = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true,
	".mp4": true, ".mov": true,
}

// 审计P2-01: 与扩展名白名单对应的 MIME 魔数白名单
var mediaMimeWhitelist = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true,
	"video/mp4": true, "video/quicktime": true,
}

const mediaMaxSize = 50 << 20 // 50MB

// Upload POST /media — multipart 字段 "file", 返回 {"path": "relative-path"}.
// 该路径填入广播 payload 的 media.source; 文件经 /media/* 提供访问.
func (h *MediaHandler) Upload(c echo.Context) error {
	file, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "缺少文件字段 file"})
	}
	if file.Size > mediaMaxSize {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("文件过大(最大 %dMB)", mediaMaxSize>>20)})
	}
	// 审计P2-01: 魔数校验(防伪装扩展名上传可执行文件)
	src0, err := file.Open()
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "文件读取失败"})
	}
	head := make([]byte, 512)
	n, _ := src0.Read(head)
	src0.Close()
	mime := http.DetectContentType(head[:n])
	if !mediaMimeWhitelist[mime] {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("文件内容类型不合法(%s), 仅支持真实图片/视频", mime)})
	}
	ext := filepath.Ext(file.Filename)
	if !mediaExtWhitelist[ext] {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": "不支持的文件类型(仅 jpg/png/webp/gif/mp4/mov)"})
	}
	if err := os.MkdirAll(h.cfg.MediaDir, 0o750); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": "媒体目录创建失败"})
	}
	name := uuid.NewString() + ext
	dst := filepath.Join(h.cfg.MediaDir, name)
	src, err := file.Open()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": "读取上传文件失败"})
	}
	defer src.Close()
	out, err := os.Create(dst)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": "保存文件失败"})
	}
	defer out.Close()
	if _, err := io.Copy(out, src); err != nil {
		os.Remove(dst)
		return c.JSON(http.StatusInternalServerError, map[string]any{"error": "写入文件失败"})
	}
	return c.JSON(http.StatusOK, map[string]any{"path": name})
}
