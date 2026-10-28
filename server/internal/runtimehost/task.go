package runtimehost

import (
	"context"

	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
)

// TaskResources 将会话资源租约与调用期授权明确分开；所有方法须支持并发撤销。
type TaskResources interface {
	c.ResourceProvider
	// LaunchEnvironment 只在原生启动前合并本次任务环境；base 是独立副本。
	LaunchEnvironment(base map[string]string) map[string]string
	// Revoke 立即撤销调用授权且可重复执行，不释放仍被原生进程使用的文件。
	Revoke()
}

// TaskRequest 为独立工具任务提供冻结输入与显式资源，不接受聊天或恢复身份。
type TaskRequest struct {
	// TextRequest 保存模型、账号和本次完整消息。
	TextRequest
	// Contributions 保存必须交付给驱动的指令和技能描述。
	Contributions c.Contributions
	// Resources 负责物化任务文件，租约由引擎持有至原生退出。
	Resources TaskResources
}

// ExecuteTask 复用独立调用内核，以全工具授权、全新会话和严格最终回答策略执行。
func (h *Host) ExecuteTask(ctx context.Context, input TaskRequest) (TextResult, error) {
	return h.invoke(ctx, input.TextRequest, input.Contributions, input.Resources)
}

// taskDriver 仅适配任务资源的启动环境和授权终点，不执行或重试任务。
type taskDriver struct {
	// Driver 是共享原生协议驱动。
	c.Driver
	// resources 保留本次任务的独立授权撤销端口。
	resources TaskResources
}

func (d *taskDriver) Open(ctx context.Context, request c.OpenRequest) (c.DriverSession, error) {
	values := make(map[string]string, len(request.Credential.Environment))
	for key, value := range request.Credential.Environment {
		values[key] = value
	}
	request.Credential.Environment = d.resources.LaunchEnvironment(values)
	session, err := d.Driver.Open(ctx, request)
	if err != nil {
		d.resources.Revoke()
	}
	if session != nil {
		return &taskSession{DriverSession: session, resources: d.resources}, err
	}
	return nil, err
}

// taskSession 在 Prompt 返回时先撤销领域授权，再由引擎关闭原生会话与租约。
type taskSession struct {
	// DriverSession 是唯一原生会话所有者。
	c.DriverSession
	// resources 只撤销调用授权，不提前释放会话资源。
	resources TaskResources
}

func (s *taskSession) Prompt(ctx context.Context, request c.TurnRequest, sink c.DriverSink) c.DriverResult {
	defer s.resources.Revoke()
	return s.DriverSession.Prompt(ctx, request, sink)
}
