package runtimes

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"os"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

const runtimeConnectionTestPrompt = "Reply with a short acknowledgement and do not use tools."

// connectionTestSink 只记录隔离 ACP Prompt 是否返回正文或尝试使用工具。
type connectionTestSink struct {
	// mutex 保护 ACP 事件交付 goroutine 写入的状态。
	mutex sync.Mutex
	// visibleOutput 表示 Agent 已返回至少一段用户可见正文。
	visibleOutput bool
	// toolUse 表示 Agent 在连接测试中尝试调用工具。
	toolUse bool
}

// Accept 记录连接测试所需的最小产品语义，不持久化事件。
func (sink *connectionTestSink) Accept(_ context.Context, event Event) error {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	switch event.Type {
	case EventTypeTextDelta:
		sink.visibleOutput = true
	case EventTypeToolUse, EventTypeToolInputSnapshot, EventTypeToolUpdate:
		sink.toolUse = true
	}
	return nil
}

// result 返回连接测试已经观察到的正文与工具状态。
func (sink *connectionTestSink) result() (bool, bool) {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	return sink.visibleOutput, sink.toolUse
}

// TestConnection 通过与对话相同的 ACP Agent 和 Prompt 路径执行隔离连接测试。
//
// 测试 Session 在返回前关闭，Session ID 和事件均不会交给业务持久化层。
func (sdk *SDK) TestConnection(ctx context.Context, request DetectRequest) error {
	if ctx == nil {
		return status_error.BadRequest
	}
	prepared, err := sdk.prepare(ctx, request)
	if err != nil {
		return err
	}
	workspace, err := os.MkdirTemp("", "ally-runtime-connection-")
	if err != nil {
		return status_error.RuntimeUnavailable
	}
	defer os.RemoveAll(workspace)

	sink := &connectionTestSink{}
	result := sdk.acp.Run(ctx, acpRunRequest{
		InvocationID: util.New(),
		Prepared:     prepared,
		Request: RunRequest{
			Target: RuntimeTarget{RuntimeID: request.RuntimeID},
			Workspace: Workspace{
				Path:    workspace,
				Managed: true,
			},
			Message: Message{Text: runtimeConnectionTestPrompt},
			Session: SessionDirective{Mode: SessionModeNew},
			Policy:  RunPolicy{Execution: defaultExecutionPolicy()},
		},
		Prompt: runtimeConnectionTestPrompt,
		Sink:   sink,
		// 连接测试不属于聊天，返回前必须释放隔离 Session。
		EphemeralSession: true,
	})
	if result.Failure != nil {
		return status_error.RuntimeUnavailable
	}
	visibleOutput, toolUse := sink.result()
	if toolUse {
		return status_error.RuntimeUnavailable
	}
	if !result.ProtocolHealthy || !visibleOutput {
		return status_error.RuntimeUnavailable
	}
	return nil
}
