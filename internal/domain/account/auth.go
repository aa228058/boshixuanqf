package account

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"tg-sender/internal/safego"
	"time"

	"github.com/mtgo-labs/mtgo/telegram"
	"tg-sender/internal/crypto"
	"tg-sender/internal/domain"

	"github.com/google/uuid"
)

func (e *Engine) ConnectAccount(ctx context.Context, tenantID, accountID uuid.UUID, phone string, sessionName string, level domain.AccountLevel, keepOnline ...bool) error {
	keep := len(keepOnline) > 0 && keepOnline[0]

	// R181: 用户主动发起重新登录 → 立即解除死密钥熔断。此前要等 30 分钟 TTL 到期才放行，
	// 用户重新登录成功后任务仍被熔断表挡住，表现为"登录好了但任务死活不跑"。
	e.clearDeadKey(accountID)

	// E修复: e.mu 是引擎全局锁。原来从 check-and-set 到 clients[key]=mc 全程持写锁,
	// 中间夹着多次 DB 写和 NewClient(秒级), 期间所有 e.mu.RLock 调用方
	// (GetClient 保活分支/GetAuthState/保活检查) 全部冻结。
	// 改为: 锁外做全部重活, 只在最后原子占位(二次检查防并发双开)。
	key := accountKey(tenantID, accountID)

	e.mu.RLock()
	_, exists := e.clients[key]
	e.mu.RUnlock()
	if exists {
		return fmt.Errorf("auth already in progress")
	}

	acc, err := e.repo.GetByID(ctx, tenantID, accountID)
	if err != nil {
		return fmt.Errorf("get account: %w", err)
	}

	// Use saved device profile, or generate new if empty
	deviceType := ""
	if acc.DeviceProfile != nil {
		if v, ok := acc.DeviceProfile["device_type"].(string); ok {
			deviceType = v
		} else if raw, exists := acc.DeviceProfile["device_type"]; exists && raw != nil {
			// 值存在但类型不对才是异常(历史脏数据)；键缺失/空对象是
			// 新账号的正常初始状态，断言失败日志会严重误导排查方向
			log.Printf("[auth] device_type 类型异常(应为 string) account=%s value=%T", accountID, raw)
		}
	}
	if deviceType == "" {
		deviceType = pickDeviceType(accountID)
	}

	// R182: 手机号只在"无存档需登录兜底"时使用。DB 有手机号 ≠ 要走登录流——
	// 协议号导入后合法 session 已存档, 读档直连即可; 发码登录仅在无存档时兜底。
	// (旧逻辑让协议号每次连接都被发码, 反复 sendCode 触发 TG 风控吊销存档 key)
	phoneNumber := acc.Phone
	if phoneNumber == "" {
		phoneNumber = phone
	}

	// 认证协程的独立可取消上下文：HTTP 请求的 ctx 会在接口返回后被 cancel，
	// 不能传给 runAuth；这里自建 ctx 并由 mc.cancel 控制，
	// 修复：此前用 `_, cancel :=` 把 ctx 丢弃，runAuth 拿着独立的
	// context.Background()，DisconnectAccount 调 mc.cancel() 根本停不掉认证协程
	authCtx, cancel := context.WithCancel(context.Background())
	mc := &managedClient{
		accountID:     accountID,
		tenantID:      tenantID,
		codeCh:        make(chan string, 1),
		passwordCh:    make(chan string, 1),
		cancel:        cancel,
		deviceProfile: acc.DeviceProfile,
		deviceType:    deviceType,
	}

	// Use deterministic session name based on accountID
	newSessionName := sessionName
	// R160: 登录流程用临时会话名 —— gotd 连接即把新(未授权)密钥写入存储,
	// 若与正式会话同名会覆盖号上原有合法密钥, 池下次重连即 401 AUTH_KEY_UNREGISTERED
	loginSessionName := fmt.Sprintf("%s_login_%d", newSessionName, time.Now().Unix())
	log.Printf("[AUTH] 登录会话(临时): %s (正式: %s)\n", loginSessionName, newSessionName)
	if newSessionName == "" {
		newSessionName = fmt.Sprintf("acc_%s", accountID.String())
	}
	// Save session name to DB immediately
	acc.SessionName = newSessionName
	dbCtx2, dbCancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	if err := e.repo.UpdateSessionName(dbCtx2, tenantID, accountID, newSessionName); err != nil {
		log.Printf("[AUTH] 更新账号 %s 的会话名称失败: %v\n", accountID, err)
	}
	dbCancel2()

	deviceCfg := deviceProfileToConfig(acc.DeviceProfile, newSessionName)
	if deviceCfg.DeviceModel == "" {
		deviceCfg = generateDeviceConfig(newSessionName, deviceType)
	}
	acc.DeviceProfile = deviceConfigToProfile(deviceCfg, deviceType)
	if err := e.repo.Update(ctx, acc); err != nil {
		log.Printf("[auth] 更新账号信息失败: %v", err)
	}
	mc.deviceProfile = acc.DeviceProfile

	// Use shared PostgreSQL storage for session persistence
	pgStorage := e.pgStorage

	// R182 读档优先判定(用正式名): 有存档 → 用正式名纯读档连接, PhoneNumber 置空(gotd 绝不发码)。
	// runAuth 后续 IsConnected 分支照常走, RenameSessionStorage 因 login 名下无记录自动跳过, 安全。
	effectiveSessionName := loginSessionName
	if newSessionName == "" {
		newSessionName = fmt.Sprintf("acc_%s", accountID.String())
	}
	if hasStored, serr := e.repo.SessionExists(ctx, newSessionName); serr == nil && hasStored {
		effectiveSessionName = newSessionName
		phoneNumber = ""
		log.Printf("[AUTH] 账号 %s 已有存档会话, 读档直连(不发码)\n", accountID)
	} else if phoneNumber != "" {
		// 无存档: 登录兜底, 此时才回写手机号到 DB
		acc.Phone = phoneNumber
		dbCtx0, dbCancel0 := context.WithTimeout(context.Background(), 5*time.Second)
		if err := e.repo.Update(dbCtx0, acc); err != nil {
			log.Printf("[AUTH] 更新账号 %s 的手机号失败: %v\n", accountID, err)
		}
		dbCancel0()
	}

	clientCfg := &telegram.Config{
		APIID:       int32(e.cfg.TGAPIID),
		APIHash:     e.cfg.TGAPIHash,
		SessionName: effectiveSessionName,
		PhoneNumber: phoneNumber,
		Device:      deviceCfg,
		Storage:     pgStorage,
		NoUpdates:   false,
		AutoConnect: false,
		// CodeFunc/PasswordFunc 属于本 literal 字段, 闭合与 applyProxy 见下方
		CodeFunc: func(codeCtx context.Context, phone string) (string, error) {
			mc.mu.Lock()
			// R158: gotd 重新回调有两种原因 —— ①传输抖动重试(应复用上次码); ②TG 明确拒绝才等新码
			if mc.codeSubmitted {
				if mc.lastCode != "" && mc.codeAutoResend < 2 {
					mc.codeAutoResend++
					mc.mu.Unlock()
					log.Printf("[AUTH] 账号 %s 传输重试, 自动复用上次的验证码(第 %d 次)\n", accountID, mc.codeAutoResend)
					return mc.lastCode, nil
				}
				mc.codeRetries++
				log.Printf("[AUTH] 账号 %s 验证码错误，第 %d 次重试\n", accountID, mc.codeRetries)
			}
			mc.authPhase = "waiting_code"
			mc.mu.Unlock()
			log.Printf("[AUTH] 等待账号 %s 的验证码...\n", accountID)
			select {
			case code := <-mc.codeCh:
				mc.mu.Lock()
				mc.codeSubmitted = true
				mc.lastCode = code
				mc.codeAutoResend = 0
				mc.mu.Unlock()
				log.Printf("[AUTH] 收到账号 %s 的验证码\n", accountID)
				return code, nil
			case <-codeCtx.Done():
				return "", codeCtx.Err()
			}
		},
		PasswordFunc: func(pwdCtx context.Context, hint string) (string, error) {
			mc.mu.Lock()
			if mc.pwdSubmitted {
				// gotd 重新回调 = 上次两步验证密码被 TG 拒绝
				mc.pwdRetries++
				log.Printf("[AUTH] 账号 %s 两步验证密码错误，第 %d 次重试\n", accountID, mc.pwdRetries)
			}
			mc.authPhase = "waiting_password"
			mc.passwordHint = hint
			mc.mu.Unlock()
			// 提示内容不打日志：2FA 提示是用户设置的找回线索，明文入日志属于敏感信息泄露。
			// 提示仍通过 mc.passwordHint 提供给前端展示，需要时去查库/前端，不在服务器日志留痕。
			log.Printf("[AUTH] 等待账号 %s 的两步验证密码（提示内容已脱敏，长度 %d 字符）...\n", accountID, len(hint))

			// 自动消费存档的 2FA: 首次回调且账号存了 twofa → 直接提交, 不打扰用户。
			// (重新回调 = 上次密码被 TG 拒绝, 说明存档的也不对, 转人工)
			mc.mu.Lock()
			retried := mc.pwdSubmitted
			mc.mu.Unlock()
			if !retried {
				if acc, err := e.repo.GetByID(pwdCtx, tenantID, accountID); err == nil && acc != nil && acc.DeviceProfile != nil {
					// R182: 优先读加密存档 twofa_enc(2FA 曾明文落 JSONB, 审计报告#11), 兼容历史明文 twofa
					twoFA := ""
					if enc, ok := acc.DeviceProfile["twofa_enc"].(string); ok && enc != "" {
						if dec, derr := crypto.Decrypt(enc); derr == nil {
							twoFA = string(dec)
						}
					}
					if twoFA == "" {
						if v, ok := acc.DeviceProfile["twofa"].(string); ok && v != "" {
							twoFA = v
						}
					}
					if twoFA != "" {
						log.Printf("[AUTH] 账号 %s 使用存档的 2FA 自动通过验证\n", accountID)
						mc.mu.Lock()
						mc.pwdSubmitted = true
						mc.mu.Unlock()
						return twoFA, nil
					}
				}
			}

			select {
			case pwd := <-mc.passwordCh:
				mc.mu.Lock()
				mc.pwdSubmitted = true
				mc.mu.Unlock()
				log.Printf("[AUTH] 收到账号 %s 的两步验证密码\n", accountID)
				return pwd, nil
			case <-pwdCtx.Done():
				return "", pwdCtx.Err()
			}
		},
	}

	client, err := telegram.NewClient(int32(e.cfg.TGAPIID), e.cfg.TGAPIHash, clientCfg)
	if err != nil {
		cancel()
		return fmt.Errorf("create client: %w", err)
	}
	mc.client = client

	// E修复: 锁外做完所有重活(DB/网络)后, 最后才原子占位。
	// 二次检查防并发双开: 两个 ConnectAccount 同时走到这里,
	// 后到者丢弃自己创建的客户端, 绝不覆盖先到的认证流程。
	e.mu.Lock()
	if _, exists := e.clients[key]; exists {
		e.mu.Unlock()
		cancel()
		client.Stop()
		return fmt.Errorf("auth already in progress")
	}
	e.clients[key] = mc
	e.mu.Unlock()

		mc.mu.Lock()
	mc.loginSessionName = loginSessionName
	mc.mu.Unlock()

safego.Go(func() { e.runAuth(authCtx, mc, key, newSessionName, keep) })
	return nil
}

