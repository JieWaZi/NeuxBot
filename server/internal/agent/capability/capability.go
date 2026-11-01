// Package capability 管理受管执行的软件能力、精确授权、连接文件和领域事务回执，不拥有领域状态。
package capability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
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
	Record func(context.Context, Operation, Result) error
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
	record func(context.Context, Operation, Result) error
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
		op, ok := Find(binding.Operation)
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
		if op, ok := Find(operation); ok {
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
		items := []Operation{}
		for id := range current.bindings {
			op, _ := Find(id)
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
		op, _ := Find(id)
		writeSuccess(w, "describe", constants.AgentCLICompletionImmediate, Result{Data: op})
	case request.Method == http.MethodGet && strings.HasPrefix(path, "/guides/"):
		id := strings.TrimPrefix(path, "/guides/")
		allowed := false
		for operation := range current.bindings {
			op, _ := Find(operation)
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
		op, _ := Find(id)
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
		op, _ := Find(input.Operation)
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

// DecodeParams 只转换已经通过操作 Schema 的参数，不允许再添加调用身份。
func DecodeParams(params map[string]any, target any) error {
	data, err := json.Marshal(params)
	if err != nil || json.Unmarshal(data, target) != nil {
		return status_error.AgentInputInvalid
	}
	return nil
}

// Page 按明确上限切出当前资源列表，不将一页误称为完整覆盖。
func Page[T any](items []T, params map[string]any) (map[string]any, error) {
	var query struct {
		// Cursor 是上一页明确返回的偏移位置。
		Cursor string `json:"cursor"`
		// PageNum 是从一开始的页码。
		PageNum int `json:"pageNum"`
		// PageSize 是有界页面大小。
		PageSize int `json:"pageSize"`
	}
	if err := DecodeParams(params, &query); err != nil {
		return nil, err
	}
	if query.PageNum == 0 {
		query.PageNum = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 50
	}
	if query.PageNum < 1 || query.PageSize < 1 || query.PageSize > 100 || query.PageNum > 1_000_000 {
		return nil, status_error.AgentInputInvalid
	}
	start := (query.PageNum - 1) * query.PageSize
	if query.Cursor != "" {
		if _, explicit := params["pageNum"]; explicit {
			return nil, status_error.AgentInputInvalid
		}
		value, err := strconv.Atoi(query.Cursor)
		if err != nil || value < 0 || value > 100_000_000 {
			return nil, status_error.AgentInputInvalid
		}
		start = value
	}
	if start > len(items) {
		start = len(items)
	}
	end := min(start+query.PageSize, len(items))
	next := ""
	if end < len(items) {
		next = strconv.Itoa(end)
	}
	return map[string]any{"nextCursor": next, "items": items[start:end], "total": len(items), "pageNum": query.PageNum, "pageSize": query.PageSize}, nil
}

// Observe 先建立精确监听，再读取权威状态；退出、期限和缓冲失效均有明确语义。
func Observe(ctx context.Context, params map[string]any, subscribe func() events.Subscription, read func(context.Context) (any, bool, error)) (Result, error) {
	var input struct {
		// TimeoutMS 是本次观察期限，不是业务运行期限。
		TimeoutMS int64 `json:"timeoutMs"`
	}
	if err := DecodeParams(params, &input); err != nil {
		return Result{}, err
	}
	if input.TimeoutMS == 0 {
		input.TimeoutMS = 30000
	}
	if input.TimeoutMS < 1 || input.TimeoutMS > 60000 {
		return Result{}, status_error.AgentInputInvalid
	}
	subscription := subscribe()
	defer subscription.Close()
	timer := time.NewTimer(time.Duration(input.TimeoutMS) * time.Millisecond)
	defer timer.Stop()
	value, completed, err := read(ctx)
	if err != nil {
		return Result{}, err
	}
	for !completed {
		select {
		case <-ctx.Done():
			return Result{}, status_error.AgentContextUnauthorized
		case <-timer.C:
			return Result{Data: map[string]any{"completed": false, "resource": value}}, nil
		case _, ok := <-subscription.Events:
			if !ok {
				return Result{}, status_error.AgentConnectionUnavailable
			}
			value, completed, err = read(ctx)
			if err != nil {
				return Result{}, err
			}
		}
	}
	return Result{Data: map[string]any{"completed": true, "resource": value}}, nil
}

// Connection 是只供 CLI 进程读取的受管连接描述，不进入模型指令。
type Connection struct {
	// ProtocolVersion 验证 CLI 与 Core 同包协议。
	ProtocolVersion int `json:"protocolVersion"`
	// Endpoint 是经过回环与固定路径校验的 Core 地址。
	Endpoint string `json:"endpoint"`
	// CoreEpoch 区分不同 Core 生命周期。
	CoreEpoch string `json:"coreEpoch"`
	// GrantID 是当前真实执行授权身份。
	GrantID string `json:"grantId"`
	// Token 是只保存在受管文件和请求头中的凭据。
	Token string `json:"token"`
}

// Validate 验证连接地址不能把执行令牌转交外部地址。
func (c Connection) Validate() error {
	u, err := url.Parse(c.Endpoint)
	if err != nil || c.ProtocolVersion != constants.AgentCLIProtocolVersion || c.Token == "" || c.GrantID == "" || c.CoreEpoch == "" || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != constants.AgentCLIPath {
		return status_error.AgentConnectionUnavailable
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || u.Port() == "" {
		return status_error.AgentConnectionUnavailable
	}
	return nil
}

// ReadConnection 固定读取一次完整描述文件；在途命令不能换用下一轮身份。
func ReadConnection(path string) (Connection, error) {
	if !filepath.IsAbs(path) {
		return Connection{}, status_error.AgentContextStorageError
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || !privateFile(info) {
		return Connection{}, status_error.AgentContextStorageError
	}
	file, err := os.Open(path)
	if err != nil {
		return Connection{}, status_error.AgentContextStorageError
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return Connection{}, status_error.AgentContextStorageError
	}
	data, err := io.ReadAll(io.LimitReader(file, constants.AgentCLIContextMaximumBytes+1))
	if err != nil || len(data) > constants.AgentCLIContextMaximumBytes {
		return Connection{}, status_error.AgentContextStorageError
	}
	var result Connection
	if json.Unmarshal(data, &result) != nil {
		return Connection{}, status_error.AgentContextStorageError
	}
	return result, result.Validate()
}

// WriteConnection 在应用独占目录发布文件，拒绝父目录或目标的符号链接替换。
func WriteConnection(directory, filename string, value Connection) (string, error) {
	if !filepath.IsAbs(directory) || !filepath.IsLocal(filename) || strings.ContainsAny(filename, "/\\") {
		return "", status_error.AgentContextStorageError
	}
	if err := value.Validate(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		logx.Errorf("create agent context directory failed: error=%v", err)
		return "", status_error.AgentContextStorageError
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(directory) {
		return "", status_error.AgentContextStorageError
	}
	if err := protectDirectory(directory); err != nil {
		return "", status_error.AgentContextStorageError
	}
	path := filepath.Join(directory, filename)
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return "", status_error.AgentContextStorageError
	} else if err != nil && !os.IsNotExist(err) {
		return "", status_error.AgentContextStorageError
	}
	file, err := os.CreateTemp(directory, "connection-staging-")
	if err != nil {
		return "", status_error.AgentContextStorageError
	}
	defer os.Remove(file.Name())
	data, err := json.Marshal(value)
	if err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return "", status_error.AgentContextStorageError
	}
	if err := replaceConnection(file.Name(), path); err != nil {
		logx.Errorf("publish agent context file failed: error=%v", err)
		return "", status_error.AgentContextStorageError
	}
	return path, nil
}

// Receipt 是与领域变更一同提交的最小接纳事实。
type Receipt struct {
	// ID 对同一作用域、操作和请求保持稳定。
	ID string
	// Digest 拒绝相同请求身份的异内容重放。
	Digest string
	// Data 记录首次接纳结果，不保存凭据或完整输入。
	Data map[string]any
}

// Store 必须使用传入 Context 中领域事务的同一连接。
type Store interface {
	Find(context.Context, string) (Receipt, bool, error)
	Create(context.Context, Receipt) error
}

// pending 只在当前调用栈传递待提交事实。
type pending struct {
	// store 是业务数据库的回执仓储。
	store Store
	// receipt 固定身份和输入摘要。
	receipt Receipt
}

type contextKey struct{}

// Active 表示当前调用必须在自身领域事务内提交接纳事实。
func Active(ctx context.Context) bool { _, ok := ctx.Value(contextKey{}).(pending); return ok }

// ID 根据服务端拥有的作用域计算不可跨场景复用的命令身份。
func ID(scope, operation, requestID string) string {
	encoded, _ := json.Marshal([]string{scope, operation, requestID})
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

// Lookup 读取已接纳事实，不尝试重新执行命令。
func Lookup(ctx context.Context, store Store, scope, operation, requestID string) (Result, error) {
	if store == nil {
		return Result{}, status_error.AgentConnectionUnavailable
	}
	receipt, found, err := store.Find(ctx, ID(scope, operation, requestID))
	if err != nil {
		return Result{}, err
	}
	if !found {
		return Result{}, status_error.AgentReceiptNotFound
	}
	return Result{CommandID: receipt.ID, Replayed: true, Data: receipt.Data}, nil
}

// Execute 先重放，再调用领域命令；只有同库事务内的 Commit 才构成成功。
func Execute(ctx context.Context, store Store, scope string, request Request, call func(context.Context) error) (Result, error) {
	if store == nil {
		return Result{}, status_error.AgentConnectionUnavailable
	}
	encoded, err := json.Marshal(request.Params)
	if len(request.InputDigests) > 0 {
		encoded, err = json.Marshal([]any{request.Params, request.InputDigests})
	}
	if err != nil {
		return Result{}, status_error.AgentInputInvalid
	}
	hash := sha256.Sum256(encoded)
	wanted := Receipt{ID: ID(scope, request.Operation, request.RequestID), Digest: hex.EncodeToString(hash[:])}
	read := func() (Result, bool, error) {
		receipt, found, err := store.Find(ctx, wanted.ID)
		if err != nil || !found {
			return Result{}, found, err
		}
		if receipt.Digest != wanted.Digest {
			return Result{}, true, status_error.AgentCommandRequestConflict
		}
		return Result{CommandID: receipt.ID, Replayed: true, Data: receipt.Data}, true, nil
	}
	if result, found, err := read(); err != nil || found {
		return result, err
	}
	callErr := call(context.WithValue(ctx, contextKey{}, pending{store: store, receipt: wanted}))
	result, found, err := read()
	if err != nil {
		if errors.Is(err, status_error.AgentCommandRequestConflict) {
			return Result{}, err
		}
		return Result{}, status_error.AgentCommandOutcomeUnknown
	}
	if found {
		// 即使提交后的派发或投影失败，原接纳事实仍然成立。
		result.Replayed = callErr != nil
		return result, nil
	}
	if callErr != nil {
		return Result{}, callErr
	}
	return Result{}, status_error.AgentCommandOutcomeUnknown
}

// Commit 由领域在自身目标变更事务内显式调用；普通业务入口不产生 CLI 回执。
func Commit(ctx context.Context, data map[string]any) error {
	value, ok := ctx.Value(contextKey{}).(pending)
	if !ok {
		return nil
	}
	value.receipt.Data = data
	return value.store.Create(ctx, value.receipt)
}
