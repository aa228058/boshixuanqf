package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/storage"

	"tg-sender/internal/config"
	"tg-sender/internal/crypto"
	"tg-sender/internal/domain"
	"tg-sender/internal/domain/account"
	"tg-sender/internal/probe"
	"tg-sender/internal/repository"
	"tg-sender/internal/safego"
)

// GroupSyncer syncs joined Telegram groups for an account.
type GroupSyncer interface {
	SyncJoinedGroups(ctx context.Context, tenantID, accountID uuid.UUID) error
}

// AccountService handles account business logic.
type AccountService struct {
	cfg         *config.Config
	repo        *repository.AccountRepository
	probeRepo   *repository.ProbeConfigRepository // R119: 探测配置
	quota       *repository.QuotaRepository
	risk        *repository.RiskRepository
	engine      AccountEngine
	groupSyncer GroupSyncer
	tenantRepo  domain.TenantRepository
	taskLogRepo *repository.TaskLogRepository
}

// AccountEngine interface for account lifecycle operations.
type AccountEngine interface {
	ConnectAccount(ctx context.Context, tenantID, accountID uuid.UUID, phone string, sessionName string, level domain.AccountLevel, keepOnline ...bool) error
	DisconnectAccount(ctx context.Context, tenantID, accountID uuid.UUID) error
	GetAccountStatus(ctx context.Context, tenantID, accountID uuid.UUID) (domain.AccountStatus, error)
	Update2FA(ctx context.Context, tenantID, accountID uuid.UUID, current, next string) error
	// R176: 通知引擎重拉头像(内部读档/取现有连接, 绝不发起登录)
	KickAvatarRefresh(tenantID, accountID uuid.UUID)
	SubmitCode(ctx context.Context, tenantID, accountID uuid.UUID, code string) error
	SubmitPassword(ctx context.Context, tenantID, accountID uuid.UUID, password string) error
	GetAuthState(tenantID, accountID uuid.UUID) account.AuthState
	GetTelegramDialogs(ctx context.Context, tenantID, accountID uuid.UUID) ([]map[string]any, error)
	GetTelegramContacts(ctx context.Context, tenantID, accountID uuid.UUID) ([]map[string]any, error)
	// R181 群转移: 导出源账号已加入群组的邀请链接(公开群 t.me链接 / 私群管理员导出)
	ExportJoinedGroupLinks(ctx context.Context, tenantID, accountID uuid.UUID) (*account.TransferGroupsExport, error)
	ResolvePeer(ctx context.Context, tenantID, accountID uuid.UUID, identifier string) (map[string]any, error)
	// R119: 能力探测用客户端获取(实现方 account.Engine 本就有此两方法)
	GetClient(ctx context.Context, tenantID, accountID uuid.UUID) (*telegram.Client, error)
	ReleaseClient(tenantID, accountID uuid.UUID, client *telegram.Client)
	// 协议号导入与资料管理
	StoreImportedSession(ctx context.Context, sessionName string, serverAddr string, sess *storage.Session) error
	UpdateProfile(ctx context.Context, tenantID, accountID uuid.UUID, firstName, lastName, about, username string) error
	UploadAvatar(ctx context.Context, tenantID, accountID uuid.UUID, photo []byte) error
	MigrateDC(ctx context.Context, tenantID, accountID uuid.UUID, targetDC int32) error
	// 2026-10 新增: 头像/代理(检测/申诉已于2026-10-21随前端一并移除)
	AvatarFilePath(ctx context.Context, tenantID, accountID uuid.UUID) (string, error)
	TestProxy(ctx context.Context, tenantID, accountID uuid.UUID) (map[string]interface{}, error)
	// 2026-10-21 资料运营: 修改资料 + 批量克隆他人资料。
	// 注意: 这里是 AccountEngine(引擎抽象)接口——只放引擎方法;
	// UpdateRemark 是 repo 本地操作(引擎无此方法), 不放这里, 报错"missing method UpdateRemark"即此因
	CloneProfile(ctx context.Context, tenantID uuid.UUID, sourceUsername string, targetIDs []uuid.UUID) (ok, failed int, err error)
}

// NewAccountService creates a new account service.
func NewAccountService(cfg *config.Config, repo *repository.AccountRepository, probeRepo *repository.ProbeConfigRepository, quota *repository.QuotaRepository, risk *repository.RiskRepository, engine AccountEngine, tenantRepo domain.TenantRepository, taskLogRepo *repository.TaskLogRepository) *AccountService {
	return &AccountService{cfg: cfg, repo: repo, probeRepo: probeRepo, quota: quota, risk: risk, engine: engine, tenantRepo: tenantRepo, taskLogRepo: taskLogRepo}
}

