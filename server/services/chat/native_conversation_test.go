package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
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
