package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/mcpclient"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowfunction "github.com/JieWaZi/NeuxBot/server/internal/workflow/function"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// validateChildChain 拒绝同定义同版本的循环调用，并限制损坏父链的遍历。
func (s *Service) validateChildChain(ctx context.Context, parent models.WorkflowRun, target models.WorkflowRevision) error {
	for depth := 0; ; depth++ {
		if depth > constants.WorkflowMaxChildDepth {
			return status_error.WorkflowBudgetExceeded.WithField("budget", "child_depth")
		}
		if parent.DefinitionID == target.DefinitionID && parent.RevisionID == target.ID {
			return status_error.WorkflowBudgetExceeded.WithField("budget", "child_cycle")
		}
		if parent.ParentRunID == "" {
			return nil
		}
		ancestor, err := s.runs.GetRun(ctx, parent.ParentRunID)
		if err != nil {
			return err
		}
		parent = ancestor
	}
}

func (s *Service) dispatchChildOrAction(ctx context.Context, run models.WorkflowRun, effect models.WorkflowEffect, cancelling bool) {
	switch effect.Boundary.Primitive {
	case "workflow", "startWorkflow", "joinWorkflow":
		s.dispatchChild(ctx, run, effect, cancelling)
	case "functionTask", "action":
		s.dispatchDeterministic(ctx, run, effect, cancelling)
	default:
		s.finishEffect(ctx, effect, WorkerReceipt{}, status_error.WorkflowCapabilityUnsupported)
	}
}
func (s *Service) dispatchChild(ctx context.Context, run models.WorkflowRun, effect models.WorkflowEffect, cancelling bool) {
	args := effect.Boundary.Arguments
	if len(args) == 0 {
		s.finishEffect(ctx, effect, WorkerReceipt{}, status_error.WorkflowStateInvalid)
		return
	}
	var child models.WorkflowRun
	var err error
	if effect.Boundary.Primitive == "joinWorkflow" {
		if args[0].Kind != constants.WorkflowSlotChildHandle {
			s.finishEffect(ctx, effect, WorkerReceipt{}, status_error.WorkflowStateInvalid)
			return
		}
		handle, _ := args[0].Data.(map[string]any)
		id, _ := handle["runId"].(string)
		child, err = s.runs.GetRun(ctx, id)
		if err == nil && child.ParentRunID != run.ID {
			err = status_error.WorkflowStateConflict
		}
	} else if effect.ChildRunID != "" {
		child, err = s.runs.GetRun(ctx, effect.ChildRunID)
	} else {
		target, _ := args[0].Data.(string)
		options := workflowstate.BoundaryOptions(effect.Boundary)
		revisionID, _ := options["revisionId"].(string)
		mode, _ := options["mode"].(string)
		joined := mode != "detached"
		child, err = s.StartRun(ctx, StartRunCommand{AutomaticRunID: run.AutomaticRunID, DefinitionID: target, RequestID: effect.ID, RevisionID: revisionID, Args: options["args"], Defaults: run.Defaults, ParentRunID: run.ID, Joined: joined, Depth: run.Depth + 1, TriggerSource: "workflow"})
	}
	if err != nil {
		s.finishEffect(ctx, effect, WorkerReceipt{}, err)
		return
	}
	timedOut := effect.Boundary.Primitive != "startWorkflow" && effect.DeadlineAt <= time.Now().UnixMilli() && (child.CompletedAt == nil || *child.CompletedAt > effect.DeadlineAt)
	if (cancelling || timedOut) && child.Joined && !workflowstate.TerminalRun(child.Status) {
		if err = s.CancelRun(ctx, child.ID); err != nil {
			s.finishEffect(ctx, effect, WorkerReceipt{}, err)
			return
		}
		if !workflowstate.TerminalRun(child.Status) {
			return
		}
	}
	err = s.state.MutateEffect(ctx, run.ID, effect.ID, func(ctx context.Context, _ *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
		if workflowstate.StepTerminal(current.Status) {
			return nil
		}
		current.ChildRunID = child.ID
		if effect.Boundary.Primitive == "startWorkflow" {
			value := workflowdsl.Value{Kind: constants.WorkflowSlotChildHandle, Data: map[string]any{"runId": child.ID, "joined": child.Joined}}
			current.Result = &value
			current.Status = constants.WorkflowStepStatusSucceeded
			return nil
		}
		if timedOut {
			current.Status = constants.WorkflowStepStatusFailed
			current.Failure = workflowstate.FailureValue("CHILD_TIMEOUT")
			return nil
		}
		switch child.Status {
		case constants.WorkflowRunStatusSucceeded:
			value := workflowdsl.JSONValue(map[string]any{"runId": child.ID, "revisionId": child.RevisionID, "status": "succeeded", "result": child.Result})
			current.Result = &value
			current.Status = constants.WorkflowStepStatusSucceeded
		case constants.WorkflowRunStatusCancelled, constants.WorkflowRunStatusFailed:
			current.Status = constants.WorkflowStepStatusFailed
			current.Failure = child.Failure
			if current.Failure == nil {
				current.Failure = workflowstate.FailureValue("CHILD_CANCELLED")
			}
		default:
			current.Status = constants.WorkflowStepStatusRunning
		}
		return nil
	})
	if err != nil && ctx.Err() == nil {
		s.finishEffect(ctx, effect, WorkerReceipt{}, err)
	}
}

