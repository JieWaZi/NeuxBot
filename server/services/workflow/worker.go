package workflow

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// WorkerDispatch 保存已经在 outbox 冻结的真实派发输入。
type WorkerDispatch struct {
	// Run 是版本、配置和范围快照。
	Run models.WorkflowRun
	// Effect 是固定尝试和执行身份。
	Effect models.WorkflowEffect
	// ResultBindings 是本次效果与 fence 绑定的结果能力。
	ResultBindings []capability.Binding
}

// WorkerReceipt 是从既有 Core 与权威 Chat 读取的最小执行事实。
type WorkerReceipt struct {
	// FinalMessageID 是首次执行最终正文的权威消息身份。
	FinalMessageID string
	// FinalPresent 表示权威消息中确有最终正文，合法空正文与缺失结果分别处理。
	FinalPresent bool
	// ChatID 是独立 worker Chat。
	ChatID string
	// ExecutionID 是首次执行。
	ExecutionID string
	// Status 是权威执行状态。
	Status constants.ExecutionStatus
	// Text 是非结构化节点的首次执行最终文本。
	Text string
	// ErrorMessage 是执行返回的公开原生原因。
	ErrorMessage string
	// ErrorMessageSource 是原因的明确来源。
	ErrorMessageSource constants.ErrorMessageSource
	// ErrorParams 是同一错误的安全模板参数。
	ErrorParams map[string]interface{}
	// ErrorCode 是执行失败的稳定错误码。
	ErrorCode string
	// OutputFailure 保留无法确认产出的终止与交付证据，不改写真实执行状态。
	OutputFailure *workflowdsl.Value
}

// WorkerPort 复用真实 Execution 的接纳、查询和停止，测试可在此窄边界控制时间。
type WorkerPort interface {
	// ResolveAdmission 在接纳时将节点有效选择交给 Execution 解析并冻结。
	ResolveAdmission(context.Context, map[string]any, models.WorkflowExecutionDefaults, bool) (models.WorkflowExecutionDefaults, error)
	// Dispatch 使用效果身份幂等建立独立 Chat。
	Dispatch(context.Context, WorkerDispatch) (WorkerReceipt, error)
	// Inspect 只读取首次执行，续聊不会覆盖节点结果。
	Inspect(context.Context, models.WorkflowEffect) (WorkerReceipt, error)
	// Cancel 等待对应执行真实停止。
	Cancel(context.Context, models.WorkflowEffect) error
}

// CoreWorkerAdapter 将 Workflow 的业务输入转换为中立 WorkIntent。
type CoreWorkerAdapter struct {
	// Executions 是既有执行入口。
	Executions execution.Service
	// Chats 是首次执行与公开消息的权威查询。
	Chats chat.Module
}

