// Package ai 免费/自建 AI 客户端(OpenAI 兼容协议)。
// 内置: glm=智谱(免费档 glm-4v-flash), qwen=阿里百炼(免费额 qwen-vl), custom=任意兼容端点。
// 仅用于入群验证的图片/文字识别兜底, 纯按钮类验证不经过本包。
package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// ProviderGLM 智谱: glm-4v-flash(视觉,免费) / glm-4-flash(文本,免费)
	ProviderGLM = "glm"
	// ProviderQwen 阿里百炼: qwen-vl-max / qwen-vl-plus(免费额)
	ProviderQwen = "qwen"
	// ProviderCustom 任意 OpenAI 兼容端点(model 字段填模型名, 端点固定 /v1)
	ProviderCustom = "custom"
)

var endpoints = map[string]string{
	ProviderGLM:    "https://open.bigmodel.cn/api/paas/v4/chat/completions",
	ProviderQwen:   "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions",
	ProviderCustom: "https://api.openai.com/v1/chat/completions",
}

var defaultModels = map[string]string{
	ProviderGLM:    "glm-4v-flash",
	ProviderQwen:   "qwen-vl-max",
	ProviderCustom: "gpt-4o-mini",
}

type message struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string 或 []contentPart
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type chatRequest struct {
	Model     string    `json:"model"`
	Messages  []message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Ask 纯文本问答(数学题/文字题)
func Ask(ctx context.Context, provider, apiKey, model, prompt string) (string, error) {
	return call(ctx, provider, apiKey, model, []message{
		{Role: "user", Content: prompt},
	})
}

// AskImage 视觉问答: imageBytes(JPEG/PNG/GIF首帧由调用方保证) + prompt
func AskImage(ctx context.Context, provider, apiKey, model, mime string, imageBytes []byte, prompt string) (string, error) {
	if mime == "" {
		mime = "image/jpeg"
	}
	b64 := base64.StdEncoding.EncodeToString(imageBytes)
	return call(ctx, provider, apiKey, model, []message{
		{Role: "user", Content: []contentPart{
			{Type: "text", Text: prompt},
			{Type: "image_url", ImageURL: &imageURL{URL: "data:" + mime + ";base64," + b64}},
		}},
	})
}

func call(ctx context.Context, provider, apiKey, model string, msgs []message) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("未配置 AI API Key")
	}
	ep, ok := endpoints[provider]
	if !ok {
		return "", fmt.Errorf("未知 AI 服务商: %s", provider)
	}
	if model == "" {
		model = defaultModels[provider]
	}
	body, _ := json.Marshal(chatRequest{Model: model, Messages: msgs, MaxTokens: 64})
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, "POST", ep, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("AI 请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("AI 返回 %d: %.200s", resp.StatusCode, raw)
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Error != nil {
		return "", fmt.Errorf("AI 错误: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("AI 无响应")
	}
	return out.Choices[0].Message.Content, nil
}
