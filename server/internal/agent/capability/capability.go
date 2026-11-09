// Package capability manages execution capabilities, exact grants, connection files, and domain transaction receipts without owning domain state.
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

// Request is a single invocation that has passed closed-schema validation.
type Request struct {
	// CWD is the CLI process's absolute working directory; it resolves relative paths without expanding file access.
	CWD string `json:"cwd,omitempty"`
	// InputDigests contains content digests computed after the domain freezes real files; HTTP input cannot supply them.
	InputDigests map[string]string `json:"-"`
	// Operation is the exact operation selected for this invocation.
	Operation string `json:"operation"`
	// RequestID is the invocation identity that writes must reuse.
	RequestID string `json:"requestId,omitempty"`
	// Params contains business parameters matching the operation contract exactly.
	Params map[string]any `json:"params"`
}

// Result is the minimal receipt returned after a real domain commit or observation.
type Result struct {
	// CommandID identifies the durable domain receipt; reads may leave it empty.
	CommandID string `json:"commandId,omitempty"`
	// Replayed reports that the original admission result for this request was read.
	Replayed bool `json:"replayed"`
	// Data retains the actual domain result without duplicating the domain state machine.
	Data any `json:"data"`
}

// Binding binds an exact command to its authorized scope and domain commit boundary.
type Binding struct {
	// Operation is the unique operation identity in the static catalog.
	Operation string
	// Call executes the domain operation only after grant and parameter validation.
	Call func(context.Context, Request) (Result, error)
	// Receipt queries the original scoped request in its authoritative store; reads may omit it.
	Receipt func(context.Context, string) (Result, error)
}

// GrantSpec freezes the exact operations available to an execution and its current identity validation.
type GrantSpec struct {
	// ExecutionID is the stable identity of the admitted execution.
	ExecutionID string
	// Bindings is the set of exact invocations granted by domain entry points.
	Bindings []Binding
	// FileRoots freezes directory access during execution preparation and rejects HTTP overrides.
	FileRoots []string
	// FilePaths grants only the validated referenced files, never their parent directories.
	FilePaths []string
	// Record registers real results with their execution; registration failure cannot undo domain admission.
	Record func(context.Context, Operation, Result) error
	// Validate revalidates the execution, role, and current fence on every call.
	Validate func(context.Context) error
}

// Grant holds authentication data for the connection-file preparer, hidden from the model.
type Grant struct {
	// ID is the non-reusable identity of this grant.
	ID string
	// Token is a random execution credential written only to the managed connection file.
	Token string
	// Epoch identifies the current Core lifecycle.
	Epoch string
}

// lease holds the actual server-side execution binding.
type lease struct {
	// id scopes the grant identity in the connection file.
	id string
	// executionID identifies the source execution for revocation and diagnostics.
	executionID string
	// bindings keeps invocation and receipt queries within the same object scope.
	bindings map[string]Binding
	// fileRoots holds the execution's sole file-read scope.
	fileRoots []string
	// filePaths preserves exact file access in the same authenticated execution grant.
	filePaths []string
	// record writes safe command facts to the current execution.
	record func(context.Context, Operation, Result) error
	// recordMutex serializes deduplicated command recording within an execution.
	recordMutex sync.Mutex
	// recorded deduplicates by real receipt identity so replay cannot create another successful activity.
	recorded map[string]bool
	// calls ensures execution termination waits for revocation to cancel and drain all in-flight calls.
	calls sync.WaitGroup
	// validate checks that the domain identity remains valid.
	validate func(context.Context) error
	// ctx cancels all in-flight observations when execution access is revoked.
	ctx context.Context
	// cancel revokes this lease and terminates in-flight requests.
	cancel context.CancelFunc
	// waits limits an execution to four concurrent bounded observations.
	waits chan struct{}
	// active reports whether the runtime can consume execution capabilities.
	active bool
}

// Registry stores only active grants and stops issuing or admitting calls after closure.
type Registry struct {
	// mutex protects active leases and closure state.
	mutex sync.Mutex
	// leases indexes grants by token digest without retaining plaintext transport tokens.
	leases map[string]*lease
	// epoch distinguishes grants across Core restarts.
	epoch string
	// closed reports whether the root lifecycle has ended.
	closed bool
	// guide reads system-skill resources by topic from the same source.
	guide func(string) (string, error)
}

