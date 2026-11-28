package main_conversations

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/cli"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestMainConversationTicketPublishesSeparateMessagesBeforeSettlement(t *testing.T) {
	configureNativeMainConversation(t)
	release := filepath.Join(t.TempDir(), "settle")
	t.Setenv("NEUX_MAIN_POST_RELEASE", release)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	request, _ := gin.CreateTestContext(httptest.NewRecorder())
	request.Request = httptest.NewRequest("POST", "/", nil).WithContext(ctx)
	const partner = "native-main-partner"
	raw, err := (&SendMainConversationMessage{SubjectType: "waker", SubjectID: partner,
		Body: MainConversationInput{SubmissionID: "separate-posts", Content: "fixture ticket posts", StartWork: true}}).Output(request)
	if err != nil {
		t.Fatal(err)
	}
	accepted := raw.(MainConversationAcceptance)
	read := func() MainConversationResponse {
		t.Helper()
		raw, err := (&GetMainConversation{SubjectType: "waker", SubjectID: partner}).Output(request)
		if err != nil {
			t.Fatal(err)
		}
		return raw.(MainConversationResponse)
	}
	active := func() bool {
		return global.ExecutionService.IsExecutionActive(accepted.Message.SourceChatID, accepted.ExecutionID)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	page := read()
	for len(page.Messages) < 3 && active() {
		select {
		case <-ctx.Done():
			t.Fatal("the Worker never published both messages")
		case <-ticker.C:
			page = read()
		}
	}
	if len(page.Messages) != 3 || !active() {
		t.Fatalf("both posts must be readable while the Worker is active: %+v", page.Messages)
	}
	ack, result := page.Messages[1], page.Messages[2]
	if ack.Content != "Checking the fixture sources." || result.Content != "\nVerified fixture evidence.\n" || ack.ID == result.ID ||
		ack.ReplyToMessageID != accepted.Message.ID || result.ReplyToMessageID != accepted.Message.ID {
		t.Fatalf("unexpected posts: %+v, %+v", ack, result)
	}
	if ack.QuoteReply || !result.QuoteReply {
		t.Fatalf("only the later reply should render the requested quote: %+v, %+v", ack, result)
	}
	address := workticket.Address{Kind: constants.WorkTicketOriginMain, ID: accepted.Message.ConversationID}
	for _, id := range []string{ack.ID, result.ID} {
		ticket, found, err := global.WorkTicketService.ForMessage(ctx, address, id)
		if err != nil || !found || ticket.ID != accepted.Message.ID ||
			ticket.Status != constants.WorkTicketStatusActive || ticket.Summary != "" {
			t.Fatalf("a standalone post must link to the current ticket without changing state: %+v, %t, %v", ticket, found, err)
		}
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for active() {
		select {
		case <-ctx.Done():
			t.Fatal("the Worker never settled")
		case <-ticker.C:
		}
	}
	page = read()
	ticket, found, err := global.WorkTicketService.ForMessage(ctx, address, result.ID)
	if err != nil || !found || ticket.Status != constants.WorkTicketStatusCompleted ||
		ticket.Summary != strings.TrimSpace(result.Content) || len(page.Messages) != 3 || page.Messages[2].ID != result.ID {
		t.Fatalf("completion must reuse the published conclusion: ticket=%+v, %t, %v, messages=%+v", ticket, found, err, page.Messages)
	}
	execution, err := global.ChatService.ReadExecution(ctx, accepted.Message.SourceChatID, accepted.ExecutionID)
	if err != nil || execution.Status != constants.ExecutionStatusCompleted {
		t.Fatalf("execution did not complete: %+v, %v", execution, err)
	}
}

func TestMainConversationPublishesPlanAndFiveFindingsBeforeWorkerFinishes(t *testing.T) {
	root := t.TempDir()
	configureNativeMainConversationAt(t, root, "", true)
	t.Setenv("NEUX_MAIN_NEWS_FIXTURE_ROOT", root)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	queue := newNativeMainQueueFixture(t, root)
	if err := queue.Open(ctx); err != nil {
		t.Fatal(err)
	}
	const partner = "native-main-partner"
	request, _ := gin.CreateTestContext(httptest.NewRecorder())
	request.Request = httptest.NewRequest("POST", "/", nil).WithContext(ctx)
	send := SendMainConversationMessage{SubjectType: "waker", SubjectID: partner,
		Body: MainConversationInput{SubmissionID: "news-progress", Content: "fixture 先后采集五条新闻"}}
	raw, err := send.Output(request)
	if err != nil {
		t.Fatal(err)
	}
	queued := raw.(MainConversationAcceptance).QueuedInput
	if queued == nil || queued.WorkChatID != "" {
		t.Fatalf("expected a routing receipt before starting the work: %+v", raw)
	}
	page := func() MainConversationResponse {
		t.Helper()
		out, err := (&GetMainConversation{SubjectType: "waker", SubjectID: partner}).Output(request)
		if err != nil {
			t.Fatal(err)
		}
		return out.(MainConversationResponse)
	}
	var snapshot MainConversationResponse
	waitMainQueueFixture(t, ctx, func() bool {
		snapshot = page()
		return len(snapshot.Messages) >= 2
	})
	if len(snapshot.Messages) != 2 || snapshot.Messages[0].ID != queued.ID ||
		snapshot.Messages[1].Content != "我先核对新闻来源，查实一条就发一条。" ||
		snapshot.Messages[1].SourceChatID == "" || snapshot.Messages[1].ReplyToMessageID != queued.ID {
		t.Fatalf("the plan must appear without waiting for the first finding: %+v", snapshot.Messages)
	}
	source := snapshot.Messages[1].SourceChatID
	ackID := snapshot.Messages[1].ID
	if matching, err := global.MainConversationControllerManager.FindPublishedBody(ctx, snapshot.ID, source, queued.ID, snapshot.Messages[1].Content); err != nil || matching.ID != "" {
		t.Fatalf("settlement must not reuse a preparation notice as its result: %+v, %v", matching, err)
	}
	if again, err := send.Output(request); err != nil || again.(MainConversationAcceptance).QueuedInput.ID != queued.ID {
		t.Fatalf("retry lost original assignment receipt: %+v, %v", again, err)
	}

	// The Router can answer while the selected work conversation is still executing.
	ask := SendMainConversationMessage{SubjectType: "waker", SubjectID: partner,
		Body: MainConversationInput{SubmissionID: "while-news", Content: "fixture 日本語の挨拶"}}
	if _, err := ask.Output(request); err != nil {
		t.Fatal(err)
	}
	waitMainQueueFixture(t, ctx, func() bool {
		snapshot = page()
		return len(snapshot.Messages) == 4 && snapshot.Messages[3].Content == "こんにちは。"
	})
	if !global.ExecutionService.IsExecutionActive(source, queued.ID) || snapshot.Messages[3].SourceChatID != "" {
		t.Fatalf("main conversation did not stay independent: %+v", snapshot.Messages)
	}
	for i := 1; i <= 5; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("release-news-%d", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
		waitMainQueueFixture(t, ctx, func() bool {
			snapshot = page()
			return len(snapshot.Messages) >= 4+i
		})
		result := snapshot.Messages[3+i]
		if len(snapshot.Messages) != 4+i || result.Content != newsFixtureFinding(i) ||
			result.SourceChatID != source || result.SourceExecutionID != queued.ID ||
			!global.ExecutionService.IsExecutionActive(source, queued.ID) {
			t.Fatalf("finding %d was batched, detached or not published mid-turn: %+v", i, snapshot.Messages)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "release-news-finish"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	waitMainQueueFixture(t, ctx, func() bool {
		run, err := global.ChatService.ReadExecution(ctx, source, queued.ID)
		return err == nil && run.Status == constants.ExecutionStatusCompleted
	})
	if final := page(); len(final.Messages) != 9 || final.Messages[1].ID != ackID {
		t.Fatalf("settlement duplicated the plan or a finding: %+v", final.Messages)
	}

	send.Body = MainConversationInput{SubmissionID: "plan-only", Content: "fixture 只发准备说明"}
	accepted, err := send.Output(request)
	if err != nil {
		t.Fatal(err)
	}
	id := accepted.(MainConversationAcceptance).QueuedInput.ID
	waitMainQueueFixture(t, ctx, func() bool {
		for _, row := range page().Messages {
			if row.ID == id && row.SourceChatID != "" {
				run, err := global.ChatService.ReadExecution(ctx, row.SourceChatID, id)
				if err == nil && run.Status == constants.ExecutionStatusCompleted {
					t.Fatal("a preparation notice alone was mistaken for a delivered result")
				}
				return err == nil && run.Status == constants.ExecutionStatusFailed
			}
		}
		return false
	})
}

func newsFixtureFinding(index int) string {
	return fmt.Sprintf("样例新闻 %d：已核对。https://example.invalid/news/%d", index, index)
}

func runNewsPostFixture(t *testing.T, root, input string) bool {
	if root == "" || !strings.Contains(input, "Do not post another acknowledgment") {
		return false
	}
	wait := func(file string) {
		for {
			if _, err := os.Stat(filepath.Join(root, file)); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	io := cli.IO{Out: os.Stderr, Err: os.Stderr}
	for i := 1; i <= 5; i++ {
		key := fmt.Sprintf("news-%d", i)
		wait("release-" + key)
		args := []string{"conversations", "messages", "post", "--request-id", key, "--content", newsFixtureFinding(i)}
		if cli.Execute(t.Context(), args, io) != 0 || cli.Execute(t.Context(), args, io) != 0 {
			return false
		}
	}
	wait("release-news-finish")
	return cli.Execute(t.Context(), []string{"work", "tickets", "report", "--request-id", "news-finish", "--kind", "completed", "--content", newsFixtureFinding(5)}, io) == 0
}

func TestMainConversationDispatchDeliversNativeResult(t *testing.T) {
	configureNativeMainConversation(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	const partner = "native-main-partner"
	request, _ := gin.CreateTestContext(httptest.NewRecorder())
	request.Request = httptest.NewRequest("POST", "/", nil).WithContext(ctx)
	sendInput := func(body MainConversationInput) MainConversationAcceptance {
		t.Helper()
		op := SendMainConversationMessage{SubjectType: "waker", SubjectID: partner, Body: body}
		raw, err := op.Output(request)
		if err != nil {
			t.Fatal(err)
		}
		accepted := raw.(MainConversationAcceptance)
		if accepted.ExecutionID == "" || accepted.Message.SourceChatID == "" {
			t.Fatalf("expected accepted Worker turn, got %+v", accepted)
		}
		return accepted
	}
	send := func(submission, content, quote string) MainConversationAcceptance {
		t.Helper()
		return sendInput(MainConversationInput{SubmissionID: submission, Content: content, ReplyToMessageID: quote, StartWork: quote == ""})
	}
	finished := func(accepted MainConversationAcceptance) MainConversationResponse {
		t.Helper()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for global.ExecutionService.IsExecutionActive(accepted.Message.SourceChatID, accepted.ExecutionID) {
			select {
			case <-ctx.Done():
				t.Fatal("Worker did not settle")
			case <-ticker.C:
			}
		}
		raw, err := (&GetMainConversation{SubjectType: "waker", SubjectID: partner}).Output(request)
		if err != nil {
			t.Fatal(err)
		}
		return raw.(MainConversationResponse)
	}
	checkFinal := func(accepted MainConversationAcceptance, page MainConversationResponse) MainConversationTimelineMessage {
		t.Helper()
		var matches []MainConversationTimelineMessage
		for _, message := range page.Messages {
			if message.Role == "assistant" && message.SourceExecutionID == accepted.ExecutionID {
				matches = append(matches, message)
			}
		}
		if len(matches) != 1 || !matches[0].Readable || matches[0].Content != "Two harmless fixture results." ||
			matches[0].SourceChatID != accepted.Message.SourceChatID {
			t.Fatalf("native final was not delivered exactly once: %+v", matches)
		}
		return matches[0]
	}
	first := send("native-final", "Collect two harmless fixture results.", "")
	publication := checkFinal(first, finished(first))
	if publication.ReplyToMessageID != first.Message.ID {
		t.Fatalf("wrong public reply origin: %+v", publication)
	}
	address := workticket.Address{Kind: constants.WorkTicketOriginMain, ID: first.Message.ConversationID}
	ticket, found, err := global.WorkTicketService.ForMessage(ctx, address, publication.ID)
	if err != nil || !found || ticket.ID != first.Message.ID || ticket.Status != constants.WorkTicketStatusWaiting {
		t.Fatalf("fallback did not preserve the open, quotable ticket: %+v, found=%t, %v", ticket, found, err)
	}
	replayed := send("native-final", "Collect two harmless fixture results.", "")
	if replayed.ExecutionID != first.ExecutionID || checkFinal(replayed, finished(replayed)).ID != publication.ID {
		t.Fatalf("retry created a new execution or public receipt: %+v", replayed)
	}
	continued := send("native-continued", "Continue with the same harmless fixtures.", publication.ID)
	if continued.ExecutionID == first.ExecutionID || continued.Message.SourceChatID != first.Message.SourceChatID {
		t.Fatalf("quote did not resume the original Worker scope: %+v", continued)
	}
	checkFinal(continued, finished(continued))
	direct := sendInput(MainConversationInput{SubmissionID: "direct-directory", Content: "Inspect the harmless fixture.", WorkingDirectory: t.TempDir()})
	directReply := checkFinal(direct, finished(direct))
	if _, found, err := global.WorkTicketService.ForMessage(ctx, address, directReply.ID); err != nil || found {
		t.Fatalf("an unticketed main reply acquired an unrelated work ticket: found=%t, %v", found, err)
	}
	for _, sample := range []struct {
		// input 让测试进程使用指定的显式报告。
		input string
		// want 是公开发言的顺序和正文。
		want []string
	}{
		{"fixture explicit completed", []string{"Explicit completed report.", "Two harmless fixture results."}},
		{"fixture duplicate completed", []string{"Two harmless fixture results."}},
		{"fixture explicit blocked", []string{"Explicit blocked report."}},
		{"fixture duplicate progress", []string{"Two harmless fixture results."}},
		{"fixture separate progress", []string{"Initial evidence collected.", "Two harmless fixture results."}},
		{"fixture post-only ticket", []string{"A directly delivered fixture result."}},
	} {
		accepted := send(sample.input, sample.input, "")
		page := finished(accepted)
		var actual []string
		for _, message := range page.Messages {
			if message.Role == "assistant" && message.SourceExecutionID == accepted.ExecutionID {
				actual = append(actual, message.Content)
			}
		}
		if strings.Join(actual, "\n") != strings.Join(sample.want, "\n") {
			t.Fatalf("public reports for %q = %q, want %q", sample.input, actual, sample.want)
		}
		if sample.input == "fixture post-only ticket" {
			run, err := global.ChatService.ReadExecution(ctx, accepted.Message.SourceChatID, accepted.ExecutionID)
			if err != nil || run.Status != constants.ExecutionStatusCompleted {
				t.Fatalf("a delivered public result must allow the native turn to end: %+v, %v", run, err)
			}
		}
		if sample.input == "fixture explicit completed" {
			final := page.Messages[len(page.Messages)-1]
			ticket, found, err := global.WorkTicketService.ForMessage(ctx, address, final.ID)
			if err != nil || !found || ticket.ID != accepted.Message.ID || ticket.Status != constants.WorkTicketStatusCompleted ||
				ticket.Summary != "Explicit completed report." || final.ReplyToMessageID != accepted.Message.ID || !final.Readable {
				t.Fatalf("final reply changed or lost the completed ticket: message=%+v ticket=%+v found=%t, %v", final, ticket, found, err)
			}
		}
	}
	for _, sample := range []struct {
		// input controls the terminal emitted by the test process.
		input string
		// want is the stable localized public failure identifier.
		want string
	}{
		{"fixture without final", string(constants.RuntimeFailureCodeEventDelivery)},
		{"fixture provider failure", status_error.RuntimeExecutionFailed.Key()},
	} {
		accepted := send(sample.input, sample.input, "")
		page := finished(accepted)
		var publicFailure string
		for _, message := range page.Messages {
			if message.SourceExecutionID != accepted.ExecutionID {
				continue
			}
			if message.Role == "assistant" {
				t.Fatalf("unproven content escaped Worker transcript: %+v", message)
			}
			if message.ID == accepted.Message.ID {
				publicFailure = message.FailureCode
			}
		}
		if publicFailure != sample.want {
			t.Fatalf("public failure for %q = %q, want %q", sample.input, publicFailure, sample.want)
		}
	}
}

func configureNativeMainConversation(t *testing.T) {
	configureNativeMainConversationAt(t, t.TempDir(), "", true)
}

func configureNativeMainConversationAt(t *testing.T, root, crashAfter string, initialize bool, alternateRuntimeIDs ...string) *conversation.Store {
	t.Helper()
	ctx := t.Context()
	previousConfig, previousAccounts, previousThreads := global.Config, global.AIAccountService, global.AgentThreads
	previousChats, previousExecution, previousTickets := global.ChatService, global.ExecutionService, global.WorkTicketService
	previousWakers, previousProjects, previousRuntime := global.WakerControllerManager, global.ProjectControllerManager, global.RuntimeEnvironmentControllerManager
	previousTeams, previousChatController := global.ChannelControllerManager, global.ChatControllerManager
	previousMain, previousStore, previousTransactions := global.MainConversationService, global.MainConversationControllerManager, global.TransactionControllerManager
	t.Cleanup(func() {
		global.Config, global.AIAccountService, global.AgentThreads = previousConfig, previousAccounts, previousThreads
		global.ChatService, global.ExecutionService, global.WorkTicketService = previousChats, previousExecution, previousTickets
		global.WakerControllerManager, global.ProjectControllerManager, global.RuntimeEnvironmentControllerManager = previousWakers, previousProjects, previousRuntime
		global.ChannelControllerManager, global.ChatControllerManager = previousTeams, previousChatController
		global.MainConversationService, global.MainConversationControllerManager, global.TransactionControllerManager = previousMain, previousStore, previousTransactions
	})
	t.Setenv("NEUX_MAIN_DELIVERY_NATIVE_CHILD", "1")
	t.Setenv("NEUX_MAIN_RECOVERY_STARTS", filepath.Join(root, "worker-starts"))
	global.Config = &global.Configuration{DataDirectory: root, ManagedFilesDirectory: filepath.Join(root, "managed")}
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(root, "main-delivery.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.ProjectControllerManager = controllers.NewProjectController(database.DB)
	global.RuntimeEnvironmentControllerManager = controllers.NewRuntimeEnvironmentController(database.DB)
	global.ChannelControllerManager = controllers.NewChannelController(database.DB)
	global.ChatControllerManager = controllers.NewChatController(database.DB)
	global.MainConversationControllerManager = controllers.NewMainConversationController(database.DB)
	global.MainConversationService = mainconversation.New(global.MainConversationControllerManager, global.TransactionControllerManager,
		global.WakerControllerManager, global.ChannelControllerManager)
	global.WorkTicketService = workticket.New(controllers.NewWorkTicketController(database.DB), global.TransactionControllerManager, nil, global.MainConversationService.PublishDeletedOriginReport)
	if initialize {
		if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: "native-main-partner", Name: "Fixture partner"}); err != nil {
			t.Fatal(err)
		}
	}
	var resolveDelegations execution.DelegationTargetResolver
	var wakeDelegations func()
	var delegationWake chan struct{}
	if os.Getenv("NEUX_DELEGATION_FIXTURE") == "1" {
		for _, id := range []string{"fixture-b-lead", "fixture-c"} {
			if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: id, Name: id}); err != nil {
				t.Fatal(err)
			}
		}
		if err := global.ChannelControllerManager.Create(ctx, models.Channel{ID: "fixture-team-b", Name: "Phase 09 fixture team", LeadWakerID: "fixture-b-lead"},
			[]models.ChannelMember{{ChannelID: "fixture-team-b", WakerID: "fixture-b-lead", RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto}}); err != nil {
			t.Fatal(err)
		}
		delegationWake = make(chan struct{}, 1)
		resolveDelegations = global.ResolveWorkTicketDelegationTarget
		wakeDelegations = func() {
			select {
			case delegationWake <- struct{}{}:
			default:
			}
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	definition := agent.RuntimeSummary{ID: constants.RuntimeIDCodex, BaseRuntimeID: constants.RuntimeIDCodex, InstructionFileName: "AGENTS.md",
		ConfigHome: t.TempDir(), PrefixArgs: []string{"-test.run=^TestMainConversationDeliveryNativeChild$", "--"}}
	definitions := []agent.RuntimeSummary{definition}
	for _, id := range alternateRuntimeIDs {
		alternate := definition
		alternate.ID = id
		definitions = append(definitions, alternate)
	}
	for _, item := range definitions {
		if err := global.RuntimeEnvironmentControllerManager.Ensure(ctx, models.RuntimeEnvironment{
			ID: item.ID, RuntimeID: item.ID, BaseRuntimeID: item.BaseRuntimeID, Enabled: true, IsDefault: item.ID == definition.ID,
			DefaultModel: "fixture-model", DefaultPermissionLevel: constants.PermissionLevelAuto,
			Detection: agent.DetectionResult{Runtime: item, State: agent.DetectionStateReady, ResolvedBinary: executable},
		}); err != nil {
			t.Fatal(err)
		}
	}
	store, err := conversation.NewStore(global.Config.ManagedFilesDirectory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	global.AIAccountService = aiaccount.New(controllers.NewAIAccountController(database.DB))
	providers := agent.NewProviderRuntime(agent.ProviderRuntimeOptions{AcquireAccount: global.AIAccountService.AcquireAccount})
	t.Cleanup(func() { _ = providers.Close(context.Background()) })
	var authority agent.ConversationStore = store
	if crashAfter != "" {
		authority = &mainDeliveryCrashStore{Store: store, crashAfter: crashAfter}
	}
	threads := agent.NewThreadLifecycle(providers, authority)
	global.AgentThreads = threads
	module, err := chat.New(chat.Config{Conversations: store, StorageDirectory: global.Config.ManagedFilesDirectory, Projects: global.ProjectControllerManager,
		PreserveOrdinaryOriginReports: global.WorkTicketService.PrepareOrdinaryOriginDeletion,
		BeforeRecovery: func(ctx context.Context, module chat.Module) error {
			if err := execution.RecoverWorkTicketDeliveries(ctx, global.Config.ManagedFilesDirectory, module, threads, global.WorkTicketService, global.MainConversationService); err != nil {
				return err
			}
			return execution.RecoverDefaultMainWork(ctx, module, threads, global.MainConversationControllerManager, global.MainConversationService, mainconversation.DefaultWorkScope)
		},
		QueryIndexes: chat.QueryIndexDependencies{Chats: controllers.NewChatController(database.DB), Transactions: global.TransactionControllerManager,
			Reading:         controllers.NewChatReadingController(database.DB),
			DeletionIntents: controllers.NewChatDeletionIntentController(database.DB)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := execution.FinishRecoveredWorkTickets(ctx, module, global.WorkTicketService); err != nil {
		t.Fatal(err)
	}
	global.ChatService = module
	t.Cleanup(module.Close)
	t.Cleanup(func() { _ = threads.Close(context.Background()) })
	registry := capability.New(nil)
	t.Cleanup(registry.Close)
	host := httptest.NewServer(registry)
	t.Cleanup(host.Close)
	binary := filepath.Join(t.TempDir(), "botcli")
	if err := os.WriteFile(binary, []byte("fixture CLI"), 0700); err != nil {
		t.Fatal(err)
	}
	skillRoot := t.TempDir()
	t.Setenv("NEUX_MAIN_FIXTURE_SKILL_ROOT", skillRoot)
	core, err := execution.New(execution.Config{
		ResolveDelegationTarget: resolveDelegations, WakeDelegations: wakeDelegations,
		Threads: threads, Journal: nativeDeliveryJournal(module, crashAfter), DormantChats: module, Agent: registry, AgentBinary: binary, AgentEndpoint: host.URL + constants.AgentCLIPath,
		RuntimeSummaries: definitions, RuntimeEnvironments: global.RuntimeEnvironmentControllerManager,
		WorkTickets: global.WorkTicketService, Accounts: global.AIAccountService, ResolveAccount: global.AIAccountService.ResolveAccount,
		StorageDirectory: global.Config.ManagedFilesDirectory, Wakers: global.WakerControllerManager, Projects: global.ProjectControllerManager,
		Profiles: controllers.NewProfileDocumentController(database.DB), SkillDataDirectory: skillRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Open(ctx); err != nil {
		t.Fatal(err)
	}
	global.ExecutionService = core
	t.Cleanup(core.Close)
	if delegationWake != nil {
		workerCtx, stopDelegations := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			global.RunWorkTicketCollaboration(workerCtx, delegationWake, core)
		}()
		t.Cleanup(func() { stopDelegations(); <-done })
	}
	return store
}

func TestMainConversationWorkerResultSurvivesRestart(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		// cut 是 Core 退出前完成的权威提交。
		cut string
		// input 选取测试 CLI 行为。
		input string
		// replies 是重启后按顺序可读的公开正文。
		replies []string
	}{
		{"canonical", "Collect two harmless fixture results.", []string{"Two harmless fixture results."}},
		{"consumption", "Collect two harmless fixture results.", []string{"Two harmless fixture results."}},
		{"main-publication", "Collect two harmless fixture results.", []string{"Two harmless fixture results."}},
		{"explicit-before-final-publication", "fixture explicit completed", []string{"Explicit completed report.", "Two harmless fixture results."}},
		{"explicit-after-final-publication", "fixture explicit completed", []string{"Explicit completed report.", "Two harmless fixture results."}},
		{"explicit-without-native-body", "fixture explicit completed; fixture without final", []string{"Explicit completed report."}},
		{"post-without-native-body", "fixture post-only ticket", []string{"A directly delivered fixture result."}},
		{"without-public-body", "fixture without final", nil},
	} {
		t.Run(sample.cut, func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, executable, "-test.run=^TestMainConversationRecoveryCoreChild$")
			cut := sample.cut
			switch cut {
			case "consumption", "main-publication":
			case "explicit-after-final-publication":
				cut = "main-publication"
			default:
				cut = "canonical"
			}
			child.Env = append(os.Environ(), "NEUX_MAIN_RECOVERY_ROOT="+root, "NEUX_MAIN_RECOVERY_CUT="+cut, "NEUX_MAIN_RECOVERY_INPUT="+sample.input)
			if out, err := child.CombinedOutput(); err == nil || child.ProcessState.ExitCode() != 91 {
				t.Fatalf("Core did not reach %s crash: %s (%v)", cut, out, err)
			}
			configureNativeMainConversationAt(t, root, "", false)
			request, _ := gin.CreateTestContext(httptest.NewRecorder())
			request.Request = httptest.NewRequest("GET", "/", nil).WithContext(ctx)
			raw, err := (&GetMainConversation{SubjectType: "waker", SubjectID: "native-main-partner"}).Output(request)
			if err != nil {
				t.Fatal(err)
			}
			page := raw.(MainConversationResponse)
			wantStatus := constants.ExecutionStatusCompleted
			if len(sample.replies) == 0 {
				wantStatus = constants.ExecutionStatusFailed
				if len(page.Messages) != 1 || page.Messages[0].FailureCode != string(constants.RuntimeFailureCodeEventDelivery) {
					t.Fatalf("restart misclassified a Worker without a trusted result: %+v", page.Messages)
				}
			} else {
				if len(page.Messages) != 1+len(sample.replies) || page.Messages[0].Role != "user" || page.Messages[0].FailureCode != "" {
					t.Fatalf("restart lost the already completed Worker reply: %+v", page.Messages)
				}
				for index, want := range sample.replies {
					reply := page.Messages[index+1]
					if reply.Role != "assistant" || reply.Content != want || reply.ReplyToMessageID != page.Messages[0].ID {
						t.Fatalf("unexpected reply after recovery: %+v, want %q", reply, want)
					}
				}
			}
			input := page.Messages[0]
			state, err := global.ChatService.ReadExecution(ctx, input.SourceChatID, input.SourceExecutionID)
			if err != nil || state.Status != wantStatus {
				t.Fatalf("restart did not settle the original execution: %+v, %v", state, err)
			}
			data, err := os.ReadFile(filepath.Join(root, "worker-starts"))
			if err != nil || string(data) != "worker\n" {
				t.Fatalf("recovery dispatched the Worker again: %q, %v", data, err)
			}
		})
	}
}

func TestOrdinaryWorkTicketReportSurvivesRestart(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, cut := range []string{"ordinary-intent", "ordinary-destination"} {
		t.Run(cut, func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, executable, "-test.run=^TestMainConversationRecoveryCoreChild$")
			child.Env = append(os.Environ(), "NEUX_MAIN_RECOVERY_ROOT="+root, "NEUX_MAIN_RECOVERY_CUT="+cut)
			if out, err := child.CombinedOutput(); err == nil || child.ProcessState.ExitCode() != 91 {
				t.Fatalf("Core did not reach %s crash: %s (%v)", cut, out, err)
			}
			configureNativeMainConversationAt(t, root, "", false)
			chats, err := global.ChatService.ListChats(ctx, chat.ListChatsInput{WakerID: "native-main-partner", Limit: 10})
			if err != nil || len(chats.Resources) != 1 {
				t.Fatalf("wrong origin chat after restart: %+v, %v", chats, err)
			}
			chatID := chats.Resources[0].ID
			origin, err := global.ChatService.ReadInitialExecution(ctx, chatID)
			if err != nil || origin.InitialExecution == nil {
				t.Fatalf("missing original durable admission: %+v, %v", origin, err)
			}
			executionID := origin.InitialExecution.ID
			records, err := global.ChatService.ReadExecutionRecords(chatID, executionID)
			if err != nil {
				t.Fatal(err)
			}
			reports, err := workticket.OrdinaryWorkReportReceipts(records, executionID)
			if err != nil || len(reports) != 1 || reports[0].Content != "Explicit completed report." {
				t.Fatalf("restart did not yield exactly one original report: %+v, %v", reports, err)
			}
			address := workticket.Address{Kind: constants.WorkTicketOriginChat, ID: chatID}
			ticket, found, err := global.WorkTicketService.ForMessage(ctx, address, executionID)
			if err != nil || !found || ticket.Status != constants.WorkTicketStatusCompleted || ticket.ID != origin.InitialExecution.UserMessageID {
				t.Fatalf("ordinary report did not settle its original receipt: %+v, found=%v, %v", ticket, found, err)
			}
			data, err := os.ReadFile(filepath.Join(root, "worker-starts"))
			if err != nil || string(data) != "worker\n" {
				t.Fatalf("recovery dispatched the Worker again: %q, %v", data, err)
			}
		})
	}
}

func TestWorkTicketNativeDelegationReturnsThroughTeam(t *testing.T) {
	testWorkTicketNativeDelegationReturnsThroughTeam(t, 5)
}

func TestWorkTicketNativeDelegationAnswersPrivateQuestion(t *testing.T) {
	t.Setenv("NEUX_DELEGATED_PRIVATE_QUESTION", "1")
	testWorkTicketNativeDelegationReturnsThroughTeam(t, 7)
}

func testWorkTicketNativeDelegationReturnsThroughTeam(t *testing.T, launches int) {
	t.Helper()
	t.Setenv("NEUX_DELEGATION_FIXTURE", "1")
	root := t.TempDir()
	configureNativeMainConversationAt(t, root, "", true)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	accepted, err := global.ExecutionService.Start(ctx, execution.WorkIntent{
		ClientRequestID: "fixture-delegation-root", Chat: execution.ChatTarget{Create: &execution.CreateChatSpec{WakerID: "native-main-partner"}},
		Input: execution.WorkInput{Actor: "user", Content: "fixture delegate team"},
		Options: execution.WorkOptions{EntryPoint: execution.WorkbenchEntryPoint, RuntimeID: constants.RuntimeIDCodex,
			PermissionLevel: constants.PermissionLevelAuto}, Ticket: execution.WorkTicketInput{Start: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	var records []eventlog.Record
	for {
		summary, err := global.ChatService.GetChatSummary(ctx, accepted.Chat.ID)
		if err == nil && summary.Chat.LastEndedExecutionID != "" && summary.Chat.LastEndedExecutionID != accepted.Execution.ID && summary.Chat.ActiveExecutionID == "" {
			records, err = global.ChatService.ReadExecutionRecords(accepted.Chat.ID, summary.Chat.LastEndedExecutionID)
			if err == nil {
				reports, err := workticket.OrdinaryWorkReportReceipts(records, summary.Chat.LastEndedExecutionID)
				if err != nil || len(reports) != 1 || reports[0].Content != "A received B's verified result." {
					t.Fatalf("wrong public conclusion after delegated work: %+v %v", reports, err)
				}
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("root work never consumed settled direct results: %+v %v", summary, err)
		case <-tick.C:
		}
	}
	for _, target := range [][2]string{{"team", "fixture-team-b"}, {"waker", "fixture-c"}} {
		conv, err := global.MainConversationService.Ensure(ctx, target[0], target[1])
		if err != nil {
			t.Fatal(err)
		}
		messages, _, err := global.MainConversationService.Messages(ctx, conv.ID, 0, 10)
		if err != nil || len(messages) != 0 {
			t.Fatalf("private delegation leaked into target public conversation: %+v %v", messages, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "worker-starts"))
	if err != nil || strings.Count(string(data), "worker\n") != launches {
		t.Fatalf("unexpected launches for A→B→C→B→A: %q %v", data, err)
	}
}

func TestMainConversationRecoveryCoreChild(t *testing.T) {
	root := os.Getenv("NEUX_MAIN_RECOVERY_ROOT")
	if root == "" {
		return
	}
	configureNativeMainConversationAt(t, root, os.Getenv("NEUX_MAIN_RECOVERY_CUT"), true)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	if strings.HasPrefix(os.Getenv("NEUX_MAIN_RECOVERY_CUT"), "ordinary-") {
		_, err := global.ExecutionService.Start(ctx, execution.WorkIntent{
			ClientRequestID: "restart-ordinary", Chat: execution.ChatTarget{Create: &execution.CreateChatSpec{WakerID: "native-main-partner"}},
			Input: execution.WorkInput{Content: "fixture explicit completed", Actor: "user"},
			Options: execution.WorkOptions{EntryPoint: execution.WorkbenchEntryPoint, RuntimeID: constants.RuntimeIDCodex,
				PermissionLevel: constants.PermissionLevelAuto}, Ticket: execution.WorkTicketInput{Start: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		<-ctx.Done()
		t.Fatal("the ordinary report fault boundary was never reached")
	}
	request, _ := gin.CreateTestContext(httptest.NewRecorder())
	request.Request = httptest.NewRequest("POST", "/", nil).WithContext(ctx)
	input := os.Getenv("NEUX_MAIN_RECOVERY_INPUT")
	op := SendMainConversationMessage{SubjectType: "waker", SubjectID: "native-main-partner",
		Body: MainConversationInput{SubmissionID: "restart-worker", Content: input, StartWork: strings.Contains(input, "explicit") || strings.Contains(input, "ticket")}}
	if _, err := op.Output(request); err != nil {
		t.Fatal(err)
	}
	<-ctx.Done()
	t.Fatal("the fault boundary was never reached")
}

type deliveryCrashJournal struct {
	// NativeJournal 提供接纳、交付和真正的权威聊天日志。
	chat.NativeJournal
	// crashAfter 指定普通回件的故障位置。
	crashAfter string
}

func nativeDeliveryJournal(module chat.Module, crashAfter string) chat.Journal {
	if strings.HasPrefix(crashAfter, "ordinary-") || crashAfter == "main-publication" {
		return &deliveryCrashJournal{NativeJournal: module.(chat.NativeJournal), crashAfter: crashAfter}
	}
	return module
}

func (j *deliveryCrashJournal) AppendExecutionFacts(ctx context.Context, chatID string, inputs []eventlog.AppendInput) ([]eventlog.Record, error) {
	if len(inputs) == 1 && inputs[0].Type == constants.ChatEventTypeExecutionDelivery && j.crashAfter == "ordinary-intent" {
		os.Exit(91)
	}
	records, err := j.NativeJournal.AppendExecutionFacts(ctx, chatID, inputs)
	if err == nil && len(inputs) == 1 && inputs[0].Type == constants.ChatEventTypeExecutionDelivery && j.crashAfter == "ordinary-destination" {
		os.Exit(91)
	}
	return records, err
}

func (j *deliveryCrashJournal) CommitTerminal(ctx context.Context, fact chat.TerminalFact) (chat.TerminalCommit, error) {
	if j.crashAfter == "main-publication" && fact.Status == constants.ExecutionStatusCompleted {
		os.Exit(91)
	}
	return j.NativeJournal.CommitTerminal(ctx, fact)
}

type mainDeliveryCrashStore struct {
	// Store 提供故障时已真正提交的原生和会话日志。
	*conversation.Store
	// crashAfter 指定进程退出前写入的最后一个边界。
	crashAfter string
}

func (s *mainDeliveryCrashStore) CommitNativeResult(ctx context.Context, result agent.NativeResult) error {
	err := s.Store.CommitNativeResult(ctx, result)
	if err == nil && s.crashAfter == "canonical" && len(result.Turn.ConsumedInputIDs) == 1 {
		inputs, lookupErr := s.Store.ReadNativeInputs(ctx, result.Key.Session, result.Turn.ConsumedInputIDs)
		if lookupErr == nil && len(inputs) == 1 && inputs[0].Ref.ChatID != "" {
			os.Exit(91)
		}
	}
	return err
}

func (s *mainDeliveryCrashStore) Commit(ctx context.Context, batch agent.ConversationBatch) (agent.CommitReceipt, error) {
	receipt, err := s.Store.Commit(ctx, batch)
	if err == nil && s.crashAfter == "consumption" {
		for _, change := range batch.Changes {
			if change.Consumption != nil {
				os.Exit(91)
			}
		}
	}
	return receipt, err
}

func runTicketPostFixture(t *testing.T) bool {
	post := func(key, body string) int {
		return cli.Execute(t.Context(), []string{"conversations", "messages", "post", "--request-id", key, "--input", "-"},
			cli.IO{In: strings.NewReader(body), Out: os.Stderr, Err: os.Stderr})
	}
	const ack = `{"content":"Checking the fixture sources.","quoteReply":true}`
	const result = `{"content":"\nVerified fixture evidence.\n","quoteReply":true}`
	if post("fixture-ack", ack) != 0 ||
		post("fixture-result", result) != 0 || post("fixture-result", result) != 0 ||
		post("fixture-result", `{"content":"Verified fixture evidence."}`) == 0 {
		return false
	}
	for {
		if _, err := os.Stat(os.Getenv("NEUX_MAIN_POST_RELEASE")); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if cli.Execute(t.Context(), []string{"work", "tickets", "report", "--request-id", "finish", "--kind", "completed", "--content", "Verified fixture evidence."},
		cli.IO{Out: os.Stderr, Err: os.Stderr}) != 0 {
		return false
	}
	return post("fixture-ack", ack) == 0 &&
		post("fixture-ack", `{"content":"Checking the fixture sources."}`) != 0 &&
		post("fixture-result", result) == 0 &&
		post("fixture-result", `{"content":"Verified fixture evidence."}`) != 0 &&
		post("fixture-late", `{"content":"This must stay private."}`) != 0
}

func TestMainConversationDeliveryNativeChild(t *testing.T) {
	if os.Getenv("NEUX_MAIN_DELIVERY_NATIVE_CHILD") == "" {
		return
	}
	read := bufio.NewScanner(os.Stdin)
	write := json.NewEncoder(os.Stdout)
	var threadID string
	var router bool
	ordinal := 0
	for read.Scan() {
		var call struct {
			// ID is the identifier echoed in the protocol response.
			ID any `json:"id"`
			// Method is the native app-server operation.
			Method string `json:"method"`
			// Params carries the operation-specific request.
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(read.Bytes(), &call); err != nil {
			os.Exit(2)
		}
		respond := func(value any) {
			_ = write.Encode(map[string]any{"id": call.ID, "result": value})
		}
		emit := func(method string, params any) {
			_ = write.Encode(map[string]any{"method": method, "params": params})
		}
		switch call.Method {
		case "initialize":
			respond(map[string]any{"userAgent": "codex/0.163.0"})
		case "model/list":
			respond(map[string]any{"data": []any{
				map[string]any{"id": "fixture-model", "model": "fixture-model", "displayName": "Fixture", "isDefault": true},
				map[string]any{"id": "fixture-alternate", "model": "fixture-alternate", "displayName": "Fixture alternate"},
			}})
		case "thread/start", "thread/resume":
			var params struct {
				// Ephemeral identifies the isolated Router request.
				Ephemeral bool `json:"ephemeral"`
				// ThreadID carries the previous Worker connection's thread.
				ThreadID string `json:"threadId"`
			}
			if err := json.Unmarshal(call.Params, &params); err != nil {
				os.Exit(2)
			}
			router = params.Ephemeral || strings.Contains(string(call.Params), "You handle one turn in the user's main conversation")
			threadID = params.ThreadID
			if threadID == "" {
				threadID = fmt.Sprintf("main-fixture-thread-%d", os.Getpid())
			}
			respond(map[string]any{"thread": map[string]any{"id": threadID, "ephemeral": params.Ephemeral}, "model": "fixture-model"})
		case "turn/start":
			if path := os.Getenv("NEUX_MAIN_RECOVERY_STARTS"); path != "" && !router {
				file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
				if err != nil {
					os.Exit(2)
				}
				_, _ = file.WriteString("worker\n")
				_ = file.Close()
			}
			ordinal++
			turnID := fmt.Sprintf("main-fixture-turn-%d-%d", os.Getpid(), ordinal)
			turn := func(status string) map[string]any {
				return map[string]any{"threadId": threadID, "turn": map[string]any{"id": turnID, "status": status}}
			}
			item := func(fields map[string]any) {
				emit("item/completed", map[string]any{"threadId": threadID, "turnId": turnID, "item": fields})
			}
			respond(turn("inProgress"))
			emit("turn/started", turn("inProgress"))
			body := string(call.Params)
			if !router {
				releaseMainQueueFixtureWorker(t, body)
			}
			switch {
			case router && strings.Contains(body, "fixture 只发准备说明"):
				item(map[string]any{"id": turnID + "-answer", "type": "agentMessage", "phase": "final_answer", "text": `{"action":"dispatch","scope":"managed","acknowledgment":"我先准备资料。"}`})
			case router && strings.Contains(body, "fixture 先后采集五条新闻"):
				item(map[string]any{"id": turnID + "-answer", "type": "agentMessage", "phase": "final_answer", "text": `{"action":"dispatch","scope":"managed","acknowledgment":"我先核对新闻来源，查实一条就发一条。"}`})
			case router && strings.Contains(body, "fixture 别另开一个任务"):
				if strings.Contains(body, `\"workRequested\":true`) {
					emit("turn/completed", turn("failed"))
					continue
				}
				item(map[string]any{"id": turnID + "-answer", "type": "agentMessage", "phase": "final_answer", "text": `{"action":"answer","message":"不新开任务。"}`})
			case router && strings.Contains(body, "fixture 日本語の挨拶"):
				item(map[string]any{"id": turnID + "-answer", "type": "agentMessage", "phase": "final_answer", "text": `{"action":"answer","message":"こんにちは。"}`})
			case router && strings.Contains(body, "fixture 別件は作らないで"):
				message := "引き続き同じ会話です。"
				if ordinal != 2 {
					message = "Router の会話が変わりました。"
				}
				answer, _ := json.Marshal(map[string]string{"action": "answer", "message": message})
				item(map[string]any{"id": turnID + "-answer", "type": "agentMessage", "phase": "final_answer", "text": string(answer)})
			case router && strings.Contains(body, "router fixture second"):
				message := "New native session."
				if ordinal == 2 {
					message = "Same native session."
				}
				answer, _ := json.Marshal(map[string]string{"action": "clarify", "message": message})
				item(map[string]any{"id": turnID + "-answer", "type": "agentMessage", "phase": "final_answer", "text": string(answer)})
			case router && strings.Contains(body, "router fixture first"):
				holdNativeRouterFixture(t)
				item(map[string]any{"id": turnID + "-answer", "type": "agentMessage", "phase": "final_answer", "text": `{"action":"clarify","message":"First answer."}`})
			case router:
				item(map[string]any{"id": turnID + "-answer", "type": "agentMessage", "phase": "final_answer", "text": `{"action":"dispatch","scope":"managed"}`})
			case strings.Contains(body, "fixture scoped handoff"):
				result := fmt.Sprintf("Old history: %t. Prior switch input: %t.",
					strings.Contains(body, "Hello, please say hi."),
					strings.Contains(body, "fixture scoped handoff scope-mismatch"))
				item(map[string]any{"id": turnID + "-answer", "type": "agentMessage", "phase": "final_answer", "text": result})
			case strings.Contains(body, "fixture provider failure"):
				emit("turn/completed", turn("failed"))
				continue
			case strings.Contains(body, "fixture source conversation reply"):
				const reply = "Talk soon from the fixture."
				probe := os.Getenv("NEUX_MAIN_SOURCE_REPLY_RESULT")
				if !strings.Contains(body, "send the user-facing reply or outcome there") ||
					!strings.Contains(body, "conversations.messages.post") {
					_ = os.WriteFile(probe, []byte("source instructions missing"), 0600)
					emit("turn/completed", turn("failed"))
					continue
				}
				// 测试进程使用原生 MCP 桥接器收到的同一份常驻连接。
				paths, err := filepath.Glob(filepath.Join(os.Getenv("NEUX_MAIN_FIXTURE_SKILL_ROOT"), constants.AgentCLIContextDirectory, "*", "*", "connection.json"))
				if err != nil || len(paths) != 1 {
					_ = os.WriteFile(probe, []byte("expected one resident connection for the source"), 0600)
					emit("turn/completed", turn("failed"))
					continue
				}
				t.Setenv(constants.AgentCLIContextEnvironment, paths[0])
				var output strings.Builder
				if cli.Execute(t.Context(), []string{"conversations", "messages", "post", "--request-id", "fixture-source-reply", "--content", reply},
					cli.IO{Out: &output, Err: &output}) != 0 {
					_ = os.WriteFile(probe, []byte(output.String()), 0600)
					emit("turn/completed", turn("failed"))
					continue
				}
				item(map[string]any{"id": turnID + "-answer", "type": "agentMessage", "phase": "final_answer", "text": reply})
			case strings.Contains(body, "fixture 只发准备说明"):
				// A successful native end with no result is not successful public delivery.
			case strings.Contains(body, "fixture 先后采集五条新闻"):
				if !runNewsPostFixture(t, os.Getenv("NEUX_MAIN_NEWS_FIXTURE_ROOT"), body) {
					emit("turn/completed", turn("failed"))
					continue
				}
				item(map[string]any{"id": turnID + "-answer", "type": "agentMessage", "phase": "final_answer", "text": newsFixtureFinding(5)})
			case strings.Contains(body, "fixture ticket posts"):
				if !runTicketPostFixture(t) {
					emit("turn/completed", turn("failed"))
					continue
				}
				item(map[string]any{"id": turnID + "-answer", "type": "agentMessage", "phase": "final_answer", "text": "Verified fixture evidence."})
			case strings.Contains(body, "fixture post-only ticket"):
				if code := cli.Execute(t.Context(), []string{"conversations", "messages", "post", "--request-id", "fixture-only", "--content", "A directly delivered fixture result."},
					cli.IO{Out: os.Stderr, Err: os.Stderr}); code != 0 {
					os.Exit(3)
				}
			default:
				if os.Getenv("NEUX_DELEGATION_FIXTURE") == "1" {
					var args []string
					businessQuestion := os.Getenv("NEUX_DELEGATED_PRIVATE_QUESTION") == "1"
					switch {
					case businessQuestion && strings.Contains(body, "Answer the verified question from your direct child"):
						if !answerDelegatedFixtureQuestion(t.Context()) {
							os.Exit(3)
						}
						emit("turn/completed", turn("completed"))
						continue
					case businessQuestion && strings.Contains(body, "Continue your delegated work using the direct requester's answer"):
						if !strings.Contains(body, "Use fixture Alpha") {
							os.Exit(3)
						}
						args = []string{"work", "tickets", "report", "--kind", "completed", "--content", "C's verified evidence.", "--request-id", "final-c"}
					case strings.Contains(body, "B received C's verified result"):
						args = []string{"work", "tickets", "report", "--kind", "completed", "--content", "A received B's verified result.", "--request-id", "final-a"}
					case strings.Contains(body, "C's verified evidence"):
						args = []string{"work", "tickets", "report", "--kind", "completed", "--content", "B received C's verified result.", "--request-id", "final-b"}
					case strings.Contains(body, "fixture delegate team"):
						args = []string{"work", "tickets", "delegate", "--subject-type", "team", "--subject-id", "fixture-team-b", "--content", "fixture delegate C", "--request-id", "to-b"}
					case strings.Contains(body, "fixture delegate C"):
						args = []string{"work", "tickets", "delegate", "--subject-type", "waker", "--subject-id", "fixture-c", "--content", "fixture C evidence", "--request-id", "to-c"}
					case strings.Contains(body, "fixture C evidence"):
						if os.Getenv(delegatedNativeQuestionFixture) == "1" {
							requireDelegatedNativeQuestionAnswer(t, read, write, threadID, turnID)
						}
						if businessQuestion {
							args = []string{"work", "tickets", "report", "--kind", "waiting", "--content", "Which fixture should I use?", "--request-id", "ask-c"}
						} else {
							args = []string{"work", "tickets", "report", "--kind", "completed", "--content", "C's verified evidence.", "--request-id", "final-c"}
						}
					}
					if args != nil {
						if code := cli.Execute(t.Context(), args, cli.IO{Out: os.Stderr, Err: os.Stderr}); code != 0 {
							os.Exit(3)
						}
						emit("turn/completed", turn("completed"))
						continue
					}
				}
				var kind, report string
				switch {
				case strings.Contains(body, "fixture explicit completed"):
					kind, report = "completed", "Explicit completed report."
				case strings.Contains(body, "fixture duplicate completed"):
					kind, report = "completed", "Two harmless fixture results."
				case strings.Contains(body, "fixture explicit blocked"):
					kind, report = "blocked", "Explicit blocked report."
				case strings.Contains(body, "fixture duplicate progress"):
					kind, report = "progress", "Two harmless fixture results."
				case strings.Contains(body, "fixture separate progress"):
					kind, report = "progress", "Initial evidence collected."
				}
				if kind != "" {
					if code := cli.Execute(t.Context(), []string{"work", "tickets", "report", "--request-id", "fixture-report", "--kind", kind, "--content", report},
						cli.IO{Out: os.Stderr, Err: os.Stderr}); code != 0 {
						os.Exit(3)
					}
				}
				item(map[string]any{"id": turnID + "-comment", "type": "agentMessage", "phase": "commentary", "text": "private commentary"})
				item(map[string]any{"id": turnID + "-inspect", "type": "commandExecution", "status": "completed",
					"command": "pwd", "cwd": ".", "exitCode": 0, "aggregatedOutput": "private tool output"})
				if !strings.Contains(body, "fixture without final") {
					item(map[string]any{"id": turnID + "-answer", "type": "agentMessage", "phase": "final_answer", "text": "Two harmless fixture results."})
				}
			}
			emit("turn/completed", turn("completed"))
		default:
			if call.ID != nil {
				respond(map[string]any{})
			}
		}
	}
	if read.Err() != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func answerDelegatedFixtureQuestion(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for ctx.Err() == nil {
		var out strings.Builder
		if cli.Execute(ctx, []string{"work", "tickets", "results"}, cli.IO{Out: &out, Err: os.Stderr}) != 0 {
			return false
		}
		var receipt struct {
			// Data contains only direct child reports.
			Data struct {
				// Reports are the direct child's published updates.
				Reports []struct {
					// ID is the waiting report's stable identity.
					ID string `json:"reportId"`
					// Content is the private question sent by the child.
					Content string `json:"content"`
				} `json:"reports"`
			} `json:"data"`
		}
		if json.Unmarshal([]byte(out.String()), &receipt) != nil {
			return false
		}
		for _, report := range receipt.Data.Reports {
			if report.Content == "Which fixture should I use?" {
				out.Reset()
				if cli.Execute(ctx, []string{"work", "tickets", "results", "--report-id", report.ID}, cli.IO{Out: &out, Err: os.Stderr}) != 0 ||
					!strings.Contains(out.String(), report.Content) {
					return false
				}
				return cli.Execute(ctx, []string{"work", "tickets", "reply", "--report-id", report.ID, "--content", "Use fixture Alpha", "--request-id", "reply-b"},
					cli.IO{Out: os.Stderr, Err: os.Stderr}) == 0
			}
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	return false
}
