package im

import (
	"context"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	protocol "github.com/JieWaZi/NeuxBot/server/internal/im"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/larksuite/oapi-sdk-go/v3/scene/registration"
)

// LoginProgress 是快照查询与全局事件使用的扫码状态，不含平台令牌及密钥。
type LoginProgress struct {
	// 登录标识
	ID string `json:"id"`
	// 连接标识
	ConnectionID string `json:"connectionId"`
	// 扫码内容
	QRContent string `json:"qrContent"`
	// 登录状态
	Status string `json:"status"`
	// 错误原因
	ErrorCode string `json:"errorCode"`
	// 到期时间
	ExpiresAt int64 `json:"expiresAt"`
}

// loginSession 持有认证流程私有数据，退出时取消全部平台轮询。
type loginSession struct {
	// progress 是可安全查询的状态副本。
	progress LoginProgress
	// cancel 终止本次认证及验证码等待。
	cancel context.CancelFunc
	// verification 是用户明确输入的一次性验证码。
	verification chan string
}

// BeginLogin 启动一份可取消的扫码流程，关闭弹窗不影响 Core 连接生命周期。
func (s *Service) BeginLogin(ctx context.Context, connectionID string) (LoginProgress, error) {
	s.life.Lock()
	defer s.life.Unlock()
	connection, err := s.config.Store.Connection(ctx, connectionID)
	if err != nil {
		return LoginProgress{}, err
	}
	if s.cancel == nil || s.ctx.Err() != nil {
		return LoginProgress{}, status_error.IMServiceUnavailable
	}
	for id, session := range s.logins {
		if session.progress.ExpiresAt < s.config.Now().Add(-10*time.Minute).UnixMilli() {
			session.cancel()
			delete(s.logins, id)
			continue
		}
		if session.progress.ConnectionID == connectionID {
			session.cancel()
			session.progress.Status = "cancelled"
			s.publishLoginProgress(session.progress)
		}
	}
	lifetime := 10 * time.Minute
	if connection.Platform == constants.IMPlatformWecom {
		lifetime = constants.IMWecomQRExpirySeconds * time.Second
	}
	loginContext, cancel := context.WithTimeout(s.ctx, lifetime)
	session := &loginSession{progress: LoginProgress{ID: util.New(), ConnectionID: connectionID, Status: "preparing", ExpiresAt: s.config.Now().Add(lifetime).UnixMilli()}, cancel: cancel, verification: make(chan string, 1)}
	s.logins[session.progress.ID] = session
	s.publishLoginProgress(session.progress)
	s.workers.Add(1)
	go func() { defer s.workers.Done(); defer cancel(); s.runLogin(loginContext, connection, session) }()
	return session.progress, nil
}

// Login 返回短期进度，服务器凭据永不经过 renderer。
func (s *Service) Login(id string) (LoginProgress, error) {
	s.life.Lock()
	defer s.life.Unlock()
	session := s.logins[id]
	if session == nil {
		return LoginProgress{}, status_error.IMResourceNotFound
	}
	return session.progress, nil
}

// CancelLogin 取消一份尚未完成的认证，不移除已保存的连接。
func (s *Service) CancelLogin(id string) error {
	s.life.Lock()
	defer s.life.Unlock()
	session := s.logins[id]
	if session == nil {
		return status_error.IMResourceNotFound
	}
	session.cancel()
	if session.progress.Status != "confirmed" && session.progress.Status != "cancelled" {
		session.progress.Status = "cancelled"
		s.publishLoginProgress(session.progress)
	}
	return nil
}

// VerifyLogin 将用户看到的一次性代码提交给当前微信扫码流程。
func (s *Service) VerifyLogin(id, code string) error {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 32 {
		return status_error.IMConfigurationInvalid
	}
	s.life.Lock()
	defer s.life.Unlock()
	session := s.logins[id]
	if session == nil {
		return status_error.IMResourceNotFound
	}
	if session.progress.Status != "need_verifycode" {
		return status_error.IMConfigurationConflict
	}
	select {
	case session.verification <- code:
		return nil
	default:
		return status_error.IMConfigurationConflict
	}
}

