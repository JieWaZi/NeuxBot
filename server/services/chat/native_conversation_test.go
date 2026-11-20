package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/workspacefiles"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestNativeQuestionHistoryPagesAtRequestAndReadsLateResolution(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	store, err := conversation.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	id := "99999999999999999999999999999999"
	schema := json.RawMessage(`{
		"type":"object","properties":{
			"choice":{"type":"string","enum":["green"]},
			"credential":{"type":"string","_meta":{"codex":{"isSecret":true}}},
			"credential_custom":{"type":"string","_meta":{"_askUserQuestionCustomAnswer":{"questionId":"credential"}}},
			"credential__other":{"type":"string"},
			"codex-other-id":{"type":"string","_meta":{"codex":{"isOtherAnswer":true,"questionId":"credential"}}},
			"password":{"type":"string","format":"password"},
			"nested":{"type":"object","properties":{"token":{"type":"string","writeOnly":true}}},
			"private-list":{"type":"array","items":{"type":"string","writeOnly":true}}
		}
	}`)
	journal := &conversationJournal{Store: store}
	records, err := journal.Create(id, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "waker"})},
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "user", Data: marshalData(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Content: "input"})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "exec", Data: marshalData(eventlog.ExecutionCreatedData{UserMessageID: "user"})},
		{Type: constants.ChatEventTypeElicitationRequested, ExecutionID: "exec", Data: marshalData(eventlog.ElicitationRequestedData{ElicitationID: "first-question", Mode: "form", Message: "Which color?", RequestedSchema: schema})},
		{Type: constants.ChatEventTypeElicitationRequested, ExecutionID: "exec", Data: marshalData(eventlog.ElicitationRequestedData{ElicitationID: "second-question", Mode: "form", Message: "Anything else?", RequestedSchema: schema})},
		{Type: constants.ChatEventTypeElicitationResolved, ExecutionID: "exec", Data: marshalData(eventlog.ElicitationResolvedData{
			ElicitationID: "first-question", Result: constants.ElicitationResultAccepted,
			Content: map[string]any{
				"choice": "green", "credential": "secret-direct", "credential_custom": "secret-custom",
				"credential__other": "secret-other", "codex-other-id": "secret-codex-other",
				"password": "secret-password", "unrecognized": "secret-unknown",
				"nested": map[string]any{"token": "secret-nested"}, "private-list": []string{"secret-list"},
			},
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	key := agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "codex", Subject: "subject", SessionID: "session"}, InvocationID: "prompt"}
	native, err := store.CommitNativeFragments(ctx, agent.NativeFragmentBatch{Key: key, CommitKey: "fragment", MessageID: "native-message", Changes: []agent.MessageChange{
		{Kind: agent.MessageAppend, ContentType: constants.RuntimeEventTypeTextDelta, Text: "final answer", Revision: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Commit(ctx, agent.ConversationBatch{ChatID: id, Key: "link", ExpectedVersion: records[len(records)-1].Sequence, Changes: []agent.ConversationChange{
		{Message: &agent.MessageChange{MessageID: "exec", ExecutionID: "exec", Kind: agent.MessageNativeFragment, Fragments: &native, Revision: 1}},
		{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "exec", Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = conversation.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	chats := newTestService(t, Config{StorageDirectory: root, Conversations: store})
	defer chats.Close()

	latest, err := chats.ReadMessageTimeline(ctx, id, 0, 0, 1)
	if err != nil || len(latest.Resources) != 1 || latest.Resources[0].ID != "second-question" || latest.NextBeforeSequence != records[4].Sequence {
		t.Fatalf("newest question page = %+v, err = %v", latest, err)
	}
	secondJSON := marshalData(latest.Resources[0])
	if !strings.Contains(string(secondJSON), `"result":"invalidated"`) {
		t.Fatalf("question without resolution after terminal = %s", secondJSON)
	}
	previous, err := chats.ReadMessageTimeline(ctx, id, latest.NextBeforeSequence, 0, 1)
	if err != nil || len(previous.Resources) != 1 || previous.Resources[0].ID != "first-question" || previous.NextBeforeSequence != records[3].Sequence {
		t.Fatalf("previous question page = %+v, err = %v", previous, err)
	}
	first := previous.Resources[0]
	if first.CreatedSequence != records[3].Sequence || first.Revision != records[5].Sequence || first.Readable || first.Fork != nil {
		t.Fatalf("question identity, revision or reading state = %+v", first)
	}
	assertSafeQuestionResult(t, marshalData(first))
	older, err := chats.ReadMessageTimeline(ctx, id, previous.NextBeforeSequence, 0, 2)
	if err != nil || len(older.Resources) != 2 || older.HasMore || older.Resources[0].ID != "user" ||
		older.Resources[1].ID != "exec" || older.Resources[1].Content != "final answer" || older.Resources[1].Status != constants.ExecutionStatusCompleted {
		t.Fatalf("ordinary history separated by question cursor = %+v, err = %v", older, err)
	}
	nativePage, err := store.ReadMessages(ctx, agent.MessageQuery{ChatID: id, Limit: 10})
	if err != nil || len(nativePage.Messages) != 2 || nativePage.Messages[1].ID != "exec" {
		t.Fatalf("native message page includes presentation rows: %+v, err = %v", nativePage, err)
	}
	history, err := chats.ReadMessageHistory(ctx, id, 0, 1)
	if err != nil || len(history.Resources) != 1 || history.Resources[0].ID != "exec" || history.Resources[0].Content != "final answer" {
		t.Fatalf("question consumed a machine history slot: %+v, err = %v", history, err)
	}

	public, err := chats.ListEvents(ctx, id, records[4].Sequence, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range public {
		if event.Type == constants.ChatEventTypeElicitationResolved {
			if event.Message == nil || event.Message.ID != first.ID || event.Message.Revision != first.Revision {
				t.Fatalf("replayed question resolution message = %+v", event.Message)
			}
			assertSafeQuestionResult(t, marshalData(event))
			return
		}
	}
	t.Fatal("replayed events omitted the resolution")
}

func TestLateExecutionDeliverySurvivesPagingAndRepeatedPublication(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	store := newTestConversationJournal(t, root)
	chatID := "aabbccddaabbccddaabbccddaabbccdd"
	const executionID = "first-execution"
	facts, err := store.Create(chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "waker"})},
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "first-input", Data: marshalData(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Content: "task"})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: executionID, Data: marshalData(eventlog.ExecutionCreatedData{UserMessageID: "first-input"})},
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: executionID, Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "second-input", Data: marshalData(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Content: "later input"})},
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "delivery.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	module := newTestService(t, Config{StorageDirectory: root, Conversations: store.Store, QueryIndexes: QueryIndexDependencies{
		Chats: controllers.NewChatController(db.DB), Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)),
	}})
	defer module.Close()
	opened := 0
	delivery := ExecutionDelivery{ChatID: chatID, ExecutionID: executionID, SourceID: "run-first", Summary: "已生成正式报告", Files: []ExecutionDeliveryFile{{ID: "report-first", Name: "report.md", Open: func() (io.ReadCloser, error) {
		opened++
		return io.NopCloser(strings.NewReader("Report from original execution")), nil
	}}}}
	for range 2 {
		if err := module.PublishExecutionDelivery(ctx, delivery); err != nil {
			t.Fatal(err)
		}
	}
	page, err := module.ReadMessageHistory(ctx, chatID, facts[4].Sequence, 2)
	if err != nil || len(page.Resources) != 2 {
		t.Fatalf("older history page missing: %+v %v", page, err)
	}
	message := page.Resources[1]
	if opened != 1 || message.ID != executionID || message.Status != constants.ExecutionStatusCompleted || message.Content != delivery.Summary || len(message.Attachments) != 1 {
		t.Fatalf("delivery is missing, duplicated or changed terminal state: opens=%d message=%+v", opened, message)
	}
	directory, err := module.storage.ChatRoot(chatID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(message.Attachments[0].Path)))
	if err != nil || string(data) != "Report from original execution" {
		t.Fatalf("managed attachment bytes = %q, %v", data, err)
	}
	public, err := module.ListEvents(ctx, chatID, facts[4].Sequence, 10)
	if err != nil || len(public) != 1 || public[0].Type != constants.ChatEventTypeExecutionDelivery || public[0].Message == nil || public[0].Message.ID != executionID {
		t.Fatalf("publication did not emit one durable update: %+v, %v", public, err)
	}
}

