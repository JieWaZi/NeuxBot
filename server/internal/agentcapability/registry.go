// Package agentcapability 管理受管执行的短期能力，不拥有领域状态和外部效果。
package agentcapability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

// Request 是已经完成闭合 Schema 校验的单次调用。
type Request struct {
	// CWD 是 CLI 进程的绝对当前目录，仅用于解析相对路径，不能扩大文件授权。
	CWD string `json:"cwd,omitempty"`
	// InputDigests 是领域冻结真实文件后生成的内容摘要，不能由 HTTP 输入提供。
	InputDigests map[string]string `json:"-"`
	// Operation 是本次实际选择的精确操作。
	Operation string `json:"operation"`
	// RequestID 是写操作必须复用的调用身份。
	RequestID string `json:"requestId,omitempty"`
	// Params 是与操作契约完全一致的业务参数。
	Params map[string]any `json:"params"`
}

// Result 是领域模块真实提交或观察后返回的最小回执。
type Result struct {
	// CommandID 是领域持久回执身份；读取操作可以留空。
	CommandID string `json:"commandId,omitempty"`
	// Replayed 表示本次读取了相同请求的原接纳结果。
	Replayed bool `json:"replayed"`
	// Data 保留该操作的真实领域结果，不复制新的业务状态机。
	Data any `json:"data"`
}

// Binding 将一个精确命令绑定到真实授权范围及领域提交边界。
type Binding struct {
	// Operation 是静态目录中唯一的操作身份。
	Operation string
	// Call 只在 Grant 与参数验证通过后执行真实领域操作。
	Call func(context.Context, Request) (Result, error)
	// Receipt 从所属权威存储查询同一作用域原请求，读取操作可为空。
	Receipt func(context.Context, string) (Result, error)
}

// GrantSpec 冻结一次执行可用的精确操作和当前身份校验。
type GrantSpec struct {
	// ExecutionID 是真实接纳执行的稳定身份。
	ExecutionID string
	// Bindings 是由领域入口签发的精确调用集合。
	Bindings []Binding
	// FileRoots 是执行准备阶段冻结的目录授权，不接受 HTTP 参数覆盖。
	FileRoots []string
	// Record 将真实结果登记到所属执行；登记失败不能推翻领域接纳事实。
	Record func(context.Context, contract.Operation, Result) error
	// Validate 每次调用重新验证执行、角色和当前围栏。
	Validate func(context.Context) error
}

// Grant 是给连接文件准备器使用的非模型可见认证信息。
type Grant struct {
	// ID 是本次不可重复使用的 Grant 身份。
	ID string
	// Token 是随机执行凭据，只能写入受管连接文件。
	Token string
	// Epoch 是当前 Core 生命周期身份。
	Epoch string
}

// lease 保存服务端的真实执行绑定。
type lease struct {
	// id 限定连接文件中的 Grant 身份。
	id string
	// executionID 限定来源执行，用于撤销和诊断。
	executionID string
	// bindings 保证调用和回执查询使用相同的对象范围。
	bindings map[string]Binding
	// fileRoots 保存当前执行唯一的文件读取范围。
	fileRoots []string
	// record 写入当前执行的安全命令事实。
	record func(context.Context, contract.Operation, Result) error
	// recordMutex 串行化同执行命令事实的去重登记。
	recordMutex sync.Mutex
	// recorded 按真实回执身份去重，重放不制造另一项成功活动。
	recorded map[string]bool
	// calls 保证撤销取消并收敛全部在途调用后，Execution 才提交终态。
	calls sync.WaitGroup
	// validate 检查领域身份仍有效。
	validate func(context.Context) error
	// ctx 在执行撤销时取消所有在途观察。
	ctx context.Context
	// cancel 撤销该租约并终止在途请求。
	cancel context.CancelFunc
	// waits 限制同一执行最多四个并发有界观察。
	waits chan struct{}
	// active 标识 Runtime 已经可以消费执行能力。
	active bool
}

// Registry 只保存活跃授权，关闭后不再签发或接纳调用。
type Registry struct {
	// mutex 保护活跃租约与关闭状态。
	mutex sync.Mutex
	// leases 以令牌摘要为索引，不保留传输令牌明文。
	leases map[string]*lease
	// epoch 区分 Core 重启前后的授权。
	epoch string
	// closed 表示根生命周期已结束。
	closed bool
	// guide 按主题读取系统技能的同源资源。
	guide func(string) (string, error)
}

// New 创建空授权表，不启动 goroutine 或监听器。
func New(guide func(string) (string, error)) *Registry {
	return &Registry{leases: map[string]*lease{}, epoch: util.New(), guide: guide}
}

