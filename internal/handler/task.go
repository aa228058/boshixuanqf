package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"tg-sender/internal/safego"
	"tg-sender/internal/domain"
	"tg-sender/internal/domain/task"
	"tg-sender/internal/middleware"
	"tg-sender/internal/repository"
	"tg-sender/internal/service"
	"tg-sender/internal/sse"
)

// TaskHandler handles task APIs.
type TaskHandler struct {
	svc         *service.TaskService
	planChecker *middleware.PlanChecker
	auditRepo   *repository.AuditRepository
	engine      *task.Engine
	sseBroker   *sse.Broker // R98: 任务详情流实时转发日志用
}

// NewTaskHandler creates a new task handler.
func NewTaskHandler(svc *service.TaskService, planChecker *middleware.PlanChecker, auditRepo *repository.AuditRepository, engine *task.Engine) *TaskHandler {
	return &TaskHandler{svc: svc, planChecker: planChecker, auditRepo: auditRepo, engine: engine}
}

// SetSSEBroker injects the SSE broker (R98: StreamProgress 订阅事件流转发实时日志).
func (h *TaskHandler) SetSSEBroker(b *sse.Broker) { h.sseBroker = b }

// TaskListResponse represents a list of tasks response.
type TaskListResponse struct {
	Data     []*domain.Task `json:"data"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

// TaskResponse represents a single task response.
type TaskResponse struct {
	Data *domain.Task `json:"data"`
}

// List godoc
// @Summary 获取任务列表
// @Description 获取租户下的所有任务，支持按状态筛选和分页
// @Tags 任务
// @Accept json
// @Produce json
// @Param status query string false "任务状态筛选" Enums(pending,running,paused,completed,failed,cancelled)
// @Param page query int false "页码" default(1)
// @Param page_size query int false "每页数量" default(20)
// @Success 200 {object} TaskListResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks [get]
// List returns tasks for the tenant with pagination.
// ValidateTargets 发送前目标预检: 批量校验目标有效性, 返回可直接发送的与无效目标(附原因)
func (h *TaskHandler) ValidateTargets(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	var req struct {
		TargetIDs []string `json:"target_ids"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	valid, invalid := h.svc.ValidateTargets(c.Request().Context(), tenantID, req.TargetIDs)
	return c.JSON(http.StatusOK, map[string]any{
		"total":   len(req.TargetIDs),
		"valid":   valid,
		"invalid": invalid,
	})
}

func (h *TaskHandler) List(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	statusStr := c.QueryParam("status")
	var status *domain.TaskStatus
	if statusStr != "" {
		s := domain.TaskStatus(statusStr)
		status = &s
	}

	page := 1
	if p := c.QueryParam("page"); p != "" {
		if pi, err := strconv.Atoi(p); err == nil && pi > 0 {
			page = pi
		}
	}

	pageSize := 20
	if ps := c.QueryParam("page_size"); ps != "" {
		if psi, err := strconv.Atoi(ps); err == nil && psi > 0 && psi <= 100 {
			pageSize = psi
		}
	}

	// Parse optional account_id filter for account-level isolation
	var accountID *uuid.UUID
	var taskType *domain.TaskType
	if tStr := c.QueryParam("type"); tStr != "" {
		t := domain.TaskType(tStr)
		taskType = &t
	}
	if accStr := c.QueryParam("account_id"); accStr != "" {
		if accUUID, err := uuid.Parse(accStr); err == nil {
			accountID = &accUUID
		}
	}

	// hide_loops=1 时隐藏循环包装任务(任务列表页用, 只显示实际执行记录)
	hideLoops := c.QueryParam("hide_loops") == "1"

	// 时间范围筛选: range=today(今日) | 7d(近7日) | 缺省=全部(兼容旧调用)
	var since *time.Time
	switch c.QueryParam("range") {
	case "today":
		t := time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 0, 0, 0, 0, time.Now().Location())
		since = &t
	case "7d":
		t := time.Now().AddDate(0, 0, -7)
		since = &t
	}

	tasks, err := h.svc.List(c.Request().Context(), tenantID, status, accountID, taskType, hideLoops, since, page, pageSize)
	if err != nil {
		log.Printf("[handler] 获取任务列表失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "服务器内部错误")
	}
	total, err := h.svc.Count(c.Request().Context(), tenantID, status, accountID, taskType, since)
	if err != nil {
		total = len(tasks)
	}

	return SuccessList(c, tasks, total, page, pageSize)
}

// CreateBroadcastRequest represents broadcast task request.
type CreateBroadcastRequest struct {
	AccountIDs  []string `json:"account_ids" validate:"required"`
	TargetIDs   []string `json:"target_ids" validate:"required"`
	Message     string   `json:"message" validate:"required"`
	ScheduledAt string   `json:"scheduled_at,omitempty"`
	IntervalMin int      `json:"interval_min,omitempty"` // 最小发送间隔(秒)
	IntervalMax int      `json:"interval_max,omitempty"` // 最大发送间隔(秒)
	// Mappings enables precise account-target mapping mode.
	// If provided, each account sends only to its assigned targets (no cartesian product).
	// Format: [{"account_id": "uuid", "targets": ["@group1", "-100..."]}, ...]
	Mappings []struct {
		AccountID string   `json:"account_id"`
		Targets   []string `json:"targets"`
	} `json:"mappings,omitempty"`
	// 富媒体与 URL 按钮(F1), 注入任务 payload 供执行器使用
	Media   map[string]any   `json:"media,omitempty"`   // {"type":"photo|video","source":"url或路径"}
	Buttons []map[string]any `json:"buttons,omitempty"` // [{"text":"...","url":"..."}]
	// Priority 任务优先级 1-10(默认 5; 8=优先插队, ClaimTasks 按 priority DESC 排序)
	Priority int `json:"priority,omitempty"`
}

