package acp_test

import (
	"context"
	"sync/atomic"

	sdk "github.com/JieWaZi/acp-go/sdk"
)

// fixtureAgent 通过公开 ACP 接口控制会话准备、交付、通知和退出时序。
type fixtureAgent struct {
	// connection 保存 acpserver 注入的真实 SDK Agent 端连接。
	connection atomic.Pointer[sdk.AgentSideConnection]
	// bound 在连接注入后解除 Initialize 的构造窗口。
	bound chan struct{}
	// initialize 可覆盖初始化结果以验证准备失败的清理。
	initialize func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error)
	// newSession 可检查原生会话实际收到的工作区与配置。
	newSession func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error)
	// prompt 定义当前测试的原生调用与通知行为。
	prompt func(context.Context, sdk.PromptRequest) (sdk.PromptResponse, error)
	// configure 可检查模型参数是否通过公开配置接口应用。
	configure func(context.Context, sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error)
	// closeNative 控制每次公开Close的实际执行与重试结果。
	closeNative func(context.Context) error
	// closeErr 控制原生资源释放失败。
	closeErr error
	// closes 记录原生 Adapter 实际资源释放次数。
	closes atomic.Int32
}

func newFixtureAgent() *fixtureAgent { return &fixtureAgent{bound: make(chan struct{})} }

// SetAgentConnection 接收真实acpserver注入的唯一SDK连接。
func (a *fixtureAgent) SetAgentConnection(connection *sdk.AgentSideConnection) {
	a.connection.Store(connection)
	close(a.bound)
}

// Initialize 完成公开ACP初始化并允许情景覆盖。
func (a *fixtureAgent) Initialize(ctx context.Context, request sdk.InitializeRequest) (sdk.InitializeResponse, error) {
	select {
	case <-a.bound:
	case <-ctx.Done():
		return sdk.InitializeResponse{}, ctx.Err()
	}
	if a.initialize != nil {
		return a.initialize(ctx, request)
	}
	return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber}, nil
}

// NewSession 创建公开ACP测试会话并允许情景覆盖。
func (a *fixtureAgent) NewSession(ctx context.Context, request sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
	if a.newSession != nil {
		return a.newSession(ctx, request)
	}
	return sdk.NewSessionResponse{SessionId: "native-session"}, nil
}

// Prompt 执行当前测试指定的原生调用情景。
func (a *fixtureAgent) Prompt(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
	if a.prompt != nil {
		return a.prompt(ctx, request)
	}
	return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
}

// SetSessionConfigOption 验证公开配置请求并返回实际生效目录。
func (a *fixtureAgent) SetSessionConfigOption(ctx context.Context, request sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
	if a.configure != nil {
		return a.configure(ctx, request)
	}
	return sdk.SetSessionConfigOptionResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionSetConfigOption)
}

// Close 记录实际资源释放并返回预设清理结果。
func (a *fixtureAgent) Close(ctx context.Context) error {
	a.closes.Add(1)
	if a.closeNative != nil {
		return a.closeNative(ctx)
	}
	return a.closeErr
}

// Cancel 由SDK维护请求取消，测试Agent不再持有第二套取消状态。
func (a *fixtureAgent) Cancel(context.Context, sdk.CancelNotification) error { return nil }

// Authenticate 拒绝测试未声明的认证操作。
func (a *fixtureAgent) Authenticate(context.Context, sdk.AuthenticateRequest) (sdk.AuthenticateResponse, error) {
	return sdk.AuthenticateResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodAuthenticate)
}

// Logout 拒绝测试未声明的退出登录操作。
func (a *fixtureAgent) Logout(context.Context, sdk.LogoutRequest) (sdk.LogoutResponse, error) {
	return sdk.LogoutResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodLogout)
}

// CloseSession 拒绝测试未声明的单会话关闭操作。
func (a *fixtureAgent) CloseSession(context.Context, sdk.CloseSessionRequest) (sdk.CloseSessionResponse, error) {
	return sdk.CloseSessionResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionClose)
}

// ListSessions 拒绝测试未声明的会话枚举操作。
func (a *fixtureAgent) ListSessions(context.Context, sdk.ListSessionsRequest) (sdk.ListSessionsResponse, error) {
	return sdk.ListSessionsResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionList)
}

// ResumeSession 拒绝测试未声明的恢复操作。
func (a *fixtureAgent) ResumeSession(context.Context, sdk.ResumeSessionRequest) (sdk.ResumeSessionResponse, error) {
	return sdk.ResumeSessionResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionResume)
}

// SetSessionMode 拒绝测试未声明的原生模式切换。
func (a *fixtureAgent) SetSessionMode(context.Context, sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error) {
	return sdk.SetSessionModeResponse{}, sdk.NewMethodNotFound(sdk.AgentMethodSessionSetMode)
}