func (a CoreWorkerAdapter) Dispatch(ctx context.Context, input WorkerDispatch) (WorkerReceipt, error) {
	args := input.Effect.Boundary.Arguments
	if len(args) != 2 {
		return WorkerReceipt{}, status_error.WorkflowStateInvalid
	}
	prompt, ok := args[0].Data.(string)
	if !ok {
		return WorkerReceipt{}, status_error.WorkflowStateInvalid
	}
	options, ok := args[1].Data.(map[string]any)
	if !ok {
		return WorkerReceipt{}, status_error.WorkflowStateInvalid
	}
	resolve, _ := options["resolve"].(map[string]any)
	wakerID, _ := resolve["wakerId"].(string)
	if wakerID == "" {
		return WorkerReceipt{}, status_error.WorkflowResourceInvalid
	}
	if _, err := validateWorkerToolPolicy(options); err != nil {
		return WorkerReceipt{}, err
	}
	defaults, frozen := input.Run.WorkerConfigurations[input.Effect.Boundary.NodeID]
	if !frozen {
		return WorkerReceipt{}, status_error.WorkflowStateInvalid
	}
	schema, _ := options["schema"].(map[string]any)
	resultInstruction := ""
	if len(schema) > 0 {
		encoded, _ := json.Marshal(schema)
		resultInstruction = "Read the Worker Results guide linked from the host-provided bot skill; resolve its relative link from the supplied SKILL.md directory. " +
			"Complete the actual work, read workflow.results.schema for this attempt, then invoke workflow.results.submit with a stable requestId through the host-selected capability transport. " +
			"When bot_capabilities MCP is injected, use workflow_results_schema and workflow_results_submit with their advertised input schemas. Otherwise follow the host-provided CLI transport and current execution context. Do not switch transports to bypass an error. " +
			"Submit data as the actual JSON value matching this schema, without an invented wrapper or extra JSON encoding: " + string(encoded) +
			". Final chat text is not a result submission. An accepted candidate cannot be replaced by different content; after submission succeeds, finish this execution so the host can confirm node success separately."
	}
	tools := []execution.ToolCapability{}
	if len(schema) > 0 {
		tools = append(tools, execution.ToolCapability{ID: "workflow.result", Name: "工作流结果", Bindings: input.ResultBindings})
	}
	domains := []string{"memory", "interaction"}
	scope := input.Run.ScopeBinding
	create := &execution.CreateChatSpec{HiddenFromWakerHistory: true, WakerID: wakerID, WorkingDirectory: scope.WorkingDirectory}
	internalWorkspace, err := runWorkspace(input.Run)
	if err != nil {
		return WorkerReceipt{}, err
	}
	create.ExecutionWorkspaceDirectory = filepath.Join(internalWorkspace.TemporaryDirectory, constants.WorkflowWorkerDirectoryName, input.Effect.ID)
	encoded, _ := json.Marshal(internalWorkspace)
	for _, directory := range append([]string{scope.WorkingDirectory}, input.Run.AdditionalWorkingDirectories...) {
		canonical, err := workflowfiles.EnsureScopeDirectory(directory)
		if err != nil || canonical != directory {
			return WorkerReceipt{}, status_error.WorkflowResourceInvalid.WithField("field", "workingDirectory")
		}
	}
	create.AdditionalWorkingDirectories = append([]string{internalWorkspace.RootDirectory}, input.Run.AdditionalWorkingDirectories...)
	contextInstructions := []workinput.Instruction{{Name: constants.WorkflowWorkspaceInstructionFileName, Content: "Workflow run workspace: " + string(encoded) + "\nThe chat working directory is for this execution. Keep scripts in scriptsDirectory, persistent state in stateDirectory, caches in cacheDirectory, temporary files in temporaryDirectory, and run deliverables in runDirectory by default. Keep scripts and support files inside rootDirectory. Do not create external support directories such as /tmp or paths under the home directory. Write deliverables to an external directory only if the user specifies it explicitly. Keep scripts, caches, state, and temporary files out of external business directories."}}
	// 宿主结果协议只进入智能体上下文，聊天正文保留节点的业务要求。
	if resultInstruction != "" {
		contextInstructions = append(contextInstructions, workinput.Instruction{Name: constants.WorkflowResultsInstructionFileName, Content: resultInstruction})
	}
	accepted, err := a.Executions.Start(ctx, execution.WorkIntent{AutomaticRunID: input.Run.AutomaticRunID, ExecutionID: input.Effect.ExecutionID, ClientRequestID: input.Effect.ID, Chat: execution.ChatTarget{Create: create}, Input: execution.WorkInput{Actor: "workflow", Content: prompt, ContextInstructions: contextInstructions}, Options: execution.WorkOptions{ConfigurationResolved: true, RuntimeID: defaults.RuntimeID, AccountID: defaults.AccountID, Model: defaults.Model, Reasoning: defaults.Reasoning, Fast: defaults.Fast, PermissionLevel: defaults.PermissionLevel, WorkspaceReadOnly: defaults.WorkspaceReadOnly, SessionPolicy: constants.RuntimeSessionPolicyNew, ToolCapabilities: tools, AllowedAgentDomains: &domains}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch})
	if err != nil {
		return WorkerReceipt{}, err
	}
	if accepted.Execution.Status == constants.ExecutionStatusCompleted {
		// 接纳重放可能已经终止；与后续观察共用同一产出完整性判定。
		effect := input.Effect
		effect.ChatID = accepted.Chat.ID
		return a.Inspect(ctx, effect)
	}
	return WorkerReceipt{ChatID: accepted.Chat.ID, ExecutionID: accepted.Execution.ID, Status: accepted.Execution.Status}, nil
}

