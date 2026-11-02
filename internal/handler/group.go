package handler

import (
	"sync"
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"tg-sender/internal/safego"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"tg-sender/internal/config"
	"tg-sender/internal/domain"
	"tg-sender/internal/middleware"
	"tg-sender/internal/service"
)

// GroupSyncer syncs joined Telegram groups for an account.
type GroupSyncer interface {
	SyncJoinedGroups(ctx context.Context, tenantID, accountID uuid.UUID) error
}

// GroupHandler handles group APIs.
type GroupHandler struct {
	cfg         *config.Config
	svc         *service.GroupService
	taskSvc     *service.TaskService
	syncer      GroupSyncer
	accountRepo domain.AccountRepository
}

// NewGroupHandler creates a new group handler.
func NewGroupHandler(cfg *config.Config, svc *service.GroupService, taskSvc *service.TaskService, syncer GroupSyncer, accountRepo domain.AccountRepository) *GroupHandler {
	return &GroupHandler{cfg: cfg, svc: svc, taskSvc: taskSvc, syncer: syncer, accountRepo: accountRepo}
}

// SyncAllGroups 批量同步: 后台遍历租户在线账号串行触发群组同步(3s 间隔防同 IP 风控), 接口立即返回
func (h *GroupHandler) SyncAllGroups(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	if h.syncer == nil || h.accountRepo == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "同步服务暂不可用")
	}
	accounts, err := h.accountRepo.List(c.Request().Context(), tenantID, "")
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "获取账号列表失败")
	}
	safego.Go(func() {
		started := 0
		var wg sync.WaitGroup
		sem := make(chan struct{}, 3) // 最多 3 个号同时同步(防止同IP并发过高触发风控)
		for _, acc := range accounts {
			// 待机(离线)号也同步：GetClient 会现场建连。只跳过真正不可用的。
			if acc.Status == domain.AccountStatusBanned || acc.Status == domain.AccountStatusRestricted || acc.Status == domain.AccountStatusPendingAuth {
				continue
			}
			started++
			accID := acc.AccountID
			wg.Add(1)
			sem <- struct{}{}
			// 崩溃类修复: 脱协程必须 safego(panic 会崩进程);
			// 竞态修复: started 由外层循环递增, 直接捕获会造成 data race,
			// 先快照成局部变量再传入。
			cur := started
			safego.Go(func() {
				defer wg.Done()
				defer func() { <-sem }()
				syncCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				_ = h.syncer.SyncJoinedGroups(syncCtx, tenantID, accID)
				log.Printf("[handler] 批量同步进度: %d/%d", cur, len(accounts))
			})
			time.Sleep(500 * time.Millisecond) // 错峰启动, 防瞬时并发
		}
		wg.Wait()
	})
	return c.JSON(http.StatusOK, map[string]any{"total_accounts": len(accounts)})
}