// childResultObserved 表示失败已交给源码的 await/catch，而非无 join 的跟随任务。
func childResultObserved(effects []models.WorkflowEffect, childID string) bool {
	for _, effect := range effects {
		if effect.ChildRunID == childID && workflowstate.StepTerminal(effect.Status) && (effect.Boundary.Primitive == "workflow" || effect.Boundary.Primitive == "joinWorkflow") {
			return true
		}
	}
	return false
}

func (s *Service) dispatchDeterministic(ctx context.Context, run models.WorkflowRun, effect models.WorkflowEffect, cancelling bool) {
	revision, err := s.store.GetRevision(ctx, run.DefinitionID, run.RevisionID)
	if err != nil {
		s.completeDeterministic(ctx, effect, nil, err)
		return
	}
	options := workflowstate.BoundaryOptions(effect.Boundary)
	safe := effect.Boundary.Primitive == "functionTask" && options["language"] == "javascript"
	if effect.Boundary.Primitive == "action" {
		declaration, _ := workflowstate.EffectAction(revision.Manifest, effect)
		method, _ := options["method"].(string)
		safe = declaration.Type == "builtin" || declaration.Type == "http" && (method == "" || method == "GET" || method == "HEAD" || method == "OPTIONS")
	}
	// 已派发且没有回执的非幂等效果在重启后必须对账。
	if effect.Status == constants.WorkflowStepStatusRunning && !safe {
		s.recordUnknownDeterministic(ctx, effect, nil, nil)
		return
	}
	if cancelling {
		s.completeDeterministic(ctx, effect, nil, status_error.WorkflowCancelled)
		return
	}
	err = s.state.MutateEffect(ctx, run.ID, effect.ID, func(ctx context.Context, state *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
		if state.Run.Status == constants.WorkflowRunStatusCancelling || workflowstate.StepTerminal(current.Status) {
			return status_error.WorkflowStateConflict
		}
		for _, operation := range state.Operations {
			if operation.EffectID == current.ID && (operation.Status != constants.WorkflowStepStatusAnswered || !operation.Allowed) {
				return status_error.WorkflowStateConflict
			}
		}
		current.Status = constants.WorkflowStepStatusRunning
		return nil
	})
	if err != nil {
		return
	}
	operationCtx, cancel := context.WithDeadline(ctx, time.UnixMilli(effect.DeadlineAt))
	defer cancel()
	var value any
	var httpOutcomeUnknown bool
	var processLogs []workflowfunction.ProcessResult
	if effect.Boundary.Primitive == "functionTask" {
		language, _ := options["language"].(string)
		code, _ := options["code"].(string)
		result, process, failure := workflowfunction.Execute(operationCtx, workflowfunction.Request{RequestID: effect.ID, Code: code, Input: options["input"], StartedAt: run.StartedAt, DeadlineAt: effect.DeadlineAt}, language, s.pythonExecutable, run.ScopeBinding.WorkingDirectory)
		err = failure
		value = result.Data
		if process.Stdout != "" || process.Stderr != "" {
			processLogs = append(processLogs, process)
		}
		if err == nil {
			schema, _ := options["outputSchema"].(map[string]any)
			err = workflowdsl.ValidateJSON(schema, value)
		}
	} else {
		declaration, actionErr := workflowstate.EffectAction(revision.Manifest, effect)
		if actionErr != nil {
			err = actionErr
		} else {
			if err = workflowdsl.ValidateJSON(declaration.InputSchema, options["input"]); err == nil {
				switch declaration.Type {
				case "http":
					value, httpOutcomeUnknown, err = s.executeHTTP(operationCtx, run, declaration, options)
				case "script":
					value, err = s.executeScript(operationCtx, run, declaration, options)
				case "builtin":
					value, err = s.executeBuiltin(operationCtx, run, effect, revision, declaration, options)
				default:
					err = status_error.WorkflowCapabilityUnsupported
				}
			}
			if err == nil {
				result, _ := value.(map[string]any)
				err = workflowdsl.ValidateJSON(declaration.OutputSchema, result["data"])
				if err == nil {
					if schema, ok := options["schema"].(map[string]any); ok {
						err = workflowdsl.ValidateJSON(schema, result["data"])
					}
				}
			}
		}
	}
	// 取消节点后，收据仍通过存活的服务生命周期提交。
	receiptCtx := ctx
	if ctx.Err() != nil && s.runner != nil && s.runner.ctx.Err() == nil {
		receiptCtx = s.runner.ctx
	}
	if receiptCtx.Err() == nil {
		if httpOutcomeUnknown {
			s.recordUnknownDeterministic(receiptCtx, effect, value, err)
		} else {
			s.completeDeterministic(receiptCtx, effect, value, err, processLogs...)
		}
	}
}