func (a CoreWorkerAdapter) Inspect(ctx context.Context, effect models.WorkflowEffect) (WorkerReceipt, error) {
	chatID := effect.ChatID
	state, err := a.Chats.ReadInitialExecution(ctx, chatID)
	if err != nil {
		return WorkerReceipt{}, err
	}
	if state.CreateRequestID != effect.ID || state.InitialExecution == nil || state.InitialExecution.ID != effect.ExecutionID {
		return WorkerReceipt{}, status_error.WorkflowStateConflict
	}
	receipt := WorkerReceipt{ChatID: chatID, ExecutionID: state.InitialExecution.ID, Status: state.InitialExecution.Status, ErrorCode: state.InitialExecution.ErrorCode, ErrorMessage: state.InitialExecution.ErrorMessage, ErrorParams: state.InitialExecution.ErrorParams, ErrorMessageSource: state.InitialExecution.ErrorMessageSource}
	if receipt.Status == constants.ExecutionStatusCompleted {
		outcome, err := a.Chats.ReadExecutionOutcome(ctx, chatID, effect.ExecutionID)
		if err != nil {
			return receipt, err
		}
		reason := outcome.TerminationReason
		if reason == constants.RuntimeTerminationReasonUnspecified {
			reason = constants.RuntimeTerminationReasonUnknown
		}
		if reason != constants.RuntimeTerminationReasonNormal || outcome.FailureCode != constants.RuntimeFailureCodeUnspecified {
			failure := workflowdsl.JSONValue(map[string]any{
				"code":              constants.WorkflowResultUnconfirmedCode,
				"terminationReason": string(reason), "stopReason": outcome.StopReason,
				"failureCode": string(outcome.FailureCode), "failureStage": outcome.FailureStage,
				"failureMessage": boundedFailureText(outcome.FailureMessage),
			})
			receipt.OutputFailure = &failure
			return receipt, nil
		}
		messages, err := a.Chats.ReadExecutionMessages(ctx, chatID, effect.ExecutionID)
		if err != nil {
			return receipt, err
		}
		// 固定首次执行的结果范围；后续普通续聊不参与节点产出，也不倒翻聊天历史。
		for index := len(messages) - 1; index >= 0; index-- {
			message := messages[index]
			if message.ExecutionID != effect.ExecutionID || message.Role != "assistant" {
				continue
			}
			texts := []string{}
			for _, part := range message.DisplayParts {
				if part.Type == constants.MessageDisplayPartText {
					receipt.FinalPresent = true
					receipt.FinalMessageID = message.ID
					texts = append(texts, strings.TrimSpace(part.Text))
				}
			}
			if receipt.FinalPresent {
				receipt.Text = strings.Join(texts, "\n\n")
				break
			}
		}
	}
	return receipt, nil
}
func (a CoreWorkerAdapter) Cancel(ctx context.Context, effect models.WorkflowEffect) error {
	return a.Executions.CancelExecutionAndWait(ctx, effect.ChatID, effect.ExecutionID)
}

// workerChatID 由效果身份和冻结伙伴定位同一个首次接纳会话。
func workerChatID(effectID string, boundary workflowdsl.EffectBoundary) string {
	resolve, _ := workflowstate.BoundaryOptions(boundary)["resolve"].(map[string]any)
	wakerID, _ := resolve["wakerId"].(string)
	return requestidempotency.Identity{ID: effectID}.ResourceID("chat", wakerID)
}

