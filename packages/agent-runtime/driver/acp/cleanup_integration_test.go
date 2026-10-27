package acp_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	driver "github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp"
	agentruntime "github.com/JieWaZi/NeuxBot/agent-runtime/runtime"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// credentialCleanupFixture 验证原生使用者未停止时凭据租约仍被Core保留。
type credentialCleanupFixture struct {
	closes atomic.Int32 // closes 统计实际凭据归还次数。
}

// Acquire 提供独立测试凭据租约。
func (p *credentialCleanupFixture) Acquire(context.Context, contract.CredentialRequest) (contract.CredentialLease, error) {
	return p, nil
}

// Credential 返回不含真实秘密的测试凭据。
func (p *credentialCleanupFixture) Credential() contract.Credential { return contract.Credential{} }

// Close 记录凭据实际归还。
func (p *credentialCleanupFixture) Close(context.Context) error { p.closes.Add(1); return nil }

// resourceCleanupFixture 验证原生关闭之前不会释放其依赖资源租约。
type resourceCleanupFixture struct {
	closes atomic.Int32 // closes 统计实际资源归还次数。
}

// Acquire 提供持有空资源列表的真实测试租约。
func (p *resourceCleanupFixture) Acquire(context.Context, contract.ResourceRequest) (contract.ResourceLease, error) {
	return p, nil
}

// Resources 首切片不启用额外能力资源。
func (p *resourceCleanupFixture) Resources() []contract.ResourceBinding { return nil }

// Close 记录资源实际归还。
func (p *resourceCleanupFixture) Close(context.Context) error { p.closes.Add(1); return nil }

// TestRuntimeKeepsACPDependenciesUntilNativeCloseSucceeds 验证真实Core与SDK组合保留失败清理的依赖和容量，重试确认后只释放一次。
func TestRuntimeKeepsACPDependenciesUntilNativeCloseSucceeds(t *testing.T) {
	for _, stage := range []string{"factory", "initialize", "completed"} {
		t.Run(stage, func(t *testing.T) {
			agent := newFixtureAgent()
			agent.closeNative = func(context.Context) error {
				if agent.closes.Load() == 1 {
					return errors.New("transient native cleanup failure")
				}
				return nil
			}
			if stage == "initialize" {
				agent.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
					return sdk.InitializeResponse{}, errors.New("initialize failed")
				}
			}
			d, err := driver.New(driver.Config{ID: "fixture", Factory: func(context.Context, contract.OpenRequest) (sdk.Agent, error) {
				if stage == "factory" {
					return agent, errors.New("factory partial failure")
				}
				return agent, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			credentials := &credentialCleanupFixture{}
			resources := &resourceCleanupFixture{}
			root := t.TempDir()
			runtime, err := agentruntime.New(agentruntime.Config{Drivers: []contract.Driver{d}, Credentials: credentials, Resources: resources, Grants: contract.Grants{Directories: []string{root}, AllowAllTools: true}, MaxOpenSessions: 1})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := runtime.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := runtime.Close(context.Background()); err != nil {
					t.Error(err)
				}
			}()
			spec := contract.ExecutionSpec{Profile: contract.Profile{DriverID: d.ID(), CredentialRef: "test-credential"}, Input: contract.Input{Text: "hello"}, Workspace: contract.WorkspaceScope{Root: root}, Policies: contract.Policies{NativeConfig: contract.NativeConfigInherit, Tools: contract.ToolPolicy{Mode: contract.ToolsAllowAll}}}
			result := runtime.Invoke(ctx, spec)
			if result.CleanupFault == nil || credentials.closes.Load() != 0 || resources.closes.Load() != 0 || agent.closes.Load() != 1 {
				t.Fatalf("native owner or dependencies lost: %#v native=%d credentials=%d resources=%d", result, agent.closes.Load(), credentials.closes.Load(), resources.closes.Load())
			}
			if capacity := runtime.Invoke(ctx, spec); capacity.Fault == nil || capacity.Fault.Code != contract.FaultCapacity {
				t.Fatalf("failed cleanup stopped occupying session capacity: %#v", capacity)
			}
			if err := runtime.CloseSession(ctx, result.Invocation.SessionID); err != nil {
				t.Fatal(err)
			}
			if credentials.closes.Load() != 1 || resources.closes.Load() != 1 || agent.closes.Load() != 2 {
				t.Fatalf("cleanup retry counts native=%d credentials=%d resources=%d", agent.closes.Load(), credentials.closes.Load(), resources.closes.Load())
			}
			if err := runtime.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if credentials.closes.Load() != 1 || resources.closes.Load() != 1 || agent.closes.Load() != 2 {
				t.Fatal("successful cleanup repeated")
			}
		})
	}
}
