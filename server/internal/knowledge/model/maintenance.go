package model

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
)

// MaintenanceRuntime 提供无需伙伴、聊天或执行记录的独立 ACP 会话。
type MaintenanceRuntime interface {
	Run(context.Context, runtimes.RunRequest, runtimes.EventSink) runtimes.Result
}

// MaintenanceConfig 显式装配知识维护的账号、能力授权与运行端口。
type MaintenanceConfig struct {
	// Runtime 只拥有本次原生 ACP 调用。
	Runtime MaintenanceRuntime
	// Prepare 冻结账号配置并返回覆盖完整调用的释放回调。
	Prepare func(context.Context) (runtimes.RuntimeTarget, runtimes.DetectionResult, func(), error)
	// Registry 负责知识任务的短期操作授权。
	Registry *agentcapability.Registry
	// Endpoint 是现有软件能力服务的回环地址。
	Endpoint string
	// Binary 是应用同包交付的真实 allycli 可执行文件。
	Binary string
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

// MaintenanceRunner 只承载上游修复工作流的独立原生工具执行，不新增内容规划算法。
type MaintenanceRunner struct {
	// config 是固定模型与能力装配。
	config MaintenanceConfig
}

// NewMaintenanceRunner 构造后台调用适配器，不启动原生进程。
func NewMaintenanceRunner(config MaintenanceConfig) *MaintenanceRunner {
	return &MaintenanceRunner{config: config}
}

// Run 用临时工作区和单任务能力运行 Wiki Fixer，结束后回收原生进程、账号及授权。
func (runner *MaintenanceRunner) Run(ctx context.Context, input MaintenanceRequest) (string, error) {
	if ctx == nil || strings.TrimSpace(input.JobID) == "" || input.Validate == nil || len(input.Bindings) == 0 || strings.TrimSpace(input.Instructions) == "" || strings.TrimSpace(input.Message) == "" {
		return "", status_error.KnowledgeInvalid
	}
	if err := ctx.Err(); err != nil {
		return "", status_error.OperationCanceled
	}
	if runner == nil || runner.config.Runtime == nil || runner.config.Prepare == nil || runner.config.Registry == nil {
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
	temporary, err := os.MkdirTemp("", "neux-wiki-maintenance-")
	if err != nil {
		logx.Errorf("create knowledge maintenance workspace failed: error=%v", err)
		return "", status_error.KnowledgeStorageError
	}
	defer func() {
		if err := os.RemoveAll(temporary); err != nil {
			logx.Errorf("remove knowledge maintenance workspace failed: error=%v", err)
		}
	}()
	temporary, err = filepath.EvalSymlinks(temporary)
	if err != nil {
		return "", status_error.KnowledgeStorageError
	}
	workspace := filepath.Join(temporary, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		logx.Errorf("prepare knowledge maintenance workspace failed: error=%v", err)
		return "", status_error.KnowledgeStorageError
	}
	// 技能和 CLI 随临时工作区一同准备与回收，不留下本次任务的上下文文件。
	skill, err := builtinskills.SystemDirectory(workspace, binary)
	if err != nil {
		return "", err
	}
	binary = builtinskills.CLIPath(skill)
	target, detection, release, err := runner.config.Prepare(ctx)
	if release != nil {
		defer release()
	}
	if err != nil {
		return "", err
	}
	target.CloseAfterRun = true
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
	grant, activate, revoke, err := runner.config.Registry.Prepare(agentcapability.GrantSpec{ExecutionID: input.JobID, Bindings: bindings, FileRoots: []string{workspace}, Validate: input.Validate})
	if err != nil {
		return "", err
	}
	defer revoke()
	connection, err := agentcapability.WriteConnection(filepath.Join(temporary, "context"), "connection.json", agentcapability.Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: runner.config.Endpoint, CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token})
	if err != nil {
		return "", err
	}
	environment := make(map[string]string, len(target.Environment)+3)
	for key, value := range target.Environment {
		environment[key] = value
	}
	environment[constants.AgentCLIContextEnvironment] = connection
	environment[constants.AgentCLIBinaryEnvironment] = binary
	inheritedPath := environment["PATH"]
	if inheritedPath == "" {
		inheritedPath = os.Getenv("PATH")
	}
	environment["PATH"] = filepath.Dir(binary) + string(filepath.ListSeparator) + inheritedPath
	target.Environment = environment
	request := runtimes.RunRequest{
		Target: target, Detection: detection,
		Workspace: runtimes.Workspace{Path: workspace, Managed: true},
		Session:   runtimes.SessionDirective{Mode: runtimes.SessionModeNew},
		Message:   runtimes.Message{Text: input.Message},
		Instructions: []runtimes.Instruction{
			{Name: "WIKI_MAINTENANCE.md", Content: input.Instructions},
			{Name: "软件能力", Content: fmt.Sprintf("Read the ally skill at %q. The exact executable is %q. Start with capabilities and describe, and use the advertised knowledge commands for all knowledge reads and changes. Use this invocation's existing connection; do not discover other credentials, databases or endpoints. Use the temporary workspace for scratch files and JSON command input as described in the skill. The runtime has full access for this knowledge task. Knowledge authorization, versions and durable results remain enforced by the supplied CLI. The knowledge task owns all state; do not create a partner or chat.", filepath.Join(skill, "SKILL.md"), binary)},
		},
		Skills: []runtimes.Skill{{Name: constants.AgentCLISkillName, Path: skill, Selected: true}},
		Policy: runtimes.RunPolicy{Model: runner.config.Model, Reasoning: runner.config.Reasoning, Execution: runtimes.ExecutionPolicy{PermissionLevel: runtimes.PermissionLevelFullAccess}},
	}
	collector := &maintenanceCollector{}
	activate()
	result := runner.config.Runtime.Run(ctx, request, collector)
	if result.Failure != nil {
		return "", modelFailure(result.Failure)
	}
	if ctx.Err() != nil {
		return "", status_error.OperationCanceled
	}
	if !result.ProtocolHealthy {
		return "", status_error.RuntimeProtocolIncompatible
	}
	if result.StopReason != "end_turn" && result.StopReason != "stop" {
		return "", status_error.RuntimeExecutionFailed.WithParams(map[string]interface{}{"Detail": "knowledge maintenance did not complete: stop_reason=" + result.StopReason})
	}
	if successfulCalls.Load() == 0 {
		return "", status_error.RuntimeExecutionFailed.WithParams(map[string]interface{}{"Detail": "knowledge maintenance completed no authorized knowledge operation; result is unverified"})
	}
	return collector.result(), nil
}

// maintenanceCollector 只保存最终答复正文，原生工具过程由知识任务和领域回执核验。
type maintenanceCollector struct {
	// mutex 保护原生事件交付与终态读取。
	mutex sync.Mutex
	// text 累积面向用户的最终结果。
	text strings.Builder
}

// Accept 收集最终正文；思考、工具事件与过程说明不作为完成报告。
func (collector *maintenanceCollector) Accept(_ context.Context, event runtimes.Event) error {
	if event.Type != runtimes.EventTypeTextDelta {
		return nil
	}
	var chunk struct {
		// Text 是 Runtime 统一文本增量。
		Text string `json:"text"`
	}
	if err := json.Unmarshal(event.Data, &chunk); err != nil {
		return status_error.RuntimeEventEncodingFailed
	}
	collector.mutex.Lock()
	defer collector.mutex.Unlock()
	collector.text.WriteString(chunk.Text)
	return nil
}

func (collector *maintenanceCollector) result() string {
	collector.mutex.Lock()
	defer collector.mutex.Unlock()
	return collector.text.String()
}
