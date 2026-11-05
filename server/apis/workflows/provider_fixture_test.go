package workflows

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
)

// workflowFixtureModel supplies a domain worker result without a native process.
type workflowFixtureModel interface {
	Run(context.Context, fixtureInvocation) (*string, error)
}

type fixtureInvocation struct {
	// businessDirectory 是实际派发意图冻结的业务目录。
	businessDirectory string
	// prompt 是实际派发的节点输入。
	prompt string
	// AccountID 是派发意图固定的账号。
	AccountID string
	// Model 是派发意图固定的模型。
	Model string
	// Reasoning 是派发意图固定的推理强度。
	Reasoning string
	// Permission 是节点实际派发的权限。
	Permission constants.PermissionLevel
	// callResult 调用本次领域任务签发的真实结果操作。
	callResult func(context.Context, string, map[string]any) (map[string]any, error)
}

func rejectUnexpectedWorkflowNative(t *testing.T) agent.ProviderRuntimeOptions {
	return agent.ProviderRuntimeOptions{
		AcquireAccount: func(context.Context, agent.AccountRequest) (agent.AccountLease, error) {
			t.Error("workflow test reached native resource acquisition")
			return agent.AccountLease{}, status_error.RuntimeUnavailable
		},
	}
}