func tokenKey(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// Prepare 先准备未激活授权；返回的激活与撤销函数都可重复调用。
func (r *Registry) Prepare(spec GrantSpec) (Grant, func(), func(), error) {
	if spec.ExecutionID == "" || spec.Validate == nil {
		return Grant{}, nil, nil, status_error.AgentInputInvalid
	}
	bindings := map[string]Binding{}
	for _, binding := range spec.Bindings {
		op, ok := contract.Find(binding.Operation)
		if !ok || binding.Call == nil || (op.Effect != constants.AgentCLIEffectRead && binding.Receipt == nil) {
			return Grant{}, nil, nil, status_error.AgentInputInvalid
		}
		if _, exists := bindings[binding.Operation]; exists {
			return Grant{}, nil, nil, status_error.AgentInputInvalid
		}
		bindings[binding.Operation] = binding
	}
	grant := Grant{ID: util.New(), Token: util.New() + util.New(), Epoch: r.epoch}
	ctx, cancel := context.WithCancel(context.Background())
	current := &lease{id: grant.ID, executionID: spec.ExecutionID, bindings: bindings, fileRoots: append([]string(nil), spec.FileRoots...), record: spec.Record, recorded: map[string]bool{}, validate: spec.Validate, ctx: ctx, cancel: cancel, waits: make(chan struct{}, 4)}
	key := tokenKey(grant.Token)
	r.mutex.Lock()
	if r.closed {
		r.mutex.Unlock()
		cancel()
		return Grant{}, nil, nil, status_error.AgentConnectionUnavailable
	}
	r.leases[key] = current
	r.mutex.Unlock()
	activate := func() {
		r.mutex.Lock()
		defer r.mutex.Unlock()
		if r.leases[key] == current {
			current.active = true
		}
	}
	revoke := func() { r.mutex.Lock(); delete(r.leases, key); r.mutex.Unlock(); cancel(); current.calls.Wait() }
	return grant, activate, revoke, nil
}

// Close 撤销全部执行并唤醒在途请求，不取消已经接纳的领域工作。
func (r *Registry) Close() {
	r.mutex.Lock()
	r.closed = true
	leases := make([]*lease, 0, len(r.leases))
	for key, current := range r.leases {
		current.cancel()
		leases = append(leases, current)
		delete(r.leases, key)
	}
	r.mutex.Unlock()
	for _, current := range leases {
		current.calls.Wait()
	}
}

// Failure 是可以安全显示给 agent 的稳定失败分类。
type Failure struct {
	// Code 是 status_error 中定义的稳定标识。
	Code string `json:"code"`
	// Message 是有限英文说明，不包含底层错误、URL 或凭据。
	Message string `json:"message"`
	// EffectState 区分未接纳与不能确定的结果。
	EffectState string `json:"effectState"`
	// RetryAdvice 明确允许的恢复动作，不能暗示换键重发。
	RetryAdvice string `json:"retryAdvice"`
	// Details 仅携带领域明确提供的可修正诊断或静态发现契约，不含原始输入。
	Details map[string]any `json:"details,omitempty"`
}

// rejection 仅携带领域模块明确提供的安全修正提示。
type rejection struct {
	// cause 是稳定错误分类。
	cause error
	// message 是不含底层诊断和凭据的业务提示。
	message string
	// details 是经过领域选择的结构化修正信息。
	details map[string]any
}

func (e rejection) Error() string { return e.message }
func (e rejection) Unwrap() error { return e.cause }

// Reject 包装业务模块主动提供的有限英文提示，禁止传入底层 err.Error()。
func Reject(cause error, message string) error { return rejection{cause: cause, message: message} }

// RejectWithDetails 附加安全诊断；不得传入源码、凭据或底层异常对象。
func RejectWithDetails(cause error, message string, details map[string]any) error {
	return rejection{cause: cause, message: message, details: details}
}

// Response 是 HTTP 与 CLI 共用的单结果信封。
type Response struct {
	// ProtocolVersion 是当前同包协议版本。
	ProtocolVersion int `json:"protocolVersion"`
	// OK 表示请求处理成功，不表示异步业务完成。
	OK bool `json:"ok"`
	// Operation 标明本次读取或执行的命令。
	Operation string `json:"operation,omitempty"`
	// Completion 是真实动作的完成语义。
	Completion string `json:"completion,omitempty"`
	// Result 保存领域回执及其真实数据。
	Result
	// Error 只在失败时返回稳定分类。
	Error *Failure `json:"error,omitempty"`
}

// ErrorResponse 将已定义项目错误投影为有限安全说明。
func ErrorResponse(operation string, err error) Response {
	code := status_error.AgentConnectionUnavailable.Key()
	var keyed interface{ Key() string }
	if errors.As(err, &keyed) {
		code = keyed.Key()
	}
	advice, state := "none", "not_accepted"
	if errors.Is(err, status_error.AgentCommandOutcomeUnknown) {
		advice, state = "query_receipt", "unknown"
	}
	if errors.Is(err, status_error.DatabaseBusy) {
		advice = "same_request"
	}
	if errors.Is(err, status_error.AgentContextUnauthorized) {
		advice = "refresh_context"
	}
	message := code
	if errors.Is(err, status_error.AgentContextUnauthorized) {
		message = "The execution context is invalid or has been revoked. Stop dependent calls; the host must provide a new authorized execution context. Do not retry with this context or modify connection files."
	}
	var details map[string]any
	var rejected rejection
	if errors.As(err, &rejected) {
		message = rejected.message
		details = rejected.details
	}
	if errors.Is(err, status_error.AgentInputInvalid) && details == nil {
		if op, ok := contract.Find(operation); ok {
			details = map[string]any{"describeCommand": "allycli describe " + operation, "inputSchema": op.InputSchema}
			if op.Discovery != nil {
				details["discovery"] = op.Discovery
			}
		}
	}
	if errors.Is(err, status_error.WorkflowAuthoringSuperseded) || errors.Is(err, status_error.WorkflowAuthoringFinished) {
		advice = "new_authoring_execution"
		if errors.Is(err, status_error.WorkflowAuthoringSuperseded) {
			message = "A newer execution owns workflow authoring. Stop retrying old calls; continue in the current workflow authoring chat. Closing the editor does not restore this execution."
		} else {
			message = "This workflow authoring execution has ended. Stop retrying; send a new message in the workflow authoring chat to continue. Closing the editor does not restore this execution."
		}
	}
	if errors.Is(err, status_error.WorkflowRevisionConflict) {
		advice = "read_current_source"
	}
	return Response{ProtocolVersion: constants.AgentCLIProtocolVersion, Operation: operation, Error: &Failure{Code: code, Message: message, EffectState: state, RetryAdvice: advice, Details: details}}
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// ServeHTTP 接纳只使用短期执行认证的固定命令协议。
func (r *Registry) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	host, _, addressErr := net.SplitHostPort(request.RemoteAddr)
	ip := net.ParseIP(host)
	if addressErr != nil || ip == nil || !ip.IsLoopback() || request.Header.Get("Origin") != "" {
		writeFailure(w, "", status_error.AgentContextUnauthorized)
		return
	}
	provided, found := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
	r.mutex.Lock()
	current := r.leases[tokenKey(provided)]
	valid := found && provided != "" && current != nil && current.active && !r.closed
	if valid {
		current.calls.Add(1)
	}
	r.mutex.Unlock()
	if !valid {
		writeFailure(w, "", status_error.AgentContextUnauthorized)
		return
	}
	defer current.calls.Done()
	ctx, cancel := context.WithCancel(request.Context())
	ctx = context.WithValue(ctx, fileRootsKey{}, current.fileRoots)
	stop := context.AfterFunc(current.ctx, cancel)
	defer func() { stop(); cancel() }()
	if current.ctx.Err() != nil {
		writeFailure(w, "", status_error.AgentContextUnauthorized)
		return
	}
	if err := current.validate(ctx); err != nil {
		writeFailure(w, "", err)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, constants.AgentCLIPath)
	switch {
	case request.Method == http.MethodGet && path == "/capabilities":
		items := []contract.Operation{}
		for id := range current.bindings {
			op, _ := contract.Find(id)
			op.InputSchema = nil
			op.OutputSchema = nil
			items = append(items, op)
		}
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		writeSuccess(w, "capabilities", constants.AgentCLICompletionImmediate, Result{Data: map[string]any{"operations": items}})
	case request.Method == http.MethodGet && strings.HasPrefix(path, "/operations/"):
		id := strings.TrimPrefix(path, "/operations/")
		if _, ok := current.bindings[id]; !ok {
			writeFailure(w, id, status_error.AgentOperationForbidden)
			return
		}
		op, _ := contract.Find(id)
		writeSuccess(w, "describe", constants.AgentCLICompletionImmediate, Result{Data: op})
	case request.Method == http.MethodGet && strings.HasPrefix(path, "/guides/"):
		id := strings.TrimPrefix(path, "/guides/")
		allowed := false
		for operation := range current.bindings {
			op, _ := contract.Find(operation)
			for _, guide := range op.Guides {
				if guide == id {
					allowed = true
				}
			}
		}
		if !allowed || r.guide == nil {
			writeFailure(w, "guide", status_error.AgentOperationForbidden)
			return
		}
		content, err := r.guide(id)
		if err != nil {
			writeFailure(w, "guide", err)
			return
		}
		writeSuccess(w, "guide", constants.AgentCLICompletionImmediate, Result{Data: map[string]any{"id": id, "content": content}})
	case request.Method == http.MethodGet && path == "/receipts":
		id, key := request.URL.Query().Get("operation"), request.URL.Query().Get("requestId")
		binding, ok := current.bindings[id]
		if !ok || binding.Receipt == nil {
			writeFailure(w, id, status_error.AgentOperationForbidden)
			return
		}
		if !requestIDPattern.MatchString(key) {
			writeFailure(w, id, status_error.AgentInputInvalid)
			return
		}
		result, err := binding.Receipt(ctx, key)
		if err != nil {
			writeFailure(w, id, err)
			return
		}
		result.Replayed = true
		op, _ := contract.Find(id)
		writeSuccess(w, id, op.Completion, result)
	case request.Method == http.MethodPost && path == "/commands":
		var input Request
		decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, constants.AgentCLIRequestMaximumBytes))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
			writeFailure(w, "", status_error.AgentInputInvalid)
			return
		}
		binding, ok := current.bindings[input.Operation]
		if !ok {
			writeFailure(w, input.Operation, status_error.AgentOperationForbidden)
			return
		}
		op, _ := contract.Find(input.Operation)
		if input.Params == nil {
			input.Params = map[string]any{}
		}
		if err := op.Validate(input.Params); err != nil {
			writeFailure(w, input.Operation, err)
			return
		}
		if op.Effect != constants.AgentCLIEffectRead && !requestIDPattern.MatchString(input.RequestID) {
			writeFailure(w, input.Operation, status_error.AgentInputInvalid)
			return
		}
		if strings.HasSuffix(input.Operation, ".wait") {
			select {
			case current.waits <- struct{}{}:
				defer func() { <-current.waits }()
			default:
				writeFailure(w, input.Operation, status_error.DatabaseBusy)
				return
			}
		}
		result, err := binding.Call(ctx, input)
		if err != nil {
			if op.Effect != constants.AgentCLIEffectRead && (errors.Is(err, status_error.DatabaseError) || errors.Is(err, status_error.DatabaseOperationCancelled)) {
				err = status_error.AgentCommandOutcomeUnknown
			}
			writeFailure(w, input.Operation, err)
			return
		}
		if result.CommandID == "" {
			result.CommandID = util.New()
		}
		if current.record != nil {
			current.recordMutex.Lock()
			if !current.recorded[result.CommandID] {
				if err := current.record(ctx, op, result); err == nil {
					current.recorded[result.CommandID] = true
				}
			}
			current.recordMutex.Unlock()
		}
		writeSuccess(w, input.Operation, op.Completion, result)
	default:
		writeFailure(w, "", status_error.AgentInputInvalid)
	}
}

