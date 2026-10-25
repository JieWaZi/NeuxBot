package dsl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// Value 保留 JSON 与受限 continuation 的类型，用户 JSON 不能伪造内部 handle。
type Value struct {
	// Kind 区分普通 JSON、任务程序、子运行 handle 和有限纯内置值。
	Kind string `json:"kind"`
	// Data 是 JSON 字面数据或受限内部标量。
	Data any `json:"data,omitempty"`
	// ProgramID 是任务或纯辅助子程序的静态身份。
	ProgramID string `json:"programId,omitempty"`
	// Captures 是按值复制的词法绑定，不保存 JavaScript 闭包。
	Captures map[string]Binding `json:"captures,omitempty"`
	// Items 是任务构造数组，普通 JSON 数组仍存放在 Data。
	Items []Value `json:"items,omitempty"`
}

// Binding 是持久词法 scope 内的一个值。
type Binding struct {
	// Value 是按值保存的 JSON 或内部受限 continuation。
	Value Value `json:"value"`
	// Constant 禁止给绑定重新赋值。
	Constant bool `json:"constant"`
	// ReadOnly 禁止修改全局上下文的任意子属性。
	ReadOnly bool `json:"readOnly"`
}

// Completion 保存需要经过 finally 后继续的返回或异常。
type Completion struct {
	// Kind 区分返回、抛出和经过 finally 的循环控制跳转。
	Kind string `json:"kind"`
	// Value 是返回 JSON 或稳定错误数据。
	Value Value `json:"value"`
	// TargetPC 是 finally 完成后应恢复的循环跳转指令位置。
	TargetPC int `json:"targetPc,omitempty"`
	// TargetScopeDepth 是跳转目标应保留的实际词法层数。
	TargetScopeDepth int `json:"targetScopeDepth,omitempty"`
	// TargetHandlerDepth 是跳转目标应保留的外层异常处理数量。
	TargetHandlerDepth int `json:"targetHandlerDepth,omitempty"`
}

// ExceptionHandler 是当前词法范围的持久异常 continuation。
type ExceptionHandler struct {
	// CatchPC 是异常处理入口，负值表示没有 catch。
	CatchPC int `json:"catchPc"`
	// FinallyPC 是 finally 入口，负值表示没有 finally。
	FinallyPC int `json:"finallyPc"`
	// EndPC 是正常结束后的指令位置。
	EndPC int `json:"endPc"`
	// ScopeDepth 是进入 try 时的词法 scope 深度。
	ScopeDepth int `json:"scopeDepth"`
	// CatchUsed 防止 catch 内再次抛出被同一 catch 消费。
	CatchUsed bool `json:"catchUsed"`
	// FinallyUsed 防止 finally 自己的异常重进同一个 finally。
	FinallyUsed bool `json:"finallyUsed"`
	// Pending 是 finally 执行后需要继续的返回或异常。
	Pending *Completion `json:"pending,omitempty"`
}

// MachineState 是 checkpoint 中可以完整恢复的执行帧。
type MachineState struct {
	// ProgramID 是当前解释的静态程序身份。
	ProgramID string `json:"programId"`
	// PC 是下一条 Go 指令的位置。
	PC int `json:"pc"`
	// Scopes 是按词法嵌套保存的绑定。
	Scopes []map[string]Binding `json:"scopes"`
	// Handlers 是异常与 finally 的持久 continuation。
	Handlers []ExceptionHandler `json:"handlers,omitempty"`
	// LoopCounters 是源码循环本次运行的推进计数。
	LoopCounters map[string]int `json:"loopCounters"`
	// PureSteps 是已经消耗的纯指令总数。
	PureSteps int `json:"pureSteps"`
	// LoopSteps 是已经消耗的循环预算。
	LoopSteps int `json:"loopSteps"`
	// StartedAt 是冻结的运行开始时间，Date 不读取恢复后的墙钟。
	StartedAt int64 `json:"startedAt"`
	// CallChain 是当前纯子程序的静态调用路径，用于阻止递归。
	CallChain []string `json:"callChain,omitempty"`
	// Done 表示该程序已经形成终态。
	Done bool `json:"done"`
	// Result 是 return 的受限值，最终公开前仍需校验 JSON 和 schema。
	Result Value `json:"result"`
	// Failure 是未被源码 catch 处理的错误数据。
	Failure *Value `json:"failure,omitempty"`
}