// broadcastExtras 提取请求中的富媒体/URL按钮等扩展字段, 注入任务 payload
func broadcastExtras(req *CreateBroadcastRequest) map[string]any {
	log.Printf("[broadcast] 创建请求 media=%v buttons=%d", req.Media, len(req.Buttons))
	extra := map[string]any{}
	if len(req.Media) > 0 {
		extra["media"] = req.Media
	}
	if len(req.Buttons) > 0 {
		extra["buttons"] = req.Buttons
	}
	if req.Priority >= 1 && req.Priority <= 10 {
		extra["priority"] = req.Priority
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

// CreateRequest represents generic task creation request.
type CreateTaskRequest struct {
	Type        string   `json:"type" validate:"required"` // broadcast | scrape_members | join_groups
	AccountIDs  []string `json:"account_ids,omitempty"`
	AccountID   string   `json:"account_id,omitempty"`
	TargetIDs   []string `json:"target_ids,omitempty"`
	GroupID     string   `json:"group_id,omitempty"`
	Message     string   `json:"message,omitempty"`
	Links       []string `json:"links,omitempty"`
	ScheduledAt string   `json:"scheduled_at,omitempty"`
	// Priority 任务优先级 1-10(默认 5; 8=优先插队, ClaimTasks 按 priority DESC 排序)
	Priority int `json:"priority,omitempty"`
}

// Create godoc
// @Summary 创建任务
// @Description 根据类型创建不同类型的任务(broadcast/scrape_members/join_groups)
// @Tags 任务
// @Accept json
// @Produce json
// @Param body body CreateTaskRequest true "创建任务请求"
// @Success 201 {object} TaskResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks [post]
// Create dispatches to specific task creation handlers based on type.
func (h *TaskHandler) Create(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	// Read body once and restore so sub-handlers can re-bind
	if c.Request().ContentLength > 10*1024*1024 {
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "请求体过大（最大10MB）")
	}
	bodyBytes, err := io.ReadAll(c.Request().Body)
	if err != nil {
		log.Printf("[handler] 读取任务请求体失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}
	c.Request().Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req CreateTaskRequest
	if err := c.Bind(&req); err != nil {
		log.Printf("[handler] 解析任务请求失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}

	// 前端部分页面(如加群)传单数 account_id，此处归一化为 account_ids，
	// 否则任务不带账号，执行时永远"加入群聊中"空转
	if len(req.AccountIDs) == 0 && req.AccountID != "" {
		req.AccountIDs = []string{req.AccountID}
	}

	log.Printf("[handler] 创建任务: type=%s account_id=%q account_ids=%v target_ids=%v links=%d",
		req.Type, req.AccountID, req.AccountIDs, req.TargetIDs, len(req.Links))

	// 审计: 记录任务创建(谁/何时/什么类型/规模)
	if h.auditRepo != nil {
		_ = h.auditRepo.Create(c.Request().Context(), &domain.AuditLog{
			LogID:     uuid.New(),
			TenantID:  tenantID,
			UserID:    tenantID.String(),
			Action:    "task_create",
			Target:    fmt.Sprintf("%s accounts=%d targets=%d", req.Type, len(req.AccountIDs), len(req.TargetIDs)),
			IP:        c.RealIP(),
			CreatedAt: time.Now(),
		})
	}

	// 根据任务类型检查套餐权限
	if h.planChecker != nil {
		switch req.Type {
		case "broadcast":
			if err := h.planChecker.CheckFeature(c, tenantID.String(), middleware.FeatureBroadcast); err != nil {
				return err
			}
		case "scrape_members":
			if err := h.planChecker.CheckFeature(c, tenantID.String(), middleware.FeatureScrapeMembers); err != nil {
				return err
			}
		case "scrape_links":
			if err := h.planChecker.CheckFeature(c, tenantID.String(), middleware.FeatureScrapeLinks); err != nil {
				return err
			}
		case "join_groups":
			if err := h.planChecker.CheckFeature(c, tenantID.String(), middleware.FeatureJoinGroups); err != nil {
				return err
			}
		case "scheduled_broadcast":
			if err := h.planChecker.CheckFeature(c, tenantID.String(), middleware.FeatureScheduledTasks); err != nil {
				return err
			}
		}
	}

	// Restore body for sub-handler
	c.Request().Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	switch req.Type {
	case "broadcast":
		return h.CreateBroadcast(c)
	case "scrape_members":
		if len(req.TargetIDs) == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "成员采集需要指定目标群组")
		}
		return h.CreateScrapeMembers(c)
	case "scrape_links":
		return h.CreateScrapeLinks(c)
	case "join_groups":
		return h.CreateJoinGroups(c)
	case "monitor_scrape":
		return h.CreateMonitorScrape(c)
	case "scheduled_broadcast":
		return h.CreateScheduledBroadcast(c)
	default:
		return echo.NewHTTPError(http.StatusBadRequest, "无效的任务类型: "+req.Type)
	}
}

// CreateScheduledBroadcast creates a looping scheduled broadcast task.
func (h *TaskHandler) CreateScheduledBroadcast(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	var req struct {
		AccountIDs          []string       `json:"account_ids"`
		TargetIDs           []string       `json:"target_ids"`
		Payload             map[string]any `json:"payload"`
		IsLoop              *bool          `json:"is_loop"`
		LoopIntervalMinutes int            `json:"loop_interval_minutes"`
		MinIntervalSeconds  int            `json:"min_interval_seconds"`
		MaxIntervalSeconds  int            `json:"max_interval_seconds"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}
	message, _ := req.Payload["message"].(string)
	if message == "" || len(req.TargetIDs) == 0 || len(req.AccountIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "定时广播需要指定账号、目标群组和消息内容")
	}
	accountIDs := make([]uuid.UUID, 0, len(req.AccountIDs))
	for _, id := range req.AccountIDs {
		uid, err := uuid.Parse(id)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "账号ID格式错误: "+id)
		}
		accountIDs = append(accountIDs, uid)
	}
	isLoop := true
	if req.IsLoop != nil {
		isLoop = *req.IsLoop
	}
	interval := req.LoopIntervalMinutes
	if interval <= 0 {
		interval = 60
	}
	task, err := h.svc.CreateScheduledBroadcast(c.Request().Context(), tenantID,
		accountIDs, req.TargetIDs, message, interval, req.MinIntervalSeconds, req.MaxIntervalSeconds, isLoop)
	if err != nil {
		log.Printf("[handler] 创建定时广播任务失败: %v", err)
		log.Printf("[handler] 内部错误已脱敏: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "操作失败，请稍后重试")
	}
	return Success(c, task)
}

// CreateBroadcast godoc
// @Summary 创建广播任务
// @Description 创建TG消息广播任务，支持笛卡尔积模式和精准映射模式
// @Tags 任务
// @Accept json
// @Produce json
// @Param body body CreateBroadcastRequest true "广播任务请求"
// @Success 201 {object} TaskResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks/broadcast [post]
// CreateBroadcast creates a broadcast task.
// Supports two modes:
//  1. Cartesian (legacy): all accounts send to all targets
//  2. Mapping (precise): each account sends only to its assigned targets
//
// Mapping mode is triggered when req.Mappings is non-empty.
func (h *TaskHandler) CreateBroadcast(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	var req CreateBroadcastRequest
	if err := c.Bind(&req); err != nil {
		log.Printf("[handler] 解析群发请求失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}

	// Parse scheduled_at (支持 ISO 格式和 datetime-local 格式)
	var scheduledAt *time.Time
	if req.ScheduledAt != "" {
		var t time.Time
		var err error
		// 先尝试 RFC3339/ISO 格式 (前端 toISOString)
		t, err = time.Parse(time.RFC3339, req.ScheduledAt)
		if err != nil {
			// 回退到 datetime-local 格式
			t, err = time.ParseInLocation("2006-01-02T15:04", req.ScheduledAt, time.Local)
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "计划时间格式错误，应为: YYYY-MM-DDTHH:MM")
			}
		}
		scheduledAt = &t
	}

	// Mapping mode: precise account-target control
	if len(req.Mappings) > 0 {
		mappings := make([]domain.AccountTargetMapping, len(req.Mappings))
		for i, m := range req.Mappings {
			accID, err := uuid.Parse(m.AccountID)
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "映射账号ID格式错误: "+m.AccountID)
			}
			mappings[i] = domain.AccountTargetMapping{
				AccountID: accID,
				Targets:   m.Targets,
			}
		}
		task, err := h.svc.CreateBroadcastWithMapping(c.Request().Context(), tenantID, mappings, nil, nil, req.Message, scheduledAt, req.IntervalMin, req.IntervalMax, broadcastExtras(&req))
		if err != nil {
			log.Printf("[handler] 创建精准映射群发任务失败: %v", err)
			return echo.NewHTTPError(http.StatusBadRequest, "创建群发任务失败")
		}
		return Created(c, task)
	}

	// Cartesian mode (legacy): all accounts × all targets
	var accountIDs []uuid.UUID
	for _, id := range req.AccountIDs {
		uid, err := uuid.Parse(id)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "账号ID格式错误: "+id)
		}
		accountIDs = append(accountIDs, uid)
	}

	task, err := h.svc.CreateBroadcast(c.Request().Context(), tenantID, accountIDs, req.TargetIDs, req.Message, scheduledAt, req.IntervalMin, req.IntervalMax, broadcastExtras(&req))
	if err != nil {
		log.Printf("[handler] 创建群发任务失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "创建群发任务失败")
	}
	return Created(c, task)
}

// CreateScrapeMembersRequest represents scrape members request.
type CreateScrapeMembersRequest struct {
	AccountID string         `json:"account_id" validate:"required"`
	TargetIDs []string       `json:"target_ids" validate:"required,min=1,max=1"`
	Payload   map[string]any `json:"payload,omitempty"` // 扩展参数(如采集过滤 filter)
}

// CreateScrapeLinksRequest represents scrape links request.
type CreateScrapeLinksRequest struct {
	AccountID       string   `json:"account_id" validate:"required"`
	AccountIDs      []string `json:"account_ids,omitempty"` // 兼容数组格式(与其它任务类型一致)
	TargetIDs       []string `json:"target_ids,omitempty"`
	DurationMinutes int      `json:"duration_minutes,omitempty"` // 采集时长(分钟), 0=默认10分钟
}

// CreateScrapeMembers godoc
// @Summary 创建抓取成员任务
// @Description 创建抓取群组成员的任务
// @Tags 任务
// @Accept json
// @Produce json
// @Param body body CreateScrapeMembersRequest true "抓取成员请求"
// @Success 201 {object} TaskResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks/scrape-members [post]
// CreateScrapeMembers creates a scrape members task.
func (h *TaskHandler) CreateScrapeMembers(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	var req CreateScrapeMembersRequest
	if err := c.Bind(&req); err != nil {
		log.Printf("[handler] 解析采集成员请求失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}

	accountID, err := uuid.Parse(req.AccountID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "账号ID格式错误")
	}

	var groupID uuid.UUID
	if len(req.TargetIDs) > 0 {
		groupID, err = uuid.Parse(req.TargetIDs[0])
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "群组ID格式错误")
		}
	}
	task, err := h.svc.CreateScrapeMembers(c.Request().Context(), tenantID, accountID, groupID, req.Payload)
	if err != nil {
		log.Printf("[handler] 创建采集成员任务失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "创建成员采集任务失败")
	}
	return Created(c, task)
}

// CreateScrapeLinks godoc
// @Summary 创建抓取链接任务
// @Description 创建抓取群组链接的任务
// @Tags 任务
// @Accept json
// @Produce json
// @Param body body CreateScrapeLinksRequest true "抓取链接请求"
// @Success 201 {object} TaskResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks/scrape-links [post]
// CreateScrapeLinks creates a scrape links task.
func (h *TaskHandler) CreateScrapeLinks(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	var req CreateScrapeLinksRequest
	if err := c.Bind(&req); err != nil {
		log.Printf("[handler] 解析采集链接请求失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}

	// 兼容 account_id 单数与 account_ids 数组两种格式
	accStr := req.AccountID
	if accStr == "" && len(req.AccountIDs) > 0 {
		accStr = req.AccountIDs[0]
	}
	log.Printf("[handler] scrape_links 收到: account_id=%q account_ids=%v 取accStr=%q", req.AccountID, req.AccountIDs, accStr)
	accountID, err := uuid.Parse(accStr)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "账号ID格式错误")
	}

	task, err := h.svc.CreateScrapeLinks(c.Request().Context(), tenantID, accountID, req.TargetIDs, req.DurationMinutes)
	if err != nil {
		log.Printf("[handler] 创建采集链接任务失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "创建链接采集任务失败")
	}
	return Created(c, task)
}

// CreateJoinGroupsRequest represents join groups request.
type CreateJoinGroupsRequest struct {
	AccountID string   `json:"account_id" validate:"required"`
	Links     []string `json:"links" validate:"required"`
}

// BatchTaskIDsRequest represents a batch operation on multiple tasks.
type BatchTaskIDsRequest struct {
	TaskIDs []string `json:"task_ids" validate:"required,min=1"`
}

// CreateJoinGroups godoc
// @Summary 创建加群任务
// @Description 创建加入Telegram群组的任务
// @Tags 任务
// @Accept json
// @Produce json
// @Param body body CreateJoinGroupsRequest true "加群请求"
// @Success 201 {object} TaskResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks/join-groups [post]
// CreateJoinGroups creates a join groups task.
func (h *TaskHandler) CreateJoinGroups(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	var req CreateJoinGroupsRequest
	if err := c.Bind(&req); err != nil {
		log.Printf("[handler] 解析加群请求失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}

	accountID, err := uuid.Parse(req.AccountID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "账号ID格式错误")
	}

	task, err := h.svc.CreateJoinGroups(c.Request().Context(), tenantID, accountID, req.Links)
	if err != nil {
		log.Printf("[handler] 创建加群任务失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "创建加群任务失败")
	}
	return Created(c, task)
}

// Pause godoc
// @Summary 暂停任务
// @Description 暂停指定任务
// @Tags 任务
// @Accept json
// @Produce json
// @Param id path string true "任务ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks/{id}/pause [post]
// Pause pauses a task.
func (h *TaskHandler) Pause(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "任务ID格式错误")
	}

	if err := h.svc.Pause(c.Request().Context(), tenantID, taskID); err != nil {
		log.Printf("[handler] 暂停任务失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "暂停任务失败")
	}
	return Success(c, map[string]string{"status": "paused"})
}

// Resume godoc
// @Summary 恢复任务
// @Description 恢复已暂停的任务
// @Tags 任务
// @Accept json
// @Produce json
// @Param id path string true "任务ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks/{id}/resume [post]
// Resume resumes a task.
func (h *TaskHandler) Resume(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "任务ID格式错误")
	}

	if err := h.svc.Resume(c.Request().Context(), tenantID, taskID); err != nil {
		log.Printf("[handler] 恢复任务失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "恢复任务失败")
	}
	return Success(c, map[string]string{"status": "resumed"})
}

// Cancel godoc
// @Summary 取消任务
// @Description 取消指定任务
// @Tags 任务
// @Accept json
// @Produce json
// @Param id path string true "任务ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks/{id}/cancel [post]
// Cancel cancels a task.
func (h *TaskHandler) Cancel(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "任务ID格式错误")
	}

	if err := h.svc.Cancel(c.Request().Context(), tenantID, taskID); err != nil {
		log.Printf("[handler] 取消任务失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "取消任务失败")
	}
	return Success(c, map[string]string{"status": "cancelled"})
}

// Retry godoc
// @Summary 重试任务
// @Description 重试失败的任务
// @Tags 任务
// @Accept json
// @Produce json
// @Param id path string true "任务ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks/{id}/retry [post]
// Retry retries a failed task.
func (h *TaskHandler) Retry(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "任务ID格式错误")
	}

	// scope=failed 时仅对失败目标断点续发(克隆新任务), 否则整任务重置重跑
	scope := c.QueryParam("scope")

	if err := h.svc.Retry(c.Request().Context(), tenantID, taskID, scope); err != nil {
		log.Printf("[handler] 重试任务失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "重试任务失败")
	}
	return Success(c, map[string]string{"status": "retrying"})
}

// GetLogs godoc
// @Summary 获取任务日志
// @Description 获取指定任务的执行日志
// @Tags 任务
// @Accept json
// @Produce json
// @Param id path string true "任务ID"
// @Success 200 {array} domain.TaskLog
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// ListScrapedLinks handles GET /scrape-links?account_id=xxx&since=RFC3339&page=1&page_size=50
// 返回链接采集结果。account_id 必填; since(任务创建时间)可选, 用于圈出单次任务窗口。
func (h *TaskHandler) ListScrapedLinks(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	accountIDStr := c.QueryParam("account_id")
	if accountIDStr == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "account_id is required")
	}
	accountID, err := uuid.Parse(accountIDStr)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account_id")
	}
	var since time.Time
	if sinceStr := c.QueryParam("since"); sinceStr != "" {
		since, err = time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid since (RFC3339)")
		}
	}
	page, _ := strconv.Atoi(c.QueryParam("page"))
	pageSize, _ := strconv.Atoi(c.QueryParam("page_size"))
	links, total, err := h.svc.ListScrapedLinks(c.Request().Context(), tenantID, accountID, since, page, pageSize)
	if err != nil {
		log.Printf("[handler] 内部错误已脱敏: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "操作失败，请稍后重试")
	}
	return c.JSON(http.StatusOK, map[string]any{"items": links, "total": total})
}

// ToggleLinkScraping handles POST /scrape-links/toggle  body: {account_id, enabled}
// 开启/关闭当前账号的常驻链接监听(开启=监听所有群组的邀请链接)。
func (h *TaskHandler) ToggleLinkScraping(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	var req struct {
		AccountID string `json:"account_id"`
		Enabled   bool   `json:"enabled"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
	}
	accountID, err := uuid.Parse(req.AccountID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account_id")
	}
	h.svc.SetLinkScraping(c.Request().Context(), tenantID, accountID, req.Enabled)
	return c.JSON(http.StatusOK, map[string]any{"enabled": req.Enabled})
}

// LinkScrapingStatus handles GET /scrape-links/status?account_id=xxx
func (h *TaskHandler) LinkScrapingStatus(c echo.Context) error {
	if _, err := middleware.GetTenantID(c); err != nil {
		return err
	}
	accountID, err := uuid.Parse(c.QueryParam("account_id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account_id")
	}
	return c.JSON(http.StatusOK, map[string]any{"enabled": h.svc.LinkScrapingEnabled(accountID)})
}

// ExportScrapedLinks handles GET /scrape-links/export?account_id=xxx&since=RFC3339
// 导出纯文本, 每行一个链接 —— 与前端"自动加群-导入链接"的格式一致, 可直接导入。
func (h *TaskHandler) ExportScrapedLinks(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	accountIDStr := c.QueryParam("account_id")
	if accountIDStr == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "account_id is required")
	}
	accountID, err := uuid.Parse(accountIDStr)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account_id")
	}
	var since time.Time
	if sinceStr := c.QueryParam("since"); sinceStr != "" {
		since, err = time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid since (RFC3339)")
		}
	}
	// 导出上限 5000 条, 足够单次任务使用
	links, _, err := h.svc.ListScrapedLinks(c.Request().Context(), tenantID, accountID, since, 1, 5000)
	if err != nil {
		log.Printf("[handler] 内部错误已脱敏: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "操作失败，请稍后重试")
	}
	// format=csv 时输出带表头的 CSV, 默认 txt(每行一个链接, 与导入器两种格式都兼容)
	if c.QueryParam("format") == "csv" {
		var b strings.Builder
		b.WriteString("link,title,member_count,source_chat\n")
		for _, link := range links {
			b.WriteString(fmt.Sprintf("%s,%s,%d,%s\n",
				csvField(link.FullURL), csvField(link.ResolvedTitle), link.MemberCount, csvField(link.SourceChatTitle)))
		}
		filename := fmt.Sprintf("scrape-links-%s.csv", time.Now().Format("20060102-150405"))
		c.Response().Header().Set(echo.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%s", filename))
		return c.Blob(http.StatusOK, "text/csv; charset=utf-8", []byte(b.String()))
	}
	var b strings.Builder
	for _, link := range links {
		b.WriteString(link.FullURL)
		b.WriteString("\n")
	}
	filename := fmt.Sprintf("scrape-links-%s.txt", time.Now().Format("20060102-150405"))
	c.Response().Header().Set(echo.HeaderContentType, "text/plain; charset=utf-8")
	c.Response().Header().Set(echo.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%s", filename))
	return c.String(http.StatusOK, b.String())
}

// csvField 包裹含逗号/引号的字段, 防止破坏 CSV 结构。
func csvField(s string) string {
	if strings.ContainsAny(s, ",\"\n") {
		return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
	}
	return s
}

// @Router /tasks/{id}/logs [get]
// DeleteTaskLogs handles DELETE /tasks/:id/logs 清空该任务的日志。
func (h *TaskHandler) DeleteTaskLogs(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid task id")
	}
	if err := h.svc.DeleteTaskLogs(c.Request().Context(), tenantID, taskID); err != nil {
		log.Printf("[handler] 内部错误已脱敏: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "操作失败，请稍后重试")
	}
	return c.JSON(http.StatusOK, map[string]any{"success": true})
}

// CleanupLogs handles DELETE /logs/cleanup?days=N 手动清理 N 天前日志。
// R116: CleanupLogs handler 已删(/logs/cleanup 路由删除后零调用)

// fillLogMessages 给无 message 的历史日志补可读文案。
func fillLogMessages(logs []*domain.TaskLog) {
	for _, l := range logs {
		if l.Message == "" {
			l.Message = task.FormatLogMessage(l)
		}
	}
}

// GetLogs returns task logs.
func (h *TaskHandler) GetLogs(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "任务ID格式错误")
	}

	logs, err := h.svc.GetLogs(c.Request().Context(), tenantID, taskID, 1, 50)
	if err != nil {
		log.Printf("[handler] 获取任务日志失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "服务器内部错误")
	}
	fillLogMessages(logs)
	return Success(c, logs)
}

// RaffleResults 抽奖参与记录: GET /raffle/results
func (h *TaskHandler) RaffleResults(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	list, err := h.svc.RaffleResultList(c.Request().Context(), tenantID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "查询失败")
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"list": list})
}

