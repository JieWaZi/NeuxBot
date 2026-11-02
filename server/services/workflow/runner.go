package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Tick 推进一个运行的有界 Go 指令并提交检查点，外部派发始终在事务外。
func (s *Service) Tick(ctx context.Context, id string) error {
	_, err := s.tick(ctx, id)
	return err
}

// tick 将本次已经核验的效果和许可交还调度者，不在事务后重复读取完整聚合。
func (s *Service) tick(ctx context.Context, id string) (controllers.WorkflowRunState, error) {
	var confirmed controllers.WorkflowRunState
	s.state.Commands.Lock()
	defer s.state.Commands.Unlock()
	err := s.transactions.Transaction(ctx, func(ctx context.Context) error {
		state, err := s.state.OwnedRun(ctx, id)
		if err != nil {
			return err
		}
		defer func() { confirmed = state }()
		if workflowstate.TerminalRun(state.Run.Status) {
			return nil
		}
		revision, err := s.store.GetRevision(ctx, state.Run.DefinitionID, state.Run.RevisionID)
		if state.Run.Status != constants.WorkflowRunStatusCancelling {
			if err != nil {
				return s.abortRunState(ctx, state, err)
			}
			if err := verifyRevision(revision); err != nil {
				return s.abortRunState(ctx, state, err)
			}
		}
		now := time.Now().UnixMilli()
		if state.Run.DeadlineAt <= now && state.Run.Status != constants.WorkflowRunStatusCancelling {
			state.Run.Failure = workflowstate.FailureValue("RUN_TIMEOUT")
			state.Run.Status = constants.WorkflowRunStatusCancelling
		}
		beforeState := workflowstate.Digest(state)
		changed := false
		for index := range state.Humans {
			human := &state.Humans[index]
			if human.Status == constants.WorkflowStepStatusWaiting && human.DeadlineAt > 0 && human.DeadlineAt <= now {
				human.Status = constants.WorkflowStepStatusTimedOut
				human.Source = "timeout"
				human.Value = workflowstate.HumanFallback(human.Options)
				human.AnsweredAt = &now
				changed = true
			}
		}
		for index := range state.Effects {
			effect := &state.Effects[index]
			if effect.Failure != nil && effect.Boundary.Primitive == "askUser" && !workflowstate.StepTerminal(effect.Status) {
				effect.Status = constants.WorkflowStepStatusCancelled
				for humanIndex := range state.Humans {
					if state.Humans[humanIndex].EffectID == effect.ID {
						state.Humans[humanIndex].Status = constants.WorkflowStepStatusCancelled
					}
				}
				changed = true
			}
			if effect.Boundary.Primitive == "askUser" && effect.Status == constants.WorkflowStepStatusPrepared {
				for _, human := range state.Humans {
					if human.EffectID == effect.ID && human.Status != constants.WorkflowStepStatusWaiting {
						result := workflowdsl.JSONValue(map[string]any{"value": human.Value, "skipped": human.Source != "user", "source": human.Source, "requestId": human.ID})
						effect.Result = &result
						effect.Status = constants.WorkflowStepStatusSucceeded
						changed = true
					}
				}
			}
		}
		if state.Run.Status == constants.WorkflowRunStatusCancelling {
			waiting := false
			children, err := s.runs.ListJoinedChildren(ctx, state.Run.ID)
			if err != nil {
				return err
			}
			for _, child := range children {
				if !workflowstate.TerminalRun(child.Status) {
					waiting = true
				}
			}
			for index := range state.Effects {
				effect := &state.Effects[index]
				if workflowstate.StepTerminal(effect.Status) || effect.Status == constants.WorkflowStepStatusUnknown {
					continue
				}
				if effect.Status == constants.WorkflowStepStatusRunning || effect.Boundary.Primitive == "worker" && effect.ChatID != "" || s.state.EffectActive(effect.ID) {
					waiting = true
					continue
				}
				effect.Status = constants.WorkflowStepStatusCancelled
				effect.Failure = workflowstate.FailureValue("CANCELLED")
			}
			if !waiting {
				state.Run.Status = constants.WorkflowRunStatusCancelled
				if state.Run.Failure != nil {
					state.Run.Status = constants.WorkflowRunStatusFailed
				}
				state.Run.CompletedAt = &now
				for index := range state.Fibers {
					if !workflowstate.StepTerminal(state.Fibers[index].Status) {
						state.Fibers[index].Status = constants.WorkflowStepStatusCancelled
					}
				}
				for index := range state.Humans {
					if state.Humans[index].Status == constants.WorkflowStepStatusWaiting {
						state.Humans[index].Status = constants.WorkflowStepStatusCancelled
					}
				}
				for index := range state.Operations {
					if state.Operations[index].Status == constants.WorkflowStepStatusWaiting {
						state.Operations[index].Status = constants.WorkflowStepStatusCancelled
					}
				}
				for index := range state.Instances {
					if !workflowstate.StepTerminal(state.Instances[index].Status) {
						state.Instances[index].Status = constants.WorkflowStepStatusCancelled
						state.Instances[index].CompletedAt = &now
					}
				}
			}
			return s.state.CommitRun(ctx, state)
		}
		// 收据与当前 frame 一次提交；重启只读取已经提交的结果，不重复执行效果。
		for index := range state.Fibers {
			fiber := &state.Fibers[index]
			if fiber.Status != constants.WorkflowStepStatusWaiting || fiber.Boundary == nil {
				continue
			}
			for _, effect := range state.Effects {
				if effect.ID != fiber.WaitingID || !workflowstate.StepTerminal(effect.Status) {
					continue
				}
				if prepareWorkerRetry(&state, index, effect) {
					changed = true
					break
				}
				value := workflowdsl.JSONValue(nil)
				if effect.Result != nil {
					value = *effect.Result
				}
				if err := workflowdsl.ApplyEffect(revision.Bundle, &fiber.State, *fiber.Boundary, value, effect.Failure); err != nil {
					return err
				}
				fiber.Status = constants.WorkflowStepStatusReady
				fiber.Boundary = nil
				fiber.WaitingID = ""
				changed = true
			}
		}
		if err := advanceGroups(revision.Bundle, &state); err != nil {
			return s.abortRunState(ctx, state, err)
		}
		ready := 0
		for index := 0; index < len(state.Fibers) && ready < runConcurrency(state.Run); index++ {
			if state.Fibers[index].Status != constants.WorkflowStepStatusReady || !fiberAdmitted(state, index) {
				continue
			}
			ready++
			changed = true
			boundary, err := workflowdsl.Advance(ctx, revision.Bundle, &state.Fibers[index].State, constants.WorkflowCheckpointSteps)
			if err != nil {
				return s.abortRunState(ctx, state, err)
			}
			fiber := &state.Fibers[index]
			if fiber.State.Done {
				fiber.Status = constants.WorkflowStepStatusSucceeded
				if fiber.State.Failure != nil {
					fiber.Status = constants.WorkflowStepStatusFailed
				}
				continue
			}
			if boundary == nil {
				continue
			}
			fiber.Boundary = boundary
			switch boundary.Primitive {
			case "phase", "log":
				event := constants.WorkflowPhaseEvent
				if boundary.Primitive == "log" {
					event = constants.WorkflowLogEvent
				}
				data := any(nil)
				if len(boundary.Arguments) > 0 {
					data = boundary.Arguments[0].Data
				}
				encoded, _ := json.Marshal(data)
				if len(encoded) > constants.WorkflowLogLimitBytes {
					return s.abortRunState(ctx, state, status_error.WorkflowBudgetExceeded)
				}
				if _, err = s.store.AppendEvent(ctx, models.WorkflowEvent{StreamID: id, EventID: util.New(), Type: event, EntityID: id, Payload: map[string]any{"value": data, "fiberId": fiber.ID}}); err != nil {
					return err
				}
				if err = workflowdsl.ApplyEffect(revision.Bundle, &fiber.State, *boundary, workflowdsl.JSONValue(nil), nil); err != nil {
					return err
				}
				fiber.Boundary = nil
			case "parallel", "pipeline":
				if err := createGroup(revision.Bundle, &state, index, *boundary); err != nil {
					return err
				}
			default:
				if err := prepareEffect(revision.Bundle, revision.Manifest, &state, index, *boundary); err != nil {
					return s.abortRunState(ctx, state, err)
				}
			}
		}
		if err := advanceGroups(revision.Bundle, &state); err != nil {
			return s.abortRunState(ctx, state, err)
		}
		pureSteps, loopSteps := 0, 0
		for _, fiber := range state.Fibers {
			pureSteps += fiber.State.PureSteps
			loopSteps += fiber.State.LoopSteps
		}
		if pureSteps > constants.WorkflowMaxPureSteps || loopSteps > loopStepLimit(state.Run) || len(state.Instances) > instanceLimit(state.Run) {
			return s.abortRunState(ctx, state, status_error.WorkflowBudgetExceeded)
		}
		for _, fiber := range state.Fibers {
			if fiber.ID != state.Run.RootFiberID || !fiber.State.Done {
				continue
			}
			if fiber.State.Failure != nil {
				children, err := s.runs.ListJoinedChildren(ctx, state.Run.ID)
				if err != nil {
					return err
				}
				pending := false
				for _, child := range children {
					if !workflowstate.TerminalRun(child.Status) {
						pending = true
					}
				}
				if pending {
					state.Run.Status = constants.WorkflowRunStatusCancelling
					state.Run.Failure = fiber.State.Failure
					return s.state.CommitRun(ctx, state)
				}
				state.Run.Status = constants.WorkflowRunStatusFailed
				state.Run.Failure = fiber.State.Failure
			} else {
				children, err := s.runs.ListJoinedChildren(ctx, state.Run.ID)
				if err != nil {
					return err
				}
				pending := false
				for _, child := range children {
					if !workflowstate.TerminalRun(child.Status) {
						pending = true
					}
					if (child.Status == constants.WorkflowRunStatusFailed || child.Status == constants.WorkflowRunStatusCancelled) && !childResultObserved(state.Effects, child.ID) {
						state.Run.Status = constants.WorkflowRunStatusCancelling
						state.Run.Failure = workflowstate.FailureValue("JOINED_CHILD_FAILED")
						return s.state.CommitRun(ctx, state)
					}
				}
				if pending {
					continue
				}
				result, err := fiber.State.Result.JSON()
				if err != nil {
					return err
				}
				if len(revision.Manifest.Meta.OutputSchema) > 0 {
					if err = workflowdsl.ValidateJSON(revision.Manifest.Meta.OutputSchema, result); err != nil {
						state.Run.Status = constants.WorkflowRunStatusFailed
						state.Run.Failure = workflowstate.FailureValue("OUTPUT_INVALID")
					} else {
						state.Run.Status = constants.WorkflowRunStatusSucceeded
						state.Run.Result = result
					}
				} else {
					state.Run.Status = constants.WorkflowRunStatusSucceeded
					state.Run.Result = result
				}
			}
			state.Run.CompletedAt = &now
			changed = true
		}
		if !workflowstate.TerminalRun(state.Run.Status) {
			state.Run.Status = constants.WorkflowRunStatusWaiting
			for _, fiber := range state.Fibers {
				if fiber.Status == constants.WorkflowStepStatusReady {
					state.Run.Status = constants.WorkflowRunStatusRunning
					break
				}
			}
		}
		if !changed && beforeState == workflowstate.Digest(state) {
			return nil
		}
		slots := []any{}
		for _, fiber := range state.Fibers {
			slots = append(slots, fiber.State)
		}
		for _, group := range state.Groups {
			slots = append(slots, group.OriginalItems, group.Results, group.Stages)
		}
		for _, effect := range state.Effects {
			if effect.BuiltinState != nil {
				slots = append(slots, effect.BuiltinState)
			}
		}
		encoded, _ := json.Marshal(slots)
		if len(encoded) > constants.WorkflowSlotsLimitBytes {
			return s.abortRunState(ctx, state, status_error.WorkflowBudgetExceeded)
		}
		return s.state.CommitRun(ctx, state)
	})
	if err != nil {
		return controllers.WorkflowRunState{}, err
	}
	return confirmed, nil
}