// ValidateAdmission 在接纳事务之外检查节点的智能体选择。
func (a CoreWorkerAdapter) ValidateAdmission(ctx context.Context, options map[string]any, defaults models.WorkflowExecutionDefaults) error {
	configuration, _ := workerConfiguration(options, defaults)
	_, err := a.ResolveAdmission(ctx, options, configuration, false)
	return err
}

// ResolveAdmission 通过 Execution 共用解析边界冻结节点选择，已冻结的选择仅校验。
func (a CoreWorkerAdapter) ResolveAdmission(ctx context.Context, _ map[string]any, defaults models.WorkflowExecutionDefaults, resolved bool) (models.WorkflowExecutionDefaults, error) {
	if !constants.SelectablePermissionLevel(defaults.PermissionLevel) {
		return defaults, status_error.BadRequest
	}
	config := execution.ExecutionConfiguration{Resolved: resolved, RuntimeID: defaults.RuntimeID, AccountID: defaults.AccountID, Model: defaults.Model, Reasoning: defaults.Reasoning, Fast: defaults.Fast, PermissionLevel: defaults.PermissionLevel}

	configuration, err := a.Executions.ResolveConfiguration(ctx, config)
	if err != nil {
		return defaults, err
	}
	defaults.RuntimeID, defaults.AccountID, defaults.Model, defaults.Reasoning = configuration.RuntimeID, configuration.AccountID, configuration.Model, configuration.Reasoning
	defaults.Fast = configuration.Fast
	return defaults, nil
}

func workerConfiguration(options map[string]any, defaults models.WorkflowExecutionDefaults) (models.WorkflowExecutionDefaults, bool) {
	resolve, _ := options["resolve"].(map[string]any)
	defaults.RuntimeID = strings.TrimSpace(defaults.RuntimeID)
	defaults.AccountID = strings.TrimSpace(defaults.AccountID)
	defaults.Model = strings.TrimSpace(defaults.Model)
	runtimeID, _ := resolve["runtimeId"].(string)
	runtimeID = strings.TrimSpace(runtimeID)
	if runtimeID == "" {
		runtimeID = defaults.RuntimeID
	}
	sameRuntime := runtimeID == defaults.RuntimeID
	accountID, explicitAccount := resolve["accountId"].(string)
	accountID = strings.TrimSpace(accountID)
	if !explicitAccount && sameRuntime {
		accountID = defaults.AccountID
	}
	sameAccount := sameRuntime && accountID == defaults.AccountID
	model, explicitModel := resolve["model"].(string)
	model = strings.TrimSpace(model)
	if !explicitModel && sameAccount {
		model = defaults.Model
	}
	sameModel := sameAccount && model == defaults.Model && (!explicitModel || model != "")
	reasoning, explicitReasoning := resolve["reasoning"].(string)
	reasoning = strings.TrimSpace(reasoning)
	if !explicitReasoning && sameModel {
		reasoning = defaults.Reasoning
	}
	if !sameModel {
		defaults.Fast = nil
	}
	defaults.RuntimeID, defaults.AccountID, defaults.Model, defaults.Reasoning = runtimeID, accountID, model, reasoning
	if permission, ok := resolve["permissionLevel"].(string); ok && permission != "" {
		defaults.PermissionLevel = constants.PermissionLevel(permission)
	}
	// 同一模型沿用调用方冻结的原生空值；显式清空表示重新选择默认值。
	inherited := sameModel && (!explicitReasoning || reasoning != "")
	return defaults, inherited
}