// recordUnknownDeterministic 保留未取得可靠结果的外部操作，运行自动失败且禁止盲目重发。
func (s *Service) recordUnknownDeterministic(ctx context.Context, effect models.WorkflowEffect, result any, cause error) {
	err := s.state.MutateEffect(ctx, effect.RunID, effect.ID, func(_ context.Context, state *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
		if workflowstate.StepTerminal(current.Status) || current.Status == constants.WorkflowStepStatusUnknown {
			return nil
		}
		failure := map[string]any{"code": "EFFECT_OUTCOME_UNKNOWN"}
		if cause != nil {
			failure["cause"] = failureFromError(cause, &current.Boundary, result).Data
		}
		if state.Run.Status == constants.WorkflowRunStatusCancelling {
			failure[constants.WorkflowFailureStopRequestedField] = true
		}
		current.Status = constants.WorkflowStepStatusUnknown
		value := workflowdsl.JSONValue(failure)
		current.Failure = &value
		return nil
	})
	if err != nil && ctx.Err() == nil {
		logx.Errorf("record unknown workflow action failed: effect_id=%s error=%v", effect.ID, err)
	}
}

func (s *Service) completeDeterministic(ctx context.Context, effect models.WorkflowEffect, result any, failure error, logs ...workflowfunction.ProcessResult) {
	err := s.state.MutateEffect(ctx, effect.RunID, effect.ID, func(ctx context.Context, state *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
		if current.Status == constants.WorkflowStepStatusUnknown {
			return nil
		}
		if workflowstate.StepTerminal(current.Status) {
			return nil
		}
		encoded, err := json.Marshal(result)
		if err != nil || len(encoded) > constants.WorkflowOutputLimitBytes {
			current.Status = constants.WorkflowStepStatusFailed
			current.Failure = workflowstate.FailureValue("OUTPUT_LIMIT")
			return nil
		}
		for _, process := range logs {
			if _, err := s.store.AppendEvent(ctx, models.WorkflowEvent{StreamID: effect.RunID, EventID: effect.ID + "-log", Type: constants.WorkflowLogEvent, EntityID: effect.RunID, Payload: processLogPayload(effect.ID, process)}); err != nil {
				return err
			}
		}
		value := workflowdsl.JSONValue(result)
		current.Result = &value
		if failure != nil {
			current.Status = constants.WorkflowStepStatusFailed
			current.Failure = failureFromError(failure, &current.Boundary, result)
			return nil
		}
		current.Status = constants.WorkflowStepStatusSucceeded
		return nil
	})
	if err != nil && ctx.Err() == nil {
		logx.Errorf("commit deterministic effect failed: effect_id=%s error=%v", effect.ID, err)
	}
}
func processLogPayload(effectID string, process workflowfunction.ProcessResult) map[string]any {
	payload := map[string]any{"effectId": effectID, "stdout": process.Stdout, "stderr": process.Stderr, "logsTruncated": process.Truncated}
	for {
		encoded, _ := json.Marshal(payload)
		if len(encoded) <= constants.WorkflowLogLimitBytes {
			return payload
		}
		process.Stdout = shrinkLog(process.Stdout)
		process.Stderr = shrinkLog(process.Stderr)
		payload["stdout"], payload["stderr"], payload["logsTruncated"] = process.Stdout, process.Stderr, true
	}
}

