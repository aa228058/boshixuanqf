package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"tg-sender/internal/ai"
	"tg-sender/internal/crypto"
	"tg-sender/internal/domain"
	"tg-sender/internal/middleware"
	"tg-sender/internal/repository"
)

// AIConfigHandler AI 配置 + 入群验证开关 + 验证记录 + 抽奖助手开关
type AIConfigHandler struct {
	repo       *repository.AIConfigRepository
	vl         *repository.VerificationLogRepository
	tenantRepo *repository.TenantRepository
	// watchSync 由 container 注入: 总开关开启后异步拉起勾选账号的监听会话(抽奖对账)
	watchSync func(ctx context.Context, tenantID uuid.UUID)
}

func NewAIConfigHandler(repo *repository.AIConfigRepository) *AIConfigHandler {
	return &AIConfigHandler{repo: repo}
}

// SetLogRepo 验证记录查询(由 container 补充注入)
func (h *AIConfigHandler) SetLogRepo(vl *repository.VerificationLogRepository) {
	h.vl = vl
}

// SetTenantRepo 抽奖助手开关读写(由 container 补充注入)
func (h *AIConfigHandler) SetTenantRepo(tr *repository.TenantRepository) {
	h.tenantRepo = tr
}

// SetRaffleWatchSync 注入抽奖监听对账回调(由 container 补充注入, 可为 nil)
func (h *AIConfigHandler) SetRaffleWatchSync(fn func(ctx context.Context, tenantID uuid.UUID)) {
	h.watchSync = fn
}

// Get handles GET /ai-config (脱敏: 永不返回明文 key)
func (h *AIConfigHandler) Get(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	provider, _, model, hasKey, _ := h.repo.Load(c.Request().Context(), tenantID)
	enabled, _ := h.repo.VerifierEnabled(c.Request().Context(), tenantID)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"provider": provider, "model": model, "has_key": hasKey, "enabled": enabled,
	})
}

// Save handles PUT /ai-config {"provider","api_key","model"} api_key 传空=不修改
func (h *AIConfigHandler) Save(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	var req struct {
		Provider string `json:"provider"`
		APIKey   string `json:"api_key"`
		Model    string `json:"model"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
	}
	ctx := c.Request().Context()
	provider, enc, model, _, _ := h.repo.Load(ctx, tenantID)
	if req.Provider != "" {
		provider = req.Provider
	}
	if req.Model != "" {
		model = req.Model
	}
	if req.APIKey != "" {
		encStr, err := crypto.Encrypt([]byte(strings.TrimSpace(req.APIKey)))
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Key 加密失败")
		}
		enc = encStr
	}
	if err := h.repo.Save(ctx, tenantID, provider, enc, model); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// Test handles POST /ai-config/test 发一道 sample 题验证连通与答题质量
func (h *AIConfigHandler) Test(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), 50*time.Second)
	defer cancel()
	provider, enc, model, hasKey, _ := h.repo.Load(ctx, tenantID)
	if !hasKey {
		return echo.NewHTTPError(http.StatusBadRequest, "请先保存 API Key")
	}
	raw, err := crypto.Decrypt(enc)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Key 解密失败: "+err.Error())
	}
	ans, err := ai.Ask(ctx, provider, string(raw), model,
		"这是入群验证测试: 3 + 5 = ? 请只输出数字答案, 不要输出任何其他内容。")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "AI 调用失败: "+err.Error())
	}
	ok := strings.Contains(ans, "8")
	return c.JSON(http.StatusOK, map[string]interface{}{"ok": ok, "answer": strings.TrimSpace(ans)})
}

// Toggle handles POST /verifier/toggle {"enabled": bool}
func (h *AIConfigHandler) Toggle(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
	}
	if err := h.repo.SetVerifierEnabled(c.Request().Context(), tenantID, req.Enabled); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// RaffleStatus handles GET /raffle/status
func (h *AIConfigHandler) RaffleStatus(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	en, err := h.tenantRepo.IsRaffleEnabled(ctx, tenantID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	// 中奖回复(060)/私聊激活(061)设置一并返回, 前端设置卡片初始化用
	replyOn, replyText, _ := h.tenantRepo.GetRaffleReply(ctx, tenantID)
	pingOn, pingText, _ := h.tenantRepo.GetRaffleBotPing(ctx, tenantID)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"enabled":       en,
		"reply_enabled": replyOn,
		"reply_text":    replyText,
		"ping_enabled":  pingOn,
		"ping_text":     pingText,
	})
}

// RafflePing handles POST /raffle/ping {"ping_enabled": bool, "ping_text": string}
// 私聊机器人激活(061): 参与成功后私聊发公告的机器人, 建立私聊通道(中奖私聊通知=铁证)
func (h *AIConfigHandler) RafflePing(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	var req struct {
		PingEnabled bool   `json:"ping_enabled"`
		PingText    string `json:"ping_text"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
	}
	if req.PingText == "" {
		req.PingText = "/start" // 默认激活语
	}
	if err := h.tenantRepo.SetRaffleBotPing(c.Request().Context(), tenantID, req.PingEnabled, req.PingText); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// RaffleReply handles POST /raffle/reply {"reply_enabled": bool, "reply_text": string}
// 中奖回复: 确认中奖的账号自动在抽奖群发感谢语(普通群消息, 支持 {奖品} 变量)
func (h *AIConfigHandler) RaffleReply(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	var req struct {
		ReplyEnabled bool   `json:"reply_enabled"`
		ReplyText    string `json:"reply_text"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
	}
	// 长度护栏: 200 字上限, 与发送侧截断一致, 防止小作文把号搞封
	if rr := []rune(req.ReplyText); len(rr) > 200 {
		req.ReplyText = string(rr[:200])
	}
	if err := h.tenantRepo.SetRaffleReply(c.Request().Context(), tenantID, req.ReplyEnabled, strings.TrimSpace(req.ReplyText)); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// RaffleToggle handles POST /raffle/toggle {"enabled": bool}
// 兼容模式: body 为空或 {}(旧版前端) 时按"取反当前状态"处理, 新旧前端都能正常开关
func (h *AIConfigHandler) RaffleToggle(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	current, _ := h.tenantRepo.IsRaffleEnabled(ctx, tenantID)
	next := !current
	if c.Request().Body != nil {
		body, _ := io.ReadAll(c.Request().Body)
		trimmed := strings.TrimSpace(string(body))
		if trimmed != "" && trimmed != "{}" {
			var req struct {
				Enabled bool `json:"enabled"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
			}
			next = req.Enabled
		}
	}
	if err := h.tenantRepo.SetRaffleEnabled(ctx, tenantID, next); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	// 开启后异步拉起勾选账号的监听会话(不等 60 秒对账周期), 失败不影响开关响应
	if next && h.watchSync != nil {
		go h.watchSync(context.Background(), tenantID)
	}
	// 必须回传最新状态: 前端依此更新开关 UI, 缺省 undefined 会被判 false("点开启却提示已关闭"的元凶)
	return c.JSON(http.StatusOK, map[string]interface{}{"ok": true, "enabled": next})
}

// Logs handles GET /verification-logs?limit=50
func (h *AIConfigHandler) Logs(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	if h.vl == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "log repo 未注入")
	}
	limit := 50
	list, err := h.vl.List(c.Request().Context(), tenantID, limit)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"list": list})
}

var _ = domain.VerificationLog{} // 保留 domain 引用
var _ = uuid.UUID{}
