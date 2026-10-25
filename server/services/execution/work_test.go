package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// testMessageInput 统一表达测试中的创建首发和已有对话续发输入。
type testMessageInput struct {
	// ChatID 是续发测试使用的既有对话标识。
	ChatID string
	// ClientRequestID 是首发测试稳定复用的客户端幂等标识；为空时自动生成。
	ClientRequestID string
	// NewChat 是首发测试使用的对话身份和工作范围。
	NewChat *testChatInput
	// Message 是测试提交的用户输入。
	Message userMessage
	// Runtime 是测试提交的 Runtime 选择。
	Runtime runtimeSelection
}

func TestHiddenEntryCreatesStandardChatAndPreservesInitialExecution(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir(), &fakeRuntime{})
	ctx := context.Background()
	intent := WorkIntent{
		ClientRequestID: "automatic-run-receipt",
		ExecutionID:     util.New(),
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir(), HiddenFromWakerHistory: true}},
		Input:           WorkInput{Actor: constants.AutomaticTaskActor, Content: "整理项目资料"},
		Options:         WorkOptions{RuntimeID: runtimes.RuntimeIDCodex, PermissionLevel: runtimes.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyContinue},
		Admission:       constants.ExecutionAdmissionCommitBeforeDispatch,
	}
	accepted, err := manager.execution.Start(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	module := manager.chats.(chatservice.Module)
	initial, err := module.ReadInitialExecution(ctx, accepted.Chat.ID)
	if err != nil || !initial.Chat.HiddenFromWakerHistory || initial.InitialExecution == nil || initial.InitialExecution.ID != intent.ExecutionID {
		t.Fatalf("hidden standard Chat facts: %+v %v", initial, err)
	}
	listed, err := manager.chats.ListChats(ctx, chatservice.ListChatsInput{WakerID: "waker-1", Limit: 20})
	if err != nil || len(listed.Resources) != 0 {
		t.Fatalf("hidden Chat entered normal history: %+v %v", listed, err)
	}
	replayed, err := manager.execution.Start(ctx, intent)
	if err != nil || !replayed.Replayed || replayed.Chat.ID != accepted.Chat.ID || replayed.Execution.ID != intent.ExecutionID {
		t.Fatalf("stable first admission: %+v %v", replayed, err)
	}
	changed := *intent.Chat.Create
	changed.HiddenFromWakerHistory = false
	changedIntent := intent
	changedIntent.Chat.Create = &changed
	if _, err := manager.execution.Start(ctx, changedIntent); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("changed visibility reused original identity: %v", err)
	}
	continued, err := manager.sendTestMessage(ctx, testMessageInput{ChatID: accepted.Chat.ID, Message: userMessage{Text: "继续整理"}, Runtime: runtimeSelection{RuntimeID: runtimes.RuntimeIDCodex}})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	after, err := module.ReadInitialExecution(ctx, accepted.Chat.ID)
	if err != nil || after.InitialExecution.ID != intent.ExecutionID || after.InitialExecution.ID == continued.Execution.ID || after.InitialExecution.Status != initial.InitialExecution.Status {
		t.Fatalf("continuation changed first execution: %+v %v", after, err)
	}
}