func prepareEffect(bundle workflowdsl.ProgramBundle, manifest workflowdsl.Manifest, state *controllers.WorkflowRunState, index int, boundary workflowdsl.EffectBoundary) error {
	if len(state.Instances) >= instanceLimit(state.Run) {
		return status_error.WorkflowBudgetExceeded.WithField("budget", constants.WorkflowInstanceBudget).WithField("hint", "increase meta.limits.maxInstances or omit it to use the host limit")
	}
	options := workflowstate.BoundaryOptions(boundary)
	label, _ := options["label"].(string)
	phaseID, _ := options["phase"].(string)
	id := util.New()
	instanceID := util.New()
	now := time.Now().UnixMilli()
	timeout := workflowstate.EffectTimeout(manifest, boundary)
	deadline := int64(0)
	if timeout > 0 {
		deadline = now + timeout
	}
	fiber := &state.Fibers[index]
	token := util.New() + util.New()
	effect := models.WorkflowEffect{ID: id, RunID: state.Run.ID, FiberID: fiber.ID, InstanceID: instanceID, Boundary: boundary, Status: constants.WorkflowStepStatusPrepared, AttemptGeneration: 1, ExecutionID: util.New(), AuthToken: token, TokenDigest: workflowdsl.Digest([]byte(token)), DeadlineAt: deadline}
	instance := models.WorkflowNodeInstance{ID: instanceID, RunID: state.Run.ID, FiberID: fiber.ID, NodeID: boundary.NodeID, Kind: boundary.Primitive, Path: fiber.Path + workflowdsl.ScopeSuffix(bundle, fiber.State, boundary), Label: label, PhaseID: phaseID, Status: constants.WorkflowStepStatusWaiting, EffectID: id, ExecutionID: effect.ExecutionID, Input: boundary.Arguments, Options: options, GroupID: fiber.GroupID, ItemIndex: fiber.ItemIndex, StageIndex: fiber.StageIndex, AttemptGeneration: effect.AttemptGeneration}
	operation, err := prepareOperation(manifest, state.Run, &effect)
	if err != nil {
		return err
	}
	if operation != nil {
		state.Operations = append(state.Operations, *operation)
	}
	if boundary.Primitive == "askUser" {
		prompt := ""
		if len(boundary.Arguments) > 0 {
			prompt, _ = boundary.Arguments[0].Data.(string)
		}
		deadline := int64(0)
		if value, ok := options["timeoutMs"].(float64); ok {
			deadline = now + int64(value)
		}
		effect.DeadlineAt = deadline
		state.Humans = append(state.Humans, models.WorkflowHumanRequest{ID: util.New(), RunID: state.Run.ID, EffectID: id, InstanceID: instanceID, Prompt: prompt, Options: options, Status: constants.WorkflowStepStatusWaiting, DeadlineAt: deadline})
	}
	state.Effects = append(state.Effects, effect)
	state.Instances = append(state.Instances, instance)
	fiber.WaitingID = id
	fiber.Status = constants.WorkflowStepStatusWaiting
	return nil
}