func validateWorkerToolPolicy(options map[string]any) (string, error) {
	policy, _ := options["toolPolicy"].(map[string]any)
	// 原生执行没有隔离继承工具与连接器的端口，按节点限制的流程应拒绝接纳。
	for _, key := range []string{"nativeTools", "connectors"} {
		scope, _ := policy[key].(map[string]any)
		if scope["mode"] == "allowlist" {
			field := "toolPolicy." + key
			return field, status_error.WorkflowCapabilityUnsupported.WithField("capability", field+".allowlist")
		}
	}
	return "", nil
}

// workerRetryPolicy 是创作预检、配置检查和运行接纳共用的重试能力判定。
// 未提供执行配置时只检查宿主确定不支持的策略，不把源码导入当作运行接纳。
func workerRetryPolicy(safety string, configuration *models.WorkflowExecutionDefaults) status_error.StatusError {
	if safety == "idempotent" {
		// 声明幂等键不能代替逐工具去重，当前宿主尚无此强制端口。
		return status_error.WorkflowRetryIdempotencyUnsupported
	}
	if safety == "pure" && configuration != nil && !configuration.WorkspaceReadOnly {
		return status_error.WorkflowRetryRequiresReadOnly
	}
	return 0
}

func workerRetryDiagnostic(node workflowdsl.Node, configuration *models.WorkflowExecutionDefaults) (workflowdsl.Diagnostic, error) {
	retry, _ := node.Options["retry"].(map[string]any)
	safety, _ := retry["safety"].(string)
	policy := workerRetryPolicy(safety, configuration)
	if policy == 0 {
		return workflowdsl.Diagnostic{}, nil
	}
	message := "Automatic worker retry requires workspaceReadOnly=true in the effective node configuration."
	hint := "Read execution.workerRetry and execution.defaults from workflow.authoring.prepare. A pure declaration does not enable read-only execution. Ask the user before changing permissions or removing requested retry behavior; do not emulate retries with extra worker calls."
	if policy == status_error.WorkflowRetryIdempotencyUnsupported {
		message = "The host cannot enforce per-tool idempotency for automatic worker retry."
		hint = "An idempotencyKey declaration does not prove deduplication. Ask the user how to revise the requested retry behavior; do not relabel the task as pure or add replacement worker calls to bypass this restriction."
	}
	diagnostic := workflowdsl.Diagnostic{Code: policy.Key(), Severity: "error", Message: message, Range: node.Range, NodeID: node.ID, Field: "retry", Hint: hint}
	return diagnostic, policy.WithField("node", node.Label).WithField("nodeId", node.ID)
}

// validateWorkerRetryPolicies 一次返回全部冲突节点，避免创作逐个发现同类问题。
func validateWorkerRetryPolicies(manifest workflowdsl.Manifest, configuration *models.WorkflowExecutionDefaults) ([]workflowdsl.Diagnostic, error) {
	diagnostics := []workflowdsl.Diagnostic{}
	var first error
	for _, node := range manifest.Nodes {
		if node.Kind != "worker" {
			continue
		}
		diagnostic, err := workerRetryDiagnostic(node, configuration)
		if err != nil {
			diagnostics = append(diagnostics, diagnostic)
			if first == nil {
				first = err
			}
		}
	}
	return diagnostics, first
}