func (e *Engine) runAuth(ctx context.Context, mc *managedClient, key, sessionName string, keep bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("认证协程异常: %v\n%s\n", r, debug.Stack())
		}
	}()

	accountID := mc.accountID
	tenantID := mc.tenantID

	// 连接/发码阶段标记：TG 真正进入等码阶段(CodeFunc 回调)时会被覆盖为
	// waiting_code。前端据此区分"发送中"与"已发送"，杜绝"假已发送"。
	mc.mu.Lock()
	mc.authPhase = "connecting"
	mc.mu.Unlock()

	// Use independent context for DB updates to avoid "context canceled"
	dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := e.updateStatus(dbCtx, tenantID, accountID, domain.AccountStatusPendingAuth); err != nil {
		log.Printf("[auth] 更新账号状态失败: %v", err)
	}
	dbCancel()

	// Start connection. This triggers CodeFunc/PasswordFunc if needed.
	err := mc.client.Connect(0)
	if err != nil && !isConnLoss(err) {
		// Non-auth errors (banned, revoked, etc.)
		e.handleConnectError(mc, key, err)
		return
	}

	// Start Idle so mtgo can complete auth in background
	idleDone := make(chan struct{})
	safego.Go(func() {
		defer close(idleDone)
		mc.client.Idle()
	})

	// Poll connection status until auth succeeds or timeout
	ticker := time.NewTicker(e.cfg.AuthPollInterval)
	defer ticker.Stop()
	timeout := time.NewTimer(e.cfg.AuthTimeout)
	defer timeout.Stop()

	for {
		select {
		case <-ctx.Done():
			mc.client.Stop()
			<-idleDone
			statusCtx, statusCancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := e.updateStatus(statusCtx, tenantID, accountID, domain.AccountStatusOffline); err != nil {
				log.Printf("[AUTH] 更新账号 %s 为离线状态失败: %v\n", accountID, err)
			}
			statusCancel()
			e.removeClient(key)
			return
		case <-idleDone:
			log.Printf("账号 %s 认证期间空闲退出\n", accountID)
			statusCtx2, statusCancel2 := context.WithTimeout(context.Background(), 5*time.Second)
			if err := e.updateStatus(statusCtx2, tenantID, accountID, domain.AccountStatusOffline); err != nil {
				log.Printf("[AUTH] 更新账号 %s 为离线状态失败: %v\n", accountID, err)
			}
			statusCancel2()
			e.removeClient(key)
			return
		case <-timeout.C:
			log.Printf("账号 %s 认证超时\n", accountID)
			mc.client.Stop()
			statusCtx3, statusCancel3 := context.WithTimeout(context.Background(), 5*time.Second)
			if err := e.updateStatus(statusCtx3, tenantID, accountID, domain.AccountStatusOffline); err != nil {
				log.Printf("[AUTH] 更新账号 %s 为离线状态失败: %v\n", accountID, err)
			}
			statusCancel3()
			e.removeClient(key)
			return
		case <-ticker.C:
			if mc.client.IsConnected() {
				mc.mu.Lock()
				mc.authPhase = "connected"
				mc.mu.Unlock()
				log.Printf("账号 %s 认证成功，正在保存会话\n", accountID)
				dbCtx2, dbCtx2Cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer dbCtx2Cancel()
				// R160-2: 登录成功立即接管(双保险1): 改名后立即清字段, 下方/别处的钩子会因此跳过, 保证只执行一次
				if mc.loginSessionName != "" {
					if err := e.repo.RenameSessionStorage(dbCtx2, mc.loginSessionName, sessionName); err != nil {
						log.Printf("[AUTH] 会话改名失败 account=%s: %v(本次登录不受影响, 但需重新登录修复)", accountID, err)
					} else {
						log.Printf("[AUTH] 会话已接管 account=%s (%s → %s)", accountID, mc.loginSessionName, sessionName)
					}
					mc.mu.Lock()
					// R182.2: 暂存登录会话名——统一空闲退出时据此二次搬运 gotd 最终 flush 的密钥
					// (rename 只搬中间态, 最终 key 在退出时才写完 login 行)
					mc.finalMoveSession = mc.loginSessionName
					mc.loginSessionName = ""
					mc.mu.Unlock()
				}
				// Use independent context with timeout
				dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
				if err := e.repo.UpdateSessionName(dbCtx, tenantID, accountID, sessionName); err != nil {
					log.Printf("[AUTH] 更新账号 %s 的会话名称失败: %v\n", accountID, err)
				// R160: 登录成功 —— 临时会话存储改名为正式名(先删旧正式记录), 完成接管
				if mc.loginSessionName != "" {
					if err := e.repo.RenameSessionStorage(dbCtx, mc.loginSessionName, sessionName); err != nil {
						log.Printf("[AUTH] 会话改名失败 account=%s: %v(不影响本次登录)", accountID, err)
					} else {
						log.Printf("[AUTH] 会话已接管 account=%s(%s → %s)", accountID, mc.loginSessionName, sessionName)
					}
				}
				}
				if err := e.updateStatus(dbCtx, tenantID, accountID, domain.AccountStatusOnline); err != nil {
					log.Printf("[AUTH] 更新账号 %s 为已认证状态失败: %v\n", accountID, err)
				}
				if mc.deviceProfile != nil {
					acc, _ := e.repo.GetByID(dbCtx, tenantID, accountID)
					if acc != nil {
						acc.DeviceProfile = mc.deviceProfile
						if err := e.repo.Update(dbCtx, acc); err != nil {
							log.Printf("[AUTH] 更新账号 %s 的设备信息失败: %v\n", accountID, err)
						}
					}
				}
				dbCancel()
				log.Printf("账号 %s 状态已设为已认证\n", accountID)
				// R182.1: 记录认证成功, markDeadKey 5 分钟内宽限(gotd flush 时序竞态防误熔断)
				e.noteAuthOK(accountID)
				// 标记认证成功: 前端轮询依赖该 phase 关闭弹窗/刷新列表
				// (不设的话 phase 永远停在 waiting_code, 前端只能等超时, 表现为"验证后毫无反应")
				mc.mu.Lock()
				mc.authPhase = "success"
				mc.mu.Unlock()
				// R179: 协议号不再走"管理客户端常驻保活" —— 管理客户端与连接池客户端共用同一把密钥
				// 同时在线, 会被 TG 判定异常并吊销密钥(AUTH_KEY_UNREGISTERED 连环 401, 10-30 实测)。
				// gotd 天条: 一把会话密钥同一时刻只能有一个客户端。统一改为空闲退出:
				//   - 有监控的号: 监控引擎 keepAlive 经连接池保活(池读档恢复, 全系统单客户端单密钥)
				//   - 没监控的号: 安心休眠, 用时读档秒上(会话已存档, R160 改名接管后官方记录唯一)
				accCtx, accCancel := context.WithTimeout(context.Background(), 5*time.Second)
				acc, aerr := e.repo.GetByID(accCtx, tenantID, accountID)
				accCancel()
				if aerr != nil {
					log.Printf("[AUTH] 账号 %s 判定账号来源失败(按空闲退出处理, 保活由监控/连接池负责): %v\n", accountID, aerr)
				} else if acc != nil {
					log.Printf("[AUTH] 账号 %s 来源=%s, 统一空闲退出(单客户端保密钥安全)\n", accountID, acc.Source)
				}

				// 头像: 正常号随后立即停机, 趁连接还在同步拉取一次(45s 封顶, 失败仅日志)
				avCtx, avCancel := context.WithTimeout(context.Background(), 45*time.Second)
				if err := e.downloadSelfAvatar(avCtx, tenantID, accountID, mc.client); err != nil {
					log.Printf("[avatar] 账号 %s 头像拉取失败(不影响使用): %v\n", accountID, err)
				}
				avCancel()

				// Save session by stopping client gracefully
				mc.client.Stop()
				// Wait for idle with timeout
				select {
				case <-idleDone:
					log.Printf("账号 %s 空闲退出，会话已保存\n", accountID)
				case <-time.After(e.cfg.AuthIdleTimeout):
					log.Printf("账号 %s 空闲退出超时，会话可能未完全保存\n", accountID)
				}

				// R182.2: gotd 的最终 flush 写的是 login_xxx 名(adapter 会话名未随 rename 变),
				// 认证时那次 rename 只搬了中间态。此处 flush 已完成, 必须把 login 行的最终内容
				// 再搬一次到正式名——否则池读档永远拿到中间态 key, -404 死循环("正常号登录后必死")。
				if mc.finalMoveSession != "" && mc.finalMoveSession != sessionName {
					mvCtx, mvCancel := context.WithTimeout(context.Background(), 5*time.Second)
					if err := e.repo.RenameSessionStorage(mvCtx, mc.finalMoveSession, sessionName); err != nil {
						log.Printf("[AUTH] 账号 %s 最终会话搬运失败: %v\n", accountID, err)
					} else {
						log.Printf("[AUTH] 账号 %s 最终会话已搬运到正式名(内容整体迁移)\n", accountID)
						mc.finalMoveSession = ""
					}
					mvCancel()
				}
				// C修复: 空闲退出同样要摘除注册 + 关闭提交通道,
				// 否则死条目常驻, 该账号永远无法再次认证。
				e.removeClient(key)
				mc.finish()
				return
			}
		}
	}
}