// Probe R119: 账号能力探测 —— 冷却检查 → 执行 8 项矩阵 → 存结果。
// 冷却期内直接返回最近一次结果(前端据此显示倒计时)。
func (s *AccountService) Probe(ctx context.Context, tenantID, accountID uuid.UUID) (*probe.Result, error) {
	// 24h 冷却
	until, err := s.repo.ProbeCooldownUntil(ctx, tenantID, accountID)
	if err == nil && time.Now().Before(until) {
		raw, rerr := s.repo.GetProbeResult(ctx, tenantID, accountID)
		if rerr == nil && len(raw) > 2 {
			var cached probe.Result
			if json.Unmarshal(raw, &cached) == nil {
				return &cached, nil // 冷却中: 返回缓存结果
			}
		}
		return nil, fmt.Errorf("冷却中, 请于 %s 后再检测", until.Format("01-02 15:04"))
	}
	res, err := probe.Run(ctx, probe.Deps{Engine: s.engine, Configs: s.probeRepo}, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(res)
	if err := s.repo.SaveProbeResult(ctx, tenantID, accountID, raw); err != nil {
		log.Printf("[probe] 结果保存失败 account=%s: %v", accountID, err)
	}
	return res, nil
}

// GetProbeResult 读取最近一次探测结果(不改冷却)。
func (s *AccountService) GetProbeResult(ctx context.Context, tenantID, accountID uuid.UUID) (*probe.Result, error) {
	raw, err := s.repo.GetProbeResult(ctx, tenantID, accountID)
	if err != nil || len(raw) <= 2 {
		return nil, nil
	}
	var res probe.Result
	if json.Unmarshal(raw, &res) != nil {
		return nil, nil
	}
	return &res, nil
}

// List returns accounts for a tenant.
func (s *AccountService) List(ctx context.Context, tenantID uuid.UUID, source string) ([]*domain.Account, error) {
	list, err := s.repo.List(ctx, tenantID, source)
	if err != nil {
		return nil, err
	}
	// 填充 TG 资料（first_name/username）：accounts 表不存，从 sessions 表批量取
	names := make([]string, 0, len(list))
	for _, a := range list {
		names = append(names, a.SessionName)
	}
	if profs, err := s.repo.GetSessionProfiles(ctx, names); err == nil {
		for _, a := range list {
			if p, ok := profs[a.SessionName]; ok {
				a.FirstName, a.LastName, a.Username = p.FirstName, p.LastName, p.Username
			}
		}
	}
	return list, nil
}

// Create creates a new account atomically (count + create in transaction).
func (s *AccountService) Create(ctx context.Context, tenantID uuid.UUID, phone string, remark string, level domain.AccountLevel) (*domain.Account, error) {
	// 获取租户信息，检查套餐账号数量限制
	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	maxAccounts := tenant.MaxAccounts
	if maxAccounts <= 0 {
		maxAccounts = s.cfg.MaxAccounts
	}

	accountID := uuid.New()
	acc := &domain.Account{
		AccountID:     accountID,
		TenantID:      tenantID,
		Phone:         phone,
		Remark:        remark,
		SessionName:   fmt.Sprintf("acc_%s", accountID.String()),
		Status:        domain.AccountStatusPendingAuth,
		Source:        "phone",
		Level:         level,
		HealthScore:   100,
		DeviceProfile: map[string]any{},
	}

	_, _, err = s.repo.CountAndCreate(ctx, acc, maxAccounts)
	if err != nil {
		return nil, fmt.Errorf("create account: %w", err)
	}

	// Init quotas
	if err := s.quota.InitAccountQuotas(ctx, acc.AccountID, level); err != nil {
		return nil, fmt.Errorf("init quotas: %w", err)
	}

	// 批次2(⑧⑪): 配置了默认代理(env DEFAULT_PROXY)且未指定时自动挂上——
	// 新号开局就走代理线路, 不用事后逐个配
	if s.cfg.DefaultProxy != "" {
		if err := s.SetProxy(ctx, tenantID, acc.AccountID, s.cfg.DefaultProxy); err != nil {
			log.Printf("[account] 默认代理挂载失败 %s: %v(不影响账号创建)", phone, err)
		}
	}

	return acc, nil
}

// ImportResult 批量导入结果
type ImportResult struct {
	Created   int      `json:"created"`
	Failed    int      `json:"failed"`
	Errors    []string `json:"errors"`
	AccountIDs []uuid.UUID `json:"account_ids"`
}

// ImportSessions 批量导入协议号会话（Telethon/Pyrogram 等格式自动识别）。
// 每个号：解析 → 建账号记录 → 会话加密落库 → 后台排队连接（5s 间隔防 TG 登录限流）。
func (s *AccountService) ImportSessions(ctx context.Context, tenantID uuid.UUID, sessionStrings []string, remark string) (*ImportResult, error) {
	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	// 套餐门槛：协议号功能仅限名单内套餐（默认 专业版/企业版）
	if !s.canUseProtocol(tenant) {
		return nil, fmt.Errorf("协议号功能仅限专业版/企业版套餐使用，请升级套餐")
	}
	maxAccounts := tenant.MaxAccounts
	if maxAccounts <= 0 {
		maxAccounts = s.cfg.MaxAccounts
	}

	result := &ImportResult{Errors: []string{}}
	var connectQueue []uuid.UUID

	for i, raw := range sessionStrings {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		sess, serverAddr, err := account.DecodeImportedSession(line)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("第%d行: %v", i+1, err))
			continue
		}
		if sess.IsBot {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("第%d行: Bot 会话不支持导入", i+1))
			continue
		}

		accountID := uuid.New()
		acc := &domain.Account{
			AccountID:     accountID,
			TenantID:      tenantID,
			Source:        "protocol",
			Remark:        remark,
			SessionName:   fmt.Sprintf("acc_%s", accountID.String()),
			Status:        domain.AccountStatusOffline,
			Level:         domain.AccountLevelNormal,
			HealthScore:   100,
			DeviceProfile: map[string]any{},
		}
		if _, _, err := s.repo.CountAndCreate(ctx, acc, maxAccounts); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("第%d行: 创建失败(%v)", i+1, err))
			continue
		}
		if err := s.quota.InitAccountQuotas(ctx, acc.AccountID, acc.Level); err != nil {
			log.Printf("[account] init quotas %s: %v", accountID, err)
		}
		if err := s.engine.StoreImportedSession(ctx, acc.SessionName, serverAddr, sess); err != nil {
			_ = s.repo.Delete(ctx, tenantID, accountID)
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("第%d行: 会话落库失败(%v)", i+1, err))
			continue
		}
		result.Created++
		result.AccountIDs = append(result.AccountIDs, accountID)
		connectQueue = append(connectQueue, accountID)
	}

	// 后台串行连接验证，5s 间隔避免触发 TG 登录限流
	if len(connectQueue) > 0 {
		safego.Go(func() {
			for j, id := range connectQueue {
				if j > 0 {
					time.Sleep(5 * time.Second)
				}
				cctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				if err := s.engine.ConnectAccount(cctx, tenantID, id, "", fmt.Sprintf("acc_%s", id.String()), domain.AccountLevelNormal); err != nil {
					log.Printf("[account] 导入账号连接失败 %s: %v", id, err)
					cancel()
					continue
				}
				cancel()
				// 导入自动体检已随检测功能整体移除(2026-10-21)
			}
		})
	}
	return result, nil
}