// prepareWorkerRetry 保留旧收据，只在实际 Execution 已终止且声明只读重试时创建新 outbox。
func prepareWorkerRetry(state *controllers.WorkflowRunState, fiberIndex int, previous models.WorkflowEffect) bool {
	configuration, frozen := state.Run.WorkerConfigurations[previous.Boundary.NodeID]
	if previous.Boundary.Primitive != "worker" || previous.Status != constants.WorkflowStepStatusFailed || previous.SkipRequested || !frozen || !configuration.WorkspaceReadOnly {
		return false
	}
	if previous.ExecutionStatus != constants.ExecutionStatusFailed && previous.ExecutionStatus != constants.ExecutionStatusCompleted {
		return false
	}
	if previous.Failure != nil {
		failure, _ := previous.Failure.Data.(map[string]any)
		if failure["code"] == constants.WorkflowResultUnconfirmedCode {
			return false
		}
	}
	options := workflowstate.BoundaryOptions(previous.Boundary)
	// 已完成的业务执行仅允许独立文本通道格式纠正，耗尽后不能通过普通节点重试重跑业务。
	if schema, _ := options["schema"].(map[string]any); len(schema) > 0 && previous.ExecutionStatus == constants.ExecutionStatusCompleted {
		return false
	}
	retry, _ := options["retry"].(map[string]any)
	maximum, _ := retry["maxAttempts"].(float64)
	if retry["safety"] != "pure" || previous.AttemptGeneration >= int64(maximum) {
		return false
	}
	token := util.New() + util.New()
	next := models.WorkflowEffect{ID: util.New(), RunID: previous.RunID, FiberID: previous.FiberID, InstanceID: previous.InstanceID, Boundary: previous.Boundary, Status: constants.WorkflowStepStatusPrepared, AttemptGeneration: previous.AttemptGeneration + 1, ExecutionID: util.New(), AuthToken: token, TokenDigest: workflowdsl.Digest([]byte(token))}
	next.ChatID = workerChatID(next.ID, next.Boundary)
	state.Effects = append(state.Effects, next)
	state.Fibers[fiberIndex].WaitingID = next.ID
	for index := range state.Instances {
		instance := &state.Instances[index]
		if instance.ID != previous.InstanceID {
			continue
		}
		instance.EffectID, instance.ExecutionID, instance.AttemptGeneration = next.ID, next.ExecutionID, next.AttemptGeneration
		instance.Status, instance.ChatID = constants.WorkflowStepStatusWaiting, ""
		instance.Output, instance.Failure, instance.CompletedAt = nil, nil, nil
	}
	return true
}

func (s *Service) dispatchEffect(ctx context.Context, run models.WorkflowRun, effect models.WorkflowEffect) {
	// 所有者变化不能改变已准备的 attemptGeneration。只在当前有效 fence 下派发。
	current, err := s.runs.GetRun(ctx, run.ID)
	if err != nil || current.LeaseOwner != s.state.Owner || current.Fence != run.Fence || current.LeaseUntil <= time.Now().UnixMilli() {
		return
	}
	effect, err = s.runs.GetEffect(ctx, effect.ID)
	if err != nil || workflowstate.StepTerminal(effect.Status) {
		return
	}
	cancelling := current.Status == constants.WorkflowRunStatusCancelling || effect.Failure != nil || effect.SkipRequested
	if effect.Boundary.Primitive != "worker" {
		s.dispatchChildOrAction(ctx, run, effect, cancelling)
		return
	}
	if s.worker == nil {
		s.finishEffect(ctx, effect, WorkerReceipt{}, status_error.WorkflowCapabilityUnsupported)
		return
	}
	// 不使用旧运行持久化的节点期限；整体运行取消仍通过原有边界停止伙伴。
	if cancelling {
		err := s.worker.Cancel(ctx, effect)
		if err != nil {
			receipt, inspectErr := s.worker.Inspect(ctx, effect)
			if inspectErr == nil && workflowstate.ExecutionTerminal(receipt.Status) {
				s.finishEffect(ctx, effect, receipt, nil)
			} else {
				s.recordWorkerCancellationUnknown(ctx, effect, err)
			}
			return
		}
		s.finishEffect(ctx, effect, WorkerReceipt{ChatID: effect.ChatID, ExecutionID: effect.ExecutionID, Status: constants.ExecutionStatusCanceled}, nil)
		return
	}
	var receipt WorkerReceipt
	if effect.Status == constants.WorkflowStepStatusPrepared {
		receipt, err = s.worker.Dispatch(ctx, WorkerDispatch{Run: run, Effect: effect, ResultBindings: s.AgentBindings(workflowtool.Scope{Key: effect.ID, Token: effect.AuthToken, Kind: constants.WorkflowCapabilityScopeResult})})
	} else {
		receipt, err = s.worker.Inspect(ctx, effect)
	}
	if ctx.Err() != nil {
		if s.runner != nil && s.runner.ctx.Err() == nil {
			cancelErr := s.worker.Cancel(s.runner.ctx, effect)
			if cancelErr != nil {
				s.recordWorkerCancellationUnknown(s.runner.ctx, effect, cancelErr)
				return
			}
			receipt.Status = constants.ExecutionStatusCanceled
			s.finishEffect(s.runner.ctx, effect, receipt, nil)
		}
		return
	}
	if err != nil {
		// 派发响应丢失或查询暂不可用时，先核对固定 Execution，不能把已接纳的任务记成失败。
		actual, inspectErr := s.worker.Inspect(ctx, effect)
		if inspectErr == nil {
			s.finishEffect(ctx, effect, actual, nil)
			return
		}
		missingExecution := workflowstate.ErrorKey(inspectErr) == status_error.ChatNotFound.Key() ||
			workflowstate.ErrorKey(inspectErr) == status_error.ExecutionNotFound.Key()
		if !missingExecution {
			s.stopUncertainWorker(ctx, effect, err)
			return
		}
		if effect.Status != constants.WorkflowStepStatusPrepared {
			// 已运行的首次执行记录消失也不能证明外部工作没有发生。
			s.stopUncertainWorker(ctx, effect, inspectErr)
			return
		}
	}
	if err == nil {
		receipt, err = s.repairWorkerOutput(ctx, run, effect, receipt)
	}
	s.finishEffect(ctx, effect, receipt, err)
}

