// Package state 维护工作流 API、Runner 与 MCP 共用的租约、并发协调和原子状态投影。
package state

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Coordinator 为所有入口提供同一命令锁、租约身份和活跃效果集合。
type Coordinator struct {
	// Commands 串行协调 API、源码提交和 Runner 的状态写入。
	Commands sync.Mutex
	// CreationCommands 串行接纳创作新回合，工具保存仍可独立提交。
	CreationCommands sync.Mutex
	// Owner 标识本应用生命周期持有运行租约的主体。
	Owner string
	// AgentBindings 为创作、使用和结果场景签发精确领域能力。
	AgentBindings func(string, string, constants.WorkflowCapabilityScope) []agentcapability.Binding
	// jobs 记录真实在途外部效果及其取消函数。
	jobs map[string]context.CancelFunc
	// jobsMutex 保护效果集合的查询、接纳与释放。
	jobsMutex sync.Mutex
	// store 保存源码身份、收据与有序领域事件。
	store controllers.WorkflowController
	// runs 保存带 fence 的运行状态与效果事实。
	runs controllers.WorkflowRunController
	// transactions 保证效果、实例投影及事件一起提交。
	transactions controllers.TransactionController
}

// New 创建同一应用内所有入口共用的协调边界，不启动后台任务。
func New(store controllers.WorkflowController, runs controllers.WorkflowRunController, transactions controllers.TransactionController) *Coordinator {
	return &Coordinator{Owner: util.New(), jobs: map[string]context.CancelFunc{}, store: store, runs: runs, transactions: transactions}
}

// EffectActive 读取实际在途派发，人工命令不能覆盖仍在执行的副作用。
func (s *Coordinator) EffectActive(id string) bool {
	s.jobsMutex.Lock()
	defer s.jobsMutex.Unlock()
	return s.jobs[id] != nil
}

// OwnedRun 获取本协调者租约并加载包含 fence 的运行状态，拒绝其他所有者。
func (s *Coordinator) OwnedRun(ctx context.Context, id string) (controllers.WorkflowRunState, error) {
	now := time.Now().UnixMilli()
	_, owned, err := s.runs.AcquireLease(ctx, id, s.Owner, now, now+constants.WorkflowLeaseMillis)
	if err != nil {
		return controllers.WorkflowRunState{}, err
	}
	if !owned {
		return controllers.WorkflowRunState{}, status_error.WorkflowFenceConflict
	}
	return s.runs.LoadRunState(ctx, id)
}

// CommitRun 将效果终态投影到节点实例，并在调用方事务中完成版本 CAS 与事件保存。
func (s *Coordinator) CommitRun(ctx context.Context, state controllers.WorkflowRunState) error {
	// 效果收据与节点终态同事务提交，后续推进失败或停止不能覆盖已完成的事实。
	now := time.Now().UnixMilli()
	// 外部结果不确定是此次运行的失败证据，自动停止其他步骤，不能等待用户补写收据。
	for index := range state.Effects {
		effect := &state.Effects[index]
		if effect.Status != constants.WorkflowStepStatusUnknown {
			continue
		}
		effect.SlotReserved = false
		if !TerminalRun(state.Run.Status) {
			state.Run.Status = constants.WorkflowRunStatusCancelling
			failure := map[string]any{
				"code": "EFFECT_OUTCOME_UNKNOWN", "effectId": effect.ID, "nodeId": effect.Boundary.NodeID,
			}
			if effect.Failure != nil {
				failure["cause"] = effect.Failure.Data
			}
			value := workflowdsl.JSONValue(failure)
			state.Run.Failure = &value
		}
	}
	receipts := make(map[string]models.WorkflowEffect, len(state.Effects))
	for _, effect := range state.Effects {
		if StepTerminal(effect.Status) || effect.Status == constants.WorkflowStepStatusUnknown {
			receipts[effect.ID] = effect
		}
	}
	for index := range state.Instances {
		instance := &state.Instances[index]
		effect, found := receipts[instance.EffectID]
		if !found {
			continue
		}
		instance.Status = effect.Status
		if effect.Status == constants.WorkflowStepStatusUnknown {
			instance.Status = constants.WorkflowStepStatusFailed
		}
		instance.ChatID = effect.ChatID
		instance.ExecutionID = effect.ExecutionID
		instance.Failure = effect.Failure
		if effect.Status == constants.WorkflowStepStatusSkipped {
			instance.Failure = FailureValue("SKIPPED")
		}
		if instance.CompletedAt == nil {
			instance.CompletedAt = &now
		}
		if effect.Result != nil {
			instance.Output, _ = effect.Result.JSON()
		}
	}
	if err := s.runs.CommitRunState(ctx, state, state.Run.Version, time.Now().UnixMilli()); err != nil {
		return err
	}
	_, err := s.store.AppendEvent(ctx, models.WorkflowEvent{StreamID: state.Run.ID, EventID: util.New(), Type: constants.WorkflowRunChangedEvent, EntityID: state.Run.ID, Payload: map[string]any{"runId": state.Run.ID, "status": state.Run.Status, "version": state.Run.Version + 1}})
	return err
}