// UpdateAccountProfile 修改账号资料：TG 侧（名字/签名/用户名）+ 本地备注 + 2FA 存档。
func (s *AccountService) UpdateAccountProfile(ctx context.Context, tenantID, accountID uuid.UUID, firstName, lastName, about, username, remark, twoFA string) error {
	if firstName != "" || about != "" || username != "" {
		if err := s.engine.UpdateProfile(ctx, tenantID, accountID, firstName, lastName, about, username); err != nil {
			return err
		}
	}
	// 2FA 存档(存 device_profile, 号掉线重登时自动过二步验证) + 备注一次落库
	if twoFA != "" || remark != "" {
		acc, err := s.repo.GetByID(ctx, tenantID, accountID)
		if err != nil {
			return fmt.Errorf("get account: %w", err)
		}
		if acc.DeviceProfile == nil {
			acc.DeviceProfile = map[string]any{}
		}
		if twoFA != "" {
			// R182: 2FA 密码不明文落 JSONB(device_profile)——审计报告#11。
			// 加密存 twofa_enc, 同时清掉历史明文遗留键。
			if enc, err := crypto.Encrypt([]byte(twoFA)); err == nil {
				acc.DeviceProfile["twofa_enc"] = enc
			} else {
				log.Printf("[account] 2FA 密码加密存储失败: %v", err)
			}
			delete(acc.DeviceProfile, "twofa")
		}
		// 空串=保持不变(2FA已不随API返回前端, 绝不能因空误清)
		if remark != "" {
			acc.Remark = remark
		}
		return s.repo.Update(ctx, acc)
	}
	return nil
}