func TestWorkIntentWorkspaceReadOnlyFreezesNonMutationPolicy(t *testing.T) {
	logDirectory := t.TempDir()
	runtime := &fakeRuntime{}
	manager := newMessageTestService(t, logDirectory, runtime)
	accepted, err := manager.sendTestMessage(context.Background(), testMessageInput{
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: t.TempDir(),
		},
		Message: userMessage{Text: "只读分析并规划任务"},
		Runtime: runtimeSelection{
			RuntimeID:         runtimes.RuntimeIDCodex,
			PermissionLevel:   runtimes.PermissionLevelFullAccess,
			WorkspaceReadOnly: true,
		},
	})
	if err != nil {
		t.Fatalf("send read-only work intent: %v", err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	if len(runtime.requests) != 1 {
		t.Fatalf("runtime requests = %d, want 1", len(runtime.requests))
	}
	request := runtime.requests[0]
	testExecutable, executableErr := os.Executable()
	if executableErr != nil {
		t.Fatal(executableErr)
	}
	if request.Detection.State != runtimes.DetectionStateReady ||
		request.Detection.ResolvedBinary != testExecutable {
		t.Fatalf("runtime detection snapshot = %+v", request.Detection)
	}
	if !request.Policy.Execution.WorkspaceReadOnly {
		t.Fatal("runtime execution policy lost workspace read-only restriction")
	}
	if len(request.Workspace.AdditionalWritableDirectories) != 0 {
		t.Fatalf("read-only runtime received writable directories: %+v", request.Workspace.AdditionalWritableDirectories)
	}
	store, err := eventlog.NewStore(logDirectory)
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	records, err := store.ReadAll(accepted.Chat.ID)
	if err != nil {
		t.Fatalf("read coordinator execution facts: %v", err)
	}
	var created eventlog.ExecutionCreatedData
	for _, record := range records {
		if record.Type != constants.ChatEventTypeExecutionCreated {
			continue
		}
		if err := json.Unmarshal(record.Data, &created); err != nil {
			t.Fatalf("decode coordinator execution fact: %v", err)
		}
		break
	}
	if !created.WorkspaceReadOnly || !accepted.Execution.WorkspaceReadOnly {
		t.Fatalf("read-only fact was not persisted: event=%+v execution=%+v", created, accepted.Execution)
	}
}

// testChatInput 保存测试创建对话时的身份与工作范围。
type testChatInput struct {
	// WakerID 是测试对话绑定的伙伴标识。
	WakerID string
	// ProjectID 是测试选择的项目标识。
	ProjectID string
	// WorkingDirectory 是测试选择的临时目录。
	WorkingDirectory string
}

// messageTestService 在测试内统一驱动创建首发和已有对话续发两个真实用例。
type messageTestService struct {
	// execution 接收入口无关的 WorkIntent 并管理 Runtime 生命周期。
	execution Core
	// chats 提供权威事实查询和 Chat 元数据用例。
	chats chatservice.Service
	// profiles 允许测试在两次执行之间修改当前伙伴档案。
	profiles controllers.ProfileDocumentController
	// projects 允许测试准备带多目录配置的真实项目。
	projects controllers.ProjectController
	// projectSources 允许测试准备项目引用的真实目录索引。
	projectSources controllers.ProjectSourceController
	// wakers 允许测试为团队聊天准备多个伙伴身份。
	wakers controllers.WakerController
}

// sendTestMessage 把测试输入分派到 CreateChat 或 SendMessage。
func (s messageTestService) sendTestMessage(
	ctx context.Context,
	input testMessageInput,
) (AcceptedExecution, error) {
	if input.Runtime.PermissionLevel == "" {
		input.Runtime.PermissionLevel = runtimes.PermissionLevelAuto
	}
	intent := WorkIntent{
		Chat: ChatTarget{ChatID: input.ChatID},
		Input: WorkInput{
			Actor:          "test-user",
			Content:        input.Message.Text,
			Attachments:    append([]AttachmentInput(nil), input.Message.Attachments...),
			SkillIDs:       append([]string(nil), input.Message.SkillIDs...),
			ConnectorIDs:   append([]string(nil), input.Message.ConnectorIDs...),
			CodeReferences: append([]CodeReferenceInput(nil), input.Message.CodeReferences...),
		},
		Options: WorkOptions{
			AccountID:         input.Runtime.AccountID,
			RuntimeID:         input.Runtime.RuntimeID,
			Model:             input.Runtime.Model,
			Reasoning:         input.Runtime.Reasoning,
			PermissionLevel:   input.Runtime.PermissionLevel,
			WorkspaceReadOnly: input.Runtime.WorkspaceReadOnly,
			SessionPolicy:     constants.RuntimeSessionPolicyContinue,
		},
		Admission: constants.ExecutionAdmissionCommitOnRuntimeAccept,
	}
	if input.NewChat == nil {
		if input.ChatID == "" {
			return AcceptedExecution{}, status_error.BadRequest
		}
		return s.execution.Start(ctx, intent)
	}
	intent.Chat = ChatTarget{Create: &CreateChatSpec{
		WakerID:          input.NewChat.WakerID,
		ProjectID:        input.NewChat.ProjectID,
		WorkingDirectory: input.NewChat.WorkingDirectory,
	}}
	intent.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	if intent.ClientRequestID == "" {
		intent.ClientRequestID = util.New()
	}
	return s.execution.Start(ctx, intent)
}

// fakeRuntime 是消息执行测试使用的 Runtime 边界替身。
type fakeRuntime struct {
	// requests 记录 Chat 交给 Runtime 的结构化请求。
	requests []runtimes.RunRequest
	// result 是没有配置结果序列时返回的固定终态。
	result runtimes.Result
	// results 是按调用顺序返回的终态序列。
	results []runtimes.Result
	// failureStage 非空时模拟 Runtime 接收首条输入前失败。
	failureStage string
	// starts 记录 Runtime 已经接受首条输入的调用次数。
	starts int
	// panicOnStart 模拟 Runtime 内部异常被收敛为执行失败。
	panicOnStart bool
}

// ForkSession 不参与消息执行测试。
func (r *fakeRuntime) ForkSession(context.Context, runtimes.ForkSessionRequest) (runtimes.ForkSessionResult, error) {
	panic("unexpected ForkSession call")
}

// gatedRuntime 在测试允许前不接受 Invocation，用于验证派发前接纳语义。
type gatedRuntime struct {
	// fakeRuntime 提供固定 Runtime 摘要。
	fakeRuntime
	// entered 在 Runtime Run 已经开始时关闭。
	entered chan struct{}
	// proceed 在测试允许 Runtime 发出启动事件时关闭。
	proceed chan struct{}
}

// Run 等待测试门闩，然后交付唯一启动事件并正常结束。
func (r *gatedRuntime) Run(
	ctx context.Context,
	request runtimes.RunRequest,
	sink runtimes.EventSink,
) runtimes.Result {
	close(r.entered)
	select {
	case <-ctx.Done():
		return runtimes.Result{ExitCode: -1, Failure: &runtimes.Failure{
			Code:       runtimes.FailureCodeContextCanceled,
			Stage:      "runtime",
			Message:    ctx.Err().Error(),
			Transience: runtimes.FailureTransiencePermanent,
		}}
	case <-r.proceed:
	}
	invocationID := "gated-invocation"
	started, _ := json.Marshal(runtimes.InvocationStartedData{InvocationID: invocationID})
	if err := sink.Accept(ctx, runtimes.Event{
		Type: runtimes.EventTypeInvocationStarted,
		Data: started,
	}); err != nil {
		return runtimes.Result{ExitCode: -1, Failure: &runtimes.Failure{
			Code:       runtimes.FailureCodeEventDelivery,
			Stage:      "initial_delivery",
			Message:    err.Error(),
			Transience: runtimes.FailureTransiencePermanent,
		}}
	}
	return runtimes.Result{Facts: runtimes.InvocationFacts{
		InvocationID:  invocationID,
		RuntimeID:     request.Target.RuntimeID,
		BaseRuntimeID: runtimes.RuntimeIDCodex,
	}}
}

// trackingSkillController 是提交测试用于确认聊天选择只读技能关系的替身。
type trackingSkillController struct {
	// SkillController 仅补齐消息执行未使用的其他仓储端口。
	controllers.SkillController
	// enabled 是伙伴当前已经持久启用的技能快照。
	enabled []models.SkillWithState
	// upsertCalls 记录是否错误地通过聊天选择修改了持久启停关系。
	upsertCalls int
}

// CreatePackage 不参与聊天提交测试。
func (c *trackingSkillController) CreatePackage(context.Context, models.SkillPackage) error {
	return nil
}

// GetPackage 不参与聊天提交测试。
func (c *trackingSkillController) GetPackage(context.Context, string) (models.SkillPackage, error) {
	return models.SkillPackage{}, nil
}

// ListVisible 不参与聊天提交测试。
func (c *trackingSkillController) ListVisible(context.Context, string, string) ([]models.SkillWithState, error) {
	return nil, nil
}

// CreateVersion 不参与聊天提交测试。
func (c *trackingSkillController) CreateVersion(context.Context, models.SkillVersion, bool, string) error {
	return nil
}

// ListVersions 不参与聊天提交测试。
func (c *trackingSkillController) ListVersions(context.Context, string) ([]models.SkillVersion, error) {
	return nil, nil
}

// GetVersion 不参与聊天提交测试。
func (c *trackingSkillController) GetVersion(context.Context, string) (models.SkillVersion, error) {
	return models.SkillVersion{}, nil
}

// DeleteVersion 不参与聊天提交测试，仅满足技能仓储的当前接口。
func (c *trackingSkillController) DeleteVersion(context.Context, string, string) (models.SkillVersion, error) {
	return models.SkillVersion{}, nil
}

// ListVersionContentPaths 不参与聊天提交测试。
func (c *trackingSkillController) ListVersionContentPaths(context.Context) ([]string, error) {
	return nil, nil
}

// SetCurrentVersion 不参与聊天提交测试。
func (c *trackingSkillController) SetCurrentVersion(context.Context, string, string) error {
	return nil
}

// EnsureWakerSkill 记录任何不应由聊天提交触发的默认关系写入。
func (c *trackingSkillController) EnsureWakerSkill(ctx context.Context, relation models.WakerSkill) error {
	return c.UpsertWakerSkill(ctx, relation)
}

// UpsertWakerSkill 记录任何不应由聊天提交触发的持久关系写入。
func (c *trackingSkillController) UpsertWakerSkill(context.Context, models.WakerSkill) error {
	c.upsertCalls++
	return nil
}

// ListEnabled 返回伙伴在提交开始前已经启用的技能快照。
func (c *trackingSkillController) ListEnabled(context.Context, string) ([]models.SkillWithState, error) {
	return append([]models.SkillWithState(nil), c.enabled...), nil
}

// DeleteWakerSkill 不参与聊天提交测试。
func (c *trackingSkillController) DeleteWakerSkill(context.Context, string, string) error {
	return nil
}

// ListPrivateVersionsByWaker 不参与聊天提交测试。
func (c *trackingSkillController) ListPrivateVersionsByWaker(context.Context, string) ([]models.SkillVersion, error) {
	return nil, nil
}

// DeleteByWaker 不参与聊天提交测试。
func (c *trackingSkillController) DeleteByWaker(context.Context, string) error {
	return nil
}

// DeletePackage 不参与聊天提交测试。
func (c *trackingSkillController) DeletePackage(context.Context, string) error {
	return nil
}

// UpdatePackageDisplayName 不参与聊天提交测试。
func (c *trackingSkillController) UpdatePackageDisplayName(context.Context, string, string) error {
	return nil
}

// Runtimes 返回测试使用的固定 Codex 工作区布局。
func (r *fakeRuntime) Runtimes() []runtimes.RuntimeSummary {
	return []runtimes.RuntimeSummary{{
		ID:                  runtimes.RuntimeIDCodex,
		BaseRuntimeID:       runtimes.RuntimeIDCodex,
		InstructionFileName: "AGENTS.md",
		SkillDirectoryName:  ".agents",
	}}
}

// Run 模拟首条输入交付、增量事件和 Runtime 终态。
func (r *fakeRuntime) Run(
	ctx context.Context,
	request runtimes.RunRequest,
	sink runtimes.EventSink,
) runtimes.Result {
	r.requests = append(r.requests, request)
	if r.failureStage != "" {
		return runtimes.Result{ExitCode: -1, Failure: &runtimes.Failure{
			Code:       runtimes.FailureCodeRuntimeUnavailable,
			Stage:      r.failureStage,
			Message:    "runtime unavailable",
			Transience: runtimes.FailureTransiencePermanent,
		}}
	}
	r.starts++
	invocationID := "test-invocation-" + string(rune('0'+r.starts))
	started, _ := json.Marshal(runtimes.InvocationStartedData{
		InvocationID: invocationID,
	})
	if err := sink.Accept(ctx, runtimes.Event{
		Type: runtimes.EventTypeInvocationStarted,
		Data: started,
	}); err != nil {
		return runtimes.Result{ExitCode: -1, Failure: &runtimes.Failure{
			Code:       runtimes.FailureCodeEventDelivery,
			Stage:      "initial_delivery",
			Message:    err.Error(),
			Transience: runtimes.FailureTransiencePermanent,
		}}
	}
	if r.panicOnStart {
		return runtimes.Result{ExitCode: -1, Failure: &runtimes.Failure{
			Code:       runtimes.FailureCodeProcessExited,
			Stage:      "runtime",
			Message:    "runtime execution failed unexpectedly",
			Transience: runtimes.FailureTransiencePermanent,
		}}
	}
	payload, _ := json.Marshal(map[string]string{"text": "执行完成"})
	if err := sink.Accept(ctx, runtimes.Event{Type: runtimes.EventTypeTextDelta, Data: payload}); err != nil {
		return runtimes.Result{ExitCode: -1, Failure: &runtimes.Failure{
			Code:       runtimes.FailureCodeEventDelivery,
			Stage:      "event_delivery",
			Message:    err.Error(),
			Transience: runtimes.FailureTransiencePermanent,
		}}
	}
	result := r.result
	if index := r.starts - 1; index < len(r.results) {
		result = r.results[index]
	}
	result.Facts = runtimes.InvocationFacts{
		InvocationID:  invocationID,
		RuntimeID:     request.Target.RuntimeID,
		BaseRuntimeID: runtimes.RuntimeIDCodex,
		Model:         request.Policy.Model,
		Reasoning:     request.Policy.Reasoning,
		Policy:        request.Policy.Execution,
	}
	result.Session.Mode = request.Session.Mode
	return result
}

// cancelableRuntime 返回一个直到调用 Context 取消才结束的 Runtime。
type cancelableRuntime struct {
	// started 在 Start 已收到调用 Context 后关闭。
	started chan struct{}
}

// ForkSession 不参与取消执行测试。
func (r *cancelableRuntime) ForkSession(context.Context, runtimes.ForkSessionRequest) (runtimes.ForkSessionResult, error) {
	panic("unexpected ForkSession call")
}

// Runtimes 返回测试使用的 Codex 工作区布局。
func (r *cancelableRuntime) Runtimes() []runtimes.RuntimeSummary {
	return (&fakeRuntime{}).Runtimes()
}

// Run 先确认首条输入已经交付，再等待 Context 取消。
func (r *cancelableRuntime) Run(ctx context.Context, _ runtimes.RunRequest, sink runtimes.EventSink) runtimes.Result {
	started, _ := json.Marshal(runtimes.InvocationStartedData{InvocationID: "cancelable-invocation"})
	if err := sink.Accept(ctx, runtimes.Event{Type: runtimes.EventTypeInvocationStarted, Data: started}); err != nil {
		return runtimes.Result{ExitCode: -1, Failure: &runtimes.Failure{Code: runtimes.FailureCodeEventDelivery, Message: err.Error()}}
	}
	close(r.started)
	<-ctx.Done()
	return runtimes.Result{ExitCode: -1, Failure: &runtimes.Failure{
		Code:       runtimes.FailureCodeContextCanceled,
		Stage:      "runtime",
		Message:    ctx.Err().Error(),
		Transience: runtimes.FailureTransiencePermanent,
	}}
}

func TestSendMessagePreStartFailureLeavesNoAuthoritativeMessageOrExecution(
	t *testing.T,
) {
	ctx := context.Background()
	logDirectory := t.TempDir()
	workingDirectory := t.TempDir()
	runtime := &fakeRuntime{failureStage: "prepare"}
	manager := newMessageTestService(t, logDirectory, runtime)

	request := testMessageInput{
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: workingDirectory,
		},
		Message: userMessage{Text: "不要留下半提交消息"},
		Runtime: runtimeSelection{
			RuntimeID: "codex",
			Model:     "gpt-5",
			Reasoning: "high",
		},
	}
	if _, err := manager.sendTestMessage(ctx, request); !errors.Is(
		err,
		status_error.RuntimeUnavailable,
	) {
		t.Fatalf("sendTestMessage() error = %v, want RuntimeUnavailable", err)
	}
	// 再次提交仍然进入 Runtime，证明第一次失败已经释放进程内执行槽位。
	if _, err := manager.sendTestMessage(ctx, request); !errors.Is(
		err,
		status_error.RuntimeUnavailable,
	) {
		t.Fatalf("second sendTestMessage() error = %v, want RuntimeUnavailable", err)
	}
	if len(runtime.requests) != 2 {
		t.Fatalf("Runtime Run calls = %d, want 2", len(runtime.requests))
	}
	entries, err := os.ReadDir(logDirectory)
	if err != nil {
		t.Fatalf("ReadDir(logDirectory) error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("authoritative log files = %v, want none", entries)
	}
}

func TestCreateChatReplaysSameFirstRequestAndRejectsChangedContent(t *testing.T) {
	runtime := &fakeRuntime{}
	manager := newMessageTestService(t, t.TempDir(), runtime)
	request := testMessageInput{
		ClientRequestID: "stable-create-request",
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: t.TempDir(),
		},
		Message: userMessage{Text: "只创建一次对话"},
		Runtime: runtimeSelection{
			RuntimeID:       constants.RuntimeIDCodex,
			PermissionLevel: constants.PermissionLevelAuto,
		},
	}
	first, err := manager.sendTestMessage(context.Background(), request)
	if err != nil {
		t.Fatalf("first send error = %v", err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	replayed, err := manager.sendTestMessage(context.Background(), request)
	if err != nil {
		t.Fatalf("replay send error = %v", err)
	}
	if !replayed.Replayed || replayed.Chat.ID != first.Chat.ID ||
		replayed.Execution.ID != first.Execution.ID || replayed.Execution.CompletedAt == nil {
		t.Fatalf("replayed execution = %+v, first = %+v", replayed, first)
	}
	if runtime.starts != 1 {
		t.Fatalf("runtime starts = %d, want 1", runtime.starts)
	}
	request.Message.Text = "不能复用同一个标识发送其他内容"
	if _, err := manager.sendTestMessage(context.Background(), request); !errors.Is(
		err,
		status_error.CreateRequestConflict,
	) {
		t.Fatalf("changed replay error = %v, want CreateRequestConflict", err)
	}
}

func TestCreateChatRejectsConcurrentRequestBeforeAuthorityCommit(t *testing.T) {
	runtime := &gatedRuntime{
		entered: make(chan struct{}),
		proceed: make(chan struct{}),
	}
	manager := newMessageTestService(t, t.TempDir(), runtime)
	request := testMessageInput{
		ClientRequestID: "concurrent-create-request",
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: t.TempDir(),
		},
		Message: userMessage{Text: "并发首发只执行一次"},
		Runtime: runtimeSelection{
			RuntimeID:       constants.RuntimeIDCodex,
			PermissionLevel: constants.PermissionLevelAuto,
		},
	}
	type startResult struct {
		// accepted 是首次请求得到的已接纳执行。
		accepted AcceptedExecution
		// err 是首次请求返回的错误。
		err error
	}
	firstResult := make(chan startResult, 1)
	go func() {
		accepted, err := manager.sendTestMessage(context.Background(), request)
		firstResult <- startResult{accepted: accepted, err: err}
	}()
	<-runtime.entered
	if _, err := manager.sendTestMessage(context.Background(), request); !errors.Is(
		err,
		status_error.CreateRequestInProgress,
	) {
		t.Fatalf("concurrent send error = %v, want CreateRequestInProgress", err)
	}
	close(runtime.proceed)
	result := <-firstResult
	if result.err != nil {
		t.Fatalf("first send error = %v", result.err)
	}
	waitForChatTerminal(t, manager, result.accepted.Chat.ID)
}

func TestBuildRunRequestReplaysDeduplicatedMemberReportContext(t *testing.T) {
	manager := &service{}
	request := manager.buildRunRequest(
		messageChat{
			chat: models.Chat{ID: "lead-chat"},
			previousMessages: []messageview.Message{
				{
					ID:      "member-report",
					Role:    constants.MessageRoleUser,
					Actor:   string(constants.ChannelMessageActorMemberReport),
					Content: "bounded report body",
				},
				{
					ID:          "leader-summary",
					Role:        constants.MessageRoleAssistant,
					ExecutionID: "leader-execution",
					Status:      constants.ExecutionStatusCompleted,
					Content:     "leader summary",
					DisplayParts: []messageview.MessageDisplayPart{{
						Type: constants.MessageDisplayPartText, Text: "leader summary",
					}},
				},
			},
			runtimeSessions: map[string]models.RuntimeSession{},
		},
		pendingMessage{
			message: userMessage{Text: "current bounded report delivery"},
			runtime: runtimeSelection{RuntimeID: constants.RuntimeIDCodex},
		},
		&preparedMessageFiles{},
		nil,
		nil,
		models.RuntimeEnvironment{},
	)
	if len(request.History) != 2 ||
		!strings.Contains(request.History[0].Content, "Team member result id=member-report") ||
		!strings.Contains(request.History[0].Content, "bounded report body") ||
		request.History[1].Content != "leader summary" {
		t.Fatalf("runtime history = %+v, want member report and leader-authored history", request.History)
	}
	if request.Message.Text != "current bounded report delivery" {
		t.Fatalf("current report delivery = %q", request.Message.Text)
	}
}

func TestBuildRunRequestOmitsReportsRepeatedInCurrentNotification(t *testing.T) {
	manager := &service{}
	request := manager.buildRunRequest(
		messageChat{
			chat: models.Chat{ID: "lead-chat"},
			previousMessages: []messageview.Message{
				{ID: "earlier-report", Role: constants.MessageRoleUser, Actor: string(constants.ChannelMessageActorMemberReport), Content: "earlier result"},
				{ID: "current-report", Role: constants.MessageRoleUser, Actor: string(constants.ChannelMessageActorMemberReport), Content: "current result"},
				{ID: "lead-reply", Role: constants.MessageRoleAssistant, Status: constants.ExecutionStatusCompleted,
					Content: "prior reply", DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartText, Text: "prior reply"}}},
			},
			runtimeSessions: map[string]models.RuntimeSession{},
		},
		pendingMessage{
			message:                 userMessage{Text: "current result"},
			runtime:                 runtimeSelection{RuntimeID: constants.RuntimeIDCodex},
			historyMessageIDsToOmit: []string{"current-report"},
		},
		&preparedMessageFiles{}, nil, nil, models.RuntimeEnvironment{},
	)
	if len(request.History) != 2 ||
		!strings.Contains(request.History[0].Content, "earlier result") ||
		request.History[1].Content != "prior reply" ||
		request.Message.Text != "current result" {
		t.Fatalf("notification request repeats or loses a report: %+v", request)
	}
}

