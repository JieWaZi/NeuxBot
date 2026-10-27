// Package runtime 提供可独立嵌入的 Agent 执行入口。
package runtime

import (
	"context"
	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	"github.com/JieWaZi/NeuxBot/agent-runtime/internal/engine"
)

// Config 配置注入端口、授权上界与保护预算。
type Config = engine.Config

// Runtime 为 Invoke 与 Run 提供同一技术执行所有者。
type Runtime struct {
	engine *engine.Engine // engine 唯一拥有调用、会话与租约。
}

// New 校验和冻结配置；不会探测、启动原生进程或 goroutine。
func New(config Config) (*Runtime, error) {
	e, err := engine.New(config)
	if err != nil {
		return nil, err
	}
	return &Runtime{engine: e}, nil
}

// Start 明确打开本实例的调用接纳。
func (r *Runtime) Start(ctx context.Context) error { return r.engine.Start(ctx) }

// NewInvocationID 预分配可取消的唯一身份，待用身份受活动预算限制。
func (r *Runtime) NewInvocationID() (string, error) { return r.engine.NewInvocationID() }

// Invoke 使用临时新会话执行一次无人值守调用并释放所有资源。
func (r *Runtime) Invoke(ctx context.Context, spec c.ExecutionSpec) c.Result {
	return r.engine.Execute(ctx, spec, c.RunOptions{}, true)
}

// Run 执行可观察的单次回合；具备已验证能力时保留热会话。
func (r *Runtime) Run(ctx context.Context, spec c.ExecutionSpec, options c.RunOptions) c.Result {
	return r.engine.Execute(ctx, spec, options, false)
}

// Cancel 消耗未使用身份或取消对应活动调用，不作用于后续回合。
func (r *Runtime) Cancel(id string) bool { return r.engine.Cancel(id) }

// CloseSession 取消活动回合并关闭对应热会话及租约。
func (r *Runtime) CloseSession(ctx context.Context, id string) error {
	return r.engine.CloseSession(ctx, id)
}

// Close 停止接纳并有界等待唯一释放过程；可安全重复调用。
func (r *Runtime) Close(ctx context.Context) error { return r.engine.Close(ctx) }