// MutateEffect 在共同命令锁和租约下修改效果，并原子提交实例投影与运行版本。
func (s *Coordinator) MutateEffect(ctx context.Context, runID, effectID string, mutate func(context.Context, *controllers.WorkflowRunState, *models.WorkflowEffect) error) error {
	s.Commands.Lock()
	defer s.Commands.Unlock()
	return s.transactions.Transaction(ctx, func(ctx context.Context) error {
		state, err := s.OwnedRun(ctx, runID)
		if err != nil {
			return err
		}
		for index := range state.Effects {
			if state.Effects[index].ID != effectID {
				continue
			}
			before := FullEffectDigest(state.Effects[index])
			callsBefore := Digest(state.ActionCalls)
			if err := mutate(ctx, &state, &state.Effects[index]); err != nil {
				return err
			}
			current := state.Effects[index]
			for i := range state.Instances {
				if state.Instances[i].EffectID == current.ID {
					state.Instances[i].ChatID = current.ChatID
					state.Instances[i].ExecutionID = current.ExecutionID
					if current.Status == constants.WorkflowStepStatusRunning || current.Status == constants.WorkflowStepStatusUnknown {
						state.Instances[i].Status = current.Status
					}
				}
			}
			// API 不输出隐藏执行字段，摘要必须使用完整数据库效果值。
			if before == FullEffectDigest(state.Effects[index]) && Digest(state.ActionCalls) == callsBefore {
				return nil
			}
			return s.CommitRun(ctx, state)
		}
		return status_error.WorkflowNotFound
	})
}

// FullEffectDigest 计算包括隐藏执行字段的效果摘要，避免遗漏真实状态变化。
func FullEffectDigest(effect models.WorkflowEffect) string {
	return Digest([]any{effect, effect.Boundary, effect.Result, effect.Candidate, effect.BuiltinState, effect.SlotReserved, effect.SkipRequested})
}

// TerminalRun 判断运行是否已经成功、失败或取消并结束。
func TerminalRun(status constants.WorkflowRunStatus) bool {
	return status == constants.WorkflowRunStatusSucceeded || status == constants.WorkflowRunStatusFailed || status == constants.WorkflowRunStatusCancelled
}

// StepTerminal 判断节点尝试是否成功、失败、取消或已跳过。
func StepTerminal(status constants.WorkflowStepStatus) bool {
	return status == constants.WorkflowStepStatusSucceeded || status == constants.WorkflowStepStatusFailed || status == constants.WorkflowStepStatusCancelled || status == constants.WorkflowStepStatusSkipped
}

// FailureValue 把稳定失败代码包装成持久化机器可读取的 JSON 值。
func FailureValue(code string) *workflowdsl.Value {
	value := workflowdsl.JSONValue(map[string]any{"code": code})
	return &value
}