func TestWorkIntentCommitBeforeDispatchPersistsPendingFactsBeforeRuntimeAccepts(
	t *testing.T,
) {
	logDirectory := t.TempDir()
	runtime := &gatedRuntime{
		entered: make(chan struct{}),
		proceed: make(chan struct{}),
	}
	manager := newMessageTestService(t, logDirectory, runtime)
	accepted, err := manager.execution.Start(context.Background(), WorkIntent{
		ClientRequestID: "commit-before-dispatch-request",
		Chat: ChatTarget{Create: &CreateChatSpec{
			WakerID:          "waker-1",
			WorkingDirectory: t.TempDir(),
		}},
		Input: WorkInput{
			Actor:   "automation",
			Content: "可靠收件后再派发",
		},
		Options: WorkOptions{
			RuntimeID:         runtimes.RuntimeIDCodex,
			PermissionLevel:   runtimes.PermissionLevelAuto,
			SessionPolicy:     constants.RuntimeSessionPolicyNew,
			ParentExecutionID: "parent-execution-1",
		},
		Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if accepted.Execution.Status != constants.ExecutionStatusPending {
		t.Fatalf("accepted status = %q, want pending", accepted.Execution.Status)
	}
	select {
	case <-runtime.entered:
	case <-time.After(time.Second):
		t.Fatal("Runtime was not dispatched")
	}
	store, err := eventlog.NewStore(logDirectory)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	records, err := store.ReadAll(accepted.Chat.ID)
	if err != nil {
		t.Fatalf("ReadAll() before Runtime accept error = %v", err)
	}
	if len(records) != 3 ||
		records[0].Type != constants.ChatEventTypeCreated ||
		records[1].Type != constants.ChatEventTypeUserMessageCreated ||
		records[2].Type != constants.ChatEventTypeExecutionCreated {
		t.Fatalf("pre-dispatch facts = %+v", records)
	}
	var message eventlog.MessageCreatedData
	if err := json.Unmarshal(records[1].Data, &message); err != nil {
		t.Fatalf("decode input fact: %v", err)
	}
	if message.Actor != "automation" {
		t.Fatalf("input actor = %q", message.Actor)
	}
	var created eventlog.ExecutionCreatedData
	if err := json.Unmarshal(records[2].Data, &created); err != nil {
		t.Fatalf("decode execution fact: %v", err)
	}
	if created.ParentExecutionID != "parent-execution-1" {
		t.Fatalf("parent execution = %q", created.ParentExecutionID)
	}
	close(runtime.proceed)
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	records, err = store.ReadAll(accepted.Chat.ID)
	if err != nil {
		t.Fatalf("ReadAll() after Runtime accept error = %v", err)
	}
	if records[3].Type != constants.ChatEventTypeExecutionStarted ||
		records[len(records)-1].Type != constants.ChatEventTypeExecutionCompleted {
		t.Fatalf("post-dispatch facts = %+v", records)
	}
}

func TestSendMessageMapsSkillPreparationFailure(t *testing.T) {
	logDirectory := t.TempDir()
	manager := newMessageTestService(
		t,
		logDirectory,
		&fakeRuntime{failureStage: "runtime_files"},
	)
	_, err := manager.sendTestMessage(context.Background(), testMessageInput{
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: t.TempDir(),
		},
		Message: userMessage{Text: "使用当前技能"},
		Runtime: runtimeSelection{
			RuntimeID: runtimes.RuntimeIDCodex,
		},
	})
	if !errors.Is(err, status_error.SkillUnavailable) {
		t.Fatalf("sendTestMessage() error = %v, want SkillUnavailable", err)
	}
	entries, readErr := os.ReadDir(logDirectory)
	if readErr != nil {
		t.Fatalf("ReadDir(logDirectory) error = %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("authoritative log files = %v, want none", entries)
	}
}

func TestSendMessageDeliversEnabledSkillsWithoutChangingRelations(t *testing.T) {
	skillDataDirectory := t.TempDir()
	skills := &trackingSkillController{enabled: []models.SkillWithState{
		{
			SkillPackage: models.SkillPackage{ID: "skill-selected", Name: "review"},
			CurrentVersion: models.SkillVersion{
				ID:            "version-selected",
				Version:       "1.2.0",
				Name:          "review",
				ContentPath:   filepath.Join("skills", "versions", "skill-selected", "version-selected"),
				ContentDigest: "selected-digest",
			},
			Enabled: true,
		},
		{
			SkillPackage: models.SkillPackage{ID: "skill-background", Name: "diagnose"},
			CurrentVersion: models.SkillVersion{
				ID:            "version-background",
				Version:       "2.0.0",
				Name:          "diagnose",
				ContentPath:   filepath.Join("skills", "versions", "skill-background", "version-background"),
				ContentDigest: "background-digest",
			},
			Enabled: true,
		},
	}}
	runtime := &fakeRuntime{failureStage: "prepare"}
	manager := newMessageTestService(
		t,
		t.TempDir(),
		runtime,
		messageTestOptions{
			skills:             skills,
			skillDataDirectory: skillDataDirectory,
		},
	)
	request := testMessageInput{
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: t.TempDir(),
		},
		Message: userMessage{
			Text:     "只在本轮调用代码审查",
			SkillIDs: []string{"skill-selected"},
		},
		Runtime: runtimeSelection{RuntimeID: runtimes.RuntimeIDCodex},
	}
	if _, err := manager.sendTestMessage(context.Background(), request); !errors.Is(err, status_error.RuntimeUnavailable) {
		t.Fatalf("sendTestMessage() error = %v, want RuntimeUnavailable", err)
	}
	if len(runtime.requests) != 1 || len(runtime.requests[0].Skills) != 2 {
		t.Fatalf("Runtime requests = %+v, want both enabled skills", runtime.requests)
	}
	selectedByName := make(map[string]runtimes.Skill, len(runtime.requests[0].Skills))
	for _, skill := range runtime.requests[0].Skills {
		selectedByName[skill.Name] = skill
	}
	if !selectedByName["review"].Selected || selectedByName["diagnose"].Selected {
		t.Fatalf("skill selection = %+v", selectedByName)
	}
	if selectedByName["review"].Path != filepath.Join(
		skillDataDirectory,
		"skills",
		"versions",
		"skill-selected",
		"version-selected",
	) {
		t.Fatalf("selected skill path = %q", selectedByName["review"].Path)
	}
	if skills.upsertCalls != 0 {
		t.Fatalf("UpsertWakerSkill calls = %d, want 0", skills.upsertCalls)
	}

	request.Message.SkillIDs = []string{"skill-disabled"}
	if _, err := manager.sendTestMessage(context.Background(), request); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("disabled skill error = %v, want BadRequest", err)
	}
	if len(runtime.requests) != 1 {
		t.Fatalf("Runtime Run calls = %d after disabled selection, want 1", len(runtime.requests))
	}
	if skills.upsertCalls != 0 {
		t.Fatalf("UpsertWakerSkill calls = %d after rejection, want 0", skills.upsertCalls)
	}
}