// EffectBoundary 是暂停在已编译宿主调用前的完整输入。
type EffectBoundary struct {
	// Primitive 是宿主能力名称。
	Primitive string `json:"primitive"`
	// NodeID 是源码稳定职责。
	NodeID string `json:"nodeId"`
	// Arguments 是已经展开并按值冻结的调用参数。
	Arguments []Value `json:"arguments"`
	// Destination 是接收效果回执的内部绑定。
	Destination string `json:"destination"`
	// Range 是实际调用的原始源码位置。
	Range SourceRange `json:"range"`
}

// NewMachine 建立主流程帧，不启动 goroutine 或执行外部效果。
func NewMachine(bundle ProgramBundle, args, flowContext any, startedAt int64) MachineState {
	return MachineState{ProgramID: bundle.Entry, Scopes: []map[string]Binding{{"args": {Value: JSONValue(args), Constant: true, ReadOnly: true}, "flowContext": {Value: JSONValue(flowContext), Constant: true, ReadOnly: true}}}, LoopCounters: map[string]int{}, StartedAt: startedAt}
}

// NewTaskMachine 从持久任务值建立独立词法帧，参数和闭包均按值复制。
func NewTaskMachine(bundle ProgramBundle, task Value, arguments []Value, startedAt int64) (MachineState, error) {
	program, ok := bundle.Programs[task.ProgramID]
	if !ok || task.Kind != constants.WorkflowSlotFunction {
		return MachineState{}, status_error.WorkflowStateInvalid
	}
	scope := map[string]Binding{}
	for name, binding := range task.Captures {
		binding.Value = copyValue(binding.Value)
		scope[name] = binding
	}
	state := MachineState{ProgramID: task.ProgramID, Scopes: []map[string]Binding{scope, {}}, LoopCounters: map[string]int{}, StartedAt: startedAt}
	e := evaluator{ctx: context.Background(), bundle: bundle, state: &state}
	for index, parameter := range program.Parameters {
		value := Value{Kind: constants.WorkflowSlotMissing}
		if index < len(arguments) {
			value = copyValue(arguments[index])
		}
		if err := e.bind(parameter, value, false, true); err != nil {
			return MachineState{}, err
		}
	}
	return state, nil
}

// ArrayValues 返回复制后的有序数组，任务集合保留内部类型边界。
func ArrayValues(value Value) ([]Value, error) { return arrayValues(copyValue(value)) }

// ValuesArray 保留非 JSON 任务值，最终运行输出仍只允许 JSON。
func ValuesArray(values []Value) Value { return valuesArray(values) }

// JSONValue 给用户数据加内部类型边界，含 kind 字段的 JSON 仍只是普通数据。
func JSONValue(data any) Value { return Value{Kind: constants.WorkflowSlotJSON, Data: cloneJSON(data)} }

// JSON 只允许公开 JSON 值，禁止把任务程序和子运行 handle 交给模型或最终输出。
func (value Value) JSON() (any, error) {
	if value.Kind != constants.WorkflowSlotJSON {
		return nil, status_error.WorkflowStateInvalid.WithField("reason", "non-JSON value cannot cross a public boundary")
	}
	return cloneJSON(value.Data), nil
}

