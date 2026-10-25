package workflow

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
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
	ResultBindings []agentcapability.Binding
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
	// ErrorCode 是执行失败的稳定错误码。
	ErrorCode string
}

// WorkerPort 复用真实 Execution 的接纳、查询和停止，测试可在此窄边界控制时间。
type WorkerPort interface {
	// ResolveAdmission 在接纳时解析有效执行配置，派发不得重新选择默认账号或模型。
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
	allowedConnectors, err := workerConnectorAllowlist(options)
	if err != nil {
		return WorkerReceipt{}, err
	}
	defaults, frozen := input.Run.WorkerConfigurations[input.Effect.Boundary.NodeID]
	if !frozen {
		return WorkerReceipt{}, status_error.WorkflowStateInvalid
	}
	configuration := execution.ExecutionConfiguration{Resolved: true, RuntimeID: defaults.RuntimeID, AccountID: defaults.AccountID, Model: defaults.Model, Reasoning: defaults.Reasoning, PermissionLevel: defaults.PermissionLevel}
	schema, _ := options["schema"].(map[string]any)
	tools := []execution.ToolCapability{}
	if len(schema) > 0 {
		tools = append(tools, execution.ToolCapability{ID: "workflow.result", Name: "工作流结果", Bindings: input.ResultBindings})
	}
	if err := a.Executions.ValidateConfiguration(ctx, configuration); err != nil {
		return WorkerReceipt{}, err
	}
	domains := []string{"memory", "interaction"}
	scope := input.Run.ScopeBinding
	accepted, err := a.Executions.Start(ctx, execution.WorkIntent{AutomaticRunID: input.Run.AutomaticRunID, ExecutionID: input.Effect.ExecutionID, ClientRequestID: input.Effect.ID, Chat: execution.ChatTarget{Create: &execution.CreateChatSpec{HiddenFromWakerHistory: true, WakerID: wakerID, WorkingDirectory: scope.WorkingDirectory}}, Input: execution.WorkInput{Actor: "workflow", Content: prompt}, Options: execution.WorkOptions{ConfigurationResolved: true, RuntimeID: configuration.RuntimeID, AccountID: configuration.AccountID, Model: configuration.Model, Reasoning: configuration.Reasoning, PermissionLevel: defaults.PermissionLevel, WorkspaceReadOnly: defaults.WorkspaceReadOnly, SessionPolicy: constants.RuntimeSessionPolicyNew, ToolCapabilities: tools, AllowedConnectorIDs: allowedConnectors, AllowedAgentDomains: &domains}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch})
	if err != nil {
		return WorkerReceipt{}, err
	}
	return WorkerReceipt{ChatID: accepted.Chat.ID, ExecutionID: accepted.Execution.ID, Status: accepted.Execution.Status}, nil
}