// UploadAccountAvatar 上传账号头像
func (s *AccountService) UploadAccountAvatar(ctx context.Context, tenantID, accountID uuid.UUID, photo []byte) error {
	return s.engine.UploadAvatar(ctx, tenantID, accountID, photo)
}

// canUseProtocol 协议号功能套餐门槛：租户套餐 PlanID 在配置名单内（默认 pro=专业版, enterprise=企业版）。
func (s *AccountService) canUseProtocol(tenant *domain.Tenant) bool {
	if tenant == nil {
		return false
	}
	for _, id := range strings.Split(s.cfg.ProtocolPlanIDs, ",") {
		if strings.TrimSpace(id) == tenant.PlanID {
			return true
		}
	}
	return false
}

// MigrateAccountDC 把账号会话迁移到目标 DC（救活被机房路由封死的 DC）。
func (s *AccountService) MigrateAccountDC(ctx context.Context, tenantID, accountID uuid.UUID, targetDC int32) error {
	return s.engine.MigrateDC(ctx, tenantID, accountID, targetDC)
}

// Delete removes an account.
func (s *AccountService) Delete(ctx context.Context, tenantID, accountID uuid.UUID) error {
	if err := s.engine.DisconnectAccount(ctx, tenantID, accountID); err != nil {
		// Log but continue — account may not be connected
		log.Printf("[account] warn: disconnect account %s: %v", accountID, err)
	}
	return s.repo.Delete(ctx, tenantID, accountID)
}

// Connect connects an account to Telegram.
func (s *AccountService) Connect(ctx context.Context, tenantID, accountID uuid.UUID) error {
	acc, err := s.repo.GetByID(ctx, tenantID, accountID)
	if err != nil {
		return err
	}
	return s.engine.ConnectAccount(ctx, tenantID, accountID, acc.Phone, acc.SessionName, acc.Level)
}

// Disconnect disconnects an account.
func (s *AccountService) Disconnect(ctx context.Context, tenantID, accountID uuid.UUID) error {
	return s.engine.DisconnectAccount(ctx, tenantID, accountID)
}

// VerifyCode submits the phone verification code.
func (s *AccountService) VerifyCode(ctx context.Context, tenantID, accountID uuid.UUID, code string) error {
	return s.engine.SubmitCode(ctx, tenantID, accountID, code)
}

// Submit2FA submits the 2FA password.
func (s *AccountService) Submit2FA(ctx context.Context, tenantID, accountID uuid.UUID, password string) error {
	return s.engine.SubmitPassword(ctx, tenantID, accountID, password)
}

// GetAuthState returns the current authentication state for an account.
func (s *AccountService) GetAuthState(tenantID, accountID uuid.UUID) account.AuthState {
	return s.engine.GetAuthState(tenantID, accountID)
}

// GetHealthScore returns account health score.
// P2-7 审计清理: GetHealthScore 死代码已删——全项目无调用点(健康分由其他链路计算)。
// GetByID returns an account by ID after verifying ownership.
func (s *AccountService) GetByID(ctx context.Context, tenantID, accountID uuid.UUID) (*domain.Account, error) {
	return s.repo.GetByID(ctx, tenantID, accountID)
}

// Update updates account fields (level, health_score).
func (s *AccountService) Update(ctx context.Context, tenantID, accountID uuid.UUID, level *domain.AccountLevel, healthScore *int, remark *string) error {
	acc, err := s.repo.GetByID(ctx, tenantID, accountID)
	if err != nil {
		return err
	}
	if level != nil {
		acc.Level = *level
	}
	if healthScore != nil {
		acc.HealthScore = *healthScore
	}
	if remark != nil {
		acc.Remark = *remark
	}
	return s.repo.Update(ctx, acc)
}

// GetQuotas returns account quotas after verifying ownership.
func (s *AccountService) GetQuotas(ctx context.Context, tenantID, accountID uuid.UUID) ([]*domain.AccountQuota, error) {
	// Verify account belongs to tenant
	if _, err := s.repo.GetByID(ctx, tenantID, accountID); err != nil {
		return nil, fmt.Errorf("account not found or access denied")
	}
	return s.quota.GetAll(ctx, accountID)
}

