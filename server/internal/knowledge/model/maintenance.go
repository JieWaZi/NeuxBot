package model

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// MaintenanceRuntime 提供无需伙伴、聊天或执行记录的共享独立工具调用。
type MaintenanceRuntime interface {
	ExecuteTask(context.Context, agent.TaskRequest) (agent.TurnResult, error)
}

// MaintenanceConfig 显式装配知识维护的模型、能力授权与共享运行端口。
type MaintenanceConfig struct {
	// Runtime 复用共享宿主的原生调用、账号租约和清理所有权。
	Runtime MaintenanceRuntime
	// Registry 负责知识任务的短期操作授权。
	Registry *capability.Registry
	// Endpoint 是现有软件能力服务的回环地址。
	Endpoint string
	// Binary 是应用同包交付的真实 allycli 可执行文件。
	Binary string
	// Connection 是接纳时固定的程序、账号和模型。
	Connection agent.ProviderConnectionRequest
	// ResolveAccount 在本次工作目录内解析实际账号。
	ResolveAccount agent.DetachedAccountResolver
	// Reasoning 是接纳时固定的推理选项。
	Reasoning string
}

// MaintenanceRequest 使用知识作业身份限定现有 Wiki 修复能力，不关联聊天。
type MaintenanceRequest struct {
	// JobID 是本次独立知识任务身份。
	JobID string
	// Bindings 是入口已经冻结的单库操作集合。
	Bindings []capability.Binding
	// Instructions 保留上游 Wiki Fixer 工作流和当前库边界。
	Instructions string
	// Message 是本次明确的维护要求。
	Message string
	// Validate 每次工具调用重新检查任务与库授权。
	Validate func(context.Context) error
}

// MaintenanceRunner 保留领域授权和调用证据，执行及会话资源所有权交给共享宿主。
type MaintenanceRunner struct {
	// config 是固定模型与能力装配。
	config MaintenanceConfig
}

// NewMaintenanceRunner 构造后台调用适配器，不启动原生进程。
func NewMaintenanceRunner(config MaintenanceConfig) *MaintenanceRunner {
	return &MaintenanceRunner{config: config}
}