func (s *Service) stopUncertainWorker(ctx context.Context, effect models.WorkflowEffect, cause error) {
	// 尚未发出停止的派发或查询故障只尝试停止一次，随后保存原不确定证据。
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.worker.Cancel(stopCtx, effect); err != nil {
		logx.Errorf("stop uncertain workflow worker failed: effect_id=%s error=%v", effect.ID, err)
	}
	s.recordWorkerCancellationUnknown(ctx, effect, cause)
}

func (s *Service) recordWorkerCancellationUnknown(ctx context.Context, effect models.WorkflowEffect, cause error) {
	if err := s.state.MutateEffect(ctx, effect.RunID, effect.ID, func(_ context.Context, state *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
		if workflowstate.StepTerminal(current.Status) || current.Status == constants.WorkflowStepStatusUnknown {
			return nil
		}
		current.Status = constants.WorkflowStepStatusUnknown
		current.Failure = failureFromError(cause, &current.Boundary, nil)
		for index := range state.Instances {
			if state.Instances[index].EffectID == current.ID {
				state.Instances[index].Status = constants.WorkflowStepStatusUnknown
			}
		}
		return nil
	}); err != nil && ctx.Err() == nil {
		logx.Errorf("record workflow worker cancellation uncertainty failed: effect_id=%s error=%v", effect.ID, err)
	}
}

