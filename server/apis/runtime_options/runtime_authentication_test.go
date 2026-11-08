package runtime_options

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/services/runtimeenvironment"
	"github.com/gin-gonic/gin"
)

// runtimeAuthenticationServiceBoundary 替代真实 CLI 和账号服务。
type runtimeAuthenticationServiceBoundary struct {
	// Service 使本用例只实现认证相关端口。
	runtimeenvironment.Service
	// runtimeID 记录操作的实际配置身份。
	runtimeID string
	// methodID 记录用户选择的原生方法。
	methodID string
	// cancelledID 记录精确取消身份。
	cancelledID string
}

func (s *runtimeAuthenticationServiceBoundary) AuthenticationSnapshot(_ context.Context, id string, _ bool) (runtimeenvironment.RuntimeAuthenticationSnapshot, error) {
	s.runtimeID = id
	return runtimeenvironment.RuntimeAuthenticationSnapshot{RuntimeID: id, Scope: "system", JobID: "new-job"}, nil
}
func (s *runtimeAuthenticationServiceBoundary) AuthenticateRuntime(_ context.Context, id, method string) (runtimeenvironment.RuntimeAuthenticationSnapshot, error) {
	s.runtimeID, s.methodID = id, method
	return runtimeenvironment.RuntimeAuthenticationSnapshot{RuntimeID: id, Scope: "system", JobID: "new-job"}, nil
}
func (s *runtimeAuthenticationServiceBoundary) CancelRuntimeAuthentication(_ context.Context, id, job string) error {
	s.runtimeID, s.cancelledID = id, job
	return nil
}

func TestRuntimeAuthenticationOperatorsPreserveScopeAndCancelIdentity(t *testing.T) {
	previous := global.RuntimeEnvironmentService
	t.Cleanup(func() { global.RuntimeEnvironmentService = previous })
	fake := &runtimeAuthenticationServiceBoundary{}
	global.RuntimeEnvironmentService = fake
	ctx, _ := gin.CreateTestContext(nil)
	for _, path := range []string{(&GetRuntimeAuthentication{}).Path(), (&AuthenticateRuntime{}).Path(), (&CancelRuntimeAuthentication{}).Path()} {
		if path != "/:runtimeId/authentication" {
			t.Fatalf("path=%s", path)
		}
	}
	result, err := (&GetRuntimeAuthentication{RuntimeID: "grok-local"}).Output(ctx)
	if err != nil || result.(runtimeenvironment.RuntimeAuthenticationSnapshot).Scope != "system" || fake.runtimeID != "grok-local" {
		t.Fatalf("get=%+v %v", result, err)
	}
	result, err = (&AuthenticateRuntime{RuntimeID: "grok-local", Body: AuthenticateRuntimeRequest{MethodID: "grok.com/oidc"}}).Output(ctx)
	if err != nil || result.(runtimeenvironment.RuntimeAuthenticationSnapshot).JobID != "new-job" || fake.methodID != "grok.com/oidc" {
		t.Fatalf("start=%+v %v", result, err)
	}
	if _, err := (&CancelRuntimeAuthentication{RuntimeID: "grok-local", JobID: "old-job"}).Output(ctx); err != nil || fake.cancelledID != "old-job" {
		t.Fatalf("cancel=%s %v", fake.cancelledID, err)
	}
}