func (a CoreWorkerAdapter) Inspect(ctx context.Context, effect models.WorkflowEffect) (WorkerReceipt, error) {
	chatID := effect.ChatID
	if chatID == "" {
		args := effect.Boundary.Arguments
		if len(args) != 2 {
			return WorkerReceipt{}, status_error.WorkflowStateInvalid
		}
		options, _ := args[1].Data.(map[string]any)
		resolve, _ := options["resolve"].(map[string]any)
		wakerID, _ := resolve["wakerId"].(string)
		chatID = requestidempotency.Identity{ID: effect.ID}.ResourceID("chat", wakerID)
	}
	state, err := a.Chats.ReadInitialExecution(ctx, chatID)
	if err != nil {
		return WorkerReceipt{}, err
	}
	if state.CreateRequestID != effect.ID || state.InitialExecution == nil || state.InitialExecution.ID != effect.ExecutionID {
		return WorkerReceipt{}, status_error.WorkflowStateConflict
	}
	receipt := WorkerReceipt{ChatID: chatID, ExecutionID: state.InitialExecution.ID, Status: state.InitialExecution.Status, ErrorCode: state.InitialExecution.ErrorCode}
	if receipt.Status == constants.ExecutionStatusCompleted {
		before := int64(0)
		for {
			page, err := a.Chats.ReadMessageHistory(ctx, chatID, before, 100)
			if err != nil {
				return receipt, err
			}
			// 按稳定游标读取首次 Execution；不依赖最后一百条或续聊中的最新消息。
			for index := len(page.Resources) - 1; index >= 0; index-- {
				message := page.Resources[index]
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
			if receipt.FinalPresent || !page.HasMore || page.NextBeforeSequence <= 0 {
				break
			}
			before = page.NextBeforeSequence
		}

	}
	return receipt, nil
}
func (a CoreWorkerAdapter) Cancel(ctx context.Context, effect models.WorkflowEffect) error {
	if effect.ChatID == "" {
		resolve, _ := workflowstate.BoundaryOptions(effect.Boundary)["resolve"].(map[string]any)
		wakerID, _ := resolve["wakerId"].(string)
		effect.ChatID = requestidempotency.Identity{ID: effect.ID}.ResourceID("chat", wakerID)
	}
	return a.Executions.CancelAndWait(ctx, effect.ChatID)
}

// ValidateAdmission 在接纳事务之外检查真实运行配置与严格工具策略。
func (a CoreWorkerAdapter) ValidateAdmission(ctx context.Context, options map[string]any, defaults models.WorkflowExecutionDefaults) error {
	_, err := a.ResolveAdmission(ctx, options, defaults, false)
	return err
}

// ResolveAdmission 通过 Execution 共用解析边界冻结节点覆盖及有效默认选择。
func (a CoreWorkerAdapter) ResolveAdmission(ctx context.Context, options map[string]any, defaults models.WorkflowExecutionDefaults, resolved bool) (models.WorkflowExecutionDefaults, error) {
	defaults = workerConfiguration(options, defaults)
	if !constants.SelectablePermissionLevel(defaults.PermissionLevel) {
		return defaults, status_error.BadRequest
	}
	config := execution.ExecutionConfiguration{Resolved: resolved, RuntimeID: defaults.RuntimeID, AccountID: defaults.AccountID, Model: defaults.Model, Reasoning: defaults.Reasoning, PermissionLevel: defaults.PermissionLevel}
	if schema, _ := options["schema"].(map[string]any); len(schema) > 0 {
		config.RequiredConnectorTransport = constants.RuntimeConnectorTransportStreamableHTTP
	}
	if _, err := workerConnectorAllowlist(options); err != nil {
		return defaults, err
	}
	configuration, err := a.Executions.ResolveConfiguration(ctx, config)
	if err != nil {
		return defaults, err
	}
	defaults.RuntimeID, defaults.AccountID, defaults.Model, defaults.Reasoning = configuration.RuntimeID, configuration.AccountID, configuration.Model, configuration.Reasoning
	return defaults, nil
}

func workerConfiguration(options map[string]any, defaults models.WorkflowExecutionDefaults) models.WorkflowExecutionDefaults {
	resolve, _ := options["resolve"].(map[string]any)
	take := func(key, fallback string) string {
		if value, ok := resolve[key].(string); ok && value != "" {
			return value
		}
		return fallback
	}
	defaults.RuntimeID = take("runtimeId", defaults.RuntimeID)
	defaults.AccountID = take("accountId", defaults.AccountID)
	defaults.Model = take("model", defaults.Model)
	defaults.Reasoning = take("reasoning", defaults.Reasoning)
	defaults.PermissionLevel = constants.PermissionLevel(take("permissionLevel", string(defaults.PermissionLevel)))
	return defaults
}

func workerConnectorAllowlist(options map[string]any) (*[]string, error) {
	policy, _ := options["toolPolicy"].(map[string]any)
	native, _ := policy["nativeTools"].(map[string]any)
	// ACP 尚无逐原生工具的强制隔离端口，严格要求继续拒绝，不能降级为提示词。
	if native["mode"] == "allowlist" {
		return nil, status_error.WorkflowCapabilityUnsupported.WithField("capability", "toolPolicy.nativeTools.allowlist")
	}
	connectors, _ := policy["connectors"].(map[string]any)
	if connectors["mode"] != "allowlist" {
		return nil, nil
	}
	ids, err := json.Marshal(connectors["connectorIds"])
	if err != nil {
		return nil, status_error.WorkflowResourceInvalid
	}
	allowed := []string{}
	if json.Unmarshal(ids, &allowed) != nil {
		return nil, status_error.WorkflowResourceInvalid
	}
	return &allowed, nil
}

// prepareWorkerRetry 保留旧收据，只在实际 Execution 已终止且声明只读重试时创建新 outbox。
func prepareWorkerRetry(state *controllers.WorkflowRunState, fiberIndex int, previous models.WorkflowEffect) bool {
	if previous.Boundary.Primitive != "worker" || previous.Status != constants.WorkflowStepStatusFailed || previous.SkipRequested || !state.Run.Defaults.WorkspaceReadOnly {
		return false
	}
	if previous.ExecutionStatus != constants.ExecutionStatusFailed && previous.ExecutionStatus != constants.ExecutionStatusCompleted {
		return false
	}
	options := workflowstate.BoundaryOptions(previous.Boundary)
	retry, _ := options["retry"].(map[string]any)
	maximum, _ := retry["maxAttempts"].(float64)
	if retry["safety"] != "pure" || previous.AttemptGeneration >= int64(maximum) {
		return false
	}
	token := util.New() + util.New()
	next := models.WorkflowEffect{ID: util.New(), RunID: previous.RunID, FiberID: previous.FiberID, InstanceID: previous.InstanceID, Boundary: previous.Boundary, Status: constants.WorkflowStepStatusPrepared, AttemptGeneration: previous.AttemptGeneration + 1, ExecutionID: util.New(), AuthToken: token, TokenDigest: workflowdsl.Digest([]byte(token))}
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
				s.workerCancellationUnknown(ctx, effect, err)
			}
			return
		}
		s.finishEffect(ctx, effect, WorkerReceipt{ChatID: effect.ChatID, ExecutionID: effect.ExecutionID, Status: constants.ExecutionStatusCanceled}, status_error.WorkflowCancelled)
		return
	}
	var receipt WorkerReceipt
	if effect.Status == constants.WorkflowStepStatusPrepared {
		receipt, err = s.worker.Dispatch(ctx, WorkerDispatch{Run: run, Effect: effect, ResultBindings: s.AgentBindings(effect.ID, effect.AuthToken, constants.WorkflowCapabilityScopeResult)})
	} else {
		receipt, err = s.worker.Inspect(ctx, effect)
	}
	if ctx.Err() != nil {
		if s.runner != nil && s.runner.ctx.Err() == nil {
			cancelErr := s.worker.Cancel(s.runner.ctx, effect)
			if cancelErr != nil {
				s.workerCancellationUnknown(s.runner.ctx, effect, cancelErr)
				return
			}
			receipt.Status = constants.ExecutionStatusCanceled
			s.finishEffect(s.runner.ctx, effect, receipt, status_error.WorkflowCancelled)
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
			s.workerCancellationUnknown(ctx, effect, err)
			return
		}
		if effect.Status != constants.WorkflowStepStatusPrepared {
			// 已运行的首次执行记录消失也不能证明外部工作没有发生。
			s.workerCancellationUnknown(ctx, effect, inspectErr)
			return
		}
	}
	s.finishEffect(ctx, effect, receipt, err)
}

func (s *Service) workerCancellationUnknown(ctx context.Context, effect models.WorkflowEffect, cause error) {
	// 尽力停止可能已经启动的执行；停止失败也只记录证据，不再交给用户反复点击停止。
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.worker.Cancel(stopCtx, effect); err != nil {
		logx.Errorf("stop uncertain workflow worker failed: effect_id=%s error=%v", effect.ID, err)
	}
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
	err := s.state.MutateEffect(ctx, effect.RunID, effect.ID, func(ctx context.Context, state *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
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
			current.Failure = workflowstate.FailureValue(code)
		default:
			return status_error.WorkflowStateInvalid
		}
		return nil
	})
	if err != nil && ctx.Err() == nil {
		logx.Errorf("commit workflow effect receipt failed: run_id=%s effect_id=%s error=%v", effect.RunID, effect.ID, err)
	}
}