func (s *Service) finishEffect(ctx context.Context, effect models.WorkflowEffect, receipt WorkerReceipt, failure error) {
	release, err := chatstorage.AcquireReferences(s.dataDirectory, receipt.ChatID)
	if err != nil {
		logx.Errorf("defer workflow worker receipt while chat is busy: effect_id=%s error=%v", effect.ID, err)
		return
	}
	defer release()
	schema, _ := workflowstate.BoundaryOptions(effect.Boundary)["schema"].(map[string]any)
	err = s.commitOutputEffect(ctx, effect, schema, func(ctx context.Context, state *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
		if workflowstate.StepTerminal(current.Status) || current.Status == constants.WorkflowStepStatusUnknown {
			return nil
		}
		if receipt.ExecutionID != "" && receipt.ExecutionID != current.ExecutionID {
			return status_error.WorkflowStateConflict
		}
		if receipt.ChatID != "" {
			current.ChatID = receipt.ChatID
		}
		if receipt.Status != "" {
			current.ExecutionStatus = receipt.Status
		}
		if current.SkipRequested {
			if receipt.Status == constants.ExecutionStatusCanceled || receipt.Status == constants.ExecutionStatusCompleted || receipt.Status == constants.ExecutionStatusFailed {
				value := workflowdsl.JSONValue(nil)
				current.Status, current.Result, current.Failure = constants.WorkflowStepStatusSkipped, &value, nil
				return nil
			}
			// 取消未收敛不能把未知 Runtime 结果伪装成已经跳过。
			return status_error.WorkflowStateConflict
		}
		if receipt.OutputFailure != nil {
			current.Status = constants.WorkflowStepStatusFailed
			current.Failure = receipt.OutputFailure
			return nil
		}
		if failure != nil {
			current.Status = constants.WorkflowStepStatusFailed
			current.Failure = failureFromError(failure, &current.Boundary, nil)
			return nil
		}
		switch receipt.Status {
		case constants.ExecutionStatusPending, constants.ExecutionStatusRunning:
			current.Status = constants.WorkflowStepStatusRunning
		case constants.ExecutionStatusCompleted:
			current.FinalMessageID = receipt.FinalMessageID
			options := workflowstate.BoundaryOptions(current.Boundary)
			schema, _ := options["schema"].(map[string]any)
			if len(schema) > 0 {
				if current.Candidate == nil && receipt.FinalPresent {
					var data any
					if json.Unmarshal([]byte(receipt.Text), &data) == nil && workflowdsl.ValidateJSON(schema, data) == nil {
						candidate := workflowdsl.JSONValue(data)
						current.Candidate = &candidate
					}
				}
				if current.Candidate == nil {
					current.Status = constants.WorkflowStepStatusFailed
					current.Failure = workflowstate.FailureValue(constants.WorkflowResultMissingCode)
				} else {
					current.Result = current.Candidate
					current.Status = constants.WorkflowStepStatusSucceeded
				}
			} else {
				if !receipt.FinalPresent {
					current.Status = constants.WorkflowStepStatusFailed
					current.Failure = workflowstate.FailureValue(constants.WorkflowResultMissingCode)
					return nil
				}
				if len([]byte(receipt.Text)) > constants.WorkflowOutputLimitBytes {
					return status_error.WorkflowBudgetExceeded
				}
				value := workflowdsl.JSONValue(receipt.Text)
				current.Result = &value
				current.Status = constants.WorkflowStepStatusSucceeded
			}
		case constants.ExecutionStatusCanceled:
			current.Status = constants.WorkflowStepStatusCancelled
			current.Failure = workflowstate.FailureValue("CANCELLED")
		case constants.ExecutionStatusFailed:
			current.Status = constants.WorkflowStepStatusFailed
			code := receipt.ErrorCode
			if code == "" {
				code = "WORKER_FAILED"
			}
			failure := workflowdsl.JSONValue(map[string]any{"code": code, "message": receipt.ErrorMessage, "messageSource": receipt.ErrorMessageSource, "params": receipt.ErrorParams})
			current.Failure = &failure
		default:
			return status_error.WorkflowStateInvalid
		}
		if current.Status == constants.WorkflowStepStatusSucceeded && current.Result != nil {
			data, _ := current.Result.JSON()
			if !workflowdsl.EvaluateSuccessCondition(data, workflowstate.BoundaryOptions(current.Boundary)["successCondition"]) {
				current.Status = constants.WorkflowStepStatusFailed
				current.Failure = workflowstate.FailureValue("BUSINESS_CHECK_FAILED")
			}
		}
		return nil
	})
	if err != nil && ctx.Err() == nil {
		logx.Errorf("commit workflow effect receipt failed: run_id=%s effect_id=%s error=%v", effect.RunID, effect.ID, err)
	}
}