func TestExecutionDeliveryCopyAllowsChatAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	store := newTestConversationJournal(t, root)
	_, chats := newRecoveryTestService(t, root, store.Store)
	const chatID = "ab12ab12ab12ab12ab12ab12ab12ab12"
	const executionID = "generating-report"
	if _, err := store.Create(chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "waker"})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: executionID, Data: []byte(`{}`)},
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: executionID, Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
	}); err != nil {
		t.Fatal(err)
	}
	opening := make(chan struct{})
	sourceReady := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- chats.PublishExecutionDelivery(ctx, ExecutionDelivery{ChatID: chatID, ExecutionID: executionID, SourceID: "run", Files: []ExecutionDeliveryFile{{
			ID: "report", Name: "report.md", Open: func() (io.ReadCloser, error) {
				close(opening)
				select {
				case <-sourceReady:
					return io.NopCloser(strings.NewReader("report")), nil
				case <-ctx.Done():
					return nil, status_error.OperationCanceled
				}
			},
		}}})
	}()
	select {
	case <-opening:
	case err := <-done:
		t.Fatalf("delivery exited before it opened the source: %v", err)
	case <-ctx.Done():
		t.Fatal("delivery did not open its source")
	}
	if release, err := chatstorage.AcquireOperation(chats.storage.Root(), chatID); !errors.Is(err, status_error.ChatHasActiveExecution) {
		if release != nil {
			release()
		}
		t.Errorf("protect staged bytes against deletion: %v", err)
	}
	release, err := chatstorage.AcquireAdmission(chats.storage.Root(), "waker", chatID)
	if err != nil {
		t.Errorf("admit a turn while source is blocked: %v", err)
	} else {
		defer release()
	}
	close(sourceReady)
	select {
	case err := <-done:
		if !errors.Is(err, status_error.ChatHasActiveExecution) {
			t.Errorf("publication should yield to an ongoing admission: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("delivery did not finish after the source was released")
	}
	chatDirectory, err := chats.storage.ChatRoot(chatID)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(chatDirectory, constants.ExecutionWorkspaceAttachmentsDirectory))
	if err != nil || len(entries) != 0 {
		t.Fatalf("a failed publication left attachment files: %v, %v", entries, err)
	}
}