type fileRootsKey struct{}

// FileRoots 返回认证层绑定的冻结目录副本；参数和业务请求不能自行添加目录。
func FileRoots(ctx context.Context) []string {
	roots, _ := ctx.Value(fileRootsKey{}).([]string)
	return append([]string(nil), roots...)
}

func writeSuccess(w http.ResponseWriter, operation, completion string, result Result) {
	response := Response{ProtocolVersion: constants.AgentCLIProtocolVersion, OK: true, Operation: operation, Completion: completion, Result: result}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > constants.AgentCLIRequestMaximumBytes {
		writeFailure(w, operation, status_error.AgentConnectionUnavailable)
		return
	}
	if _, err := w.Write(append(encoded, '\n')); err != nil {
		logx.Errorf("write agent command response failed: operation=%s error=%v", operation, err)
	}
}

func writeFailure(w http.ResponseWriter, operation string, err error) {
	status := http.StatusBadRequest
	var coded interface{ StatusCode() int }
	if errors.As(err, &coded) {
		status = coded.StatusCode()
	}
	w.WriteHeader(status)
	if encodeErr := json.NewEncoder(w).Encode(ErrorResponse(operation, err)); encodeErr != nil {
		logx.Errorf("encode agent command failure failed: operation=%s error=%v", operation, encodeErr)
	}
}