func (e *Engine) handleConnectError(mc *managedClient, key string, err error) {
	accountID := mc.accountID
	tenantID := mc.tenantID
	errStr := strings.ToLower(err.Error())

	dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dbCancel()

	switch {
	case strings.Contains(errStr, "user_deactivated"):
		if err := e.updateStatus(dbCtx, tenantID, accountID, domain.AccountStatusBanned); err != nil {
			log.Printf("[auth] 更新账号状态失败: %v", err)
		}
		e.recordRiskEvent(dbCtx, tenantID, accountID, "account_banned", "critical", err.Error())
	case strings.Contains(errStr, "session_revoked"):
		if err := e.updateStatus(dbCtx, tenantID, accountID, domain.AccountStatusPendingAuth); err != nil {
			log.Printf("[auth] 更新账号状态失败: %v", err)
		}
		e.recordRiskEvent(dbCtx, tenantID, accountID, "session_revoked", "high", err.Error())
		case strings.Contains(errStr, "auth key invalidated") || strings.Contains(errStr, "AUTH_KEY_UNREGISTERED"):
		// R157: 会话被 TG 吊销 —— 重试无意义, 标记"需重新登录"
		e.updateStatus(dbCtx, tenantID, accountID, domain.AccountStatusSessionRevoked)
		e.recordRiskEvent(dbCtx, tenantID, accountID, "auth_key_revoked", "high", errStr)

case strings.Contains(errStr, "flood_wait"):
		waitSec := extractFloodSeconds(errStr)
		if waitSec >= 60 {
			if err := e.updateStatus(dbCtx, tenantID, accountID, domain.AccountStatusRestricted); err != nil {
				log.Printf("[auth] 更新账号状态失败: %v", err)
			}
		} else {
			if err := e.updateStatus(dbCtx, tenantID, accountID, domain.AccountStatusOffline); err != nil {
				log.Printf("[auth] 更新账号状态失败: %v", err)
			}
		}
		e.recordRiskEvent(dbCtx, tenantID, accountID, "flood_wait", "medium", err.Error())
	case strings.Contains(errStr, "connection replaced"):
		// R182: 连接被替换(多端互踢)≠ 需要验证码。正确恢复 = 读档重连,
		// 标 offline 即可(前端显示"连接"按钮, 点一下走 R182 读档秒回)。
		// 旧版标 pending_auth 会让协议号出现无意义的验证码入口(审计报告#16)。
		log.Printf("账号 %s 连接被替换（其他设备登录），标记离线，点连接即读档恢复\n", accountID)
		if err := e.updateStatus(dbCtx, tenantID, accountID, domain.AccountStatusOffline); err != nil {
			log.Printf("[auth] 更新账号状态失败: %v", err)
		}
	default:
		log.Printf("账号 %s 连接错误: %v\n", accountID, err)
		if err := e.updateStatus(dbCtx, tenantID, accountID, domain.AccountStatusOffline); err != nil {
			log.Printf("[auth] 更新账号状态失败: %v", err)
		}
	}
	mc.client.Stop()
	e.removeClient(key)
}

