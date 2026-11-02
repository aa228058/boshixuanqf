package handler

import (
	"net/http"

	"tg-sender/internal/domain"
	"tg-sender/internal/middleware"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// ContactHandler 私聊联系人缓存查询(群发目标选择器秒开用)
type ContactHandler struct {
	memberRepo domain.MemberRepository
}

func NewContactHandler(memberRepo domain.MemberRepository) *ContactHandler {
	return &ContactHandler{memberRepo: memberRepo}
}

// List GET /contacts?account_id=xxx
func (h *ContactHandler) List(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "未授权"})
	}
	aid := c.QueryParam("account_id")
	if aid == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "缺少 account_id"})
	}
	accountID, perr := uuid.Parse(aid)
	if perr != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "account_id 无效"})
	}
	rows, err := h.memberRepo.ListContactRows(c.Request().Context(), tenantID, accountID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{"list": rows, "total": len(rows)})
}