func (s *Service) loginProgress(session *loginSession, status, content, code string) {
	s.life.Lock()
	defer s.life.Unlock()
	if session.progress.Status == "cancelled" {
		return
	}
	previous := session.progress
	session.progress.Status = status
	session.progress.ErrorCode = code
	if content != "" {
		session.progress.QRContent = content
	}
	if session.progress != previous {
		s.publishLoginProgress(session.progress)
	}
}

// publishLoginProgress 只发布公开进度；调用方持有 life 锁，保证转换与事件顺序一致。
func (s *Service) publishLoginProgress(progress LoginProgress) {
	_ = events.Emit(s.config.Events, "im", "im.login.updated", events.Resource{Kind: "login", ID: progress.ID}, struct {
		// Login 是原 API 已允许页面读取的扫码快照，不含认证凭据。
		Login LoginProgress `json:"login"`
	}{Login: progress})
}

func (s *Service) runLogin(ctx context.Context, connection models.IMConnection, session *loginSession) {
	var err error
	if connection.Platform == constants.IMPlatformWeixin {
		err = s.loginWeixin(ctx, &connection, session)
	} else if connection.Platform == constants.IMPlatformWecom {
		err = s.loginWecom(ctx, &connection, session)
	} else if connection.Platform == constants.IMPlatformFeishu {
		err = s.loginFeishu(ctx, &connection, session)
	} else {
		err = status_error.IMConfigurationInvalid
	}
	if ctx.Err() != nil {
		s.loginProgress(session, "expired", "", "")
		return
	}
	if err != nil {
		s.loginProgress(session, "failed", "", errorCode(err))
		return
	}
	connection.Enabled = true
	err = s.config.Transactions.Transaction(ctx, func(ctx context.Context) error {
		return s.config.Store.SaveConnection(ctx, connection, connection.Revision)
	})
	if err != nil {
		s.loginProgress(session, "failed", "", errorCode(err))
		return
	}
	if err := s.config.Store.ConnectionState(ctx, connection.ID, constants.IMConnectionStateDisconnected, ""); err != nil {
		s.loginProgress(session, "failed", "", errorCode(err))
		return
	}
	s.loginProgress(session, "confirmed", "", "")
}