func shrinkLog(value string) string {
	if len(value) <= 1 {
		return ""
	}
	return strings.ToValidUTF8(value[:len(value)/2], "")
}

func (s *Service) executeScript(ctx context.Context, run models.WorkflowRun, declaration workflowdsl.ActionDeclaration, options map[string]any) (any, error) {
	if run.ScopeBinding.WorkingDirectory == "" {
		return nil, status_error.WorkflowResourceInvalid.WithField("field", "scope.workingDirectory")
	}
	directory, err := filepath.EvalSymlinks(run.ScopeBinding.WorkingDirectory)
	if err != nil {
		logx.Errorf("resolve action cwd failed: error=%v", err)
		return nil, status_error.WorkflowResourceInvalid
	}
	if directory != run.ScopeBinding.WorkingDirectory {
		return nil, status_error.WorkflowStateConflict.WithField("field", "scope.workingDirectory")
	}
	arguments := []string{}
	if values, ok := options["args"].([]any); ok {
		for _, value := range values {
			argument, ok := value.(string)
			if !ok {
				return nil, status_error.WorkflowSchemaInvalid
			}
			arguments = append(arguments, argument)
		}
	}
	environment := workflowfunction.SafeEnvironment()
	secrets := []string{}
	if values, ok := options["env"].(map[string]any); ok {
		for key, value := range values {
			entry, ok := value.(string)
			if !ok || !safeEnvironmentKey(key) {
				return nil, status_error.WorkflowSchemaInvalid
			}
			environment = append(environment, key+"="+entry)
		}
	}
	if values, ok := options["secretEnv"].(map[string]any); ok {
		for key, entry := range values {
			if !safeEnvironmentKey(key) {
				return nil, status_error.WorkflowSchemaInvalid
			}
			reference, _ := entry.(map[string]any)
			ref, _ := reference["secretRef"].(string)
			value, err := s.resolveSecret(ctx, run.DefinitionID, ref)
			if err != nil {
				return nil, err
			}
			secrets = append(secrets, value)
			environment = append(environment, key+"="+value)
		}
	}
	started := time.Now()
	process, err := workflowfunction.Command(ctx, declaration.Executable, arguments, directory, environment, nil)
	for _, secret := range secrets {
		if secret != "" {
			process.Stdout = strings.ReplaceAll(process.Stdout, secret, "[redacted]")
			process.Stderr = strings.ReplaceAll(process.Stderr, secret, "[redacted]")
		}
	}
	data := any(process.Stdout)
	if len(declaration.OutputSchema) > 0 || options["schema"] != nil {
		if decodeErr := json.Unmarshal([]byte(process.Stdout), &data); decodeErr != nil && err == nil {
			logx.Errorf("decode action result failed: action_id=%s error=%v", declaration.ID, decodeErr)
			err = status_error.WorkflowSchemaInvalid
		}
	}
	return map[string]any{"data": data, "durationMs": time.Since(started).Milliseconds(), "process": process}, err
}
func safeEnvironmentKey(key string) bool {
	if key == "" || strings.ContainsAny(key, "=\x00") {
		return false
	}
	switch strings.ToUpper(key) {
	case "PATH", "LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH", "PYTHONPATH", "NODE_OPTIONS":
		return false
	}
	return true
}