func (e *Engine) SubmitCode(ctx context.Context, tenantID, accountID uuid.UUID, code string) error {
	e.mu.RLock()
	mc, ok := e.clients[accountKey(tenantID, accountID)]
	e.mu.RUnlock()
	if !ok || mc == nil {
		return fmt.Errorf("no active auth flow")
	}
	// F修复: 认证成功后 channel 已关闭但条目可能仍在 map 里(协议号保活),
	// select/default 防不住 send-on-closed-channel panic。持锁检查 finished。
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if mc.finished {
		return fmt.Errorf("auth flow already finished")
	}
	// 关键修复(用户报"验证码对的却提示核验失败"): 新验证码提交时必须重置
	// codeRetries——否则前端轮询会读到上一次失败遗留的重试计数, 把正在核验的
	// 正确验证码秒判为"验证码错误"。原实现只在 gotd 重回调时 +1, 从不归零。
	mc.codeRetries = 0
	select {
	case mc.codeCh <- code:
		return nil
	default:
		return fmt.Errorf("code channel full")
	}
}

func (e *Engine) SubmitPassword(ctx context.Context, tenantID, accountID uuid.UUID, password string) error {
	e.mu.RLock()
	mc, ok := e.clients[accountKey(tenantID, accountID)]
	e.mu.RUnlock()
	if !ok || mc == nil {
		return fmt.Errorf("no active auth flow")
	}
	// F修复: 同 SubmitCode。
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if mc.finished {
		return fmt.Errorf("auth flow already finished")
	}
	// 关键修复(用户报"2FA 输对后卡死在输入界面"): 新密码提交时重置 pwdRetries。
	// 原实现从不归零——上次失败遗留的计数会让前端轮询把正在核验的正确密码
	// 秒判为"密码错误"并终止轮询, 账号随后上线却无人感知, 页面永久卡死。
	// 注意字段名是 pwdRetries(与引擎结构体一致), 不是 passwordRetries。
	mc.pwdRetries = 0
	select {
	case mc.passwordCh <- password:
		return nil
	default:
		return fmt.Errorf("password channel full")
	}
}