func (s *Service) abortRunState(ctx context.Context, state controllers.WorkflowRunState, err error) error {
	if ctx.Err() != nil {
		return err
	}
	previous, loadErr := s.runs.LoadRunState(ctx, state.Run.ID)
	if loadErr != nil {
		return loadErr
	}
	state = previous
	state.Run.Status = constants.WorkflowRunStatusCancelling
	state.Run.Failure = failureFromError(err, nil, nil)
	return s.state.CommitRun(ctx, state)
}

func createGroup(bundle workflowdsl.ProgramBundle, state *controllers.WorkflowRunState, index int, boundary workflowdsl.EffectBoundary) error {
	if len(boundary.Arguments) < 2 {
		return status_error.WorkflowStateInvalid
	}
	items, err := workflowdsl.ArrayValues(boundary.Arguments[0])
	if err != nil {
		return err
	}
	stages := []workflowdsl.Value{}
	optionsIndex := 1
	if boundary.Primitive == "pipeline" {
		stages, err = workflowdsl.ArrayValues(boundary.Arguments[1])
		if err != nil {
			return err
		}
		if len(stages) == 0 {
			return status_error.WorkflowStateInvalid
		}
		optionsIndex = 2
	}
	if len(items)+len(state.Fibers) > constants.WorkflowMaxInstances {
		return status_error.WorkflowBudgetExceeded
	}
	options := map[string]any{}
	if len(boundary.Arguments) > optionsIndex {
		options, _ = boundary.Arguments[optionsIndex].Data.(map[string]any)
	}
	concurrency := constants.WorkflowDefaultConcurrency
	if value, ok := options["concurrency"].(float64); ok {
		concurrency = int(value)
	}
	if concurrency < 1 || concurrency > 8 {
		return status_error.WorkflowBudgetExceeded
	}
	policy := constants.WorkflowGroupDefaultFailureMode
	if value, ok := options["failureMode"].(string); ok {
		policy = value
	}
	group := models.WorkflowGroupRun{ID: util.New(), RunID: state.Run.ID, ParentFiberID: state.Fibers[index].ID, NodeID: boundary.NodeID, Kind: boundary.Primitive, Policy: policy, Concurrency: concurrency, Stages: stages, OriginalItems: items, Results: make([]workflowdsl.Value, len(items)), Finished: make([]bool, len(items)), Status: constants.WorkflowStepStatusRunning}
	parentPath := state.Fibers[index].Path
	for itemIndex, item := range items {
		task := item
		arguments := []workflowdsl.Value{}
		if boundary.Primitive == "pipeline" {
			task = stages[0]
			arguments = []workflowdsl.Value{item, item, workflowdsl.JSONValue(itemIndex)}
		}
		frame, err := workflowdsl.NewTaskMachine(bundle, task, arguments, state.Run.StartedAt)
		if err != nil {
			return err
		}
		state.Fibers = append(state.Fibers, models.WorkflowFiber{ID: util.New(), RunID: state.Run.ID, GroupID: group.ID, ItemIndex: itemIndex, Path: fmt.Sprintf("%s/%s/item:%d", parentPath, boundary.NodeID, itemIndex), State: frame, Status: constants.WorkflowStepStatusReady})
		group.Results[itemIndex] = workflowdsl.JSONValue(nil)
	}
	state.Groups = append(state.Groups, group)
	state.Fibers[index].WaitingID = group.ID
	state.Fibers[index].Status = constants.WorkflowStepStatusWaiting
	return nil
}