func TestExecutionDeliveryRetrySkipsRemovedCommittedAttachment(t *testing.T) {
	ctx := t.Context()
	_, chats := newRecoveryTestService(t, t.TempDir())
	const chatID = "d32f19007b4d49b8882369538857449e"
	const executionID = "exec"
	createCompletedDeliveryExecution(t, chats, chatID, executionID)
	opened := 0
	first := ExecutionDeliveryFile{ID: "first", Name: "first.md", Open: func() (io.ReadCloser, error) {
		opened++
		return io.NopCloser(strings.NewReader("first result")), nil
	}}
	second := ExecutionDeliveryFile{ID: "second", Name: "second.md", Open: func() (io.ReadCloser, error) {
		return nil, status_error.WorkflowFileNotFound
	}}
	delivery := ExecutionDelivery{ChatID: chatID, ExecutionID: executionID, SourceID: "run", Files: []ExecutionDeliveryFile{first, second}}
	if err := chats.PublishExecutionDelivery(ctx, delivery); !errors.Is(err, status_error.WorkflowFileNotFound) {
		t.Fatalf("initial delivery = %v, want the missing second file error", err)
	}
	const removedPath = "attachments/first/first.md"
	directory, err := chats.storage.ChatRoot(chatID)
	if err != nil {
		t.Fatal(err)
	}
	files := workspacefiles.New()
	scope, err := files.OpenChat("reader", chatID, t.TempDir(), directory, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := files.DeleteChatFile("reader", scope.ID, "", removedPath); err != nil {
		t.Fatal(err)
	}
	delivery.Files[1].Open = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("second result")), nil
	}
	if err := chats.PublishExecutionDelivery(ctx, delivery); err != nil {
		t.Fatalf("retry did not finish the outstanding attachment: %v", err)
	}
	listed, err := files.ListChatFiles(ctx, "reader", scope.ID, "")
	if opened != 1 || err != nil || len(listed.RemovedPaths) != 1 || listed.RemovedPaths[0] != removedPath {
		t.Fatalf("retry restored the deleted file: source opens = %d, removed = %+v, err = %v", opened, listed, err)
	}
	page, err := chats.ReadMessageHistory(ctx, chatID, 0, 1)
	if err != nil || len(page.Resources) != 1 || len(page.Resources[0].Attachments) != 2 {
		t.Fatalf("retry did not preserve both delivery references: %+v, %v", page, err)
	}
	content, err := os.ReadFile(filepath.Join(directory, "attachments", "second", "second.md"))
	if err != nil || string(content) != "second result" {
		t.Fatalf("retry did not publish remaining file bytes: %q, %v", content, err)
	}
}

func TestExecutionDeliveryReportsSourceFailureAlongsideTargetFailure(t *testing.T) {
	ctx := t.Context()
	_, chats := newRecoveryTestService(t, t.TempDir())
	const chatID = "5488d879d966499b9774cc5c220029b2"
	const executionID = "exec"
	createCompletedDeliveryExecution(t, chats, chatID, executionID)
	var sourceError error
	delivery := ExecutionDelivery{ChatID: chatID, ExecutionID: executionID, SourceID: "run", Files: []ExecutionDeliveryFile{
		{ID: "missing", Name: "missing.md", Open: func() (io.ReadCloser, error) { return nil, status_error.WorkflowFileNotFound },
			HandleSourceError: func(err error) bool { sourceError = err; return true }},
		{ID: "invalid-target", Name: "../invalid.md"},
		{ID: "copy", Name: "report.md", Open: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("verified result")), nil
		}},
	}}
	if err := chats.PublishExecutionDelivery(ctx, delivery); !errors.Is(err, status_error.MessageAttachmentInvalid) || !errors.Is(sourceError, status_error.WorkflowFileNotFound) {
		t.Fatalf("source error=%v target error=%v", sourceError, err)
	}
	page, err := chats.ReadMessageHistory(ctx, chatID, 0, 10)
	if err != nil || len(page.Resources) != 1 || len(page.Resources[0].Attachments) != 1 || page.Resources[0].Attachments[0].Name != "report.md" {
		t.Fatalf("valid file was not committed on the original execution: %+v, %v", page, err)
	}
}