// GetTelegramDialogs returns all Telegram dialogs for an account.
func (s *AccountService) GetTelegramDialogs(ctx context.Context, tenantID, accountID uuid.UUID) ([]map[string]any, error) {
	if _, err := s.repo.GetByID(ctx, tenantID, accountID); err != nil {
		return nil, fmt.Errorf("account not found or access denied")
	}
	return s.engine.GetTelegramDialogs(ctx, tenantID, accountID)
}

// GetTelegramContacts returns the account's Telegram contact list.
func (s *AccountService) GetTelegramContacts(ctx context.Context, tenantID, accountID uuid.UUID) ([]map[string]any, error) {
	if _, err := s.repo.GetByID(ctx, tenantID, accountID); err != nil {
		return nil, fmt.Errorf("account not found or access denied")
	}
	return s.engine.GetTelegramContacts(ctx, tenantID, accountID)
}

// ExportTransferableGroups R181 群转移第一步: 校验归属后导出源账号已加入群组的邀请链接。
// 返回的 Links 交给目标账号的标准 join_groups 任务执行(间隔/熔断/面板全复用)。
func (s *AccountService) ExportTransferableGroups(ctx context.Context, tenantID, accountID uuid.UUID) (*account.TransferGroupsExport, error) {
	acc, err := s.repo.GetByID(ctx, tenantID, accountID)
	if err != nil || acc == nil {
		return nil, fmt.Errorf("account not found or access denied")
	}
	// 离线/待登录的号 GetClient 会失败, 提前给可读错误
	if acc.Status != domain.AccountStatusOnline && acc.Status != domain.AccountStatusAuthenticated {
		return nil, fmt.Errorf("源账号当前不在线, 请先连接后再转移")
	}
	return s.engine.ExportJoinedGroupLinks(ctx, tenantID, accountID)
}

// ResolvePeer resolves a Telegram identifier.
func (s *AccountService) ResolvePeer(ctx context.Context, tenantID, accountID uuid.UUID, identifier string) (map[string]any, error) {
	if _, err := s.repo.GetByID(ctx, tenantID, accountID); err != nil {
		return nil, fmt.Errorf("account not found or access denied")
	}
	return s.engine.ResolvePeer(ctx, tenantID, accountID, identifier)
}

// phoneFromNameRe 匹配文件名主体中的手机号形态(+14687014965 / 8613800138000 / 1-468-701-4965)
var phoneFromNameRe = regexp.MustCompile(`^\+?(\d[\d\s\-]{5,16}\d)$`)

// extractPhoneFromFilename 从文件名解析 E.164 手机号(+14687014965.session → +14687014965)。
// entities 表优先(见 ParseTelethonSessionFile), 文件名是第二可靠来源——session 文件内容不一定有手机号。
func extractPhoneFromFilename(path string) string {
	base := filepath.Base(path)
	stem := strings.TrimSpace(strings.TrimSuffix(base, filepath.Ext(base)))
	m := phoneFromNameRe.FindStringSubmatch(stem)
	if m == nil {
		return ""
	}
	digits := regexp.MustCompile(`\D`).ReplaceAllString(m[1], "")
	if len(digits) < 7 || len(digits) > 15 {
		return ""
	}
	return "+" + digits
}

// readSidecarJSON 读取与 session 同 stem 的 .json 侧车文件(号商导出惯例: 2FA 密码/手机号)。
// handler 解压压缩包时按同 stem 写出(handler zip/rar 分支), 此处宽松解析常见键名。
func readSidecarJSON(sessionPath string) (twoFA, phone string) {
	data, err := os.ReadFile(strings.TrimSuffix(sessionPath, filepath.Ext(sessionPath)) + ".json")
	if err != nil {
		return "", ""
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return "", ""
	}
	for _, k := range []string{"password", "pass", "twofa", "2fa", "two_fa", "twofactor"} {
		if v, ok := m[k].(string); ok && v != "" {
			twoFA = v
			break
		}
	}
	for _, k := range []string{"phone", "phone_number", "number", "手机号"} {
		switch v := m[k].(type) {
		case string:
			phone = v
		case float64:
			phone = fmt.Sprintf("+%d", int64(v))
		}
		if phone != "" {
			break
		}
	}
	return
}