func fiberAdmitted(state controllers.WorkflowRunState, index int) bool {
	fiber := state.Fibers[index]
	if fiber.GroupID == "" {
		return true
	}
	for _, group := range state.Groups {
		if group.ID != fiber.GroupID {
			continue
		}
		active := 0
		for _, other := range state.Fibers {
			if other.GroupID == group.ID && other.Status == constants.WorkflowStepStatusWaiting {
				active++
			}
		}
		return active < group.Concurrency && group.Status == constants.WorkflowStepStatusRunning
	}
	return false
}

// advanceGroups 每一项完成后立即创建下一阶段，最终按原始输入位置收集。
func advanceGroups(bundle workflowdsl.ProgramBundle, state *controllers.WorkflowRunState) error {
	for groupIndex := range state.Groups {
		group := &state.Groups[groupIndex]
		if group.Status != constants.WorkflowStepStatusRunning {
			continue
		}
		initialCount := len(state.Fibers)
		for fiberIndex := 0; fiberIndex < initialCount; fiberIndex++ {
			fiber := state.Fibers[fiberIndex]
			if fiber.GroupID != group.ID || !fiber.State.Done || group.Finished[fiber.ItemIndex] {
				continue
			}
			if fiber.State.Failure != nil {
				group.Finished[fiber.ItemIndex] = true
				group.Results[fiber.ItemIndex] = workflowdsl.JSONValue(nil)
				if group.Policy == "fail-fast" {
					group.Failure = fiber.State.Failure
				}
				continue
			}
			if group.Kind == "pipeline" && fiber.StageIndex+1 < len(group.Stages) {
				exists := false
				for _, candidate := range state.Fibers {
					if candidate.GroupID == group.ID && candidate.ItemIndex == fiber.ItemIndex && candidate.StageIndex == fiber.StageIndex+1 {
						exists = true
						break
					}
				}
				if !exists {
					arguments := []workflowdsl.Value{fiber.State.Result, group.OriginalItems[fiber.ItemIndex], workflowdsl.JSONValue(fiber.ItemIndex)}
					frame, err := workflowdsl.NewTaskMachine(bundle, group.Stages[fiber.StageIndex+1], arguments, state.Run.StartedAt)
					if err != nil {
						return err
					}
					state.Fibers = append(state.Fibers, models.WorkflowFiber{ID: util.New(), RunID: state.Run.ID, GroupID: group.ID, ItemIndex: fiber.ItemIndex, StageIndex: fiber.StageIndex + 1, Path: fiber.Path, State: frame, Status: constants.WorkflowStepStatusReady})
				}
			} else {
				group.Finished[fiber.ItemIndex] = true
				group.Results[fiber.ItemIndex] = fiber.State.Result
			}
		}
		complete := true
		for _, finished := range group.Finished {
			if !finished {
				complete = false
				break
			}
		}
		if group.Failure != nil {
			// 组失败先形成停止意图；外部效果真实取消后才能把异常交回父程序。
			pending := false
			for index := range state.Fibers {
				fiber := &state.Fibers[index]
				if fiber.GroupID != group.ID || workflowstate.StepTerminal(fiber.Status) {
					continue
				}
				if fiber.Status == constants.WorkflowStepStatusWaiting {
					for nestedIndex := range state.Groups {
						nested := &state.Groups[nestedIndex]
						if nested.ID == fiber.WaitingID && !workflowstate.StepTerminal(nested.Status) {
							if nested.Failure == nil {
								nested.Failure = group.Failure
							}
							pending = true
						}
					}
					for effectIndex := range state.Effects {
						effect := &state.Effects[effectIndex]
						if effect.ID == fiber.WaitingID && !workflowstate.StepTerminal(effect.Status) {
							effect.Failure = workflowstate.FailureValue("GROUP_CANCELLED")
							pending = true
						}
					}
				} else {
					fiber.Status = constants.WorkflowStepStatusCancelled
				}
			}
			if pending {
				continue
			}
			complete = true
		}
		if !complete {
			continue
		}
		group.Status = constants.WorkflowStepStatusSucceeded
		if group.Failure != nil {
			group.Status = constants.WorkflowStepStatusFailed
		}
		for index := range state.Fibers {
			parent := &state.Fibers[index]
			if parent.ID != group.ParentFiberID || parent.WaitingID != group.ID || parent.Boundary == nil {
				continue
			}
			if err := workflowdsl.ApplyEffect(bundle, &parent.State, *parent.Boundary, workflowdsl.ValuesArray(group.Results), group.Failure); err != nil {
				return err
			}
			parent.Boundary = nil
			parent.WaitingID = ""
			parent.Status = constants.WorkflowStepStatusReady
		}
	}
	return nil
}