func TestExecutionDeliveryRetryRechecksReceiptsAfterStaging(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, chats := newRecoveryTestService(t, t.TempDir())
	const chatID = "19477c10bb0541cfafee37fb75be9c58"
	const executionID = "exec"
	createCompletedDeliveryExecution(t, chats, chatID, executionID)
	opening, firstCommitted := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- chats.PublishExecutionDelivery(ctx, ExecutionDelivery{ChatID: chatID, ExecutionID: executionID, SourceID: "run", Files: []ExecutionDeliveryFile{{
			ID: "report", Name: "report.md", Open: func() (io.ReadCloser, error) {
				close(opening)
				select {
				case <-firstCommitted:
					return io.NopCloser(strings.NewReader("concurrent result")), nil
				case <-ctx.Done():
					return nil, status_error.OperationCanceled
				}
			},
		}}})
	}()
	select {
	case <-opening:
	case err := <-done:
		t.Fatalf("concurrent delivery did not open the source: %v", err)
	case <-ctx.Done():
		t.Fatal("concurrent delivery did not open the source")
	}
	if err := chats.PublishExecutionDelivery(ctx, ExecutionDelivery{ChatID: chatID, ExecutionID: executionID, SourceID: "run", Files: []ExecutionDeliveryFile{{
		ID: "report", Name: "report.md", Open: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("first result")), nil
		},
	}}}); err != nil {
		t.Fatal(err)
	}
	directory, err := chats.storage.ChatRoot(chatID)
	if err != nil {
		t.Fatal(err)
	}
	files := workspacefiles.New()
	scope, err := files.OpenChat("reader", chatID, t.TempDir(), directory, nil)
	if err != nil {
		t.Fatal(err)
	}
	const removedPath = "attachments/report/report.md"
	if err := files.DeleteChatFile("reader", scope.ID, "", removedPath); err != nil {
		t.Fatal(err)
	}
	close(firstCommitted)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a competing receipt should supersede the staged copy: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("concurrent delivery did not finish")
	}
	listed, err := files.ListChatFiles(ctx, "reader", scope.ID, "")
	if err != nil || len(listed.RemovedPaths) != 1 || listed.RemovedPaths[0] != removedPath {
		t.Fatalf("concurrent delivery restored the deleted file: %+v, %v", listed, err)
	}
}

func createCompletedDeliveryExecution(t *testing.T, chats *service, chatID, executionID string) {
	t.Helper()
	workspace := t.TempDir()
	if _, err := chats.logStore.Create(chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "waker", WorkspaceDirectory: workspace, AdditionalWorkingDirectories: []string{workspace}})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: executionID, Data: []byte(`{}`)},
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: executionID, Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := chats.LoadExecutionChat(t.Context(), chatID); err != nil {
		t.Fatal(err)
	}
}

func TestLateExecutionDeliveryReplaysNativeMetricsAndCurrentForkBoundary(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	store := newTestConversationJournal(t, root)
	const chatID = "ab34ab34ab34ab34ab34ab34ab34ab34"
	const executionID = "first-execution"
	const newestID = "newer-execution"
	binding := forkTestBinding("native-session")
	key := agent.NativeResultKey{Session: binding.Session, InvocationID: "native-turn"}
	inputTokens := int64(23)
	if err := store.CommitNativeResult(ctx, agent.NativeResult{Key: key, ForkMode: acpmeta.ForkLatest,
		FinalMessage: &agent.NativeFinalMessage{ID: "native-message", Text: "Original answer"},
		Turn: acpmeta.NativeTurn{InvocationID: key.InvocationID, Origin: acpmeta.NativeTurnOriginHost, Status: acpmeta.NativeTurnCompleted,
			ConsumedInputIDs: []string{"input"}, Usage: &acpmeta.TurnUsage{Source: "fixture", Coverage: "native_turn", Complete: true, InputTokens: &inputTokens}},
	}); err != nil {
		t.Fatal(err)
	}
	terminal := marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})
	receipt, err := store.Commit(ctx, agent.ConversationBatch{ChatID: chatID, Key: "first", Changes: []agent.ConversationChange{
		{Fact: &agent.ChatFact{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "waker"})}},
		{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: executionID, Data: []byte(`{}`)}},
		{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: executionID, InvocationID: "host"}},
		{Binding: &binding},
		{Input: &agent.HostInput{Session: binding.Session, HostInvocationID: "host", AdapterInputID: "input", ExecutionID: executionID, AssistantMessageID: executionID}},
		{Consumption: &agent.InputConsumption{HostInvocationID: "host", Result: key}},
		{Message: &agent.MessageChange{MessageID: executionID, ExecutionID: executionID, Kind: agent.MessageNativeFinal, Result: &key}},
		{Message: &agent.MessageChange{MessageID: executionID, ExecutionID: executionID, Kind: agent.MessageComplete}},
		{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: executionID, Data: terminal}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	chats := newTestService(t, Config{StorageDirectory: root, Conversations: store.Store})
	t.Cleanup(chats.Close)
	assertDelivery := func(after int64, summary string, available bool) {
		t.Helper()
		if _, err := store.Append(chatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeExecutionDelivery, MessageID: executionID,
			ExecutionID: executionID, Data: marshalData(eventlog.ExecutionDeliveryData{SourceID: "run", Summary: summary})}}); err != nil {
			t.Fatal(err)
		}
		events, err := chats.ListEvents(ctx, chatID, after, 1)
		if err != nil || len(events) != 1 || events[0].Message == nil || events[0].Type != constants.ChatEventTypeExecutionDelivery {
			t.Fatalf("late delivery replay = %+v, %v", events, err)
		}
		message := events[0].Message
		if message.Content != "Original answer\n\n"+summary || message.RunMetrics == nil || message.RunMetrics.InputTokens != inputTokens || message.RunMetrics.TokenUsage == nil {
			t.Errorf("late delivery did not preserve the native answer and usage: %+v", message)
		}
		if message.Fork == nil || !message.Fork.LatestOnly || message.Fork.Available != available || (!available && message.Fork.ReasonCode != "latest_only") {
			t.Errorf("late delivery fork = %+v, want latest-only with availability %v", message.Fork, available)
		}
		history, err := chats.ReadMessageHistory(ctx, chatID, 0, 2)
		if err != nil || len(history.Resources) == 0 || !reflect.DeepEqual(*message, history.Resources[0]) {
			t.Fatalf("late replay message %+v differs from latest history %+v: %v", message, history, err)
		}
	}
	assertDelivery(receipt.Version, "First delivery", true)
	newer, err := store.Append(chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: newestID, Data: []byte(`{}`)},
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: newestID, Data: terminal},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertDelivery(newer[len(newer)-1].Sequence, "Updated delivery", false)
}