type AuthState struct {
	Phase        string `json:"phase"`
	PasswordHint string `json:"hint,omitempty"`
	CodeRetries  int    `json:"code_retries"` // >0 = 验证码已被拒绝过 N 次, 前端提示重新输入
	PwdRetries   int    `json:"pwd_retries"`  // >0 = 两步验证密码已被拒绝过 N 次
}

// Update2FA 在 TG 侧修改二次验证密码(完整版, mtgo 库内置 SRP/KDF 处理):
// 已设密码 → ChangeCloudPassword(需当前密码); 未设密码 → EnableCloudPassword。
// 成功后由上层 service 走 UpdateAccountProfile 密码通道存档(掉线重登自动输入)。
func (e *Engine) Update2FA(ctx context.Context, tenantID, accountID uuid.UUID, current, next string) error {
	if len(next) < 2 {
		return fmt.Errorf("新密码至少 2 个字符")
	}
	client, err := e.GetClient(ctx, tenantID, accountID)
	if err != nil {
		return err
	}
	defer e.ReleaseClient(tenantID, accountID, client)

	err = client.ChangeCloudPassword(ctx, current, next, "")
	if errors.Is(err, telegram.ErrPasswordNotEnabled) {
		// 该号从未开启 2FA, 走首次启用路径(无需当前密码)
		return client.EnableCloudPassword(ctx, next, "")
	}
	return err
}