func runConcurrency(run models.WorkflowRun) int {
	if run.Limits.Concurrency > 0 {
		return run.Limits.Concurrency
	}
	return constants.WorkflowDefaultConcurrency
}

func instanceLimit(run models.WorkflowRun) int {
	if run.Limits.MaxInstances > 0 {
		return run.Limits.MaxInstances
	}
	return constants.WorkflowMaxInstances
}

func loopStepLimit(run models.WorkflowRun) int {
	if run.Limits.MaxLoopSteps > 0 {
		return run.Limits.MaxLoopSteps
	}
	return constants.WorkflowMaxLoopSteps
}

func (s *Service) reserveExternalSlot(ctx context.Context, runID, effectID string) error {
	return s.state.MutateEffect(ctx, runID, effectID, func(ctx context.Context, state *controllers.WorkflowRunState, effect *models.WorkflowEffect) error {
		if effect.SlotReserved {
			return nil
		}
		if workflowstate.TerminalRun(state.Run.Status) || workflowstate.StepTerminal(effect.Status) {
			return status_error.WorkflowStateConflict
		}
		active := 0
		for _, item := range state.Effects {
			if item.SlotReserved && !workflowstate.StepTerminal(item.Status) {
				active++
			}
		}
		if active >= runConcurrency(state.Run) {
			return status_error.WorkflowStateConflict
		}
		global, err := s.runs.CountReservedEffects(ctx)
		if err != nil {
			return err
		}
		if global >= constants.WorkflowGlobalConcurrency {
			return status_error.WorkflowStateConflict
		}
		effect.SlotReserved = true
		return nil
	})
}