func (s *Service) loginWeixin(ctx context.Context, connection *models.IMConnection, session *loginSession) error {
	adapter, ok := s.config.Platforms[constants.IMPlatformWeixin].(*protocol.Weixin)
	if !ok {
		return status_error.IMServiceUnavailable
	}
	qr, err := adapter.QR(ctx)
	if err != nil {
		return err
	}
	s.loginProgress(session, "wait", qr.Content, "")
	base := constants.IMWeixinBaseURL
	verification := ""
	for ctx.Err() == nil {
		result, err := adapter.QRStatus(ctx, base, qr.Code, verification)
		verification = ""
		if err != nil {
			return err
		}
		switch result.Status {
		case "confirmed":
			if result.Token == "" || result.BotID == "" || result.OwnerID == "" || !protocol.OfficialURL(result.BaseURL, false) {
				return status_error.IMProtocolInvalid
			}
			if connection.AppID != "" && connection.AppID != result.BotID || connection.OwnerID != "" && connection.OwnerID != result.OwnerID {
				return status_error.IMConfigurationConflict
			}
			connection.Credential = result.Token
			connection.AppID = result.BotID
			connection.OwnerID = result.OwnerID
			connection.BaseURL = result.BaseURL
			return nil
		case "expired", "verify_code_blocked":
			return status_error.IMLoginRequired
		case "scaned_but_redirect":
			redirect := result.RedirectHost
			if !strings.HasPrefix(redirect, "https://") {
				redirect = "https://" + redirect
			}
			if !protocol.OfficialURL(redirect, false) {
				return status_error.IMProtocolInvalid
			}
			base = redirect
		case "binded_redirect":
			return status_error.IMLoginRequired
		case "need_verifycode":
			s.loginProgress(session, result.Status, "", "")
			select {
			case <-ctx.Done():
				return ctx.Err()
			case verification = <-session.verification:
			}
			continue
		case "wait", "scaned":
			s.loginProgress(session, result.Status, "", "")
		default:
			return status_error.IMProtocolInvalid
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return ctx.Err()
}

func (s *Service) loginFeishu(ctx context.Context, connection *models.IMConnection, session *loginSession) error {
	// 固定已有应用身份重扫；仓储保存时继续校验应用和所有者，禁止继承其他账号授权。
	minimal := false
	var preset *registration.AppPreset
	if connection.AppID == "" {
		preset = &registration.AppPreset{Name: "NeuxBot 助理", Desc: "通过飞书与 NeuxBot 伙伴和团队一起工作"}
	}
	result, err := registration.RegisterApp(ctx, &registration.Options{
		Source:    "ally",
		AppID:     connection.AppID,
		AppPreset: preset,
		Addons: &registration.AppAddons{
			Preset: &minimal,
			Scopes: registration.AppAddonsScopes{Tenant: []string{
				"im:message:send_as_bot", "im:message.p2p_msg:readonly",
				"im:message.group_at_msg:readonly", "im:resource",
			}},
			Events: registration.AppAddonsEvents{
				Items: registration.AppAddonsEventItems{Tenant: []string{"im.message.receive_v1"}},
			},
		},
		OnQRCode: func(info *registration.QRCodeInfo) {
			s.loginProgress(session, "wait", info.URL, "")
		},
		OnStatusChange: func(info *registration.StatusChangeInfo) {
			if info.Status == registration.StatusDomainSwitched {
				s.loginProgress(session, "domain_switched", "", "")
			}
		},
	})
	if err != nil {
		return status_error.IMLoginRequired
	}
	if result == nil || result.ClientID == "" || result.ClientSecret == "" || result.UserInfo == nil || result.UserInfo.OpenID == "" {
		return status_error.IMProtocolInvalid
	}
	// 首版使用国内飞书；国际 Lark 的账号和域名需要独立平台支持。
	if result.UserInfo.TenantBrand != "" && strings.EqualFold(result.UserInfo.TenantBrand, "lark") {
		return status_error.IMConfigurationInvalid
	}
	connection.AppID = result.ClientID
	connection.Credential = result.ClientSecret
	connection.OwnerID = result.UserInfo.OpenID
	return nil
}

// loginWecom 复用可取消扫码会话及统一事件，平台确认后只在 Core 内保存凭据。
func (s *Service) loginWecom(ctx context.Context, connection *models.IMConnection, session *loginSession) error {
	adapter, ok := s.config.Platforms[constants.IMPlatformWecom].(*protocol.Wecom)
	if !ok {
		return status_error.IMServiceUnavailable
	}
	qr, err := adapter.QR(ctx)
	if err != nil {
		return err
	}
	s.loginProgress(session, "wait", qr.Content, "")
	for {
		result, err := adapter.QRStatus(ctx, qr.Code)
		if err != nil {
			return err
		}
		switch result.Status {
		case "success":
			if result.Bot.ID == "" || result.Bot.Secret == "" {
				return status_error.IMProtocolInvalid
			}
			// 重扫不能将已有聊天授权转移给另一个机器人。
			if connection.AppID != "" && connection.AppID != result.Bot.ID {
				return status_error.IMConfigurationConflict
			}
			connection.AppID = result.Bot.ID
			connection.Credential = result.Bot.Secret
			return nil
		case "expired", "cancelled", "canceled", "failed":
			return status_error.IMLoginRequired
		}
		timer := time.NewTimer(constants.IMWecomQRPollSeconds * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return status_error.IMLoginRequired
		case <-timer.C:
		}
	}
}
