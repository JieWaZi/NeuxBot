package model

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/host"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
)

// MaintenanceRuntime 提供无需伙伴、聊天或执行记录的共享独立工具调用。
type MaintenanceRuntime interface {
	ExecuteTask(context.Context, runtimehost.TaskRequest) (runtimehost.TextResult, error)
}

// MaintenanceConfig 显式装配知识维护的模型、能力授权与共享运行端口。
type MaintenanceConfig struct {
	// Runtime 复用共享宿主的原生调用、账号租约和清理所有权。
	Runtime MaintenanceRuntime
	// Registry 负责知识任务的短期操作授权。
	Registry *agentcapability.Registry
	// Endpoint 是现有软件能力服务的回环地址。
	Endpoint string
	// Binary 是应用同包交付的真实 allycli 可执行文件。
	Binary string
	// RuntimeID 是接纳时固定的智能体或 Profile。
	RuntimeID string
	// AccountID 是接纳时固定的账号选择。
	AccountID string
	// Model 是接纳时固定的模型。
	Model string
	// Reasoning 是接纳时固定的推理选项。
	Reasoning string
}

// MaintenanceRequest 使用知识作业身份限定现有 Wiki 修复能力，不关联聊天。
type MaintenanceRequest struct {
	// JobID 是本次独立知识任务身份。
	JobID string
	// Bindings 是入口已经冻结的单库操作集合。
	Bindings []agentcapability.Binding
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
	// 只认服务端真正完成的领域调用，发现命令和模型自述不能证明执行过维护检查。
	var successfulCalls atomic.Uint64
	bindings := append([]agentcapability.Binding(nil), input.Bindings...)
	for index := range bindings {
		call := bindings[index].Call
		if call == nil || !strings.HasPrefix(bindings[index].Operation, "knowledge.") {
			continue
		}
		bindings[index].Call = func(ctx context.Context, request agentcapability.Request) (agentcapability.Result, error) {
			result, err := call(ctx, request)
			if err == nil {
				successfulCalls.Add(1)
			}
			return result, err
		}
	}
	resources := &maintenanceResources{config: runner.config, input: input, binary: binary, bindings: bindings}
	defer resources.Revoke()
	result, err := runner.config.Runtime.ExecuteTask(ctx, runtimehost.TaskRequest{
		TextRequest: runtimehost.TextRequest{RuntimeID: runner.config.RuntimeID, AccountID: runner.config.AccountID, Model: runner.config.Model, Reasoning: runner.config.Reasoning, Thinking: true, Messages: []runtimehost.Message{{Role: "user", Content: input.Message}}},
		Contributions: c.Contributions{
			Instructions: []c.Instruction{
				{Name: "WIKI_MAINTENANCE.md", Text: input.Instructions},
				{Name: "软件能力", Text: fmt.Sprintf("Read the supplied ally skill. The exact executable is provided by %s. Start with capabilities and describe, and use the advertised knowledge commands for all knowledge reads and changes. Use this invocation's existing connection; do not discover other credentials, databases or endpoints. Use the temporary workspace for scratch files and JSON command input as described in the skill. The runtime has full access for this knowledge task. Knowledge authorization, versions and durable results remain enforced by the supplied CLI. The knowledge task owns all state; do not create a partner or chat.", constants.AgentCLIBinaryEnvironment)},
			},
			Skills: []c.Skill{{Name: constants.AgentCLISkillName, ResourceID: constants.AgentCLISkillName}},
		}, Resources: resources,
	})
	if err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", status_error.WithoutRetry(status_error.OperationCanceled)
	}
	if (result.StopReason != "end_turn" && result.StopReason != "stop") || strings.TrimSpace(result.Text) == "" {
		return "", status_error.WithoutRetry(status_error.RuntimeExecutionFailed.WithParams(map[string]interface{}{"Detail": "knowledge maintenance did not produce a complete report: stop_reason=" + result.StopReason}))
	}
	if successfulCalls.Load() == 0 {
		return "", status_error.WithoutRetry(status_error.RuntimeExecutionFailed.WithParams(map[string]interface{}{"Detail": "knowledge maintenance completed no authorized knowledge operation; result is unverified"}))
	}
	return result.Text, nil
}

