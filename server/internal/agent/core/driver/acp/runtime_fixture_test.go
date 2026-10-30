package acp_test

import (
	"context"
	"errors"

	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/internal/plan"
	r "github.com/JieWaZi/NeuxBot/server/internal/agent/core/runtime"
)

// testCredentialLease 仅表示集成测试调用方取得的账号资源。
type testCredentialLease interface {
	Credential() c.Credential
	Close(context.Context) error
}

// testCredentialRequest 是集成测试本地的账号准备参数。
type testCredentialRequest struct{}

// testCredentialProvider 只在调用方模拟账号取得，不注入 Runtime。
type testCredentialProvider interface {
	Acquire(context.Context, testCredentialRequest) (testCredentialLease, error)
}

// runtimeFixtureConfig 组装调用方的实际资源，不扩展 Runtime 公共配置。
type runtimeFixtureConfig struct {
	Drivers     []c.Driver             // Drivers 提供本次交接的唯一驱动。
	Credentials testCredentialProvider // Credentials 模拟领域账号取得。
	Resources   c.ResourceProvider     // Resources 模拟领域资源取得。
	Grants      c.Grants               // Grants 是测试静态声明的授权上界。
}

// runtimeFixture 始终以真实 Prepare、Bind、Run 顺序驱动受测内核。
type runtimeFixture struct {
	*r.Runtime                      // Runtime 是实际受测的唯一会话所有者。
	config     runtimeFixtureConfig // config 是调用方冻结的资源取得依赖。
}

func newRuntimeFixture(config runtimeFixtureConfig) (*runtimeFixture, error) {
	runtime, err := r.New(r.Config{})
	if err != nil {
		return nil, err
	}
	return &runtimeFixture{Runtime: runtime, config: config}, nil
}

// runtimeFixtureLease 保存调用方已取得的资源，按依赖逆序释放并保留失败栈顶。
type runtimeFixtureLease struct {
	resources c.ResourceLease               // resources 提供实际测试资源视图。
	closers   []func(context.Context) error // closers 保存已取得的领域租约。
}

func (l *runtimeFixtureLease) Resources() []c.ResourceBinding {
	if l.resources != nil {
		return l.resources.Resources()
	}
	return nil
}
func (l *runtimeFixtureLease) Close(ctx context.Context) error {
	for len(l.closers) > 0 {
		last := len(l.closers) - 1
		if err := l.closers[last](ctx); err != nil {
			return err
		}
		l.closers = l.closers[:last]
	}
	return nil
}
func invokeRuntime(runtime *runtimeFixture, ctx context.Context, spec plan.Spec) c.Result {
	return submitRuntime(runtime, ctx, spec, c.RunOptions{}, c.SessionTemporary)
}
func runRuntime(runtime *runtimeFixture, ctx context.Context, spec plan.Spec, options c.RunOptions) c.Result {
	return submitRuntime(runtime, ctx, spec, options, c.SessionPersistent)
}
func submitRuntime(runtime *runtimeFixture, ctx context.Context, spec plan.Spec, options c.RunOptions, lifetime c.SessionLifetime) c.Result {
	declaration := c.SessionPlan{Profile: spec.Profile, Workspace: spec.Workspace, Contributions: spec.Contributions, Policies: c.SessionPolicies{Tools: spec.Policies.Tools, NativeConfig: spec.Policies.NativeConfig}, Grants: runtime.config.Grants, Tags: []string{"fixture"}}
	prepared, err := runtime.Prepare(ctx, c.PrepareRequest{Plan: declaration, Session: spec.Session, Lifetime: lifetime})
	failure := c.Result{Status: c.StatusFailed, StopReason: c.StopFailed, Output: c.Output{Status: c.OutputUnavailable}}
	if err != nil {
		errors.As(err, &failure.Fault)
		return failure
	}
	defer prepared.Close(context.Background())
	failure.Invocation = prepared.Invocation()
	if prepared.NeedsResources() {
		lease := &runtimeFixtureLease{}
		actual := c.SessionResources{Driver: runtime.config.Drivers[0], Resources: lease}
		var acquireErr error
		if runtime.config.Credentials != nil {
			var credentials testCredentialLease
			credentials, acquireErr = runtime.config.Credentials.Acquire(prepared.Context(), testCredentialRequest{})
			if credentials != nil {
				actual.Credential = credentials.Credential()
				lease.closers = append(lease.closers, credentials.Close)
			}
		}
		if acquireErr == nil && runtime.config.Resources != nil {
			lease.resources, acquireErr = runtime.config.Resources.Acquire(prepared.Context(), c.ResourceRequest{Invocation: prepared.Invocation(), Workspace: declaration.Workspace, Contributions: declaration.Contributions})
			if lease.resources != nil {
				lease.closers = append(lease.closers, lease.resources.Close)
			}
		}
		bindErr := prepared.Bind(context.Background(), actual)
		if acquireErr != nil {
			failure.Fault = &c.Fault{Code: c.FaultPreparation, Cause: acquireErr}
			return failure
		}
		if bindErr != nil {
			errors.As(bindErr, &failure.Fault)
			return failure
		}
	}
	return prepared.Run(ctx, c.TurnInput{Input: spec.Input, Seed: spec.Seed, Limits: spec.Limits, Interaction: spec.Policies.Interaction, Output: spec.Policies.Output, Options: options})
}