// RaffleResultsClear R128: 清空全部抽奖参与记录
func (h *TaskHandler) RaffleResultsClear(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	if err := h.svc.RaffleResultClear(c.Request().Context(), tenantID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "清空失败")
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"message": "已清空全部参与记录"})
}

// LeaveAllGroups godoc
// @Summary 一键退出账号加入的全部群组(频道/超级群)
// @Tags tasks
// @Produce json
// @Param id path string true "账号ID"
// @Success 200 {object} task.LeaveAllResult
// @Router /accounts/{id}/leave-all-groups [post]
func (h *TaskHandler) LeaveAllGroups(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "无效的账号ID")
	}
	res, err := h.svc.LeaveAllGroups(c.Request().Context(), tenantID, accountID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, res)
}

// GetStatsByAccount godoc
// @Summary 单任务按账号聚合成功/失败/跳过
// @Tags tasks
// @Produce json
// @Param id path string true "任务ID"
// @Success 200 {object} map[string]map[string]int
// @Router /tasks/{id}/stats-by-account [get]
func (h *TaskHandler) GetStatsByAccount(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "无效的任务ID")
	}
	stats, err := h.svc.TaskLogStatsByAccount(c.Request().Context(), tenantID, taskID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "统计失败")
	}
	return c.JSON(http.StatusOK, stats)
}