func assertSafeQuestionResult(t *testing.T, encoded []byte) {
	t.Helper()
	body := string(encoded)
	if !strings.Contains(body, `"result":"accepted"`) || !strings.Contains(body, `"answers":{"choice":"green"}`) || strings.Contains(body, "secret-") {
		t.Fatalf("unsafe or missing public question result: %s", body)
	}
}

func TestChatHistoryProjectsCommittedNativeFragments(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	id := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	store, err := conversation.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := store.Commit(ctx, agent.ConversationBatch{ChatID: id, Key: "create", Changes: []agent.ConversationChange{
		{Fact: &agent.ChatFact{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "waker"})}},
		{Fact: &agent.ChatFact{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "user", Data: marshalData(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Content: "question"})}},
		{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "exec", Data: marshalData(eventlog.ExecutionCreatedData{UserMessageID: "user"})}},
		{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "exec"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	key := agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "codex", Subject: "subject", SessionID: "session"}, InvocationID: "prompt"}
	native, err := store.CommitNativeFragments(ctx, agent.NativeFragmentBatch{Key: key, CommitKey: "fragment", MessageID: "native-message", Changes: []agent.MessageChange{
		{Kind: agent.MessageAppend, ContentType: constants.RuntimeEventTypeTextDelta, Text: "answer", Revision: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Commit(ctx, agent.ConversationBatch{ChatID: id, Key: "link", ExpectedVersion: receipt.Version, Changes: []agent.ConversationChange{
		{Message: &agent.MessageChange{MessageID: "exec", ExecutionID: "exec", Kind: agent.MessageNativeFragment, Fragments: &native, Revision: 1}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	chats := newTestService(t, Config{StorageDirectory: root, Conversations: store})
	defer chats.Close()
	page, err := chats.ReadMessageHistory(ctx, id, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Resources) != 1 || page.Resources[0].Content != "answer" || !page.HasMore {
		t.Fatalf("native history page = %+v", page)
	}
	older, err := chats.ReadMessageHistory(ctx, id, page.NextBeforeSequence, 1)
	if err != nil || len(older.Resources) != 1 || older.Resources[0].Content != "question" || older.HasMore {
		t.Fatalf("older user message = %+v, err = %v", older, err)
	}
}

func TestChatNativeAdmissionStreamsTerminalAndReopens(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "chat.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store, err := conversation.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	reading := controllers.NewChatReadingController(db.DB)
	config := Config{
		StorageDirectory: root,
		Conversations:    store,
		Projects:         controllers.NewProjectController(db.DB),
		QueryIndexes: QueryIndexDependencies{
			Reading:         reading,
			Chats:           controllers.NewChatController(db.DB),
			DeletionIntents: controllers.NewChatDeletionIntentController(db.DB),
			Transactions:    controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)),
		},
	}
	chats, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer chats.Close()
	if err := chats.Start(ctx); err != nil {
		t.Fatal(err)
	}
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	thread := chats.(NativeJournal)
	chat := models.Chat{ID: id, WakerID: "waker", WorkspaceDirectory: root, AdditionalWorkingDirectories: []string{root}}
	execution := models.Execution{ID: "exec", ChatID: id, UserMessageID: "user", RuntimeID: "codex"}
	turn := StartedTurn{NewChat: true, Chat: chat, Execution: execution, Facts: []eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: chat.WakerID, WorkspaceDirectory: root, AdditionalWorkingDirectories: []string{root}})},
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "user", Data: marshalData(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Content: "question"})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "exec", Data: marshalData(eventlog.ExecutionCreatedData{UserMessageID: "user", RuntimeID: "codex"})},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "exec", InvocationID: "host"},
	}}
	batch, err := thread.PrepareTurnAdmission(ctx, turn, "admission")
	if err != nil {
		t.Fatal(err)
	}
	session := agent.NativeSessionKey{Provider: "codex", Subject: "subject", SessionID: "session"}
	batch.Changes = append(batch.Changes,
		agent.ConversationChange{Binding: &agent.NativeBinding{Session: session}},
		agent.ConversationChange{Input: &agent.HostInput{Session: session, HostInvocationID: "host", AdapterInputID: "input", ExecutionID: "exec", AssistantMessageID: "exec"}},
	)
	admission, err := store.Commit(ctx, batch)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := thread.ProjectTurnAdmission(ctx, turn, admission)
	if err != nil || accepted.Execution.Status != constants.ExecutionStatusRunning {
		t.Fatalf("admission = %+v, err = %v", accepted, err)
	}
	subscription, err := chats.SubscribeEvents(ctx, id, admission.Version)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	key := agent.NativeResultKey{Session: session, InvocationID: "prompt"}
	started := admission.First + int64(len(turn.Facts)) - 1
	live := thread.BeginNativeLive(id, execution.ID, started)
	defer live.Close()
	nextEvent := func() PublicEvent {
		t.Helper()
		select {
		case event, open := <-subscription.Events:
			if !open {
				t.Fatal("subscription closed")
			}
			return event
		case <-ctx.Done():
			t.Fatal("no native message event")
			return PublicEvent{}
		}
	}
	version, revision := admission.Version, admission.Version
	checkpoint := func(lane string, change agent.MessageChange) *messageview.Message {
		t.Helper()
		commitKey := fmt.Sprintf("fragment-%d", change.Revision)
		native, err := store.CommitNativeFragments(ctx, agent.NativeFragmentBatch{
			Key: key, CommitKey: commitKey, MessageID: lane, Changes: []agent.MessageChange{change},
		})
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := store.Commit(ctx, agent.ConversationBatch{ChatID: id, Key: commitKey, ExpectedVersion: version, Changes: []agent.ConversationChange{
			{Message: &agent.MessageChange{MessageID: execution.ID, ExecutionID: execution.ID, Kind: agent.MessageNativeFragment, Fragments: &native, Revision: change.Revision}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		version = receipt.Version
		change.MessageID = execution.ID
		if change.Kind == agent.MessageToolUpdate {
			change.Kind = agent.MessageToolSnapshot
		}
		live.Checkpoint(ctx, receipt, change)
		event := nextEvent()
		if event.Type != constants.ChatEventTypeAssistantMessage || event.Sequence != version ||
			event.Message == nil || event.Message.LiveRevision != change.Revision {
			t.Fatalf("checkpoint event = %+v", event)
		}
		return event.Message
	}
	appendPart := func(text string, kind constants.RuntimeEventType, frame, base int64) (agent.MessageChange, string) {
		t.Helper()
		revision++
		change := agent.MessageChange{Kind: agent.MessageAppend, ContentType: kind, Text: text, Revision: revision}
		live.Append(change)
		event := nextEvent()
		if !event.Transient || event.Sequence != 0 || event.Live == nil || event.Live.Sequence != frame ||
			event.Live.BaseRevision != base || event.Live.Revision != revision {
			t.Fatalf("live event = %+v", event)
		}
		part := event.Live.Updates[0].Parts[0]
		if part.Text != text {
			t.Fatalf("live part = %+v", part)
		}
		return change, part.PartID
	}
	first, partID := appendPart("partial", constants.RuntimeEventTypeTextDelta, 1, started)
	uncommitted, err := reading.Get(ctx, constants.LocalReaderID, id)
	if err != nil || uncommitted.UnreadMessageCount != 0 {
		t.Fatalf("transient text was counted: %+v %v", uncommitted, err)
	}
	if message := checkpoint("native-message", first); message.Content != "partial" || message.DisplayParts[0].ID != partID {
		t.Fatalf("first checkpoint changed the live part: %+v", message)
	}
	// 在没有阅读请求和终态的情况下，原投影所有者会覆盖已提交的公开正文。
	deadline := time.NewTimer(4 * time.Second)
	t.Cleanup(func() { deadline.Stop() })
	probe := time.NewTicker(10 * time.Millisecond)
	defer probe.Stop()
	var seen models.ChatReadingState
readingCheckpoint:
	for {
		select {
		case <-deadline.C:
			t.Fatalf("durable native answer remained unreadable before terminal: %+v", seen)
		case <-probe.C:
			seen, err = reading.Get(ctx, constants.LocalReaderID, id)
			if err != nil {
				t.Fatal(err)
			}
			if seen.UnreadMessageCount == 1 && seen.FirstUnreadMessageID == "exec" {
				break readingCheckpoint
			}
		}
	}
	if err := reading.MarkRead(ctx, constants.LocalReaderID, id, "exec", seen.UnreadGeneration, constants.ReadingIntentExplicit); err != nil {
		t.Fatal(err)
	}
	second, samePart := appendPart(" suffix", constants.RuntimeEventTypeTextDelta, 2, first.Revision)
	if samePart != partID {
		t.Fatalf("live suffix changed part from %q to %q", partID, samePart)
	}
	checkpoint("native-message", second)

	revision++
	tool := agent.MessageChange{Kind: agent.MessageToolUpdate, ContentType: constants.RuntimeEventTypeToolUpdate,
		ToolCallID: "tool", Data: []byte(`{"id":"tool","status":"in_progress","kind":"execute","input":{"command":"go test ./example"}}`),
		Text: strings.Repeat("TERMINAL_SECRET", 6000), Revision: revision}
	checkpoint("tool", tool)
	third, separatedPart := appendPart(" then", constants.RuntimeEventTypeTextDelta, 3, tool.Revision)
	checkpoint("native-message", third)
	revision++
	tool.Kind, tool.Revision, tool.Data, tool.Text = agent.MessageToolUpdate, revision, []byte(`{"id":"tool","status":"completed"}`), ""
	updated := checkpoint("tool", tool)
	if len(updated.DisplayParts) != 2 || updated.DisplayParts[0].Text != "partial suffix then" {
		t.Fatalf("tool checkpoint did not merge adjacent text: %+v", updated.DisplayParts)
	}
	_, nextPart := appendPart(" reasoning", constants.RuntimeEventTypeThinkingDelta, 4, tool.Revision)
	if nextPart != separatedPart {
		t.Fatalf("live output did not continue after reordered tool: part %q, want %q", nextPart, separatedPart)
	}
	live.Close()
	inputTokens := int64(11)
	if err := store.CommitNativeResult(ctx, agent.NativeResult{
		Key: key, StopReason: "end_turn", FinalMessage: &agent.NativeFinalMessage{ID: "native-message", Text: "final"},
		Turn: acpmeta.NativeTurn{InvocationID: key.InvocationID, Origin: acpmeta.NativeTurnOriginHost, Status: acpmeta.NativeTurnCompleted,
			ConsumedInputIDs: []string{"input"}, Usage: &acpmeta.TurnUsage{InputTokens: &inputTokens, Coverage: "last_request", Source: "fixture"}},
	}); err != nil {
		t.Fatal(err)
	}
	settled, err := store.Commit(ctx, agent.ConversationBatch{ChatID: id, Key: "consumed", ExpectedVersion: version, Changes: []agent.ConversationChange{
		{Consumption: &agent.InputConsumption{HostInvocationID: "host", Result: key}},
		{Message: &agent.MessageChange{MessageID: "exec", ExecutionID: "exec", Kind: agent.MessageNativeFinal, Result: &key}},
		{Message: &agent.MessageChange{MessageID: "exec", ExecutionID: "exec", Kind: agent.MessageComplete}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	thread.ObserveCommitted(id, settled)
	terminal, err := thread.CommitTerminal(ctx, TerminalFact{Execution: accepted.Execution, Type: constants.ChatEventTypeExecutionCompleted,
		Status: constants.ExecutionStatusCompleted, Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})})
	if err != nil {
		t.Fatal(err)
	}
	thread.PublishTerminal(terminal)
	for {
		select {
		case event, open := <-subscription.Events:
			if !open {
				t.Fatal("subscription closed before terminal")
			}
			if event.Type != constants.ChatEventTypeExecutionCompleted {
				continue
			}
			if event.Message == nil || event.Message.Content != "final" || event.Message.RunMetrics == nil ||
				event.Message.RunMetrics.StartedAt == 0 || event.Message.RunMetrics.InputTokens != 0 || event.Sequence <= settled.Version {
				t.Fatalf("terminal event = %+v message = %+v", event, event.Message)
			}
			goto reopen
		case <-ctx.Done():
			t.Fatal("no terminal event")
		}
	}

reopen:
	page, err := chats.ListEvents(ctx, id, admission.Version, 1)
	if err != nil || len(page) != 1 || page[0].Sequence != settled.Version || page[0].Message == nil || page[0].Message.Content != "final" {
		t.Fatalf("coalesced replay page = %+v, err = %v", page, err)
	}
	readingState, err := reading.Get(ctx, constants.LocalReaderID, id)
	if err != nil || readingState.UnreadMessageCount != 0 || readingState.LatestReadableMessageID != "exec" {
		t.Fatalf("native answer reading state: %+v %v", readingState, err)
	}
	subscription.Close()
	chats.Close()
	reopened, err := conversation.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	config.Conversations = reopened
	chats, err = New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer chats.Close()
	if err := chats.Start(ctx); err != nil {
		t.Fatal(err)
	}
	history, err := chats.ReadMessageHistory(ctx, id, 0, 1)
	if err != nil || len(history.Resources) != 1 {
		t.Fatalf("recovered history = %+v, err = %v", history, err)
	}
	message := history.Resources[0]
	if message.Content != "final" || message.Status != constants.ExecutionStatusCompleted ||
		message.RunMetrics == nil || message.RunMetrics.StartedAt == 0 || message.RunMetrics.InputTokens != 0 || !history.HasMore {
		t.Fatalf("recovered assistant message = %+v", message)
	}
	summary, err := chats.GetChatSummary(ctx, id)
	if err != nil || summary.Chat.ActiveExecutionID != "" || summary.Chat.LastEndedExecutionID != "exec" {
		t.Fatalf("recovered summary = %+v, err = %v", summary, err)
	}
	canonical, err := reopened.ReadNativeResult(ctx, key)
	if err != nil || canonical.Turn.Usage == nil || canonical.Turn.Usage.InputTokens == nil ||
		*canonical.Turn.Usage.InputTokens != inputTokens || canonical.Turn.Usage.Complete || canonical.Turn.Usage.Coverage != "last_request" {
		t.Fatalf("archived partial usage = %+v, err = %v", canonical.Turn.Usage, err)
	}
	outcome, err := chats.ReadExecutionOutcome(ctx, id, "exec")
	if err != nil || outcome.InvocationID != "prompt" || outcome.TerminationReason != constants.RuntimeTerminationReasonNormal {
		t.Fatalf("recovered outcome = %+v, err = %v", outcome, err)
	}
	messages, err := chats.ReadExecutionMessages(ctx, id, "exec")
	if err != nil || len(messages) != 2 || messages[0].Content != "question" || messages[1].Content != "final" {
		t.Fatalf("recovered execution messages = %+v, err = %v", messages, err)
	}
}

func TestChatTerminalUsageProjectionFollowsAuthority(t *testing.T) {
	for _, input := range []*int64{nil, new(int64)} {
		name := "unavailable"
		var wantInput int64
		if input != nil {
			name = "reported"
			*input, wantInput = 60, 60
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			id := "dddddddddddddddddddddddddddddddd"
			_, chats := newRecoveryTestService(t, t.TempDir())
			store := chats.logStore.(*conversationJournal).Store
			session := agent.NativeSessionKey{Provider: "codex", Subject: "subject", SessionID: "session"}
			key := agent.NativeResultKey{Session: session, InvocationID: "native-outcome"}
			turn := acpmeta.NativeTurn{
				InvocationID: key.InvocationID, Origin: acpmeta.NativeTurnOriginHost,
				Status: acpmeta.NativeTurnCompleted, ConsumedInputIDs: []string{"input"},
			}
			if input != nil {
				turn.Usage = &acpmeta.TurnUsage{Source: "fixture", Coverage: "native_turn", Complete: true, InputTokens: input}
			}
			if err := store.CommitNativeResult(ctx, agent.NativeResult{Key: key, Turn: turn}); err != nil {
				t.Fatal(err)
			}
			_, err := store.Commit(ctx, agent.ConversationBatch{ChatID: id, Key: "native-outcome",
				Changes: []agent.ConversationChange{
					{Fact: &agent.ChatFact{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "waker"})}},
					{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "exec", Data: []byte(`{}`)}},
					{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "exec", InvocationID: "host"}},
					{Binding: &agent.NativeBinding{Session: session}},
					{Input: &agent.HostInput{Session: session, HostInvocationID: "host", AdapterInputID: "input", ExecutionID: "exec", AssistantMessageID: "exec"}},
					{Consumption: &agent.InputConsumption{HostInvocationID: "host", Result: key}},
					{Message: &agent.MessageChange{MessageID: "exec", ExecutionID: "exec", Kind: agent.MessageComplete}},
					{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "exec",
						Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})}},
				}})
			if err != nil {
				t.Fatal(err)
			}
			history, err := chats.ReadMessageHistory(ctx, id, 0, 1)
			if err != nil || len(history.Resources) != 1 || history.Resources[0].RunMetrics == nil ||
				history.Resources[0].RunMetrics.InputTokens != wantInput {
				t.Fatalf("message history = %+v, err = %v", history, err)
			}
			subscription, err := chats.SubscribeEvents(ctx, id, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer subscription.Close()
			for _, event := range subscription.Backlog {
				if event.Type != constants.ChatEventTypeExecutionCompleted {
					continue
				}
				if event.Message == nil || event.Message.RunMetrics == nil || event.Message.RunMetrics.InputTokens != wantInput {
					t.Fatalf("terminal = %+v, message = %+v, want input tokens %d", event, event.Message, wantInput)
				}
				return
			}
			t.Fatal("terminal missing from public replay")
		})
	}
}