// ImportSessionFiles 批量导入 .session 文件（Telethon SQLite 格式）。
// paths 为已落盘的临时文件路径，解析走 ParseTelethonSessionFile。
func (s *AccountService) ImportSessionFiles(ctx context.Context, tenantID uuid.UUID, paths []string, remark string) (*ImportResult, error) {
	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	// 套餐门槛：协议号功能仅限名单内套餐（默认 专业版/企业版）
	if !s.canUseProtocol(tenant) {
		return nil, fmt.Errorf("协议号功能仅限专业版/企业版套餐使用，请升级套餐")
	}
	maxAccounts := tenant.MaxAccounts
	if maxAccounts <= 0 {
		maxAccounts = s.cfg.MaxAccounts
	}

	result := &ImportResult{Errors: []string{}}
	var connectQueue []uuid.UUID

	for i, p := range paths {
		sess, serverAddr, phone, err := account.ParseTelethonSessionFile(ctx, p)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("第%d个文件: %v", i+1, err))
			continue
		}
		if phone == "" {
			// R182: entities 表没存手机号时, 从文件名兜底(+14687014965.session / 8613xxx.session)。
			// 手机号用于连接路径判定, 不能丢。
			phone = extractPhoneFromFilename(p)
		}
		// R182: sidecar json(压缩包内同 stem .json, 号商惯例) 提供 2FA 密码与第三手机号来源
		twoFA, sidePhone := readSidecarJSON(p)
		if phone == "" && sidePhone != "" {
			digits := regexp.MustCompile(`\D`).ReplaceAllString(sidePhone, "")
			if len(digits) >= 7 && len(digits) <= 15 {
				phone = "+" + digits
			}
		}
		if sess.IsBot {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("第%d个文件: Bot 会话不支持导入", i+1))
			continue
		}

		accountID := uuid.New()
		acc := &domain.Account{
			AccountID:     accountID,
			TenantID:      tenantID,
			Source:        "protocol",
			Phone:         phone, // R182: entities.phone 或文件名解析, 此前为空导致导入后无手机号
			Remark:        remark,
			SessionName:   fmt.Sprintf("acc_%s", accountID.String()),
			Status:        domain.AccountStatusOffline,
			Level:         domain.AccountLevelNormal,
			HealthScore:   100,
			DeviceProfile: map[string]any{},
		}
		if twoFA != "" {
			// R182: sidecar 2FA 密码加密入库(与 UpdateAccountProfile 同规则, 不明文落 JSONB)
			if enc, err := crypto.Encrypt([]byte(twoFA)); err == nil {
				acc.DeviceProfile["twofa_enc"] = enc
			}
		}
		if _, _, err := s.repo.CountAndCreate(ctx, acc, maxAccounts); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("第%d个文件: 创建失败(%v)", i+1, err))
			continue
		}
		if err := s.quota.InitAccountQuotas(ctx, acc.AccountID, acc.Level); err != nil {
			log.Printf("[account] init quotas %s: %v", accountID, err)
		}
		if err := s.engine.StoreImportedSession(ctx, acc.SessionName, serverAddr, sess); err != nil {
			_ = s.repo.Delete(ctx, tenantID, accountID)
			// R182: 归还已扣额度(审计报告#17: 失败账号继续占额度, 新号导不进来)
			if s.quota != nil {
				_ = s.quota.DeleteByAccount(ctx, accountID)
			}
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("第%d个文件: 会话落库失败(%v)", i+1, err))
			continue
		}
		result.Created++
		result.AccountIDs = append(result.AccountIDs, accountID)
		connectQueue = append(connectQueue, accountID)
	}

	if len(connectQueue) > 0 {
		safego.Go(func() {
			for j, id := range connectQueue {
				if j > 0 {
					time.Sleep(5 * time.Second)
				}
				cctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				if err := s.engine.ConnectAccount(cctx, tenantID, id, "", fmt.Sprintf("acc_%s", id.String()), domain.AccountLevelNormal); err != nil {
					log.Printf("[account] 导入文件连接失败 %s: %v", id, err)
				}
				cancel()
			}
		})
	}
	return result, nil
}

// GetAvatarPath 返回账号头像文件的绝对路径(校验租户归属, 未设置返回 error)。
// RefreshAvatar 手动刷新头像: 立即触发异步补拉(已有头像则强制重拉覆盖)
func (s *AccountService) RefreshAvatar(ctx context.Context, tenantID, accountID uuid.UUID) error {
	acc, err := s.repo.GetByID(ctx, tenantID, accountID)
	if err != nil {
		return err
	}
	if acc.PoolStatus == "standby" {
		return fmt.Errorf("备用号请先转正再操作")
	}
	// R176: 只清路径并通知引擎重拉 —— 在线号立即重拉, 离线号(空闲退出, 会话已存档)下次上线自动重拉。
	// 原实现 return s.Connect(...) 会走完整重新登录(发验证码), 把号踢下线, 是本晚"点刷新头像号死了"的根。
	if err := s.repo.ClearAvatar(ctx, tenantID, accountID); err != nil {
		return fmt.Errorf("清空头像失败: %w", err)
	}
	if s.engine != nil {
		s.engine.KickAvatarRefresh(tenantID, accountID)
	}
	return nil
}