// HumanFallback 为人工请求的超时或跳过返回显式默认值或契约对应的空值。
func HumanFallback(options map[string]any) any {
	if value, present := options["defaultValue"]; present {
		return value
	}
	if _, structured := options["schema"].(map[string]any); structured {
		return nil
	}
	if multi, _ := options["multiSelect"].(bool); multi {
		return []any{}
	}
	return ""
}

// EffectAction 按冻结的效果参数读取不可变版本中的操作声明。
func EffectAction(manifest workflowdsl.Manifest, effect models.WorkflowEffect) (workflowdsl.ActionDeclaration, error) {
	if len(effect.Boundary.Arguments) < 1 {
		return workflowdsl.ActionDeclaration{}, status_error.WorkflowStateInvalid
	}
	id, _ := effect.Boundary.Arguments[0].Data.(string)
	for _, declaration := range manifest.Meta.Actions {
		if declaration.ID == id {
			return declaration, nil
		}
	}
	return workflowdsl.ActionDeclaration{}, status_error.WorkflowStateInvalid
}

// EffectTimeout 为伙伴节点返回零期限，其余节点按类别选择默认超时并应用正数覆盖。
func EffectTimeout(manifest workflowdsl.Manifest, boundary workflowdsl.EffectBoundary) int64 {
	if boundary.Primitive == "worker" {
		return constants.WorkflowWorkerTimeoutMillis
	}
	timeout := constants.WorkflowScriptTimeoutMillis
	if boundary.Primitive == "functionTask" {
		timeout = constants.WorkflowFunctionTimeoutMillis
	}
	if boundary.Primitive == "action" {
		timeout = constants.WorkflowScriptTimeoutMillis
		declaration, _ := EffectAction(manifest, models.WorkflowEffect{Boundary: boundary})
		if declaration.Type == "http" {
			timeout = constants.WorkflowHTTPTimeoutMillis
		}
	}
	if value, ok := BoundaryOptions(boundary)["timeoutMs"].(float64); ok && value > 0 {
		timeout = int64(value)
	}
	return timeout
}

// BoundaryOptions 从不同执行原语的冻结参数位置读取节点选项。
func BoundaryOptions(boundary workflowdsl.EffectBoundary) map[string]any {
	index := 1
	if boundary.Primitive == "functionTask" {
		index = 0
	}
	if len(boundary.Arguments) <= index {
		return map[string]any{}
	}
	options, _ := boundary.Arguments[index].Data.(map[string]any)
	return options
}

// ExecutionTerminal 判断 Core Execution 是否已经完成、取消或失败。
func ExecutionTerminal(status constants.ExecutionStatus) bool {
	return status == constants.ExecutionStatusCompleted || status == constants.ExecutionStatusCanceled || status == constants.ExecutionStatusFailed
}

// ErrorKey 沿错误链读取稳定状态错误码，未知错误归为效果执行失败。
func ErrorKey(err error) string {
	var value interface{ Key() string }
	if errors.As(err, &value) {
		return value.Key()
	}
	return status_error.WorkflowEffectFailed.Key()
}

// Digest 计算规范 JSON 值的 SHA 摘要，用于幂等校验与状态变化识别。
func Digest(value any) string {
	encoded, _ := json.Marshal(value)
	return workflowdsl.Digest(encoded)
}

// BeginEffect 在同一临界区取消已有派发、校验重复身份和并发容量，再登记新派发。
func (s *Coordinator) BeginEffect(ctx context.Context, id string, cancelExisting bool, limit int) (context.Context, context.CancelFunc, bool) {
	s.jobsMutex.Lock()
	defer s.jobsMutex.Unlock()
	previous := s.jobs[id]
	if previous != nil && cancelExisting {
		previous()
	}
	if previous != nil || len(s.jobs) >= limit {
		return nil, nil, false
	}
	child, cancel := context.WithCancel(ctx)
	s.jobs[id] = cancel
	return child, cancel, true
}

// EndEffect 在实际派发退出后释放效果身份和全局并发占用。
func (s *Coordinator) EndEffect(id string) {
	s.jobsMutex.Lock()
	defer s.jobsMutex.Unlock()
	delete(s.jobs, id)
}