// maintenanceResources 物化任务技能和连接；授权在调用终点撤销，文件由引擎租约保护。
type maintenanceResources struct {
	// config 固定领域授权服务及同包 CLI 来源。
	config MaintenanceConfig
	// input 保存本次作业身份和当前授权检查。
	input MaintenanceRequest
	// binary 是已验证的同包 CLI 来源。
	binary string
	// bindings 只包含本次冻结范围，并登记成功领域调用。
	bindings []agentcapability.Binding
	// workspace 是共享宿主拥有的原生工作区。
	workspace string
	// directory 是本租约独占的资源目录，不包含账号投射。
	directory string
	// skill 是已安装的不可变 ally 技能目录。
	skill string
	// connection 是供原生 CLI 读取的本次连接文件。
	connection string
	// mutex 串行化启动环境读取、授权激活和并发撤销。
	mutex sync.Mutex
	// revoke 撤销已经签发的短期授权，不负责删除文件。
	revoke func()
	// revoked 记录物化完成前发生的取消，防止随后激活新授权。
	revoked bool
}

// Acquire 由引擎在账号准备后调用；返回部分租约以保留失败准备的清理所有权。
func (resources *maintenanceResources) Acquire(ctx context.Context, request c.ResourceRequest) (c.ResourceLease, error) {
	if ctx.Err() != nil {
		return resources, status_error.OperationCanceled
	}
	resources.workspace = request.Workspace.Root
	resources.directory = filepath.Join(request.Workspace.Root, "maintenance-resources")
	if err := os.Mkdir(resources.directory, 0700); err != nil {
		logx.Errorf("prepare knowledge maintenance resources failed: error=%v", err)
		return resources, status_error.KnowledgeStorageError
	}
	skill, err := builtinskills.SystemDirectory(resources.directory, resources.binary)
	if err != nil {
		return resources, err
	}
	grant, activate, revoke, err := resources.config.Registry.Prepare(agentcapability.GrantSpec{ExecutionID: resources.input.JobID, Bindings: resources.bindings, FileRoots: []string{request.Workspace.Root}, Validate: resources.input.Validate})
	if err != nil {
		return resources, err
	}
	resources.mutex.Lock()
	if resources.revoked {
		resources.mutex.Unlock()
		revoke()
		return resources, status_error.OperationCanceled
	}
	resources.revoke = revoke
	resources.mutex.Unlock()
	connection, err := agentcapability.WriteConnection(filepath.Join(resources.directory, "context"), "connection.json", agentcapability.Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: resources.config.Endpoint, CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token})
	if err != nil {
		return resources, err
	}
	resources.mutex.Lock()
	defer resources.mutex.Unlock()
	if resources.revoked || ctx.Err() != nil {
		return resources, status_error.OperationCanceled
	}
	resources.skill, resources.connection = skill, connection
	activate()
	return resources, nil
}

// Resources 交付必需技能的目录绑定，内容安装与释放均由同一租约拥有。
func (resources *maintenanceResources) Resources() []c.ResourceBinding {
	resources.mutex.Lock()
	defer resources.mutex.Unlock()
	return []c.ResourceBinding{{ID: constants.AgentCLISkillName, Path: resources.skill}}
}

// LaunchEnvironment 只在原生启动边界注入本次连接与 CLI，并保留账号和原生环境。
func (resources *maintenanceResources) LaunchEnvironment(base map[string]string) map[string]string {
	resources.mutex.Lock()
	defer resources.mutex.Unlock()
	if base == nil {
		base = map[string]string{}
	}
	binary := builtinskills.CLIPath(resources.skill)
	base[constants.AgentCLIContextEnvironment] = resources.connection
	base[constants.AgentCLIBinaryEnvironment] = binary
	inherited := base["PATH"]
	if inherited == "" {
		inherited = os.Getenv("PATH")
	}
	base["PATH"] = filepath.Dir(binary) + string(filepath.ListSeparator) + inherited
	return base
}

// Revoke 先阻止后续激活，再撤销并收敛领域调用；不会提前删除原生仍在使用的文件。
func (resources *maintenanceResources) Revoke() {
	resources.mutex.Lock()
	defer resources.mutex.Unlock()
	resources.revoked = true
	if resources.revoke != nil {
		resources.revoke()
		resources.revoke = nil
	}
}

// Close 仅在原生退出后回收任务资源；失败保持租约所有权供引擎重试。
func (resources *maintenanceResources) Close(ctx context.Context) error {
	if ctx.Err() != nil {
		return status_error.OperationCanceled
	}
	resources.Revoke()
	if err := os.RemoveAll(resources.directory); err != nil {
		logx.Errorf("remove knowledge maintenance resources failed: error=%v", err)
		return status_error.KnowledgeStorageError
	}
	return nil
}