// List godoc
// @Summary 获取群组列表
// @Description 获取租户下的所有群组
// @Tags 群组
// @Accept json
// @Produce json
// @Param page query int false "页码" default(1)
// @Param page_size query int false "每页数量" default(50)
// @Success 200 {array} domain.Group
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /groups [get]
// List returns groups for the tenant.
func (h *GroupHandler) List(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	page := 1
	if ps := c.QueryParam("page"); ps != "" {
		p, err := strconv.Atoi(ps)
		if err != nil || p < 1 {
			return echo.NewHTTPError(http.StatusBadRequest, "页码参数错误")
		}
		page = p
	}

	// Check account_id first for page_size override
	accountIDStr := c.QueryParam("account_id")

	pageSize := h.cfg.GroupDefaultPageSize
	if pss := c.QueryParam("page_size"); pss != "" {
		ps, err := strconv.Atoi(pss)
		if err != nil || ps < 1 {
			return echo.NewHTTPError(http.StatusBadRequest, "每页数量参数错误")
		}
		pageSize = ps
	} else if accountIDStr != "" {
		// 按账号查询群组时（GroupSelector），默认返回全部
		pageSize = 10000
	}
	log.Printf("[handler] 调用群组列表, tenantID=%s, accountID=%s, page=%d, pageSize=%d", tenantID, accountIDStr, page, pageSize)
	var groups []*domain.Group
	var total int
	if accountIDStr != "" {
		accountID, parseErr := uuid.Parse(accountIDStr)
		if parseErr != nil {
			log.Printf("[handler] 无效的账号ID: %s", accountIDStr)
			return echo.NewHTTPError(http.StatusBadRequest, "账号ID格式错误")
		}
		groups, err = h.svc.ListByAccount(c.Request().Context(), tenantID, accountID, page, pageSize)
		var countErr error
		total, countErr = h.svc.CountByAccount(c.Request().Context(), tenantID, accountID)
		if countErr != nil {
			log.Printf("[handler] CountByAccount failed: %v", countErr)
			total = 0
		}
		log.Printf("[handler] ListByAccount返回%d个群组，总计=%d，错误=%v", len(groups), total, err)
	} else {
		groups, err = h.svc.List(c.Request().Context(), tenantID, page, pageSize)
		total = len(groups)
		log.Printf("[handler] List返回%d个群组，错误=%v", len(groups), err)
	}
	if err != nil {
		log.Printf("[handler] 获取群组列表失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}
	log.Printf("[handler] 获取群组列表成功，返回%d个群组（总计=%d）", len(groups), total)
	return SuccessList(c, groups, total, page, pageSize)
}

// CreateRequest represents create group request.
type CreateGroupRequest struct {
	TGChatID int64  `json:"tg_chat_id" validate:"required"`
	Title    string `json:"title"`
	Username string `json:"username"`
	Type     string `json:"type" validate:"oneof=group channel supergroup"`
}

// Create godoc
// @Summary 创建群组
// @Description 创建新的群组记录
// @Tags 群组
// @Accept json
// @Produce json
// @Param body body CreateGroupRequest true "创建群组请求"
// @Success 201 {object} domain.Group
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /groups [post]
// Create creates a new group.
func (h *GroupHandler) Create(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	var req CreateGroupRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}
	if req.Type == "" {
		req.Type = "group"
	}

	group, err := h.svc.Create(c.Request().Context(), tenantID, req.TGChatID, req.Title, req.Username, req.Type)
	if err != nil {
		log.Printf("[handler] 创建群组失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "创建群组失败")
	}
	return Created(c, group)
}

// GetByID godoc
// @Summary 获取群组详情
// @Description 获取指定群组详情
// @Tags 群组
// @Accept json
// @Produce json
// @Param id path string true "群组ID"
// @Success 200 {object} domain.Group
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /groups/{id} [get]
// GetByID returns a single group.
func (h *GroupHandler) GetByID(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	groupID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "群组ID格式错误")
	}
	group, err := h.svc.GetByID(c.Request().Context(), tenantID, groupID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "群组不存在")
		}
		log.Printf("[handler] GetByID failed: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "服务器内部错误")
	}
	return Success(c, group)
}

// UpdateGroupRequest represents update group request.
type UpdateGroupRequest struct {
	Title    *string `json:"title,omitempty"`
	Username *string `json:"username,omitempty"`
}