// GetAuthState 查询账号认证状态（验证码登录流程）。客户端不在常驻表时落内存查。
func (e *Engine) GetAuthState(tenantID, accountID uuid.UUID) AuthState {
	e.mu.RLock()
	mc, ok := e.clients[accountKey(tenantID, accountID)]
	if !ok || mc == nil {
		e.mu.RUnlock()
		return AuthState{Phase: "none"}
	}
	mc.mu.Lock()
	phase := mc.authPhase
	hint := mc.passwordHint
	codeRetries := mc.codeRetries
	pwdRetries := mc.pwdRetries
	mc.mu.Unlock()
	e.mu.RUnlock()
	return AuthState{
		Phase:        phase,
		PasswordHint: hint,
		CodeRetries:  codeRetries,
		PwdRetries:   pwdRetries,
	}
}

// keepClientAlive 保持在线模式的保活循环: 每 30 秒检查连接, 掉线自动重连。
// 失败不放弃: 死DC的号全靠保活抽到签才能自动迁移, 放弃=号永远卡死。
//
// B修复: mc.mu 是"任务独占锁"(GetClient 保活分支持锁返回)。原实现解锁后
// 裸用 client(IsConnected/GetMe/Connect), 与正在执行的任务并发使用同一
// mtgo 实例(非线程安全) → 数据竞争、状态损坏。
// 现改为 TryLock: 任务占用中跳过本轮(绝不阻塞等锁, 防①死锁链);
// 拿到锁后探活/重连全程在锁内。
func (e *Engine) keepClientAlive(mc *managedClient, tenantID, accountID uuid.UUID, sessionName string) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	failures := 0
	for range t.C {
		// 账号已被删除/断开 → 保活退出
		e.mu.RLock()
		_, stillManaged := e.clients[accountKey(tenantID, accountID)]
		e.mu.RUnlock()
		if !stillManaged {
			return
		}

		// 自动迁移判定放在抢锁之前: 它自己不碰 client, 但会触发 MigrateDC
		// 去 TryLock mc.mu —— 若带着 mc.mu 调用, 迁移必然" busy "失败进冷却,
		// 且冷却期后下一轮保活持锁再触发, 永远迁不动。
		e.maybeAutoMigrate(tenantID, accountID)

		if !mc.mu.TryLock() {
			continue // 任务正在使用 client, 本轮跳过
		}
		c := mc.client
		phase := mc.authPhase
		if c == nil || phase != "success" {
			mc.mu.Unlock()
			return
		}
		// 2026-09-22 修复：IsConnected 是状态标志会假活（TCP 已死标志未翻转），
		// 裸信它会让死连接永远走不到下方重连逻辑——探活成功才算真活。
		alive := c.IsConnected()
		if alive {
			probeCtx, pcancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, perr := c.GetMe(probeCtx)
			pcancel()
			// R181: 死密钥(AUTH_KEY_UNREGISTERED)=TG 已吊销此会话，重连多少次都没用，
			// 且带着在线会话重试会反复触发 401 RPC 风暴。标"需重新登录"后退场。
			if perr != nil && isAuthKeyDead(perr) {
				mc.mu.Unlock()
				e.markDeadKey(context.Background(), tenantID, accountID)
				log.Printf("[keepalive] 账号 %s 会话密钥已被吊销，退出保活，等用户重新登录\n", accountID)
				return
			}
			alive = perr == nil
		}
		if alive {
			if failures > 0 {
				log.Printf("[keepalive] 账号 %s 重连成功\n", accountID)
			}
			failures = 0
			// 在线即自动判定: DC 在迁移黑名单 → 后台自动迁移(全自动化, 无需手动点)
			e.maybeAutoMigrate(tenantID, accountID)
			mc.mu.Unlock()
			continue
		}
		failures++
		// 每100次失败打个汇总日志, 避免刷屏。
		if failures%100 == 0 {
			log.Printf("[keepalive] 账号 %s 已连续重连失败 %d 次(DC链路抽风, 继续重试直到连通)\n", accountID, failures)
		} else if failures <= 3 || failures%10 == 0 {
			log.Printf("[keepalive] 账号 %s 掉线(%d), 重连...\n", accountID, failures)
		}
		if err := c.Connect(45 * time.Second); err != nil {
			// R181: Connect 阶段命中死密钥同样熔断+退场(部分路径 Connect 内部就会探到 401)
			if isAuthKeyDead(err) {
				mc.mu.Unlock()
				e.markDeadKey(context.Background(), tenantID, accountID)
				log.Printf("[keepalive] 账号 %s 重连时发现会话密钥已吊销，退出保活，等用户重新登录\n", accountID)
				return
			}
			log.Printf("[keepalive] 账号 %s 重连失败: %v\n", accountID, err)
		}
		mc.mu.Unlock()
	}
}