func TestSendMessageRunsThroughRuntimeAndPersistsCompletedChat(
	t *testing.T,
) {
	skillDataDirectory := t.TempDir()
	skillContentPath := filepath.Join("skills", "versions", "skill-review", "version-review")
	if err := os.MkdirAll(filepath.Join(skillDataDirectory, skillContentPath), 0o700); err != nil {
		t.Fatal(err)
	}
	skills := &trackingSkillController{enabled: []models.SkillWithState{{
		SkillPackage: models.SkillPackage{ID: "skill-review", Name: "代码审查"},
		CurrentVersion: models.SkillVersion{
			ID:          "version-review",
			Version:     "1.4.0",
			ContentPath: skillContentPath,
		},
		Enabled: true,
	}}}
	runtime := &fakeRuntime{}
	manager := newMessageTestService(
		t,
		t.TempDir(),
		runtime,
		messageTestOptions{
			skills:             skills,
			skillDataDirectory: skillDataDirectory,
		},
	)
	workingDirectory := t.TempDir()
	resolvedWorkingDirectory, err := filepath.EvalSymlinks(workingDirectory)
	if err != nil {
		t.Fatalf("resolve temporary working directory: %v", err)
	}
	result, err := manager.sendTestMessage(
		context.Background(),
		testMessageInput{
			NewChat: &testChatInput{
				WakerID:          "waker-1",
				WorkingDirectory: workingDirectory,
			},
			Message: userMessage{
				Text:     "通过 Runtime 执行",
				SkillIDs: []string{"skill-review"},
			},
			Runtime: runtimeSelection{
				RuntimeID: runtimes.RuntimeIDCodex,
				Model:     "gpt-5",
				Reasoning: "high",
			},
		},
	)
	if err != nil {
		t.Fatalf("sendTestMessage() error = %v", err)
	}
	if result.Chat.AdditionalWorkingDirectories[0] != resolvedWorkingDirectory {
		t.Fatalf(
			"temporary default working directory = %q, want %q",
			result.Chat.AdditionalWorkingDirectories[0],
			resolvedWorkingDirectory,
		)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		summary, summaryErr := manager.chats.GetChatSummary(
			context.Background(),
			result.Chat.ID,
		)
		if summaryErr == nil && summary.Chat.ActiveExecutionID == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("execution did not reach terminal state: %v", summaryErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	history, err := manager.chats.ReadMessageHistory(
		context.Background(),
		result.Chat.ID,
		0,
		10,
	)
	if err != nil {
		t.Fatalf("ReadMessageHistory() error = %v", err)
	}
	if len(history.Resources) != 2 ||
		history.Resources[0].Content != "通过 Runtime 执行" ||
		len(history.Resources[0].Skills) != 1 ||
		history.Resources[0].Skills[0].ID != "skill-review" ||
		history.Resources[0].Skills[0].Name != "代码审查" ||
		history.Resources[0].Skills[0].Version != "1.4.0" ||
		history.Resources[1].Content != "执行完成" ||
		history.Resources[1].Status != constants.ExecutionStatusCompleted {
		t.Fatalf("message history = %+v", history.Resources)
	}
	if runtime.starts != 1 {
		t.Fatalf("Runtime Run calls = %d, want 1", runtime.starts)
	}
}

func TestSendMessageKeepsWholeFileCodeReferencesOutOfAttachments(t *testing.T) {
	project := t.TempDir()
	referencedFile := filepath.Join(project, "server", "service.go")
	if err := os.MkdirAll(filepath.Dir(referencedFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(referencedFile, []byte("package server"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := &fakeRuntime{}
	manager := newMessageTestService(
		t,
		t.TempDir(),
		runtime,
	)

	result, err := manager.sendTestMessage(context.Background(), testMessageInput{
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: project,
		},
		Message: userMessage{
			Text: "检查这个文件",
			CodeReferences: []CodeReferenceInput{{
				Path: referencedFile,
			}},
		},
		Runtime: runtimeSelection{RuntimeID: runtimes.RuntimeIDCodex},
	})
	if err != nil {
		t.Fatalf("sendTestMessage() error = %v", err)
	}
	waitForChatTerminal(t, manager, result.Chat.ID)

	history, err := manager.chats.ReadMessageHistory(
		context.Background(),
		result.Chat.ID,
		0,
		10,
	)
	if err != nil {
		t.Fatalf("ReadMessageHistory() error = %v", err)
	}
	userMessage := history.Resources[0]
	if len(userMessage.Attachments) != 0 || len(userMessage.CodeReferences) != 1 ||
		userMessage.CodeReferences[0].Path != "projects/"+filepath.Base(project)+"/server/service.go" {
		t.Fatalf("user message files = attachments:%+v references:%+v", userMessage.Attachments, userMessage.CodeReferences)
	}
	attachmentEntries, err := os.ReadDir(filepath.Join(result.Chat.WorkspaceDirectory, "attachments"))
	if err != nil {
		t.Fatalf("ReadDir(attachments) error = %v", err)
	}
	if len(attachmentEntries) != 1 ||
		attachmentEntries[0].Name() != constants.ExecutionWorkspaceGeneratedFilesDirectory ||
		!attachmentEntries[0].IsDir() {
		t.Fatalf(
			"attachment entries = %v, want only %s directory",
			attachmentEntries,
			constants.ExecutionWorkspaceGeneratedFilesDirectory,
		)
	}
	if runtime.starts != 1 {
		t.Fatalf("Runtime Run calls = %d, want 1", runtime.starts)
	}
}

func TestSendMessageForwardsAbsoluteCodeSnippetPath(t *testing.T) {
	absolutePath := filepath.Join(t.TempDir(), "AI新闻简报.md")
	runtime := &fakeRuntime{}
	manager := newMessageTestService(
		t,
		t.TempDir(),
		runtime,
	)

	result, err := manager.sendTestMessage(context.Background(), testMessageInput{
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: t.TempDir(),
		},
		Message: userMessage{
			Text: "这是什么",
			CodeReferences: []CodeReferenceInput{{
				Path:      absolutePath,
				StartLine: 13,
				EndLine:   13,
				Content:   "其中政策与市场两项变化最快。",
			}},
		},
		Runtime: runtimeSelection{RuntimeID: runtimes.RuntimeIDCodex},
	})
	if err != nil {
		t.Fatalf("sendTestMessage() error = %v", err)
	}
	waitForChatTerminal(t, manager, result.Chat.ID)

	if len(runtime.requests) != 1 || len(runtime.requests[0].Message.CodeReferences) != 1 {
		t.Fatalf("runtime code references = %+v", runtime.requests)
	}
	reference := runtime.requests[0].Message.CodeReferences[0]
	if reference.Path != absolutePath || reference.StartLine != 13 ||
		reference.EndLine != 13 || reference.Content != "其中政策与市场两项变化最快。" {
		t.Fatalf("runtime code reference = %+v", reference)
	}
}

func TestSendMessageRollsBackNewWorkspaceWhenWholeFileReferenceIsOutsideProject(t *testing.T) {
	project := t.TempDir()
	outsideFile := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outsideFile, []byte("package outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	logDirectory := t.TempDir()
	manager := newMessageTestService(
		t,
		logDirectory,
		&fakeRuntime{failureStage: "prepare"},
	)

	_, err := manager.sendTestMessage(context.Background(), testMessageInput{
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: project,
		},
		Message: userMessage{
			Text: "检查范围外文件",
			CodeReferences: []CodeReferenceInput{{
				Path: outsideFile,
			}},
		},
		Runtime: runtimeSelection{RuntimeID: runtimes.RuntimeIDCodex},
	})
	if !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("sendTestMessage() error = %v, want BadRequest", err)
	}
	entries, readErr := os.ReadDir(logDirectory)
	if readErr != nil {
		t.Fatalf("ReadDir(logDirectory) error = %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("chat workspace entries = %v, want none", entries)
	}
}

func TestSendMessagePersistsSessionAndResumesAfterModelChange(t *testing.T) {
	runtime := &fakeRuntime{
		result: runtimes.Result{
			Session: runtimes.SessionResult{
				OutputID: "session-1",
				State:    runtimes.SessionStateCreated,
			},
		},
	}
	manager := newMessageTestService(
		t,
		t.TempDir(),
		runtime,
	)
	first, err := manager.sendTestMessage(context.Background(), testMessageInput{
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: t.TempDir(),
		},
		Message: userMessage{Text: "建立 Session"},
		Runtime: runtimeSelection{
			RuntimeID: runtimes.RuntimeIDCodex,
			Model:     "gpt-5",
			Reasoning: "high",
		},
	})
	if err != nil {
		t.Fatalf("first sendTestMessage() error = %v", err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)

	second, err := manager.sendTestMessage(context.Background(), testMessageInput{
		ChatID:  first.Chat.ID,
		Message: userMessage{Text: "继续 Session"},
		Runtime: runtimeSelection{
			RuntimeID: runtimes.RuntimeIDCodex,
			Model:     "gpt-5.1",
			Reasoning: "medium",
		},
	})
	if err != nil {
		t.Fatalf("second sendTestMessage() error = %v", err)
	}
	waitForChatTerminal(t, manager, second.Chat.ID)
	if runtime.starts != 2 {
		t.Fatalf("Runtime Run calls = %d, want 2", runtime.starts)
	}
}

// forkContinuationRuntime 仅替换真实 CLI 分叉，保留工作接纳和权威日志流程。
type forkContinuationRuntime struct {
	// fakeRuntime 提供会话执行及结构化请求记录。
	fakeRuntime
}

func (r *forkContinuationRuntime) ForkSession(context.Context, runtimes.ForkSessionRequest) (runtimes.ForkSessionResult, error) {
	return runtimes.ForkSessionResult{SessionID: "fork-child-session"}, nil
}

func TestForkFirstContinuationValidatesFrozenNativePluginScope(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprintf("external_changed=%t", changed), func(t *testing.T) {
			runtime := &forkContinuationRuntime{fakeRuntime: fakeRuntime{result: runtimes.Result{Session: runtimes.SessionResult{OutputID: "parent-session", State: runtimes.SessionStateCreated, ForkMode: "turn", ForkPosition: "parent-turn"}}}}
			environment, _ := (readyRuntimeEnvironmentReader{}).Get(context.Background(), runtimes.RuntimeIDCodex)
			environment.BaseRuntimeID = runtimes.RuntimeIDCodex
			manager := newMessageTestService(t, t.TempDir(), runtime, messageTestOptions{runtimeEnvironments: configurationEnvironmentReader{environment: environment}})
			first, err := manager.sendTestMessage(context.Background(), testMessageInput{NewChat: &testChatInput{WakerID: "waker-1", WorkingDirectory: t.TempDir()}, Message: userMessage{Text: "原会话"}, Runtime: runtimeSelection{RuntimeID: runtimes.RuntimeIDCodex}})
			if err != nil {
				t.Fatal(err)
			}
			waitForChatTerminal(t, manager, first.Chat.ID)
			child, err := manager.execution.ForkChat(context.Background(), chatservice.ForkInput{ChatID: first.Chat.ID, ExecutionID: first.Execution.ID, ClientRequestID: "fork-scope"})
			if err != nil {
				t.Fatal(err)
			}
			if changed {
				home := runtime.requests[0].Target.ConfigHome
				if err := os.MkdirAll(home, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[plugins.\"external@community\"]\nenabled = true\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			continued, err := manager.sendTestMessage(context.Background(), testMessageInput{ChatID: child, Message: userMessage{Text: "继续分支"}, Runtime: runtimeSelection{RuntimeID: runtimes.RuntimeIDCodex}})
			if changed {
				if !errors.Is(err, status_error.PluginNotReady) {
					t.Fatalf("changed native scope accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unchanged fork cannot continue without a local scope cache: %v", err)
			}
			waitForChatTerminal(t, manager, continued.Chat.ID)
			if runtime.requests[1].Session.Mode != runtimes.SessionModeResume || runtime.requests[1].Session.ID != "fork-child-session" {
				t.Fatal("fork continuation lost the native child session")
			}
		})
	}
}

func TestSendMessageMergesSupplementalInstructionsBeforeToolCapabilities(t *testing.T) {
	runtime := &fakeRuntime{}
	manager := newMessageTestService(t, t.TempDir(), runtime)
	accepted, err := manager.execution.Start(context.Background(), WorkIntent{
		ClientRequestID: "supplemental-instructions-request",
		Chat: ChatTarget{Create: &CreateChatSpec{
			WakerID:          "waker-1",
			WorkingDirectory: t.TempDir(),
		}},
		Input: WorkInput{
			Actor:   "test-user",
			Content: "规划团队任务",
		},
		Options: WorkOptions{
			RuntimeID:       runtimes.RuntimeIDCodex,
			PermissionLevel: runtimes.PermissionLevelAuto,
			SessionPolicy:   constants.RuntimeSessionPolicyContinue,
			SupplementalInstructions: []runtimes.Instruction{{
				Name:    "TEAM.md",
				Content: "团队协作规则",
			}},
		},
		Admission: constants.ExecutionAdmissionCommitOnRuntimeAccept,
	})
	if err != nil {
		t.Fatalf("start work intent: %v", err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	if len(runtime.requests) != 1 {
		t.Fatalf("runtime requests = %d, want 1", len(runtime.requests))
	}
	names := make([]string, 0, len(runtime.requests[0].Instructions))
	for _, instruction := range runtime.requests[0].Instructions {
		names = append(names, instruction.Name)
	}
	if !slices.Contains(names, "TEAM.md") {
		t.Fatalf("missing TEAM.md: %v", names)
	}
	if !hasInstructionContent(runtime.requests[0].Instructions, "团队协作规则") {
		t.Fatalf("missing supplemental instruction content: %+v", runtime.requests[0].Instructions)
	}
	if len(runtime.requests[0].Connectors) != 0 {
		t.Fatalf("internal team tool must be mounted without a user selection hint: %+v", runtime.requests[0].Connectors)
	}
	if len(runtime.requests[0].Policy.Execution.TrustedControlTools) != 0 {
		t.Fatalf(
			"writable execution unexpectedly trusted internal tools: %+v",
			runtime.requests[0].Policy.Execution.TrustedControlTools,
		)
	}
}

func TestSendMessageReadsCurrentProfileAndProjectInstructions(t *testing.T) {
	project := t.TempDir()
	instructionPath := filepath.Join(project, "AGENTS.md")
	if err := os.WriteFile(instructionPath, []byte("项目约束 v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := &fakeRuntime{}
	manager := newMessageTestService(t, t.TempDir(), runtime)
	first, err := manager.sendTestMessage(context.Background(), testMessageInput{
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: project,
		},
		Message: userMessage{Text: "第一次执行"},
		Runtime: runtimeSelection{
			RuntimeID: runtimes.RuntimeIDCodex,
		},
	})
	if err != nil {
		t.Fatalf("first message error = %v", err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	if err := manager.profiles.UpdateWakerDocument(
		context.Background(),
		"waker-1",
		constants.ProfileDocumentKindIdentity,
		"伙伴档案 v2",
		nowMillis(),
	); err != nil {
		t.Fatalf("update profile document: %v", err)
	}
	if err := os.WriteFile(instructionPath, []byte("项目约束 v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := manager.sendTestMessage(context.Background(), testMessageInput{
		ChatID:  first.Chat.ID,
		Message: userMessage{Text: "第二次执行"},
		Runtime: runtimeSelection{
			RuntimeID: runtimes.RuntimeIDCodex,
		},
	})
	if err != nil {
		t.Fatalf("second message error = %v", err)
	}
	waitForChatTerminal(t, manager, second.Chat.ID)
	if len(runtime.requests) != 2 {
		t.Fatalf("Runtime requests = %d, want 2", len(runtime.requests))
	}
	current := runtime.requests[1].Instructions
	if !hasInstructionContent(current, "伙伴档案 v2") ||
		!hasInstructionContent(current, "项目约束 v2") ||
		hasInstructionContent(current, "项目约束 v1") {
		t.Fatalf("second Runtime instructions = %+v", current)
	}
	for _, instruction := range current {
		if instruction.Name == constants.ProfileDocumentMemoryFileName {
			t.Fatalf("legacy MEMORY.md instruction was injected: %+v", current)
		}
	}
}

func TestSendMessageFreezesDefaultDirectoryAndLoadsOnlyEnabledProjectInstructions(t *testing.T) {
	firstDirectory := t.TempDir()
	secondDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(firstDirectory, "AGENTS.md"), []byte("目录 A 指令"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secondDirectory, "AGENTS.md"), []byte("目录 B 指令"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := &fakeRuntime{}
	manager := newMessageTestService(t, t.TempDir(), runtime)
	now := nowMillis()
	firstSource, err := resolveWorkingDirectory(firstDirectory)
	if err != nil {
		t.Fatalf("resolve first project directory: %v", err)
	}
	secondSource, err := resolveWorkingDirectory(secondDirectory)
	if err != nil {
		t.Fatalf("resolve second project directory: %v", err)
	}
	for _, source := range []models.ProjectSource{firstSource, secondSource} {
		source.CreatedAt = now
		source.UpdatedAt = now
		if err := manager.projectSources.Ensure(context.Background(), source); err != nil {
			t.Fatalf("ensure project directory: %v", err)
		}
	}
	project := models.Project{
		ID:        "project-with-default-directory",
		WakerID:   "waker-1",
		Name:      "多目录项目",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := manager.projects.Create(context.Background(), project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := manager.projects.ReplaceDirectories(
		context.Background(),
		project.ID,
		[]models.ProjectDirectory{
			{Source: firstSource, LoadInstructions: false},
			{Source: secondSource, LoadInstructions: true, IsDefault: true},
		},
	); err != nil {
		t.Fatalf("configure project directories: %v", err)
	}

	result, err := manager.sendTestMessage(context.Background(), testMessageInput{
		NewChat: &testChatInput{
			WakerID:   "waker-1",
			ProjectID: project.ID,
		},
		Message: userMessage{Text: "检查项目目录配置"},
		Runtime: runtimeSelection{
			RuntimeID: runtimes.RuntimeIDCodex,
		},
	})
	if err != nil {
		t.Fatalf("send project message: %v", err)
	}
	waitForChatTerminal(t, manager, result.Chat.ID)
	if result.Chat.AdditionalWorkingDirectories[0] != secondSource.Path {
		t.Fatalf(
			"default working directory = %q, want %q",
			result.Chat.AdditionalWorkingDirectories[0],
			secondSource.Path,
		)
	}
	if len(runtime.requests) != 1 ||
		hasInstructionContent(runtime.requests[0].Instructions, "目录 A 指令") ||
		!hasInstructionContent(runtime.requests[0].Instructions, "目录 B 指令") {
		t.Fatalf("Runtime instructions = %+v", runtime.requests)
	}
	projectDirectories := runtime.requests[0].Workspace.ProjectDirectories
	if len(projectDirectories) != 2 {
		t.Fatalf("Runtime project directories = %+v, want two entries", projectDirectories)
	}
	wantDefaultPath := filepath.ToSlash(filepath.Join("projects", secondSource.Name))
	for _, directory := range projectDirectories {
		if directory.Default != (directory.Path == wantDefaultPath) {
			t.Fatalf(
				"Runtime project directories = %+v, want only %q as default",
				projectDirectories,
				wantDefaultPath,
			)
		}
	}
}

func hasInstructionContent(instructions []runtimes.Instruction, content string) bool {
	for _, instruction := range instructions {
		if instruction.Content == content {
			return true
		}
	}
	return false
}

func TestSelectedConnectorReferencesRequireEnabledConnectors(t *testing.T) {
	enabled := []models.MCPConnector{{
		ID:   "connector-browser",
		Name: "浏览器自动化",
	}}
	if !selectedConnectorsAreEnabled([]string{"connector-browser"}, enabled) {
		t.Fatal("enabled connector selection was rejected")
	}
	if selectedConnectorsAreEnabled([]string{"connector-disabled"}, enabled) {
		t.Fatal("disabled connector selection was accepted")
	}
	references := selectedConnectorReferences(
		[]string{"connector-browser"},
		enabled,
	)
	if len(references) != 1 || references[0].ID != "connector-browser" ||
		references[0].Name != "浏览器自动化" {
		t.Fatalf("connector references = %+v", references)
	}
}

func TestSendMessageDoesNotRebuildMissingSession(t *testing.T) {
	runtime := &fakeRuntime{
		results: []runtimes.Result{
			{
				Session: runtimes.SessionResult{
					OutputID: "session-1",
					State:    runtimes.SessionStateCreated,
				},
			},
			{
				Session: runtimes.SessionResult{State: runtimes.SessionStateMissing},
				Failure: &runtimes.Failure{
					Code:       runtimes.FailureCodeSessionNotFound,
					Stage:      "runtime-protocol",
					Message:    "session missing",
					Transience: runtimes.FailureTransiencePermanent,
				},
			},
		},
	}
	manager := newMessageTestService(
		t,
		t.TempDir(),
		runtime,
	)
	first, err := manager.sendTestMessage(context.Background(), testMessageInput{
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: t.TempDir(),
		},
		Message: userMessage{Text: "建立旧 Session"},
		Runtime: runtimeSelection{RuntimeID: runtimes.RuntimeIDCodex},
	})
	if err != nil {
		t.Fatalf("first sendTestMessage() error = %v", err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	second, err := manager.sendTestMessage(context.Background(), testMessageInput{
		ChatID:  first.Chat.ID,
		Message: userMessage{Text: "恢复丢失 Session"},
		Runtime: runtimeSelection{RuntimeID: runtimes.RuntimeIDCodex},
	})
	if err != nil {
		t.Fatalf("second sendTestMessage() error = %v", err)
	}
	waitForChatTerminal(t, manager, second.Chat.ID)
	if runtime.starts != 2 {
		t.Fatalf("Runtime Run calls = %d, want 2 without transcript reseed", runtime.starts)
	}
	history, err := manager.chats.ReadMessageHistory(
		context.Background(),
		second.Chat.ID,
		0,
		10,
	)
	if err != nil {
		t.Fatalf("ReadMessageHistory() error = %v", err)
	}
	assistant := history.Resources[len(history.Resources)-1]
	if assistant.Status != constants.ExecutionStatusFailed ||
		assistant.ErrorCode != string(runtimes.FailureCodeSessionNotFound) {
		t.Fatalf("assistant terminal = %+v, want session-not-found failure", assistant)
	}
}

func TestSendMessageContainsRuntimeFailureAndReleasesChat(t *testing.T) {
	runtime := &fakeRuntime{
		panicOnStart: true,
	}
	manager := newMessageTestService(
		t,
		t.TempDir(),
		runtime,
	)
	result, err := manager.sendTestMessage(context.Background(), testMessageInput{
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: t.TempDir(),
		},
		Message: userMessage{Text: "触发 Backend panic"},
		Runtime: runtimeSelection{RuntimeID: runtimes.RuntimeIDCodex},
	})
	if err != nil {
		t.Fatalf("sendTestMessage() error = %v", err)
	}
	waitForChatTerminal(t, manager, result.Chat.ID)
	history, err := manager.chats.ReadMessageHistory(
		context.Background(),
		result.Chat.ID,
		0,
		10,
	)
	if err != nil {
		t.Fatalf("ReadMessageHistory() error = %v", err)
	}
	assistant := history.Resources[len(history.Resources)-1]
	if assistant.Status != constants.ExecutionStatusFailed ||
		assistant.ErrorCode != string(runtimes.FailureCodeProcessExited) {
		t.Fatalf("assistant terminal = %+v, want contained runtime failure", assistant)
	}

	runtime.panicOnStart = false
	if _, err := manager.sendTestMessage(context.Background(), testMessageInput{
		ChatID:  result.Chat.ID,
		Message: userMessage{Text: "panic 后继续"},
		Runtime: runtimeSelection{RuntimeID: runtimes.RuntimeIDCodex},
	}); err != nil {
		t.Fatalf("sendTestMessage() after failure error = %v", err)
	}
}

func TestCancelExecutionEndsRuntimeAndReleasesChat(t *testing.T) {
	runtime := &cancelableRuntime{
		started: make(chan struct{}),
	}
	manager := newMessageTestService(
		t,
		t.TempDir(),
		runtime,
	)
	result, err := manager.sendTestMessage(context.Background(), testMessageInput{
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: t.TempDir(),
		},
		Message: userMessage{Text: "等待取消"},
		Runtime: runtimeSelection{RuntimeID: runtimes.RuntimeIDCodex},
	})
	if err != nil {
		t.Fatalf("sendTestMessage() error = %v", err)
	}
	select {
	case <-runtime.started:
	case <-time.After(time.Second):
		t.Fatal("Runtime did not start")
	}
	if err := manager.execution.Cancel(result.Chat.ID); err != nil {
		t.Fatalf("CancelExecution() error = %v", err)
	}
	waitForChatTerminal(t, manager, result.Chat.ID)
	history, err := manager.chats.ReadMessageHistory(
		context.Background(),
		result.Chat.ID,
		0,
		10,
	)
	if err != nil {
		t.Fatalf("ReadMessageHistory() error = %v", err)
	}
	assistant := history.Resources[len(history.Resources)-1]
	if assistant.Status != constants.ExecutionStatusCanceled ||
		assistant.ErrorCode != string(runtimes.FailureCodeContextCanceled) {
		t.Fatalf("assistant terminal = %+v, want canceled", assistant)
	}
}

func TestChatPublicQueriesObserveMessageAndRename(t *testing.T) {
	runtime := &fakeRuntime{}
	manager := newMessageTestService(
		t,
		t.TempDir(),
		runtime,
	)
	result, err := manager.sendTestMessage(context.Background(), testMessageInput{
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: t.TempDir(),
		},
		Message: userMessage{Text: "查询对话"},
		Runtime: runtimeSelection{RuntimeID: runtimes.RuntimeIDCodex},
	})
	if err != nil {
		t.Fatalf("sendTestMessage() error = %v", err)
	}
	waitForChatTerminal(t, manager, result.Chat.ID)
	if err := manager.chats.RenameChat(
		context.Background(),
		result.Chat.ID,
		"  新的   对话标题  ",
	); err != nil {
		t.Fatalf("RenameChat() error = %v", err)
	}
	summary, err := manager.chats.GetChatSummary(
		context.Background(),
		result.Chat.ID,
	)
	if err != nil || summary.Chat.Title != "新的 对话标题" {
		t.Fatalf("GetChatSummary() = %+v, %v", summary, err)
	}
	list, err := manager.chats.ListChats(context.Background(), chatservice.ListChatsInput{
		WakerID: " waker-1 ",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("ListChats() error = %v", err)
	}
	if len(list.Resources) != 1 || list.Resources[0].Title != "新的 对话标题" {
		t.Fatalf("ListChats() = %+v", list)
	}
	if _, err := manager.chats.ListChats(context.Background(), chatservice.ListChatsInput{
		WakerID: "waker-1",
		Limit:   0,
	}); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("invalid ListChats() error = %v", err)
	}
	if err := manager.chats.RenameChat(
		context.Background(),
		result.Chat.ID,
		"",
	); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("invalid RenameChat() error = %v", err)
	}
}

// waitForChatTerminal 等待权威终态与执行占位释放，避免在持久化交接窗口续发。
func waitForChatTerminal(t *testing.T, manager messageTestService, chatID string) {
	t.Helper()
	core := manager.execution.(*service)
	deadline := time.Now().Add(2 * time.Second)
	for {
		summary, err := manager.chats.GetChatSummary(context.Background(), chatID)
		if err == nil && summary.Chat.ActiveExecutionID == "" {
			core.executions.mutex.Lock()
			_, active := core.executions.active[chatID]
			core.executions.mutex.Unlock()
			if !active {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("execution did not reach terminal state: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// newMessageTestService 使用真实临时 SQLite 和文件系统建立消息契约环境。
func newMessageTestService(
	t *testing.T,
	logDirectory string,
	runtime runtimeRunner,
	options ...messageTestOptions,
) messageTestService {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "chat-message.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get test sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	wakers := controllers.NewWakerController(database.DB)
	profiles := controllers.NewProfileDocumentController(database.DB)
	projects := controllers.NewProjectController(database.DB)
	projectSources := controllers.NewProjectSourceController(database.DB)
	if err := wakers.Create(context.Background(), models.Waker{
		ID:   "waker-1",
		Name: "测试伙伴",
	}); err != nil {
		t.Fatalf("create test waker: %v", err)
	}
	documents := make([]models.WakerDocument, 0, 4)
	for index, kind := range []constants.ProfileDocumentKind{
		constants.ProfileDocumentKindIdentity,
		constants.ProfileDocumentKindPersona,
		constants.ProfileDocumentKindPlaybook,
		constants.ProfileDocumentKindMemory,
	} {
		documents = append(documents, models.WakerDocument{
			ID:      "document-" + string(rune('1'+index)),
			WakerID: "waker-1",
			Kind:    kind,
			Content: "# Test",
		})
	}
	if err := profiles.CreateWakerDocuments(context.Background(), documents); err != nil {
		t.Fatalf("create test profile documents: %v", err)
	}

	var selectedOptions messageTestOptions
	if len(options) > 0 {
		selectedOptions = options[0]
	}
	if selectedOptions.skillDataDirectory == "" {
		selectedOptions.skillDataDirectory = t.TempDir()
	}
	transactions := controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	chatModule, err := chatservice.New(chatservice.Config{
		StorageDirectory: logDirectory,
		Projects:         projects,
		QueryIndexes: chatservice.QueryIndexDependencies{
			Chats: controllers.NewChatController(database.DB),
			RuntimeSessions: controllers.NewRuntimeSessionController(
				database.DB,
			),
			DeletionIntents: controllers.NewChatDeletionIntentController(database.DB),
			Transactions:    transactions,
		},
	})
	if err != nil {
		t.Fatalf("new Chat module: %v", err)
	}
	if err := chatModule.Start(context.Background()); err != nil {
		t.Fatalf("start Chat module: %v", err)
	}
	runtimeEnvironments := selectedOptions.runtimeEnvironments
	if runtimeEnvironments == nil {
		runtimeEnvironments = readyRuntimeEnvironmentReader{}
	}
	runtimeEnvironments = isolatedNativeTestEnvironmentReader{source: runtimeEnvironments, root: t.TempDir()}
	executionCore, err := New(Config{
		Runtime:             runtime,
		Journal:             chatModule,
		DormantChats:        chatModule,
		Wakers:              wakers,
		Profiles:            profiles,
		Projects:            projects,
		Skills:              selectedOptions.skills,
		Plugins:             selectedOptions.plugins,
		RuntimeEnvironments: runtimeEnvironments,
		SkillDataDirectory:  selectedOptions.skillDataDirectory,
		StorageDirectory:    logDirectory,
	})
	if err != nil {
		t.Fatalf("new Execution Core: %v", err)
	}
	if err := executionCore.Open(context.Background()); err != nil {
		t.Fatalf("open Execution Core: %v", err)
	}
	t.Cleanup(func() {
		executionCore.Close()
		chatModule.Close()
	})
	return messageTestService{
		execution:      executionCore,
		chats:          chatModule,
		profiles:       profiles,
		projects:       projects,
		projectSources: projectSources,
		wakers:         wakers,
	}
}

// isolatedNativeTestEnvironmentReader 让执行替身始终使用临时原生 home，避免触碰真实用户插件和凭据。
type isolatedNativeTestEnvironmentReader struct {
	// source 保留测试已经声明的模型、权限和能力快照。
	source runtimeEnvironmentReader
	// root 是本测试独占的原生配置根目录。
	root string
}

func (reader isolatedNativeTestEnvironmentReader) Get(ctx context.Context, id string) (models.RuntimeEnvironment, error) {
	entry, err := reader.source.Get(ctx, id)
	if err != nil {
		return entry, err
	}
	if entry.ConfigHome == "" {
		entry.ConfigHome = filepath.Join(reader.root, id)
	}
	if !filepath.IsAbs(entry.Detection.ResolvedBinary) {
		entry.Detection.ResolvedBinary, err = os.Executable()
	}
	return entry, err
}

// readyRuntimeEnvironmentReader 为提交测试提供稳定可用的 Runtime 快照。
type readyRuntimeEnvironmentReader struct{}

// Get 返回提交测试使用的可执行 Runtime 环境。
func (readyRuntimeEnvironmentReader) Get(
	context.Context,
	string,
) (models.RuntimeEnvironment, error) {
	return models.RuntimeEnvironment{
		RuntimeID:    "codex",
		Enabled:      true,
		Binary:       "codex",
		DefaultModel: "default",
		Detection: runtimes.DetectionResult{
			Runtime: runtimes.RuntimeSummary{
				ID:            "codex",
				BaseRuntimeID: runtimes.RuntimeIDCodex,
			},
			State:          runtimes.DetectionStateReady,
			ResolvedBinary: "codex",
			Capabilities: runtimes.CapabilitySet{
				runtimes.CapabilityMCPStreamableHTTP: true,
			},
			Models: []runtimes.ModelOption{
				readyTestModel("default"),
				readyTestModel("gpt-5"),
				readyTestModel("gpt-5.1"),
			},
		},
	}, nil
}

// readyTestModel 构造提交测试中声明全部产品权限的模型目录项。
func readyTestModel(id string) runtimes.ModelOption {
	return runtimes.ModelOption{
		ID:   id,
		Name: id,
		PermissionLevels: []runtimes.PermissionLevel{
			runtimes.PermissionLevelDefault,
			runtimes.PermissionLevelAuto,
			runtimes.PermissionLevelFullAccess,
		},
	}
}

// messageTestOptions 保存仅部分消息测试需要的可选技能依赖。
type messageTestOptions struct {
	// plugins 提供安装快照的只读端口。
	plugins controllers.PluginController
	// skills 提供伙伴已启用技能的只读快照。
	skills controllers.SkillController
	// skillDataDirectory 是断言权威技能版本绝对路径时使用的数据目录。
	skillDataDirectory string
	// runtimeEnvironments 提供权限目录差异测试使用的 Runtime 快照。
	runtimeEnvironments runtimeEnvironmentReader
}

// TestForkContinuationNeverSuppliesInheritedPrompt 验证子会话仅恢复独立 Session 并发送新增输入。
func TestForkContinuationNeverSuppliesInheritedPrompt(t *testing.T) {
	manager := &service{}
	state := messageChat{chat: models.Chat{ID: "child", ForkSourceChatID: "parent"}, previousMessages: []messageview.Message{{ID: "inherited", Role: constants.MessageRoleAssistant, ExecutionID: "boundary", Content: "must-not-reinject", Inherited: true}}, runtimeSessions: map[string]models.RuntimeSession{"custom-codex": {SessionID: "native-child", LastExecutionID: "boundary"}}}
	pending := pendingMessage{message: userMessage{Text: "new-input"}, runtime: runtimeSelection{RuntimeID: "custom-codex"}}
	request := manager.buildRunRequest(state, pending, &preparedMessageFiles{}, nil, nil, models.RuntimeEnvironment{})
	if request.Session.Mode != runtimes.SessionModeResume || request.Session.ID != "native-child" || len(request.History) != 0 || request.Message.Text != "new-input" {
		t.Fatalf("fork continuation: %+v", request)
	}
	pending.runtime.SessionPolicy = constants.RuntimeSessionPolicyNew
	request = manager.buildRunRequest(state, pending, &preparedMessageFiles{}, nil, nil, models.RuntimeEnvironment{})
	if request.Session.Mode != runtimes.SessionModeNew || request.Session.ID != "" {
		t.Fatalf("fork should start new native session: %+v", request.Session)
	}
	pending.runtime.SessionPolicy = ""
	delete(state.runtimeSessions, "custom-codex")
	request = manager.buildRunRequest(state, pending, &preparedMessageFiles{}, nil, nil, models.RuntimeEnvironment{})
	if request.Session.Mode != runtimes.SessionModeResume || len(request.History) != 0 {
		t.Fatal("missing native session fell back to history")
	}
}

func TestIntegrationAutomaticTaskExecutionProvenanceSurvivesJournalReplay(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir(), &fakeRuntime{})
	ctx := context.Background()
	intent := WorkIntent{AutomaticRunID: "original-automatic-run", ClientRequestID: "automatic-provenance", ExecutionID: util.New(), Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir(), HiddenFromWakerHistory: true}}, Input: WorkInput{Actor: constants.AutomaticTaskActor, Content: "整理正文"}, Options: WorkOptions{RuntimeID: runtimes.RuntimeIDCodex, PermissionLevel: runtimes.PermissionLevelAuto}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch}
	accepted, err := manager.execution.Start(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	initial, err := manager.chats.(chatservice.Module).ReadInitialExecution(ctx, accepted.Chat.ID)
	if err != nil || initial.InitialExecution.AutomaticRunID != intent.AutomaticRunID {
		t.Fatal("journal lost trusted automatic origin", initial, err)
	}
	replay, err := manager.execution.Start(ctx, intent)
	if err != nil || replay.Execution.AutomaticRunID != intent.AutomaticRunID {
		t.Fatal(replay, err)
	}
	changed := intent
	changed.AutomaticRunID = "another-run"
	if _, err = manager.execution.Start(ctx, changed); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatal("same execution changed its automatic origin", err)
	}
	continued, err := manager.sendTestMessage(ctx, testMessageInput{ChatID: accepted.Chat.ID, Message: userMessage{Text: "用户后续操作"}, Runtime: runtimeSelection{RuntimeID: runtimes.RuntimeIDCodex}})
	if err != nil {
		t.Fatal(err)
	}
	if continued.Execution.AutomaticRunID != "" {
		t.Fatal("manual continuation inherited API caller", continued.Execution)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
}
