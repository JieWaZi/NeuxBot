// Package function 在独立进程执行 Function，不承载主流程的 continuation。
package function

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/dop251/goja"
)

// WorkerFlag 只在独立 Function 子进程启动模式使用。
const WorkerFlag = "--workflow-function-worker"

// Request 是只含 JSON 数据的独立进程输入。
type Request struct {
	// RequestID 绑定一次已准备的效果身份。
	RequestID string `json:"requestId"`
	// Code 是版本中冻结的完整用户函数。
	Code string `json:"code"`
	// Input 是复制到子进程的业务参数。
	Input any `json:"input"`
	// StartedAt 固定 Date 的运行起始时刻。
	StartedAt int64 `json:"startedAt"`
	// DeadlineAt 是父进程同样执行的硬截止时刻。
	DeadlineAt int64 `json:"deadlineAt"`
	// Address 是父进程本机结果通道地址。
	Address string `json:"address"`
	// Token 是此通道一次性认证凭据，不进入用户 VM。
	Token string `json:"token"`
}

// Result 通过专用 IPC 返回，普通 stdout 不构成完成事实。
type Result struct {
	// RequestID 防止通道回执被关联到其他节点。
	RequestID string `json:"requestId"`
	// Token 让父进程校验此回执属于当前子进程。
	Token string `json:"token"`
	// OK 表示代码与 JSON 边界均成功。
	OK bool `json:"ok"`
	// Data 是用户代码返回的 JSON 值。
	Data any `json:"data"`
	// Error 是函数执行失败的可展示原因。
	Error string `json:"error,omitempty"`
	// DurationMillis 是此进程测量的耗时。
	DurationMillis int64 `json:"durationMs"`
}

// RunChild 只用于 ally-core 的独立 worker 启动路径；不初始化数据库或服务器。
func RunChild(input io.Reader) int {
	var request Request
	if err := json.NewDecoder(io.LimitReader(input, constants.WorkflowSourceLimitBytes+constants.WorkflowOutputLimitBytes+8192)).Decode(&request); err != nil {
		logx.Errorf("decode function worker input failed: error=%v", err)
		return 2
	}
	started := time.Now()
	result := evaluate(request)
	result.RequestID, result.Token = request.RequestID, request.Token
	result.DurationMillis = time.Since(started).Milliseconds()
	connection, err := net.DialTimeout("tcp", request.Address, time.Second)
	if err != nil {
		logx.Errorf("connect function result channel failed: error=%v", err)
		return 2
	}
	defer connection.Close()
	_ = connection.SetWriteDeadline(time.Now().Add(time.Second))
	if err = json.NewEncoder(connection).Encode(result); err != nil {
		logx.Errorf("write function result failed: error=%v", err)
		return 2
	}
	return 0
}

func evaluate(request Request) Result {
	vm := goja.New()
	vm.SetTimeSource(func() time.Time { return time.UnixMilli(request.StartedAt) })
	seed := sha256.Sum256([]byte(request.RequestID))
	random := rand.New(rand.NewPCG(binary.LittleEndian.Uint64(seed[:8]), binary.LittleEndian.Uint64(seed[8:16])))
	vm.SetRandSource(random.Float64)
	// 移除动态编译入口，同时封闭从任意函数原型取回 Function 构造器的路径。
	_, err := vm.RunString(`Object.defineProperty(Function.prototype, "constructor", {value:undefined, writable:false, configurable:false}); Object.defineProperty(globalThis,"eval",{value:undefined,writable:false,configurable:false}); Object.defineProperty(globalThis,"Function",{value:undefined,writable:false,configurable:false}); Object.freeze(Math); Object.freeze(Date); Object.freeze(JSON);`)
	if err != nil {
		logx.Errorf("initialize pure function VM failed: error=%v", err)
		return Result{Error: "FUNCTION_INITIALIZATION_FAILED"}
	}
	timer := time.AfterFunc(time.Until(time.UnixMilli(request.DeadlineAt)), func() { vm.Interrupt("FUNCTION_TIMEOUT") })
	defer timer.Stop()
	parse, _ := goja.AssertFunction(vm.Get("JSON").ToObject(vm).Get("parse"))
	stringify, _ := goja.AssertFunction(vm.Get("JSON").ToObject(vm).Get("stringify"))
	if _, err = vm.RunScript("workflow-function.js", request.Code); err != nil {
		return Result{Error: err.Error()}
	}
	main, ok := goja.AssertFunction(vm.Get("main"))
	if !ok {
		return Result{Error: "FUNCTION_MAIN_REQUIRED"}
	}
	encoded, err := json.Marshal(request.Input)
	if err != nil {
		logx.Errorf("encode pure function input failed: error=%v", err)
		return Result{Error: "FUNCTION_INPUT_INVALID"}
	}
	// JSON.parse 创建 VM 自有对象，避免输入变成可触及 Go 宿主的 map wrapper。
	argument, err := parse(goja.Undefined(), vm.ToValue(string(encoded)))
	if err != nil {
		return Result{Error: "FUNCTION_INPUT_INVALID"}
	}
	value, err := main(goja.Undefined(), argument)
	if err != nil {
		return Result{Error: err.Error()}
	}
	serialized, err := stringify(goja.Undefined(), value)
	if err != nil || goja.IsUndefined(serialized) {
		return Result{Error: "FUNCTION_OUTPUT_NOT_JSON"}
	}
	// stringify 会把 NaN 静默变为 null，因此先验证导出值的 JSON 序列化。
	if _, err = json.Marshal(value.Export()); err != nil {
		return Result{Error: "FUNCTION_OUTPUT_NOT_JSON"}
	}
	if len(serialized.String()) > constants.WorkflowOutputLimitBytes {
		return Result{Error: "FUNCTION_OUTPUT_LIMIT"}
	}
	var data any
	if err = json.Unmarshal([]byte(serialized.String()), &data); err != nil {
		return Result{Error: "FUNCTION_OUTPUT_NOT_JSON"}
	}
	return Result{OK: true, Data: data}
}

// IsChildMode 在业务进程初始化前识别专用启动参数。
func IsChildMode() bool { return len(os.Args) == 2 && os.Args[1] == WorkerFlag }

func hostError(operation string, err error) error {
	logx.Errorf("workflow function process failed: operation=%s error=%v", operation, err)
	return status_error.WorkflowEffectFailed.WithField("operation", operation)
}
