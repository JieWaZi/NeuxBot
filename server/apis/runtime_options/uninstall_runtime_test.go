package runtime_options

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/services/runtimeenvironment"
	"github.com/gin-gonic/gin"
)

// runtimeProgramServiceBoundary 隔离真实程序删除，并记录卸载所选的配置身份。
type runtimeProgramServiceBoundary struct {
	// Service 复用本用例未调用的服务端口。
	runtimeenvironment.Service
	// runtimeID 保存基础智能体或配置身份。
	runtimeID string
}

func (s *runtimeProgramServiceBoundary) UninstallRuntime(_ context.Context, id string) error {
	s.runtimeID = id
	return nil
}

func TestUninstallRuntimeOperatorPreservesProfileIdentity(t *testing.T) {
	previous := global.RuntimeEnvironmentService
	t.Cleanup(func() { global.RuntimeEnvironmentService = previous })
	fake := &runtimeProgramServiceBoundary{}
	global.RuntimeEnvironmentService = fake
	ctx, _ := gin.CreateTestContext(nil)
	operator := &UninstallRuntime{RuntimeID: "codex-profile"}
	if operator.Path() == (&CancelRuntimeInstallation{}).Path() {
		t.Fatal("program removal reused installation cancellation")
	}
	if _, err := operator.Output(ctx); err != nil || fake.runtimeID != "codex-profile" {
		t.Fatalf("uninstall lost the selected runtime scope: id=%s err=%v", fake.runtimeID, err)
	}
}