func operationDescription(manifest workflowdsl.Manifest, run models.WorkflowRun, effect models.WorkflowEffect) (string, map[string]any, bool, error) {
	options := workflowstate.BoundaryOptions(effect.Boundary)
	if effect.Boundary.Primitive == "functionTask" {
		language, _ := options["language"].(string)
		mode, _ := options["executionMode"].(string)
		if language == "javascript" {
			return "", nil, false, nil
		}
		if language != "python" || mode != "trusted_local" {
			return "", nil, false, status_error.WorkflowCapabilityUnsupported.WithField("capability", "python_pure_isolation")
		}
		if run.Defaults.WorkspaceReadOnly {
			return "", nil, false, status_error.WorkflowCapabilityUnsupported.WithField("capability", "trusted_local_readonly")
		}
		return "运行本地受信 Python：代码可访问当前用户的文件、网络和进程。", map[string]any{"language": language, "executionMode": mode, "workingDirectory": run.ScopeBinding.WorkingDirectory, "code": options["code"], "codeDigest": workflowstate.Digest(options["code"]), "input": options["input"]}, run.Defaults.PermissionLevel != constants.PermissionLevelFullAccess, nil
	}
	if effect.Boundary.Primitive != "action" {
		return "", nil, false, nil
	}
	declaration, err := workflowstate.EffectAction(manifest, effect)
	if err != nil {
		return "", nil, false, err
	}
	descriptor := map[string]any{"actionId": declaration.ID, "type": declaration.Type, "risk": declaration.Risk, "description": declaration.Description, "workingDirectory": run.ScopeBinding.WorkingDirectory}
	switch declaration.Type {
	case "script":
		if run.Defaults.WorkspaceReadOnly {
			return "", nil, false, status_error.WorkflowCapabilityUnsupported.WithField("capability", "script_readonly_isolation")
		}
		descriptor["executable"], descriptor["args"] = declaration.Executable, options["args"]
		descriptor["secretEnv"] = options["secretEnv"]
		descriptor["env"] = options["env"]
		return "启动本地命令：使用所列可执行文件、参数与工作目录，具有当前用户的系统权限。", descriptor, run.Defaults.PermissionLevel != constants.PermissionLevelFullAccess, nil
	case "http":
		method, _ := options["method"].(string)
		if method == "" {
			method = "GET"
		}
		descriptor["method"], descriptor["url"], descriptor["secretHeaders"] = method, options["url"], options["secretHeaders"]
		descriptor["headers"], descriptor["body"], descriptor["bodyDigest"] = options["headers"], options["body"], workflowstate.Digest(options["body"])
		sensitive := declaration.Risk != "read" || (method != "GET" && method != "HEAD" && method != "OPTIONS")
		return declaration.Description, descriptor, sensitive && run.Defaults.PermissionLevel == constants.PermissionLevelDefault, nil
	case "builtin":
		descriptor["handler"], descriptor["connectorIds"] = declaration.Handler, declaration.ConnectorIDs
		descriptor["input"] = options["input"]
		return declaration.Description, descriptor, run.Defaults.PermissionLevel != constants.PermissionLevelFullAccess, nil
	}
	return "", nil, false, status_error.WorkflowCapabilityUnsupported
}

func prepareOperation(manifest workflowdsl.Manifest, stateRun models.WorkflowRun, effect *models.WorkflowEffect) (*models.WorkflowOperationRequest, error) {
	description, descriptor, required, err := operationDescription(manifest, stateRun, *effect)
	if err != nil {
		return nil, err
	}
	if !required {
		return nil, nil
	}
	effect.DeadlineAt = stateRun.DeadlineAt
	return &models.WorkflowOperationRequest{ID: util.New(), RunID: stateRun.ID, EffectID: effect.ID, InstanceID: effect.InstanceID, Description: description, Descriptor: descriptor, DescriptorDigest: workflowstate.Digest(descriptor), Status: constants.WorkflowStepStatusWaiting}, nil
}