func (s *Service) resolveSecret(ctx context.Context, definitionID, reference string) (string, error) {
	// 仅映射创作已启用连接器中一个敏感项，不读取任意账号凭据。
	parts := strings.Split(reference, ":")
	if len(parts) != 4 || parts[0] != "connector" || parts[2] != "secret" || s.connectors == nil {
		return "", status_error.WorkflowResourceInvalid.WithField("field", "secretRef")
	}
	definition, err := s.store.GetDefinition(ctx, definitionID)
	if err != nil {
		return "", err
	}
	enabled, err := s.connectors.ListEnabled(ctx, definition.CreationWakerID)
	if err != nil {
		return "", err
	}
	for _, connector := range enabled {
		if connector.ID != parts[1] {
			continue
		}
		for _, secret := range connectorSecrets(connector) {
			if secret.ID == parts[3] && secret.Configured {
				return secret.Value, nil
			}
		}
	}
	return "", status_error.WorkflowResourceInvalid.WithField("field", "secretRef")
}

func (s *Service) executeBuiltin(ctx context.Context, run models.WorkflowRun, effect models.WorkflowEffect, revision models.WorkflowRevision, declaration workflowdsl.ActionDeclaration, options map[string]any) (any, error) {
	started := time.Now()
	programID := revision.Bundle.ActionPrograms[declaration.Handler]
	if programID == "" {
		return nil, status_error.WorkflowStateInvalid
	}
	frame := effect.BuiltinState
	if frame == nil {
		value, err := workflowdsl.NewTaskMachine(revision.Bundle, workflowdsl.Value{Kind: constants.WorkflowSlotFunction, ProgramID: programID}, []workflowdsl.Value{workflowdsl.JSONValue(map[string]any{"flowContext": run.Context}), workflowdsl.JSONValue(options["input"])}, run.StartedAt)
		if err != nil {
			return nil, err
		}
		frame = &value
		// ctx 的整个根 binding 只读，不能替换批准的上下文或能力。
		for name, binding := range frame.Scopes[1] {
			if name != revision.Bundle.Programs[programID].Parameters[0].Name {
				continue
			}
			binding.ReadOnly = true
			frame.Scopes[1][name] = binding
		}
	}
	for !frame.Done {
		boundary, err := workflowdsl.Advance(ctx, revision.Bundle, frame, constants.WorkflowCheckpointSteps)
		if err != nil {
			return nil, err
		}
		if boundary == nil {
			if err = s.state.MutateEffect(ctx, run.ID, effect.ID, func(ctx context.Context, _ *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
				current.BuiltinState = frame
				return nil
			}); err != nil {
				return nil, err
			}
			continue
		}
		if boundary.Primitive == "log" {
			var nextFrame *workflowdsl.MachineState
			if err = s.state.MutateEffect(ctx, run.ID, effect.ID, func(ctx context.Context, _ *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
				data := any(nil)
				if len(boundary.Arguments) > 0 {
					data = boundary.Arguments[0].Data
				}
				encoded, err := json.Marshal(data)
				if err != nil || len(encoded) > constants.WorkflowLogLimitBytes-256 {
					return status_error.WorkflowBudgetExceeded.WithField("budget", "log_bytes")
				}
				candidate, err := applyBuiltinEffectToCopy(revision.Bundle, frame, *boundary, workflowdsl.JSONValue(nil), nil)
				if err != nil {
					return err
				}
				nextFrame = candidate
				current.BuiltinState = candidate
				_, err = s.store.AppendEvent(ctx, models.WorkflowEvent{StreamID: run.ID, EventID: util.New(), Type: constants.WorkflowLogEvent, EntityID: run.ID, Payload: map[string]any{"effectId": effect.ID, "value": data}})
				return err
			}); err != nil {
				return nil, err
			}
			frame = nextFrame
			continue
		}
		if boundary.Primitive != "callTool" || len(boundary.Arguments) != 1 {
			return nil, status_error.WorkflowCapabilityUnsupported
		}
		input, ok := boundary.Arguments[0].Data.(map[string]any)
		if !ok {
			return nil, status_error.WorkflowSchemaInvalid
		}
		callID, _ := input["callId"].(string)
		connectorID, _ := input["connectorId"].(string)
		tool, _ := input["toolName"].(string)
		if !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,63}$`).MatchString(callID) || connectorID == "" || tool == "" || len(input) != 4 {
			return nil, status_error.WorkflowSchemaInvalid.WithField("field", "callId")
		}
		allowed := false
		for _, id := range declaration.ConnectorIDs {
			if id == connectorID {
				allowed = true
			}
		}
		if !allowed {
			return nil, status_error.WorkflowCapabilityUnsupported
		}
		if connectorID == constants.IMBuiltinConnectorID && tool == constants.IMBuiltinToolName {
			imInput, _ := input["input"].(map[string]any)
			if imInput["action"] == constants.IMActionCreateDelivery {
				dependencies, err := workflowdsl.IMTargetDependencies(revision.Manifest, revision.Bundle)
				if err != nil {
					return nil, err
				}
				declared := false
				for _, dependency := range dependencies {
					declared = declared || dependency.ActionID == declaration.ID && dependency.TargetID == imInput["targetId"]
				}
				if !declared {
					targetID, _ := imInput["targetId"].(string)
					return nil, status_error.WorkflowEffectFailed.WithField("code", "TOOL_FAILED").WithField("reason", status_error.WorkflowIMTargetNotDeclared.Key()).WithField("hint", constants.IMRecoveryDeclareTarget).WithField("targetId", targetID).WithField("action", constants.IMActionCreateDelivery)
				}
			}
		}
		var call models.WorkflowActionCall
		existing := false
		unknown := false
		digest := workflowstate.Digest(input)
		err = s.state.MutateEffect(ctx, run.ID, effect.ID, func(ctx context.Context, state *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
			call, existing, unknown = models.WorkflowActionCall{}, false, false
			for index := range state.ActionCalls {
				item := &state.ActionCalls[index]
				if item.EffectID != effect.ID || item.CallID != callID {
					continue
				}
				if item.PayloadDigest != digest {
					return status_error.WorkflowRequestConflict
				}
				call = *item
				existing = true
				if (item.Status == constants.WorkflowStepStatusRunning || item.Status == constants.WorkflowStepStatusUnknown) && connectorID == constants.IMBuiltinConnectorID && s.imCall != nil {
					existing = false
					return nil
				}
				if item.Status == constants.WorkflowStepStatusRunning || item.Status == constants.WorkflowStepStatusUnknown {
					item.Status = constants.WorkflowStepStatusUnknown
					current.Status = constants.WorkflowStepStatusUnknown
					unknown = true
				}
				return nil
			}
			call = models.WorkflowActionCall{ID: util.New(), RunID: run.ID, EffectID: effect.ID, CallID: callID, PayloadDigest: digest, Status: constants.WorkflowStepStatusRunning, ConnectorID: connectorID, ToolName: tool, Input: input["input"]}
			state.ActionCalls = append(state.ActionCalls, call)
			current.BuiltinState = frame
			return nil
		})
		if err != nil {
			return nil, err
		}
		if unknown {
			return nil, status_error.WorkflowStateConflict
		}
		result := call.Result
		failure := call.Failure
		if !existing {
			var connector *models.MCPConnector
			if connectorID != constants.IMBuiltinConnectorID {
				if s.connectors == nil {
					return nil, status_error.WorkflowCapabilityUnsupported
				}
				definition, err := s.store.GetDefinition(ctx, run.DefinitionID)
				if err != nil {
					return nil, err
				}
				connectors, err := s.connectors.ListEnabled(ctx, definition.CreationWakerID)
				if err != nil {
					return nil, err
				}
				for index := range connectors {
					if connectors[index].ID == connectorID {
						connector = &connectors[index]
						break
					}
				}
				if connector == nil {
					return nil, status_error.WorkflowResourceInvalid.WithField("field", "connectorId")
				}
			}
			var data any
			if connectorID == constants.IMBuiltinConnectorID {
				if s.imCall == nil {
					return nil, status_error.WorkflowCapabilityUnsupported
				}
				data, err = s.imCall(ctx, run, call)
			} else {
				data, err = mcpclient.Call(ctx, *connector, tool, input["input"])
			}
			if ctx.Err() != nil {
				// 应用关闭或调用取消没有提供外部工具终态，保留已提交的 running 回执供恢复对账。
				if s.runner != nil && s.runner.ctx.Err() == nil {
					if commitErr := s.markUnknownToolCall(s.runner.ctx, run.ID, effect.ID, call.ID, failureFromError(ctx.Err(), boundary, nil)); commitErr != nil {
						return nil, commitErr
					}
				}
				return nil, ctx.Err()
			}
			if err != nil {
				received := failureFromError(err, boundary, data)
				payload, _ := received.Data.(map[string]any)
				if payload["code"] != "TOOL_FAILED" {
					// 传输失败不是工具明确返回失败；可能已经产生外部效果，不能重发。
					if commitErr := s.markUnknownToolCall(ctx, run.ID, effect.ID, call.ID, received); commitErr != nil {
						return nil, commitErr
					}
					return nil, err
				}
			}
			if data != nil {
				value := workflowdsl.JSONValue(data)
				result = &value
			}
			if err != nil {
				failure = failureFromError(err, boundary, data)
			}
		}
		var nextFrame *workflowdsl.MachineState
		err = s.state.MutateEffect(ctx, run.ID, effect.ID, func(ctx context.Context, state *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
			for index := range state.ActionCalls {
				item := &state.ActionCalls[index]
				if item.ID != call.ID {
					continue
				}
				item.Result, item.Failure = result, failure
				item.Status = constants.WorkflowStepStatusSucceeded
				if failure != nil {
					item.Status = constants.WorkflowStepStatusFailed
				}
			}
			value := workflowdsl.JSONValue(nil)
			if result != nil {
				value = *result
			}
			candidate, err := applyBuiltinEffectToCopy(revision.Bundle, frame, *boundary, value, failure)
			if err != nil {
				return err
			}
			nextFrame = candidate
			current.BuiltinState = candidate
			return nil
		})
		if err != nil {
			return nil, err
		}
		frame = nextFrame
	}
	if frame.Failure != nil {
		data, _ := frame.Failure.JSON()
		return nil, status_error.WorkflowEffectFailed.WithParams(map[string]any{"failure": data})
	}
	data, err := frame.Result.JSON()
	if err != nil {
		return nil, err
	}
	return map[string]any{"data": data, "durationMs": time.Since(started).Milliseconds()}, nil
}

// applyBuiltinEffectToCopy 隔离每次事务尝试的帧推进，回滚不会污染下一次尝试。
func applyBuiltinEffectToCopy(bundle workflowdsl.ProgramBundle, frame *workflowdsl.MachineState, boundary workflowdsl.EffectBoundary, value workflowdsl.Value, failure *workflowdsl.Value) (*workflowdsl.MachineState, error) {
	encoded, err := json.Marshal(frame)
	if err != nil {
		logx.Errorf("encode workflow builtin frame copy failed: error=%v", err)
		return nil, status_error.WorkflowStateInvalid
	}
	var next workflowdsl.MachineState
	if err := json.Unmarshal(encoded, &next); err != nil {
		logx.Errorf("decode workflow builtin frame copy failed: error=%v", err)
		return nil, status_error.WorkflowStateInvalid
	}
	if err := workflowdsl.ApplyEffect(bundle, &next, boundary, value, failure); err != nil {
		return nil, err
	}
	return &next, nil
}

func (s *Service) markUnknownToolCall(ctx context.Context, runID, effectID, callID string, received *workflowdsl.Value) error {
	return s.state.MutateEffect(ctx, runID, effectID, func(_ context.Context, state *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
		current.Status, current.Failure = constants.WorkflowStepStatusUnknown, received
		for index := range state.ActionCalls {
			if state.ActionCalls[index].ID == callID {
				state.ActionCalls[index].Status, state.ActionCalls[index].Failure = constants.WorkflowStepStatusUnknown, received
			}
		}
		return nil
	})
}

var stableFailureCode = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

func failureFromError(err error, boundary *workflowdsl.EffectBoundary, result any) *workflowdsl.Value {
	key := workflowstate.ErrorKey(err)
	data := map[string]any{"code": key, "kind": key}
	var detail *statuserror.StatusErr
	if errors.As(err, &detail) {
		for field, value := range detail.Fields {
			name, ok := field.(string)
			if !ok {
				continue
			}
			if name == "code" {
				if stableFailureCode.MatchString(value) {
					data["code"] = value
				} else {
					data["reason"] = boundedFailureText(value)
				}
				continue
			}
			switch name {
			case "field", "reason", "capability", "budget", "operation", "jsonPointer", "hint", "targetId", "action":
				data[name] = boundedFailureText(value)
			}
		}
		for _, name := range []string{"reason", "hint", "targetId", "action"} {
			if value, ok := detail.Params[name].(string); ok {
				data[name] = boundedFailureText(value)
			}
		}
		if delivery, ok := detail.Params["delivery"].(map[string]any); ok {
			data["delivery"] = delivery
		}
		if cause, ok := detail.Params["cause"].(map[string]any); ok {
			data["cause"] = cause
		}
		if failure, ok := detail.Params["failure"].(map[string]any); ok {
			for field, value := range failure {
				data[field] = value
			}
			data["cause"] = failure
		}
	}
	if boundary != nil {
		data["sourceRange"] = boundary.Range
		data["nodeId"] = boundary.NodeID
	}
	if result != nil {
		if action, ok := result.(map[string]any); ok && (action["durationMs"] != nil || action["http"] != nil || action["process"] != nil) {
			for _, field := range []string{"data", "http", "process", "durationMs"} {
				if value, present := action[field]; present {
					data[field] = value
				}
			}
		} else {
			data["data"] = result
		}
	}
	value := workflowdsl.JSONValue(data)
	return &value
}

func boundedFailureText(value string) string {
	if len(value) <= 4096 {
		return value
	}
	value = value[:4096]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