// Update godoc
// @Summary 更新群组
// @Description 更新群组信息
// @Tags 群组
// @Accept json
// @Produce json
// @Param id path string true "群组ID"
// @Param body body UpdateGroupRequest true "更新群组请求"
// @Success 204
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /groups/{id} [put]
// Update updates group fields.
func (h *GroupHandler) Update(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	groupID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "群组ID格式错误")
	}
	var req UpdateGroupRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}
	if err := h.svc.Update(c.Request().Context(), tenantID, groupID, req.Title, req.Username); err != nil {
		log.Printf("[handler] 更新群组失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}
	return NoContent(c)
}

// GetMembers godoc
// @Summary 获取群组成员
// @Description 获取指定群组的成员列表
// @Tags 群组
// @Accept json
// @Produce json
// @Param id path string true "群组ID"
// @Param page query int false "页码" default(1)
// @Param page_size query int false "每页数量" default(100)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /groups/{id}/members [get]
// GetMembers returns members for a group.
// GetMembersGlobal 私聊选择器: 返回 contacts 表缓存的联系人(同步群组时已写入)
func (h *GroupHandler) GetMembersGlobal(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	accountID, err := uuid.Parse(c.QueryParam("account_id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "account_id required")
	}
	contacts, err := h.svc.ListGlobalMembers(c.Request().Context(), tenantID, accountID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "获取联系人列表失败")
	}
	return c.JSON(http.StatusOK, contacts)
}

func (h *GroupHandler) GetMembers(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	groupID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "群组ID格式错误")
	}

	page := 1
	if ps := c.QueryParam("page"); ps != "" {
		p, err := strconv.Atoi(ps)
		if err != nil || p < 1 {
			return echo.NewHTTPError(http.StatusBadRequest, "页码参数错误")
		}
		page = p
	}
	pageSize := 100
	if pss := c.QueryParam("page_size"); pss != "" {
		ps, err := strconv.Atoi(pss)
		if err != nil || ps < 1 {
			return echo.NewHTTPError(http.StatusBadRequest, "每页数量参数错误")
		}
		pageSize = ps
	}

	members, total, err := h.svc.GetMembers(c.Request().Context(), tenantID, groupID, page, pageSize)
	if err != nil {
		log.Printf("[handler] 获取成员失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	return Success(c, map[string]any{
		"list":      members,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ScrapeMembersRequest represents scrape members request.
type ScrapeMembersRequest struct {
	AccountID string `json:"account_id" validate:"required"`
}

// ScrapeMembers godoc
// @Summary 抓取群组成员
// @Description 触发抓取指定群组的成员
// @Tags 群组
// @Accept json
// @Produce json
// @Param id path string true "群组ID"
// @Param body body ScrapeMembersRequest true "抓取请求"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /groups/{id}/scrape [post]
// ScrapeMembers triggers scraping members for a group.
func (h *GroupHandler) ScrapeMembers(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	groupID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "群组ID格式错误")
	}

	var req ScrapeMembersRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}

	accountID, err := uuid.Parse(req.AccountID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}

	task, err := h.taskSvc.CreateScrapeMembers(c.Request().Context(), tenantID, accountID, groupID, nil)
	if err != nil {
		log.Printf("[handler] 创建采集成员任务失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	return Success(c, map[string]any{
		"status":   "scrape_task_queued",
		"task_id":  task.TaskID,
		"group_id": groupID,
	})
}

// Delete godoc
// @Summary 删除群组
// @Description 删除指定群组
// @Tags 群组
// @Accept json
// @Produce json
// @Param id path string true "群组ID"
// @Success 204
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /groups/{id} [delete]
// Delete removes a group.
func (h *GroupHandler) Delete(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	groupID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "群组ID格式错误")
	}

	if err := h.svc.Delete(c.Request().Context(), tenantID, groupID); err != nil {
		log.Printf("[handler] 删除群组失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "删除群组失败")
	}
	return NoContent(c)
}

// SyncGroupsRequest represents sync groups request.
type SyncGroupsRequest struct {
	AccountID string `json:"account_id" validate:"required"`
}

// SyncGroups godoc
// @Summary 同步已加入群组
// @Description 从Telegram同步账户已加入的群组/频道到本地数据库（异步）
// @Tags 群组
// @Accept json
// @Produce json
// @Param body body SyncGroupsRequest true "同步请求"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /groups/sync [post]
// SyncGroups triggers syncing joined groups from Telegram asynchronously.
func (h *GroupHandler) SyncGroups(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	var req SyncGroupsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}

	accountID, err := uuid.Parse(req.AccountID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}

	if h.syncer == nil {
		log.Printf("[handler] 群组同步器为空")
		return echo.NewHTTPError(http.StatusInternalServerError, "同步服务暂不可用")
	}

	log.Printf("[handler] SyncGroups开始后台同步 for tenant=%s account=%s", tenantID, accountID)
	// Run sync in background so it continues even if HTTP request times out
	safego.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[handler] 后台同步异常: %v", r)
			}
		}()
		log.Printf("[handler] 后台同步协程已启动 for account %s", accountID)
		// Use timeout context to prevent goroutine leak on network blockage
		syncCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := h.syncer.SyncJoinedGroups(syncCtx, tenantID, accountID); err != nil {
			log.Printf("[handler] 后台同步失败 for account %s: %v", accountID, err)
		} else {
			log.Printf("[handler] 后台同步完成 for account %s", accountID)
		}
	})

	log.Printf("[handler] SyncGroups返回同步状态")
	return Success(c, map[string]string{"status": "syncing"})
}