// StatsByAccount 每账号成功/失败/跳过聚合(账号行记分牌用)
func (s *AccountService) StatsByAccount(ctx context.Context, tenantID uuid.UUID) (map[string]map[string]int, error) {
	return s.taskLogRepo.StatsByAccount(ctx, tenantID)
}

// Update2FA 更新二次验证密码存档(v1.17 降级版):
// TG 官方客户端改完密码后来这里更新存档, 掉线重登自动输入(核心价值保留)。
// TG 侧直接修改需 SRP 握手, mtgo 库封装不可验证, 不做猜测性实现。
func (s *AccountService) Update2FA(ctx context.Context, tenantID, accountID uuid.UUID, current, next string) error {
	_ = current // 存档模式无需校验旧值

	acc, err := s.repo.GetByID(ctx, tenantID, accountID)
	if err != nil {
		return err
	}
	if acc.PoolStatus == "standby" {
		return fmt.Errorf("备用号请先转正再操作")
	}
	if err := s.engine.Update2FA(ctx, tenantID, accountID, current, next); err != nil {
		return err
	}
	return s.UpdateAccountProfile(ctx, tenantID, accountID, "", "", "", "", "", next)
}

// BatchUpdateProfile 批量改资料: 对选中账号逐个应用资料字段(位置参数, 与单号 UpdateAccountProfile 一致)
// random_username=true 时为每个号生成不同的随机用户名(v1.22)
func (s *AccountService) BatchUpdateProfile(ctx context.Context, tenantID uuid.UUID, accountIDs []uuid.UUID, firstName, lastName, about, username, remark string, randomUsername bool) (map[string]interface{}, error) {
	ok := 0
	var errs []string
	for i, id := range accountIDs {
		u := username
		if randomUsername && u == "" {
			u = randomUsernameGen(i) // 每个号不同用户名, 避免批量同用户名触发风控
		}
		if err := s.UpdateAccountProfile(ctx, tenantID, id, firstName, lastName, about, u, remark, ""); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", id.String()[:8], err))
		} else {
			ok++
		}
		// 改资料风控间隔: 2-4 秒抖动
		select {
		case <-ctx.Done():
			return map[string]interface{}{"success": ok, "failed": len(errs), "errors": errs}, ctx.Err()
		case <-time.After(time.Duration(2000+i%3*1000) * time.Millisecond):
		}
	}
	return map[string]interface{}{"success": ok, "failed": len(errs), "errors": errs}, nil
}

// randomUsernameGen 生成随机 TG 用户名: 字母+数字 8 位, 带序号种子避免批量重名
func randomUsernameGen(seed int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	r := seed*7919 + 104729 // 简单确定性散列
	b := make([]byte, 8)
	for i := range b {
		r = (r*9301 + 49297) % 233280
		b[i] = chars[r%len(chars)]
	}
	return string(b)
}

// BatchSetPool 批量切换启用/备用: 逐个执行, 汇总成功/失败(失败的带原因)
func (s *AccountService) BatchSetPool(ctx context.Context, tenantID uuid.UUID, accountIDs []uuid.UUID, target string) (map[string]interface{}, error) {
	ok := 0
	var errs []string
	for _, id := range accountIDs {
		if err := s.SetPoolStatus(ctx, tenantID, id, target); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", id.String()[:8], err))
		} else {
			ok++
		}
	}
	return map[string]interface{}{"success": ok, "failed": len(errs), "errors": errs}, nil
}

// SetPoolStatus 启用/备用 切换(056):
//   standby → active(转正): 校验套餐额度余量, 满额拒绝
//   active → standby(转备用): 停止该账号业务(断开连接), 释放额度
func (s *AccountService) SetPoolStatus(ctx context.Context, tenantID, accountID uuid.UUID, target string) error {
	if target != "active" && target != "standby" {
		return fmt.Errorf("无效的池状态: %s", target)
	}
	acc, err := s.repo.GetByID(ctx, tenantID, accountID)
	if err != nil {
		return err
	}
	if target == "active" && acc.PoolStatus != "active" {
		// 转正: 启用数+1, 校验是否超套餐上限
		t, err := s.tenantRepo.GetByID(ctx, tenantID)
		if err != nil {
			return err
		}
		n, err := s.repo.CountActive(ctx, tenantID)
		if err != nil {
			return err
		}
		if t.MaxAccounts > 0 && n >= t.MaxAccounts {
			return fmt.Errorf("套餐账号额度已满(%d/%d), 无法转正备用号; 可在管理端升级套餐或先转备用其他号", n, t.MaxAccounts)
		}
	}
	if err := s.repo.UpdatePoolStatus(ctx, tenantID, accountID, target); err != nil {
		return fmt.Errorf("切换池状态失败: %w", err)
	}
	if target == "standby" {
		// 转备用: 断连接停业务, 标记离线
		s.engine.DisconnectAccount(ctx, tenantID, accountID)
		_ = s.repo.UpdateStatus(ctx, tenantID, accountID, "offline")
	}
	return nil
}

