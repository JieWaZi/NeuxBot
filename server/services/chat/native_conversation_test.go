package chat

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog_next"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestChatHistoryProjectsCommittedNativeFragments(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	id := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	store, err := eventlog_next.NewStore(root)
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
	store, err := eventlog_next.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	results := controllers.NewWorkResultController(db.DB)
	if err := results.EnableTracking(ctx, 1); err != nil {
		t.Fatal(err)
	}
	config := Config{
		Results:          results,
		StorageDirectory: root,
		Conversations:    store,
		Projects:         controllers.NewProjectController(db.DB),
		QueryIndexes: QueryIndexDependencies{
			Chats: controllers.NewChatController(db.DB), RuntimeSessions: controllers.NewRuntimeSessionController(db.DB),
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
	native, err := store.CommitNativeFragments(ctx, agent.NativeFragmentBatch{Key: key, CommitKey: "fragment", MessageID: "native-message", Changes: []agent.MessageChange{
		{Kind: agent.MessageAppend, ContentType: constants.RuntimeEventTypeTextDelta, Text: "partial", Revision: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	partial, err := store.Commit(ctx, agent.ConversationBatch{ChatID: id, Key: "link", ExpectedVersion: admission.Version, Changes: []agent.ConversationChange{
		{Message: &agent.MessageChange{MessageID: "exec", ExecutionID: "exec", Kind: agent.MessageNativeFragment, Fragments: &native, Revision: 1}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	thread.ObserveCommitted(id, partial)
	select {
	case event := <-subscription.Events:
		if event.Type != constants.ChatEventTypeAssistantMessage || event.Sequence != partial.Version || event.Message == nil || event.Message.Content != "partial" {
			t.Fatalf("committed partial event = %+v", event)
		}
	case <-ctx.Done():
		t.Fatal("no committed fragment event")
	}
	inputTokens := int64(11)
	if err := store.CommitNativeResult(ctx, agent.NativeResult{
		Key: key, StopReason: "end_turn", FinalMessage: &agent.NativeFinalMessage{ID: "native-message", Text: "final"},
		Turn: acpmeta.NativeTurn{InvocationID: key.InvocationID, Origin: acpmeta.NativeTurnOriginHost, Status: acpmeta.NativeTurnCompleted,
			ConsumedInputIDs: []string{"input"}, Usage: &acpmeta.TurnUsage{InputTokens: &inputTokens, Coverage: "last_request", Source: "fixture"}},
	}); err != nil {
		t.Fatal(err)
	}
	settled, err := store.Commit(ctx, agent.ConversationBatch{ChatID: id, Key: "consumed", ExpectedVersion: partial.Version, Changes: []agent.ConversationChange{
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
	result, found, err := results.Get(ctx, constants.WorkResultOwnerKindChat, id)
	if err != nil || !found || result.Reference.MessageID != "exec" || result.LatestVersion <= settled.Version {
		t.Fatalf("result receipt = %+v, found = %v, err = %v", result, found, err)
	}
	subscription.Close()
	chats.Close()
	reopened, err := eventlog_next.NewStore(root)
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

func TestChatReadsLegacyRecordsThroughConversationStore(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	legacy, err := eventlog.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	id := "cccccccccccccccccccccccccccccccc"
	_, err = legacy.Create(id, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "waker"})},
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "user", Data: marshalData(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Content: "legacy question"})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "exec", Data: marshalData(eventlog.ExecutionCreatedData{UserMessageID: "user"})},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "exec", InvocationID: "old-invocation"},
		{Type: constants.ChatEventTypeAssistantMessage, MessageID: "exec", ExecutionID: "exec", InvocationID: "old-invocation", Data: marshalData(eventlog.RuntimeMessageData{
			Content: []eventlog.RuntimeMessageContent{{Type: constants.RuntimeEventTypeTextDelta, Data: marshalData(map[string]string{"text": "legacy answer"})}}, MessageRevision: 5,
		})},
		{Type: constants.ChatEventTypeInvocationFinished, ExecutionID: "exec", InvocationID: "old-invocation", Data: marshalData(map[string]string{"stopReason": "end_turn", "terminationReason": string(constants.RuntimeTerminationReasonNormal)})},
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "exec", Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, chats := newRecoveryTestService(t, root)
	if err := chats.Start(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := chats.ReadMessageHistory(ctx, id, 0, 1)
	if err != nil || len(page.Resources) != 1 || page.Resources[0].Content != "legacy answer" || !page.HasMore {
		t.Fatalf("legacy message page = %+v, err = %v", page, err)
	}
	outcome, err := chats.ReadExecutionOutcome(ctx, id, "exec")
	if err != nil || outcome.InvocationID != "old-invocation" || outcome.TerminationReason != constants.RuntimeTerminationReasonNormal {
		t.Fatalf("legacy outcome = %+v, err = %v", outcome, err)
	}
}

func TestChatTerminalUsageProjectionFollowsAuthority(t *testing.T) {
	for _, native := range []bool{false, true} {
		name := "schema-one"
		wantInput := int64(60)
		if native {
			name = "canonical-outcome-without-usage"
			wantInput = 0
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			root := t.TempDir()
			id := "dddddddddddddddddddddddddddddddd"
			legacy, err := eventlog.NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			finished := marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})
			facts := []eventlog.AppendInput{
				{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "waker"})},
				{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "exec", Data: []byte(`{}`)},
				{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "exec", InvocationID: "legacy-invocation"},
				{Type: constants.ChatEventTypeAssistantMessage, MessageID: "exec", ExecutionID: "exec", Data: marshalData(eventlog.RuntimeMessageData{
					Content:         []eventlog.RuntimeMessageContent{{Type: constants.RuntimeEventTypeUsage, Data: []byte(`{"turn":{"input_tokens":60}}`)}},
					MessageRevision: 4,
				})},
			}
			if !native {
				facts = append(facts, eventlog.AppendInput{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "exec", Data: finished})
			}
			records, err := legacy.Create(id, facts)
			if err != nil {
				t.Fatal(err)
			}
			_, chats := newRecoveryTestService(t, root)
			if native {
				store := chats.logStore.(*conversationJournal).Store
				session := agent.NativeSessionKey{Provider: "codex", Subject: "subject", SessionID: "session"}
				key := agent.NativeResultKey{Session: session, InvocationID: "native-outcome"}
				if err := store.CommitNativeResult(ctx, agent.NativeResult{Key: key, Turn: acpmeta.NativeTurn{
					InvocationID: key.InvocationID, Origin: acpmeta.NativeTurnOriginHost, Status: acpmeta.NativeTurnCompleted, ConsumedInputIDs: []string{"input"},
				}}); err != nil {
					t.Fatal(err)
				}
				_, err := store.Commit(ctx, agent.ConversationBatch{ChatID: id, Key: "native-outcome", ExpectedVersion: records[len(records)-1].Sequence,
					Changes: []agent.ConversationChange{
						{Binding: &agent.NativeBinding{Session: session}},
						{Input: &agent.HostInput{Session: session, HostInvocationID: "host", AdapterInputID: "input", ExecutionID: "exec", AssistantMessageID: "exec"}},
						{Consumption: &agent.InputConsumption{HostInvocationID: "host", Result: key}},
						{Message: &agent.MessageChange{MessageID: "exec", ExecutionID: "exec", Kind: agent.MessageComplete}},
						{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "exec", Data: finished}},
					}})
				if err != nil {
					t.Fatal(err)
				}
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