// Advance 解释有界纯步骤，遇到宿主效果立即停止，由 Runner 先 checkpoint 再派发。
func Advance(ctx context.Context, bundle ProgramBundle, state *MachineState, limit int) (*EffectBoundary, error) {
	if bundle.Version != constants.WorkflowIRVersion {
		return nil, status_error.WorkflowStateInvalid
	}
	program, ok := bundle.Programs[state.ProgramID]
	if !ok {
		return nil, status_error.WorkflowStateInvalid
	}
	if limit <= 0 || limit > constants.WorkflowCheckpointSteps {
		limit = constants.WorkflowCheckpointSteps
	}
	for step := 0; step < limit && !state.Done; step++ {
		if ctx.Err() != nil {
			return nil, status_error.WorkflowCancelled
		}
		if state.PC < 0 || state.PC >= len(program.Code) {
			return nil, status_error.WorkflowStateInvalid
		}
		if state.PureSteps >= constants.WorkflowMaxPureSteps {
			return nil, status_error.WorkflowBudgetExceeded
		}
		state.PureSteps++
		i := program.Code[state.PC]
		evaluation := evaluator{ctx: ctx, bundle: bundle, state: state, callChain: state.CallChain}
		var err error
		switch i.Op {
		case constants.WorkflowOpDeclare:
			var value Value
			value, err = evaluation.expression(i.Expression)
			if err == nil {
				err = evaluation.bind(i.Target, value, i.Constant, true)
			}
			if err == nil {
				state.PC++
			}
		case constants.WorkflowOpEvaluate:
			_, err = evaluation.expression(i.Expression)
			if err == nil {
				state.PC++
			}
		case constants.WorkflowOpEnterScope:
			state.Scopes = append(state.Scopes, map[string]Binding{})
			state.PC++
		case constants.WorkflowOpLeaveScope:
			if len(state.Scopes) <= 1 {
				err = status_error.WorkflowStateInvalid
			} else {
				state.Scopes = state.Scopes[:len(state.Scopes)-1]
				state.PC++
			}
		case constants.WorkflowOpBranch:
			var value Value
			value, err = evaluation.expression(i.Expression)
			if err == nil {
				if truthy(value) {
					state.PC = i.Next
				} else {
					state.PC = i.Alternate
				}
			}
		case constants.WorkflowOpJump:
			state.PC = i.Next
		case constants.WorkflowOpLoopControl:
			depth := len(state.Scopes) - i.ScopeDrop
			if depth < 1 || i.HandlerDepth > len(state.Handlers) {
				err = status_error.WorkflowStateInvalid
			} else {
				finishCompletion(state, Completion{Kind: constants.WorkflowCompletionJump, TargetPC: i.Next, TargetScopeDepth: depth, TargetHandlerDepth: i.HandlerDepth})
			}
		case constants.WorkflowOpLoop:
			state.LoopSteps++
			state.LoopCounters[i.NodeID]++
			if state.LoopSteps > constants.WorkflowMaxLoopSteps {
				err = status_error.WorkflowBudgetExceeded
			} else {
				state.PC++
			}
		case constants.WorkflowOpReturn:
			var value Value
			value, err = evaluation.expression(i.Expression)
			if err == nil {
				finishCompletion(state, Completion{Kind: constants.WorkflowCompletionReturn, Value: value})
			}
		case constants.WorkflowOpThrow:
			var value Value
			value, err = evaluation.expression(i.Expression)
			if err == nil {
				finishCompletion(state, Completion{Kind: constants.WorkflowCompletionThrow, Value: value})
			}
		case constants.WorkflowOpTry:
			state.Handlers = append(state.Handlers, ExceptionHandler{CatchPC: i.Alternate, FinallyPC: i.Finally, EndPC: i.Next, ScopeDepth: len(state.Scopes)})
			state.PC++
		case constants.WorkflowOpEndTry:
			if len(state.Handlers) == 0 {
				err = status_error.WorkflowStateInvalid
				break
			}
			handler := &state.Handlers[len(state.Handlers)-1]
			state.Scopes = state.Scopes[:handler.ScopeDepth]
			if handler.FinallyPC >= 0 && !handler.FinallyUsed {
				handler.FinallyUsed = true
				state.PC = handler.FinallyPC
			} else {
				state.PC = handler.EndPC
				state.Handlers = state.Handlers[:len(state.Handlers)-1]
			}
		case constants.WorkflowOpFinally:
			if len(state.Handlers) == 0 {
				err = status_error.WorkflowStateInvalid
				break
			}
			handler := state.Handlers[len(state.Handlers)-1]
			state.Handlers = state.Handlers[:len(state.Handlers)-1]
			if handler.Pending != nil {
				finishCompletion(state, *handler.Pending)
			} else {
				state.PC = handler.EndPC
			}
		case constants.WorkflowOpEffect:
			var value Value
			value, err = evaluation.expression(i.Expression)
			if err == nil {
				arguments, argumentErr := arrayValues(value)
				if argumentErr != nil {
					err = argumentErr
				} else {
					return &EffectBoundary{Primitive: i.Primitive, NodeID: i.NodeID, Arguments: arguments, Destination: i.Destination, Range: i.Range}, nil
				}
			}
		default:
			err = status_error.WorkflowStateInvalid
		}
		if err != nil {
			finishCompletion(state, Completion{Kind: constants.WorkflowCompletionThrow, Value: errorValue(err)})
		}
	}
	data, err := json.Marshal(state)
	if err != nil || len(data) > constants.WorkflowSlotsLimitBytes {
		return nil, status_error.WorkflowBudgetExceeded
	}
	return nil, nil
}

