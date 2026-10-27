package runtimes

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimeauth"
	acpcodex "github.com/JieWaZi/acp-go/pkg/codex"
	"time"
)

// codexCredentialAgent 关闭原生进程后同步关闭其专属刷新入口。
type codexCredentialAgent struct {
	// Agent 嵌入完整原生适配器，保留连接注入、恢复、分叉与扩展等可选接口。
	*acpcodex.Agent
	// broker 持有该进程的回环监听资源。
	broker *runtimeauth.CodexRefreshBroker
	// sourceAuth 在系统账号原生进程退出后修复其延迟写入的旧凭据。
	sourceAuth string
}

func (a *codexCredentialAgent) Close(ctx context.Context) error {

	err := a.Agent.Close(ctx)
	if err != nil {
		return err
	}
	if a.sourceAuth != "" {
		writeContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		unlock, lockErr := runtimeauth.CodexFileCredentialGate(a.sourceAuth).Lock(writeContext)
		if lockErr != nil {
			return lockErr
		}
		defer unlock()
		if _, persistErr := runtimeauth.ReconcileCodexFileAuth(a.sourceAuth); persistErr != nil && err == nil {
			err = persistErr
		}
	}
	if err != nil {
		return err
	}
	return a.broker.Close(ctx)
}