// Run 以明确指令、必需技能和单任务能力执行 Wiki Fixer，只接纳有领域证据的完整报告。
func (runner *MaintenanceRunner) Run(ctx context.Context, input MaintenanceRequest) (string, error) {
	if ctx == nil || strings.TrimSpace(input.JobID) == "" || input.Validate == nil || len(input.Bindings) == 0 || strings.TrimSpace(input.Instructions) == "" || strings.TrimSpace(input.Message) == "" {
		return "", status_error.KnowledgeInvalid
	}
	if ctx.Err() != nil {
		return "", status_error.OperationCanceled
	}
	if runner == nil || runner.config.Runtime == nil || runner.config.Registry == nil {
		return "", status_error.RuntimeUnavailable
	}
	connection := runner.config.Connection
	if !agent.SupportsDetachedPolicy(connection.Account.Provider) {
		return "", status_error.RuntimePolicyUnsupported
	}
	if err := input.Validate(ctx); err != nil {
		return "", err
	}
	binary, err := filepath.EvalSymlinks(runner.config.Binary)
	if err != nil || !filepath.IsAbs(binary) {
		return "", status_error.AgentConnectionUnavailable
	}
	if info, err := os.Stat(binary); err != nil || !info.Mode().IsRegular() {
		return "", status_error.AgentConnectionUnavailable
	}
	connection.Launch.Claude.Tools = []string{}
	connection.Launch.RequireVerifiedMCP = true
	// 验证运行中的请求时同时核对任务调用方的取消状态。
	runCtx, validate := ctx, input.Validate
	input.Validate = func(ctx context.Context) error {
		if err := runCtx.Err(); err != nil {
			return err
		}
		return validate(ctx)
	}
	const bridgeName = "ally_capabilities"
	const toolPrefix = "mcp__" + bridgeName + "__"
	instructions := input.Instructions + "\n\nUse the supplied ally_capabilities MCP tools for every knowledge read and change. In the guide, CLI command names refer to their equivalent advertised MCP tools. Use their current schemas and provide requestId for mutations. Recover uncertain outcomes with receipts_get and the original requestId. Follow the frozen library scope and verify actual operation results."
	var authorized atomic.Bool
	var successfulCalls atomic.Uint64
	// 只认服务端真正完成的领域调用，发现命令和模型自述不能证明执行过维护检查。
	bindings := append([]capability.Binding(nil), input.Bindings...)
	toolNames := map[string]bool{
		toolPrefix + "capabilities": true, toolPrefix + "guide": true, toolPrefix + "receipts_get": true,
	}
	for index := range bindings {
		binding := &bindings[index]
		if !strings.HasPrefix(binding.Operation, "knowledge.") || binding.Call == nil {
			return "", status_error.KnowledgeInvalid
		}
		toolNames[toolPrefix+strings.ReplaceAll(binding.Operation, ".", "_")] = true
		call := binding.Call
		binding.Call = func(ctx context.Context, request capability.Request) (capability.Result, error) {
			if !authorized.Load() {
				return capability.Result{}, status_error.AgentContextUnauthorized
			}
			result, err := call(ctx, request)
			if err == nil {
				successfulCalls.Add(1)
			}
			return result, err
		}
		if receipt := binding.Receipt; receipt != nil {
			binding.Receipt = func(ctx context.Context, requestID string) (capability.Result, error) {
				if !authorized.Load() {
					return capability.Result{}, status_error.AgentContextUnauthorized
				}
				return receipt(ctx, requestID)
			}
		}
	}
	var revoke func()
	result, err := runner.config.Runtime.ExecuteTask(ctx, agent.TaskRequest{
		Thread: agent.ThreadRequest{Connection: connection, Mode: "default",
			Session:   agent.AgentSessionOptions{DeveloperInstructions: &instructions},
			Selection: &agent.TurnSelection{Reasoning: runner.config.Reasoning}},
		ResolveAccount: runner.config.ResolveAccount,
		Input:          agent.TurnRequest{Text: input.Message, UsageSource: "knowledge_maintenance"},
		Ports: agent.TurnPorts{
			AcquireResources: func(ctx context.Context, _ agent.ResourceSelection) (agent.ResourceLease, error) {
				lease, revokeGrant, err := runner.acquireBridge(ctx, input, bindings, binary, bridgeName)
				revoke = revokeGrant
				return lease, err
			},
			Authorize: func(ctx context.Context, _ agent.TurnAdmission, _ agent.CommitReceipt) (func(), error) {
				if revoke == nil {
					return nil, status_error.AgentConnectionUnavailable
				}
				if err := input.Validate(ctx); err != nil {
					return nil, err
				}
				authorized.Store(true)
				return func() {
					authorized.Store(false)
					revoke()
				}, nil
			},
			Approve: func(ctx context.Context, _ agent.AgentScope, request agent.AgentApproval) (string, error) {
				if !authorized.Load() || !toolNames[request.NativeToolName] {
					return "", nil
				}
				if err := input.Validate(ctx); err != nil {
					return "", err
				}
				for _, option := range request.Options {
					if option.Kind == "allow_once" {
						return option.ID, nil
					}
				}
				return "", nil
			},
		},
	})
	if err != nil {
		if result.BusinessAdmitted {
			return "", status_error.WithoutRetry(err)
		}
		return "", err
	}
	if ctx.Err() != nil {
		return "", status_error.WithoutRetry(status_error.OperationCanceled)
	}
	outcome := result.Outcome
	if outcome == nil || (outcome.StopReason != "end_turn" && outcome.StopReason != "stop") || outcome.FinalMessage == nil ||
		strings.TrimSpace(outcome.FinalMessage.Text) == "" {
		return "", status_error.WithoutRetry(status_error.RuntimeExecutionFailed.WithParams(map[string]interface{}{"Detail": "knowledge maintenance did not produce a complete report"}))
	}
	if successfulCalls.Load() == 0 {
		return "", status_error.WithoutRetry(status_error.RuntimeExecutionFailed.WithParams(map[string]interface{}{"Detail": "knowledge maintenance completed no authorized knowledge operation; result is unverified"}))
	}
	return outcome.FinalMessage.Text, nil
}

// acquireBridge 启用原生 MCP 工具发现，实际领域调用仍须等待本轮授权。
func (runner *MaintenanceRunner) acquireBridge(ctx context.Context, input MaintenanceRequest, bindings []capability.Binding, binary, name string) (agent.ResourceLease, func(), error) {
	if err := ctx.Err(); err != nil {
		return agent.ResourceLease{}, nil, err
	}
	directory, err := os.MkdirTemp("", "neux-maintenance-")
	if err != nil {
		return agent.ResourceLease{}, nil, status_error.KnowledgeStorageError
	}
	lease := agent.ResourceLease{CloseFiles: func(context.Context) error {
		if err := os.RemoveAll(directory); err != nil {
			logx.Errorf("remove knowledge maintenance resources failed: error=%v", err)
			return status_error.KnowledgeStorageError
		}
		return nil
	}}
	grant, activate, revoke, err := runner.config.Registry.Prepare(capability.GrantSpec{
		ExecutionID: input.JobID, Bindings: bindings, Validate: input.Validate,
	})
	if err != nil {
		return lease, nil, err
	}
	lease.Close = func(context.Context) error { revoke(); return nil }
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return lease, nil, status_error.KnowledgeStorageError
	}
	path, err := capability.WriteConnection(canonical, "connection.json", capability.Connection{
		ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: runner.config.Endpoint,
		CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token,
	})
	if err != nil {
		return lease, nil, err
	}
	if err := ctx.Err(); err != nil {
		return lease, nil, err
	}
	lease.MCPServers = []agent.AgentMCPServer{{Name: name, Transport: "stdio", Command: binary, Arguments: []string{"mcp", "serve", "--context", path}}}
	activate()
	return lease, revoke, nil
}