// ApplyEffect 只在效果回执已提交的事务里推进一次 PC。
func ApplyEffect(bundle ProgramBundle, state *MachineState, boundary EffectBoundary, value Value, failure *Value) error {
	program, ok := bundle.Programs[state.ProgramID]
	if !ok || state.PC < 0 || state.PC >= len(program.Code) {
		return status_error.WorkflowStateInvalid
	}
	i := program.Code[state.PC]
	if i.Op != constants.WorkflowOpEffect || i.Destination != boundary.Destination {
		return status_error.WorkflowStateConflict
	}
	if failure != nil {
		finishCompletion(state, Completion{Kind: constants.WorkflowCompletionThrow, Value: *failure})
		return nil
	}
	state.Scopes[len(state.Scopes)-1][boundary.Destination] = Binding{Value: copyValue(value)}
	state.PC++
	return nil
}

func finishCompletion(state *MachineState, completion Completion) {
	minimumHandlers := 0
	if completion.Kind == constants.WorkflowCompletionJump {
		minimumHandlers = completion.TargetHandlerDepth
	}
	for len(state.Handlers) > minimumHandlers {
		index := len(state.Handlers) - 1
		handler := &state.Handlers[index]
		state.Scopes = state.Scopes[:handler.ScopeDepth]
		if completion.Kind == constants.WorkflowCompletionThrow && handler.CatchPC >= 0 && !handler.CatchUsed && !handler.FinallyUsed {
			handler.CatchUsed = true
			state.Scopes[len(state.Scopes)-1]["$error"] = Binding{Value: completion.Value, Constant: true}
			state.PC = handler.CatchPC
			return
		}
		if handler.FinallyPC >= 0 && !handler.FinallyUsed {
			handler.FinallyUsed = true
			handler.Pending = &completion
			state.PC = handler.FinallyPC
			return
		}
		state.Handlers = state.Handlers[:index]
	}
	if completion.Kind == constants.WorkflowCompletionJump {
		state.Scopes = state.Scopes[:completion.TargetScopeDepth]
		state.PC = completion.TargetPC
		return
	}
	state.Done = true
	if completion.Kind == constants.WorkflowCompletionThrow {
		value := completion.Value
		state.Failure = &value
	} else {
		state.Result = completion.Value
	}
}

// ScopeSuffix 用源码范围筛选仍在当前调用内的循环计数，形成稳定实例路径。
func ScopeSuffix(bundle ProgramBundle, state MachineState, boundary EffectBoundary) string {
	parts := []string{}
	program := bundle.Programs[state.ProgramID]
	for _, i := range program.Code {
		if i.Op == constants.WorkflowOpLoop && i.Range.Start.Offset <= boundary.Range.Start.Offset && i.Range.End.Offset >= boundary.Range.End.Offset {
			parts = append(parts, fmt.Sprintf("%s:%d", i.NodeID, state.LoopCounters[i.NodeID]))
		}
	}
	sort.Strings(parts)
	result := ""
	for _, part := range parts {
		result += "/" + part
	}
	return result
}

func errorCode(err error) string {
	if value, ok := err.(interface{ Key() string }); ok {
		return value.Key()
	}
	return status_error.WorkflowEffectFailed.Key()
}

// errorValue 保留纯回调原样抛出的 JSON，并把宿主校验原因写入可恢复的异常值。
func errorValue(err error) Value {
	data := map[string]any{"code": errorCode(err), "message": err.Error()}
	var detail *statuserror.StatusErr
	if errors.As(err, &detail) {
		if thrown, found := detail.Params["workflowThrownValue"]; found {
			return JSONValue(thrown)
		}
		for _, key := range []string{"reason", "field", "capability", "budget", "operation"} {
			if value, found := detail.Fields[key]; found {
				data[key] = value
			}
		}
	}
	return JSONValue(data)
}
func copyValue(value Value) Value {
	encoded, _ := json.Marshal(value)
	var copied Value
	if json.Unmarshal(encoded, &copied) != nil {
		return JSONValue(nil)
	}
	return copied
}
func arrayValues(value Value) ([]Value, error) {
	if value.Kind == constants.WorkflowSlotTasks {
		return value.Items, nil
	}
	if value.Kind != constants.WorkflowSlotJSON {
		return nil, status_error.WorkflowStateInvalid
	}
	items, ok := value.Data.([]any)
	if !ok {
		return nil, status_error.WorkflowStateInvalid.WithField("reason", "array required")
	}
	values := []Value{}
	for _, item := range items {
		values = append(values, JSONValue(item))
	}
	return values, nil
}
func valuesArray(values []Value) Value {
	data := []any{}
	for _, value := range values {
		if value.Kind != constants.WorkflowSlotJSON {
			return Value{Kind: constants.WorkflowSlotTasks, Items: values}
		}
		data = append(data, value.Data)
	}
	return JSONValue(data)
}