// GetAccountLogs godoc
// @Summary 获取账号日志
// @Description 获取指定账号的所有任务日志（独立日志页面用）
// @Tags 任务
// @Accept json
// @Produce json
// @Param account_id query string true "账号ID"
// @Param page query int false "页码" default(1)
// @Param page_size query int false "每页数量" default(50)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks/logs [get]
// GetAccountLogs returns all task logs for a specific account.
func (h *TaskHandler) GetAccountLogs(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	accountIDStr := c.QueryParam("account_id")
	if accountIDStr == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "需要指定账号ID")
	}
	accountID, err := uuid.Parse(accountIDStr)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "账号ID格式错误")
	}

	page := 1
	if p := c.QueryParam("page"); p != "" {
		if pi, err := strconv.Atoi(p); err == nil && pi > 0 {
			page = pi
		}
	}

	pageSize := 50
	if ps := c.QueryParam("page_size"); ps != "" {
		if psi, err := strconv.Atoi(ps); err == nil && psi > 0 && psi <= 200 {
			pageSize = psi
		}
	}

	logs, total, err := h.svc.GetLogsByAccount(c.Request().Context(), tenantID, accountID, page, pageSize)
	if err != nil {
		log.Printf("[handler] 获取账号日志失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "服务器内部错误")
	}

	return SuccessList(c, logs, total, page, pageSize)
}

