package runtimes

import (
	"context"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// acpManagementClient 只接收探测或分叉的协议回调，不拥有模型回合。
type acpManagementClient struct{}

// SessionUpdate 忽略目录读取期间的会话通知，配置以请求回执为准。
func (*acpManagementClient) SessionUpdate(context.Context, acp.SessionNotification) error { return nil }

// RequestPermission 管理操作不请求工具授权，意外请求按 ACP 取消。
func (*acpManagementClient) RequestPermission(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}, nil
}

// ReadTextFile 未声明对应能力，任何意外请求都返回 method-not-found。
func (*acpManagementClient) ReadTextFile(
	context.Context,
	acp.ReadTextFileRequest,
) (acp.ReadTextFileResponse, error) {
	return acp.ReadTextFileResponse{}, acp.NewMethodNotFound("fs/read_text_file")
}

// WriteTextFile 未声明对应能力，任何意外请求都返回 method-not-found。
func (*acpManagementClient) WriteTextFile(
	context.Context,
	acp.WriteTextFileRequest,
) (acp.WriteTextFileResponse, error) {
	return acp.WriteTextFileResponse{}, acp.NewMethodNotFound("fs/write_text_file")
}

// CreateTerminal 未声明 Terminal 能力，任何意外请求都返回 method-not-found。
func (*acpManagementClient) CreateTerminal(
	context.Context,
	acp.CreateTerminalRequest,
) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, acp.NewMethodNotFound("terminal/create")
}

// KillTerminal 未声明 Terminal 能力，任何意外请求都返回 method-not-found。
func (*acpManagementClient) KillTerminal(
	context.Context,
	acp.KillTerminalRequest,
) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, acp.NewMethodNotFound("terminal/kill")
}

// TerminalOutput 未声明 Terminal 能力，任何意外请求都返回 method-not-found。
func (*acpManagementClient) TerminalOutput(
	context.Context,
	acp.TerminalOutputRequest,
) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, acp.NewMethodNotFound("terminal/output")
}

// ReleaseTerminal 未声明 Terminal 能力，任何意外请求都返回 method-not-found。
func (*acpManagementClient) ReleaseTerminal(
	context.Context,
	acp.ReleaseTerminalRequest,
) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, acp.NewMethodNotFound("terminal/release")
}

// WaitForTerminalExit 未声明 Terminal 能力，任何意外请求都返回 method-not-found。
func (*acpManagementClient) WaitForTerminalExit(
	context.Context,
	acp.WaitForTerminalExitRequest,
) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, acp.NewMethodNotFound("terminal/wait_for_exit")
}

var _ acp.Client = (*acpManagementClient)(nil)