// Reconnect 死号重连: 用已存 session 直接连回, 不需要验证码(账号保持正常号, 仍占额度)
func (s *AccountService) Reconnect(ctx context.Context, tenantID, accountID uuid.UUID) error {
	acc, err := s.repo.GetByID(ctx, tenantID, accountID)
	if err != nil {
		return err
	}
	if acc.PoolStatus == "standby" {
		return fmt.Errorf("备用号请先转正再连接")
	}
	// R177: 手动重连 = 读档优先 —— 传空手机号走"恢复存档会话"路径(直接上线, 不弹验证码/2FA)。
	// 原实现传 acc.Phone 走完整登录(发验证码), 把好好的在线号踢下线 —— 是本晚多次"号突然死了"的根。
	// 存档会话失效/被吊销时, 这里会报错, 前端引导"删除重登"(那才是验证码+2FA 的完整流程)。
	if err := s.engine.ConnectAccount(ctx, tenantID, accountID, "", acc.SessionName, acc.Level); err != nil {
		return fmt.Errorf("存档会话恢复失败: %v(会话可能已失效, 请删除账号重新登录)", err)
	}
	return nil
}

// SetRaffleEnabled 切换单账号抽奖参与开关(改进#1, 三层控制最内层)
func (s *AccountService) SetRaffleEnabled(ctx context.Context, tenantID, accountID uuid.UUID, enabled bool) error {
	if err := s.repo.UpdateRaffleEnabled(ctx, tenantID, accountID, enabled); err != nil {
		return fmt.Errorf("更新抽奖开关失败: %w", err)
	}
	return nil
}

func (s *AccountService) GetAvatarPath(ctx context.Context, tenantID, accountID uuid.UUID) (string, error) {
	return s.engine.AvatarFilePath(ctx, tenantID, accountID)
}

// SetProxy 设置/清除账号代理(空串=直连)。格式非法直接拒绝。
func (s *AccountService) SetProxy(ctx context.Context, tenantID, accountID uuid.UUID, proxyURL string) error {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL != "" {
		if _, err := account.ParseProxyURL(proxyURL); err != nil {
			return err
		}
	}
	if err := s.repo.UpdateProxyURL(ctx, tenantID, accountID, proxyURL); err != nil {
		return err
	}
	// 代理变更后旧连接仍走旧线路, 主动断开让下次使用时走新代理
	_ = s.engine.DisconnectAccount(ctx, tenantID, accountID)
	return nil
}

// TestProxy 代理连通性检测(经代理连 TG DC, 返回耗时)。
// UpdateRemark 更新备注(批量资料功能附带)
func (s *AccountService) UpdateRemark(ctx context.Context, tenantID, accountID uuid.UUID, remark string) error {
	return s.repo.UpdateRemark(ctx, tenantID, accountID, remark)
}

// UpdateProfile 修改账号 TG 资料(名字/姓/签名/用户名)——复用引擎既有实现(import.go)
func (s *AccountService) UpdateProfile(ctx context.Context, tenantID, accountID uuid.UUID, firstName, lastName, about, username string) error {
	return s.engine.UpdateProfile(ctx, tenantID, accountID, firstName, lastName, about, username)
}

// CloneProfile 批量克隆他人资料: sourceUsername=@对方用户名, 应用到 targetIDs 所有勾选号
func (s *AccountService) CloneProfile(ctx context.Context, tenantID uuid.UUID, sourceUsername string, targetIDs []uuid.UUID) (int, int, error) {
	return s.engine.CloneProfile(ctx, tenantID, sourceUsername, targetIDs)
}

func (s *AccountService) TestProxy(ctx context.Context, tenantID, accountID uuid.UUID) (map[string]interface{}, error) {
	return s.engine.TestProxy(ctx, tenantID, accountID)
}