// New creates an empty grant registry without starting goroutines or listeners.
func New(guide func(string) (string, error)) *Registry {
	return &Registry{leases: map[string]*lease{}, epoch: util.New(), guide: guide}
}

func tokenKey(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// Prepare prepares an inactive grant; both returned activation and revocation functions are idempotent.
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
	current := &lease{id: grant.ID, executionID: spec.ExecutionID, bindings: bindings, fileRoots: append([]string(nil), spec.FileRoots...), filePaths: append([]string(nil), spec.FilePaths...), record: spec.Record, recorded: map[string]bool{}, validate: spec.Validate, ctx: ctx, cancel: cancel, waits: make(chan struct{}, 4)}
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

// Close revokes all executions and wakes in-flight requests without cancelling admitted domain work.
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

// Failure is a stable failure classification safe to display to an agent.
type Failure struct {
	// Code is the stable identifier defined in status_error.
	Code string `json:"code"`
	// Message is a bounded English explanation without underlying errors, URLs, or credentials.
	Message string `json:"message"`
	// EffectState distinguishes unadmitted requests from uncertain outcomes.
	EffectState string `json:"effectState"`
	// RetryAdvice explicitly permits recovery actions without suggesting resubmission under a new key.
	RetryAdvice string `json:"retryAdvice"`
	// Details contains only domain-approved corrective diagnostics or static discovery contracts, never raw input.
	Details map[string]any `json:"details,omitempty"`
}

// rejection carries only safe corrective hints explicitly supplied by the domain.
type rejection struct {
	// cause is the stable error classification.
	cause error
	// message is a business hint without underlying diagnostics or credentials.
	message string
	// details contains structured corrective information selected by the domain.
	details map[string]any
}

func (e rejection) Error() string { return e.message }
func (e rejection) Unwrap() error { return e.cause }

// Reject wraps a bounded English business hint; never pass an underlying err.Error().
func Reject(cause error, message string) error { return rejection{cause: cause, message: message} }

// RejectWithDetails attaches safe diagnostics; never pass source code, credentials, or underlying exception objects.
func RejectWithDetails(cause error, message string, details map[string]any) error {
	return rejection{cause: cause, message: message, details: details}
}

// Response is the single-result envelope shared by HTTP and CLI.
type Response struct {
	// ProtocolVersion is the protocol version shipped with this package.
	ProtocolVersion int `json:"protocolVersion"`
	// OK reports successful request handling, not completion of asynchronous work.
	OK bool `json:"ok"`
	// Operation identifies the command read or executed.
	Operation string `json:"operation,omitempty"`
	// Completion describes the actual action's completion semantics.
	Completion string `json:"completion,omitempty"`
	// Result holds the domain receipt and its actual data.
	Result
	// Error returns a stable classification only on failure.
	Error *Failure `json:"error,omitempty"`
}

// ErrorResponse projects defined project errors into bounded safe explanations.
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

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,` + strconv.Itoa(constants.AgentCLIRequestIDMaximumLength-1) + `}$`)

// ValidRequestID validates the idempotency identifier shared by CLI, MCP, and receipt queries.
func ValidRequestID(value string) bool { return requestIDPattern.MatchString(value) }

// ServeHTTP admits the fixed command protocol using only short-lived execution authentication.
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
	// Native session initialization needs the frozen tool catalog before durable
	// admission. Only this metadata route accepts a prepared, inactive grant.
	catalog := request.Method == http.MethodGet && request.URL.Path == constants.AgentCLIPath+constants.AgentCLIMCPCatalogPath
	r.mutex.Lock()
	current := r.leases[tokenKey(provided)]
	valid := found && provided != "" && current != nil && (current.active || catalog) && !r.closed
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
	ctx = context.WithValue(ctx, filePathsKey{}, current.filePaths)
	stop := context.AfterFunc(current.ctx, cancel)
	defer func() { stop(); cancel() }()
	if current.ctx.Err() != nil {
		writeFailure(w, "", status_error.AgentContextUnauthorized)
		return
	}
	if !catalog {
		if err := current.validate(ctx); err != nil {
			writeFailure(w, "", err)
			return
		}
	}
	path := strings.TrimPrefix(request.URL.Path, constants.AgentCLIPath)
	switch {
	case catalog || request.Method == http.MethodGet && path == "/capabilities":
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
		if !ValidRequestID(key) {
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
		if op.Effect != constants.AgentCLIEffectRead && !ValidRequestID(input.RequestID) {
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
type filePathsKey struct{}

// FileRoots returns a copy of the frozen authenticated directories; parameters and business requests cannot add roots.
func FileRoots(ctx context.Context) []string {
	roots, _ := ctx.Value(fileRootsKey{}).([]string)
	return append([]string(nil), roots...)
}

// FilePaths returns a copy of the exact authenticated files; tool parameters cannot add files.
func FilePaths(ctx context.Context) []string {
	files, _ := ctx.Value(filePathsKey{}).([]string)
	return append([]string(nil), files...)
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

// DecodeParams converts parameters already validated against the operation schema without allowing additional invocation identities.
func DecodeParams(params map[string]any, target any) error {
	data, err := json.Marshal(params)
	if err != nil || json.Unmarshal(data, target) != nil {
		return status_error.AgentInputInvalid
	}
	return nil
}

// Page slices the current resource list within explicit bounds without claiming complete coverage.
func Page[T any](items []T, params map[string]any) (map[string]any, error) {
	var query struct {
		// Cursor is the offset explicitly returned by the previous page.
		Cursor string `json:"cursor"`
		// PageNum is the one-based page number.
		PageNum int `json:"pageNum"`
		// PageSize is the bounded page size.
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

// Observe subscribes to exact events before reading authoritative state, with explicit exit, deadline, and buffer-invalidity semantics.
func Observe(ctx context.Context, params map[string]any, subscribe func() events.Subscription, read func(context.Context) (any, bool, error)) (Result, error) {
	var input struct {
		// TimeoutMS is the observation deadline, not the business execution deadline.
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

// Connection is a managed connection descriptor read only by the CLI process and excluded from model instructions.
type Connection struct {
	// ProtocolVersion verifies the CLI and Core protocol shipped together.
	ProtocolVersion int `json:"protocolVersion"`
	// Endpoint is the Core address validated for loopback access and a fixed path.
	Endpoint string `json:"endpoint"`
	// CoreEpoch distinguishes Core lifecycles.
	CoreEpoch string `json:"coreEpoch"`
	// GrantID identifies the current execution grant.
	GrantID string `json:"grantId"`
	// Token is a credential stored only in the managed file and request headers.
	Token string `json:"token"`
}

// Validate ensures that the connection address cannot forward execution tokens externally.
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

// ReadConnection reads one complete descriptor once; in-flight commands cannot switch to the next turn's identity.
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

// WriteConnection publishes the file in an application-exclusive directory and rejects symlink replacement of its parent or target.
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

// Receipt is the minimal admission fact committed together with the domain change.
type Receipt struct {
	// ID remains stable for the same scope, operation, and request.
	ID string
	// Digest rejects replay with different content under the same request identity.
	Digest string
	// Data records the first admission result without credentials or full input.
	Data map[string]any
}

// Store must use the same domain transaction connection supplied through Context.
type Store interface {
	Find(context.Context, string) (Receipt, bool, error)
	Create(context.Context, Receipt) error
}

// pending passes uncommitted facts only through the current call stack.
type pending struct {
	// store is the receipt repository in the business database.
	store Store
	// receipt freezes the identity and input digest.
	receipt Receipt
}

type contextKey struct{}

// Active reports whether this call must commit admission within its own domain transaction.
func Active(ctx context.Context) bool { _, ok := ctx.Value(contextKey{}).(pending); return ok }

// ID computes a server-owned scoped command identity that cannot be reused across scenarios.
func ID(scope, operation, requestID string) string {
	encoded, _ := json.Marshal([]string{scope, operation, requestID})
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

// Lookup reads admitted facts without attempting to execute the command again.
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

// Execute replays before invoking the domain command; only Commit in the same database transaction constitutes success.
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
		// The original admission remains valid even if post-commit dispatch or projection fails.
		result.Replayed = callErr != nil
		return result, nil
	}
	if callErr != nil {
		return Result{}, callErr
	}
	return Result{}, status_error.AgentCommandOutcomeUnknown
}

// Commit is explicitly called within the domain mutation transaction; ordinary business entry points create no CLI receipts.
func Commit(ctx context.Context, data map[string]any) error {
	value, ok := ctx.Value(contextKey{}).(pending)
	if !ok {
		return nil
	}
	value.receipt.Data = data
	return value.store.Create(ctx, value.receipt)
}