// GetByID godoc
// @Summary 获取任务详情
// @Description 获取指定任务的详细信息
// @Tags 任务
// @Accept json
// @Produce json
// @Param id path string true "任务ID"
// @Success 200 {object} TaskResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks/{id} [get]
// GetByID returns a task by ID.
func (h *TaskHandler) GetByID(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "任务ID格式错误")
	}

	task, err := h.svc.GetByID(c.Request().Context(), tenantID, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "任务不存在")
		}
		log.Printf("[handler] 获取任务详情失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "服务器内部错误")
	}
	return Success(c, task)
}

// Update godoc
// @Summary 更新任务
// @Description 更新待执行或已暂停的任务
// @Tags 任务
// @Accept json
// @Produce json
// @Param id path string true "任务ID"
// @Param body body object true "更新内容"
// @Success 200 {object} TaskResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks/{id} [put]
// Update updates a task.
func (h *TaskHandler) Update(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "任务ID格式错误")
	}
	var req struct {
		AccountIDs []string       `json:"account_ids"`
		TargetIDs  []string       `json:"target_ids"`
		Payload    map[string]any `json:"payload"`
		Status     string         `json:"status"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}
	// 状态流转：暂停/取消/恢复
	if req.Status != "" {
		ctx := c.Request().Context()
		switch req.Status {
		case string(domain.TaskStatusPaused):
			err = h.svc.Pause(ctx, tenantID, taskID)
		case string(domain.TaskStatusCancelled):
			err = h.svc.Cancel(ctx, tenantID, taskID)
		case string(domain.TaskStatusRunning), string(domain.TaskStatusQueued):
			err = h.svc.Resume(ctx, tenantID, taskID)
		default:
			return echo.NewHTTPError(http.StatusBadRequest, "不支持的状态: "+req.Status)
		}
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return Success(c, map[string]string{"status": "updated"})
	}
	accIDs := make([]uuid.UUID, 0, len(req.AccountIDs))
	for _, id := range req.AccountIDs {
		uid, err := uuid.Parse(id)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "账号ID格式错误: "+id)
		}
		accIDs = append(accIDs, uid)
	}
	if err := h.svc.Update(c.Request().Context(), tenantID, taskID, accIDs, req.TargetIDs, req.Payload); err != nil {
		log.Printf("[handler] 更新任务失败: %v", err)
		log.Printf("[handler] 内部错误已脱敏: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "操作失败，请稍后重试")
	}
	return Success(c, map[string]string{"status": "updated"})
}

// Delete godoc
// @Summary 删除任务
// @Description 删除指定任务
// @Tags 任务
// @Accept json
// @Produce json
// @Param id path string true "任务ID"
// @Success 204
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks/{id} [delete]
// Delete removes a task.
func (h *TaskHandler) Delete(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "任务ID格式错误")
	}

	if err := h.svc.Delete(c.Request().Context(), tenantID, taskID); err != nil {
		log.Printf("[handler] 删除任务失败: %v", err)
		if strings.Contains(err.Error(), "无法删除该状态的任务") {
			return echo.NewHTTPError(http.StatusConflict, err.Error())
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "删除任务失败")
	}
	return NoContent(c)
}

// BatchCancel godoc
// @Summary 批量取消任务
// @Description 批量取消多个任务
// @Tags 任务
// @Accept json
// @Produce json
// @Param request body BatchTaskIDsRequest true "任务ID列表"
// @Success 200 {object} map[string]int
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks/batch/cancel [post]
func (h *TaskHandler) BatchCancel(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	var req BatchTaskIDsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	taskIDs := make([]uuid.UUID, 0, len(req.TaskIDs))
	for _, id := range req.TaskIDs {
		uid, err := uuid.Parse(id)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid task id: "+id)
		}
		taskIDs = append(taskIDs, uid)
	}
	cancelled, err := h.svc.BatchCancel(c.Request().Context(), tenantID, taskIDs)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "批量取消失败")
	}
	// F-31: 批量操作审计
	// 崩溃类修复: 脱协程必须用 safego —— 裸 go 里 panic 会崩整个进程
	// (Echo Recover 只罩请求 goroutine)。
	safego.Go(func() {
		if err := h.auditRepo.Create(context.Background(), &domain.AuditLog{
			TenantID: tenantID, UserID: "tenant", Action: "task_batch_cancel",
			Target: fmt.Sprintf("count=%d", cancelled), IP: c.RealIP(),
		}); err != nil {
			log.Printf("[audit] write failed: %v", err)
		}
	})
	return Success(c, map[string]int{"cancelled": cancelled})
}

// BatchDelete godoc
// @Summary 批量删除任务
// @Description 批量删除多个已完成/失败/已取消的任务
// @Tags 任务
// @Accept json
// @Produce json
// @Param request body BatchTaskIDsRequest true "任务ID列表"
// @Success 200 {object} map[string]int
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks/batch/delete [post]
func (h *TaskHandler) BatchDelete(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	var req BatchTaskIDsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	taskIDs := make([]uuid.UUID, 0, len(req.TaskIDs))
	for _, id := range req.TaskIDs {
		uid, err := uuid.Parse(id)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid task id: "+id)
		}
		taskIDs = append(taskIDs, uid)
	}
	deleted, err := h.svc.BatchDelete(c.Request().Context(), tenantID, taskIDs)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "批量删除失败")
	}
	// F-31: 批量操作审计
	// 崩溃类修复: 同 BatchCancel, 脱协程必须 safego。
	safego.Go(func() {
		if err := h.auditRepo.Create(context.Background(), &domain.AuditLog{
			TenantID: tenantID, UserID: "tenant", Action: "task_batch_delete",
			Target: fmt.Sprintf("count=%d", deleted), IP: c.RealIP(),
		}); err != nil {
			log.Printf("[audit] write failed: %v", err)
		}
	})
	return Success(c, map[string]int{"deleted": deleted})
}

// GetProgress godoc
// @Summary 获取任务进度
// @Description 获取指定任务的执行进度
// @Tags 任务
// @Accept json
// @Produce json
// @Param id path string true "任务ID"
// @Success 200 {object} domain.TaskProgress
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks/{id}/progress [get]
// GetProgress returns task progress.
func (h *TaskHandler) GetProgress(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "任务ID格式错误")
	}

	task, err := h.svc.GetByID(c.Request().Context(), tenantID, taskID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "任务不存在")
	}
	return Success(c, task.Progress)
}

// StreamProgress godoc
// @Summary 任务进度SSE流
// @Description 通过SSE实时推送任务进度
// @Tags 任务
// @Accept json
// @Produce text/event-stream
// @Param id path string true "任务ID"
// @Success 200 {object} domain.TaskProgress
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks/{id}/progress/stream [get]
func (h *TaskHandler) StreamProgress(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "任务ID格式错误")
	}

	c.Response().Header().Set(echo.HeaderContentType, "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().WriteHeader(http.StatusOK)

	// R98: 订阅本租户事件流 —— 执行器 publishLog 经 BroadcastToTenant 发到
	// "tenant:<租户>:events" topic(双层包装: {type:"events", payload:{...内层...}})。
	// 此处解包内层, 把本任务的 task_log / task_status 实时转发给详情页连接。
	var evCh chan sse.Event
	if h.sseBroker != nil {
		topic := fmt.Sprintf("tenant:%s:events", tenantID.String())
		subID, ch := h.sseBroker.Subscribe(topic)
		defer h.sseBroker.Unsubscribe(topic, subID)
		evCh = ch
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.Request().Context().Done():
			return nil
		case ev := <-evCh:
			// R98: 实时事件转发(仅本任务) —— 解包 BroadcastToTenant 的双层包装
			if outer, ok := ev.Payload.(map[string]interface{}); ok {
				m, _ := outer["payload"].(map[string]interface{})
				// task_id 是 uuid 对象(未经 JSON 序列化), 用 %v 统一成字符串比较
				tid := fmt.Sprintf("%v", m["task_id"])
				if tid == taskID.String() && (m["type"] == "task_log" || m["type"] == "task_status" || m["type"] == "task_wait") {
					if bts, err := json.Marshal(m); err == nil {
						fmt.Fprintf(c.Response(), "data: %s\n\n", bts)
						c.Response().Flush()
					}
				}
			}
		case <-ticker.C:
			// 优先读引擎内存进度缓存：N 个前端同时看进度时 DB QPS 归零
			// （此前每个连接每秒查一次 tasks 表整行）。未命中回退查库。
			if p, ok := h.engine.GetProgress(taskID); ok {
				data, _ := json.Marshal(p)
				fmt.Fprintf(c.Response(), "data: %s\n\n", data)
				c.Response().Flush()
				continue
			}
			task, err := h.svc.GetByID(c.Request().Context(), tenantID, taskID)
			if err != nil {
				continue
			}
			data, _ := json.Marshal(task.Progress)
			fmt.Fprintf(c.Response(), "data: %s\n\n", data)
			c.Response().Flush()
		}
	}
}

// CreateMonitorScrape godoc
// @Summary 创建消息监控采集任务
// @Description 通过监控群组消息匹配规则来采集成员
// @Tags 任务
// @Accept json
// @Produce json
// @Param body body object true "监控采集请求"
// @Success 201 {object} TaskResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /tasks/monitor-scrape [post]
func (h *TaskHandler) CreateMonitorScrape(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	var req struct {
		AccountIDs []string       `json:"account_ids"`
		TargetIDs  []string       `json:"target_ids"`
		Payload    map[string]any `json:"payload"`
	}
	if err := c.Bind(&req); err != nil {
		log.Printf("[handler] 解析监控采集请求失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}

	if len(req.AccountIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "需要指定执行账号")
	}
	if len(req.TargetIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "需要指定目标群组")
	}

	if len(req.AccountIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "account_ids is empty")
	}
	accountID, err := uuid.Parse(req.AccountIDs[0])
	if len(req.TargetIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "target_ids is empty")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "账号ID格式错误")
	}
	groupID, err := uuid.Parse(req.TargetIDs[0])
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "群组ID格式错误")
	}

	payload := req.Payload
	matchType, _ := payload["match_type"].(string)
	matchContent, _ := payload["match_content"].(string)
	durationMinutes, _ := payload["duration_minutes"].(float64)
	maxResults, _ := payload["max_results"].(float64)

	task, err := h.svc.CreateMonitorScrape(c.Request().Context(), tenantID, accountID, groupID, matchType, matchContent, int(durationMinutes), int(maxResults))
	if err != nil {
		log.Printf("[handler] 创建监控采集任务失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "创建监控采集任务失败")
	}
	return Created(c, task)
}
