package eventlog_next

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"gorm.io/gorm"
)

const chatID = "0123456789abcdef0123456789abcdef"

func openTestStore(t *testing.T, root string) *Store {
	t.Helper()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

func writeBatch(t *testing.T, store *Store, key string, version int64, changes ...agent.ConversationChange) agent.CommitReceipt {
	t.Helper()
	result, err := store.Commit(context.Background(), agent.ConversationBatch{ChatID: chatID, Key: key, ExpectedVersion: version, Changes: changes})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func fact(kind constants.ChatEventType, messageID, data string) agent.ConversationChange {
	return agent.ConversationChange{Fact: &agent.ChatFact{Type: kind, MessageID: messageID, Data: json.RawMessage(data)}}
}

func TestInterruptedBatchNeverPublishesCompletePrefix(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	writeBatch(t, store, "create", 0, fact(constants.ChatEventTypeCreated, "", ""))
	path, err := store.LogPath(chatID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeBatch(t, store, "started", 1,
		fact(constants.ChatEventTypeUserMessageCreated, "input-one", `{"content":"hello"}`),
		fact(constants.ChatEventTypeExecutionCreated, "turn-one", ""))
	all, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for offset := len(before) + 1; offset < len(all); offset++ {
		if err := os.WriteFile(path, all[:offset], 0600); err != nil {
			t.Fatal(err)
		}
		recovered := openTestStore(t, root)
		page, err := recovered.ReadChanges(ctx, chatID, 0, 10)
		recovered.Close()
		if err != nil || len(page.Records) != 1 || page.Version != 1 {
			t.Fatalf("offset %d exposed interrupted batch: %+v, %v", offset, page, err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() != int64(len(before)) {
			t.Fatalf("offset %d was not repaired at committed boundary: %v, %v", offset, info, err)
		}
	}
}

func TestCommitRetryAfterRestartReturnsOriginalReceipt(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	first := writeBatch(t, store, "create", 0, fact(constants.ChatEventTypeCreated, "", ""))
	writeBatch(t, store, "next", 1, fact(constants.ChatEventTypeUserMessageCreated, "u", `{"content":"hello"}`))
	reopened := openTestStore(t, root)
	got := writeBatch(t, reopened, "create", 0, fact(constants.ChatEventTypeCreated, "", ""))
	if got != first || got.Version != 1 {
		t.Fatalf("retry receipt = %+v, original %+v", got, first)
	}
	for _, candidate := range []struct {
		batch agent.ConversationBatch
		want  error
	}{
		{agent.ConversationBatch{ChatID: chatID, Key: "next", ExpectedVersion: 1, Changes: []agent.ConversationChange{fact(constants.ChatEventTypeUserMessageCreated, "u", `{"content":"changed"}`)}}, status_error.ChatLogIdempotencyConflict},
		{agent.ConversationBatch{ChatID: chatID, Key: "new", ExpectedVersion: 1, Changes: []agent.ConversationChange{fact(constants.ChatEventTypeUserMessageCreated, "new", "")}}, status_error.ChatLogVersionConflict},
	} {
		if _, err := reopened.Commit(ctx, candidate.batch); !errors.Is(err, candidate.want) {
			t.Fatalf("commit error = %v, want %v", err, candidate.want)
		}
	}
	page, err := reopened.ReadChanges(ctx, chatID, 0, 10)
	if err != nil || len(page.Records) != 2 {
		t.Fatalf("conflict changed history: %+v, %v", page, err)
	}
}

func TestPagingRebuildsDerivedIndexAndReplaysMessageChanges(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	path, err := store.IndexPath(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	key := agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "codex", Subject: "account", SessionID: "native"}, InvocationID: "P"}
	fragment := func(id, command string, change agent.MessageChange) agent.ConversationChange {
		t.Helper()
		refs, err := store.CommitNativeFragments(ctx, agent.NativeFragmentBatch{Key: key, CommitKey: command, MessageID: id, Changes: []agent.MessageChange{change}})
		if err != nil {
			t.Fatal(err)
		}
		return agent.ConversationChange{Message: &agent.MessageChange{MessageID: id, Kind: agent.MessageNativeFragment, Fragments: &refs}}
	}
	message := func(id, command string, kind agent.MessageChangeKind, text string) agent.ConversationChange {
		return fragment(id, command, agent.MessageChange{Kind: kind, Text: text})
	}
	tool := func(command, data string) agent.ConversationChange {
		return fragment("a1", command, agent.MessageChange{Kind: agent.MessageToolSnapshot, ToolCallID: "tool", Data: json.RawMessage(data)})
	}
	writeBatch(t, store, "create", 0, fact(constants.ChatEventTypeCreated, "", ""),
		message("a1", "a1-first", agent.MessageAppend, "he"), tool("tool-first", `{"n":1}`), tool("tool-second", `{"n":2}`),
		message("a2", "a2-first", agent.MessageAppend, "second"), message("a1", "a1-second", agent.MessageAppend, "llo"),
		message("a1", "a1-replace", agent.MessageReplace, "corrected"),
		agent.ConversationChange{Message: &agent.MessageChange{MessageID: "a1", Kind: agent.MessageComplete}})
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		store = openTestStore(t, root)
		latest, err := store.ReadMessages(ctx, agent.MessageQuery{ChatID: chatID, Limit: 1})
		if err != nil || len(latest.Messages) != 1 || latest.Messages[0].ID != "a2" || latest.Version != 8 || latest.Before != 5 {
			t.Fatalf("latest = %+v, %v", latest, err)
		}
		older, err := store.ReadMessages(ctx, agent.MessageQuery{ChatID: chatID, Before: latest.Before, Limit: 1})
		if err != nil || len(older.Messages) != 1 {
			t.Fatalf("older = %+v, %v", older, err)
		}
		got := older.Messages[0]
		if got.ID != "a1" || got.Text != "corrected" || !got.Complete || len(got.Parts) != 2 || got.Parts[1].ToolCallID != "tool" || string(got.Parts[1].Data) != `{"n":2}` {
			t.Fatalf("folded message = %+v", got)
		}
		tail, err := store.ReadChanges(ctx, chatID, 5, 2)
		if err != nil || len(tail.Records) != 2 || tail.Next != 7 || !tail.More {
			t.Fatalf("resume = %+v, %v", tail, err)
		}
		if err := os.WriteFile(path, []byte("broken index"), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNativeOutcomeReconcilesEitherArrivalOrder(t *testing.T) {
	ctx := context.Background()
	for _, resultFirst := range []bool{false, true} {
		root := t.TempDir()
		store := openTestStore(t, root)
		session := agent.NativeSessionKey{Provider: "codex", Subject: "account-1", SessionID: "native-1"}
		key := agent.NativeResultKey{Session: session, InvocationID: "P"}
		tokens, overlapping := int64(17), int64(12)
		result := agent.NativeResult{Key: key, Turn: acpmeta.NativeTurn{InvocationID: "P",
			Origin: acpmeta.NativeTurnOriginProvider, Status: acpmeta.NativeTurnCompleted,
			ConsumedInputIDs: []string{"adapter-1", "adapter-2"},
			Usage: &acpmeta.TurnUsage{Source: "native", InputTokens: &tokens,
				Observations: []acpmeta.UsageObservation{{Source: "native", Coverage: "overlap",
					Tokens: acpmeta.ModelUsage{InputTokens: &overlapping}}}}},
			FinalMessage: &agent.NativeFinalMessage{ID: "answer", Text: "answer body"}}
		input1 := agent.HostInput{HostInvocationID: "H1", AdapterInputID: "adapter-1", Session: session, ExecutionID: "E1"}
		input2 := agent.HostInput{HostInvocationID: "H2", AdapterInputID: "adapter-2", Session: session, ExecutionID: "E2"}
		refs := []agent.HostInputRef{{ChatID: chatID, HostInvocationID: "H1"}, {ChatID: chatID, HostInvocationID: "H2"}}
		put := func() {
			t.Helper()
			if err := store.CommitNativeResult(ctx, result); err != nil {
				t.Fatal(err)
			}
		}
		if resultFirst {
			put()
			if err := store.ConfirmNativeResult(ctx, key, refs); err == nil {
				t.Fatal("result without Chat associations authorized ACK")
			}
		}
		writeBatch(t, store, "before-submit", 0, fact(constants.ChatEventTypeCreated, "", ""),
			agent.ConversationChange{Binding: &agent.NativeBinding{Session: session}},
			agent.ConversationChange{Input: &input1}, agent.ConversationChange{Input: &input2})
		put()
		if err := store.ConfirmNativeResult(ctx, key, refs); err == nil {
			t.Fatal("admission without consumption authorized ACK")
		}
		link := func(host string) agent.ConversationChange {
			return agent.ConversationChange{Consumption: &agent.InputConsumption{HostInvocationID: host, Result: key}}
		}
		writeBatch(t, store, "consume", 4, link("H1"), link("H2"))
		if err := store.ConfirmNativeResult(ctx, key, refs); err != nil {
			t.Fatal(err)
		}
		turn, err := store.ReadTurn(ctx, chatID, "E2")
		if err != nil || len(turn) != 2 || turn[0].Change.Input == nil || turn[1].Change.Consumption == nil {
			t.Fatalf("execution relation: %+v, %v", turn, err)
		}
		put()
		changed := result
		changed.FinalMessage = &agent.NativeFinalMessage{ID: "answer", Text: "different"}
		if err := store.CommitNativeResult(ctx, changed); !errors.Is(err, status_error.ChatLogIdempotencyConflict) {
			t.Fatalf("conflicting terminal = %v", err)
		}
		store = openTestStore(t, root)
		if err := store.Delete(chatID); err != nil {
			t.Fatal(err)
		}
		got, err := store.ReadNativeResult(ctx, key)
		if err != nil || got.FinalMessage.Text != "answer body" || *got.Turn.Usage.InputTokens != 17 ||
			got.Turn.Usage.OutputTokens != nil || *got.Turn.Usage.Observations[0].Tokens.InputTokens != 12 {
			t.Fatalf("native outcome after deletion = %+v, %v", got, err)
		}
	}
}

func TestReadLegacyPrefixAndAppendCandidateBatch(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	path, err := store.LogPath(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := "{\"schemaVersion\":1,\"recordedAt\":1,\"type\":\"chat.created\"}\n" +
		"{\"schemaVersion\":1,\"recordedAt\":2,\"type\":\"message.user_created\",\"messageId\":\"u\",\"data\":{\"content\":\"legacy user\"}}\n" +
		"{\"schemaVersion\":1,\"recordedAt\":3,\"type\":\"execution.created\",\"executionId\":\"e\"}\n" +
		"{\"schemaVersion\":1,\"recordedAt\":4,\"type\":\"assistant.message\",\"invocationId\":\"inv\",\"data\":{\"content\":[{\"type\":\"text_delta\",\"data\":{\"text\":\"legacy answer\"}}]}}\n" +
		"{\"schemaVersion\":1,\"recordedAt\":5,\"type\":\"execution.completed\"}\n"
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	writeBatch(t, store, "new", 5, fact(constants.ChatEventTypeUserMessageCreated, "new", `{"content":"new user"}`))
	reopened := openTestStore(t, root)
	page, err := reopened.ReadMessages(ctx, agent.MessageQuery{ChatID: chatID, Limit: 3})
	if err != nil || len(page.Messages) != 3 || page.Version != 6 {
		t.Fatalf("mixed messages = %+v, %v", page, err)
	}
	if got := page.Messages; got[0].Text != "legacy user" || got[1].Text != "legacy answer" || !got[1].Complete || got[2].Text != "new user" {
		t.Fatalf("mixed message content = %+v", got)
	}
	resume, err := reopened.ReadChanges(ctx, chatID, 3, 3)
	if err != nil || len(resume.Records) != 3 || resume.Records[0].Change.Fact.InvocationID != "inv" || resume.Next != 6 {
		t.Fatalf("mixed resume = %+v, %v", resume, err)
	}
}

func TestInternalDamageDoesNotTruncateCommittedFile(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	writeBatch(t, store, "create", 0, fact(constants.ChatEventTypeCreated, "", `{"title":"early"}`))
	writeBatch(t, store, "next", 1, fact(constants.ChatEventTypeUserMessageCreated, "u", `{"content":"later"}`))
	path, err := store.LogPath(chatID)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	damaged := bytes.Replace(original, []byte("early"), []byte("wrong"), 1)
	if bytes.Equal(damaged, original) {
		t.Fatal("fixture did not corrupt the first batch")
	}
	if err := os.WriteFile(path, damaged, 0600); err != nil {
		t.Fatal(err)
	}
	store = openTestStore(t, root)
	if _, err := store.ReadChanges(ctx, chatID, 0, 10); !errors.Is(err, status_error.ChatLogCorrupted) {
		t.Fatalf("internal damage error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, damaged) {
		t.Fatalf("corruption recovery changed authority, size %d, err %v", len(got), err)
	}
}

func TestMatchingTerminalReusesNativeBodyAndPreservesOrderedParts(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	const body = "complete synthetic answer with unusual text 引号"
	k := agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "codex", Subject: "account", SessionID: "native"}, InvocationID: "P"}
	fragment := agent.NativeFragmentBatch{Key: k, CommitKey: "fragment", MessageID: "native-message", Changes: []agent.MessageChange{
		{Kind: agent.MessageAppend, ContentType: constants.RuntimeEventTypeCommentaryDelta, Text: "progress"},
		{Kind: agent.MessageEvent, ContentType: constants.RuntimeEventTypeThinkingStart, Data: json.RawMessage(`{}`)},
		{Kind: agent.MessageAppend, ContentType: constants.RuntimeEventTypeThinkingDelta, Text: "thought"},
		{Kind: agent.MessageAppend, Text: body, Revision: 7},
	}}
	rng, err := store.CommitNativeFragments(ctx, fragment)
	if err != nil {
		t.Fatal(err)
	}
	if retry, err := store.CommitNativeFragments(ctx, fragment); err != nil || retry != rng {
		t.Fatalf("fragment retry = %+v, %v", retry, err)
	}
	result := agent.NativeResult{Key: k, Turn: acpmeta.NativeTurn{InvocationID: "P", Origin: acpmeta.NativeTurnOriginProvider, Status: acpmeta.NativeTurnCompleted},
		FinalMessage: &agent.NativeFinalMessage{ID: "native-message", Text: body}}
	if err := store.CommitNativeResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	_, path := store.nativeStream(k)
	payload, err := os.ReadFile(path)
	if err != nil || bytes.Count(payload, []byte(body)) != 1 {
		t.Fatalf("canonical log should contain one physical body; got %d, %v", bytes.Count(payload, []byte(body)), err)
	}
	if err := store.CommitNativeResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	retried, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(retried, payload) {
		t.Fatalf("duplicate outcome wrote authority bytes: %d != %d, %v", len(retried), len(payload), err)
	}

	writeBatch(t, store, "chat", 0, fact(constants.ChatEventTypeCreated, "", ""),
		agent.ConversationChange{Message: &agent.MessageChange{MessageID: "assistant", Kind: agent.MessageNativeFragment, Fragments: &rng}},
		agent.ConversationChange{Message: &agent.MessageChange{MessageID: "assistant", Kind: agent.MessageNativeFinal, Result: &k, Revision: 8}},
		agent.ConversationChange{Message: &agent.MessageChange{MessageID: "assistant", Kind: agent.MessageComplete}})
	chatPath, err := store.LogPath(chatID)
	if err != nil {
		t.Fatal(err)
	}
	chat, err := os.ReadFile(chatPath)
	if err != nil || bytes.Contains(chat, []byte(body)) {
		t.Fatalf("public log should contain only references; %v", err)
	}
	store = openTestStore(t, root)
	page, err := store.ReadMessages(ctx, agent.MessageQuery{ChatID: chatID, Limit: 1})
	if err != nil || len(page.Messages) != 1 {
		t.Fatalf("read page %+v, %v", page, err)
	}
	got := page.Messages[0]
	want := []constants.RuntimeEventType{constants.RuntimeEventTypeCommentaryDelta, constants.RuntimeEventTypeThinkingStart,
		constants.RuntimeEventTypeThinkingDelta, constants.RuntimeEventTypeTextDelta}
	if !got.Complete || got.Revision != 8 || got.Text != body || len(got.Parts) != len(want) {
		t.Fatalf("projected result %+v", got)
	}
	for i, kind := range want {
		if got.Parts[i].Type != kind {
			t.Fatalf("part %d = %+v, want %s", i, got.Parts[i], kind)
		}
	}
	if err := store.Delete(chatID); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.ReadNativeResult(ctx, k)
	if err != nil || reloaded.FinalMessage == nil || reloaded.FinalMessage.Text != body {
		t.Fatalf("canonical body after Chat delete = %+v, %v", reloaded, err)
	}
}

func TestConcurrentRetryAndCASHaveOneDurableWinner(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, t.TempDir())
	writeBatch(t, store, "create", 0, fact(constants.ChatEventTypeCreated, "", ""))
	var group sync.WaitGroup
	type outcome struct {
		// key identifies one of the two competing commands.
		key string
		// receipt is returned unchanged to successful retries.
		receipt agent.CommitReceipt
		// err is a CAS conflict for the losing command.
		err error
	}
	outcomes := make(chan outcome, 16)
	for i := 0; i < 16; i++ {
		group.Go(func() {
			key := strconv.Itoa(i % 2)
			receipt, err := store.Commit(ctx, agent.ConversationBatch{ChatID: chatID, Key: key, ExpectedVersion: 1,
				Changes: []agent.ConversationChange{fact(constants.ChatEventTypeUserMessageCreated, key, `{"content":"one"}`)}})
			outcomes <- outcome{key, receipt, err}
		})
	}
	group.Wait()
	close(outcomes)
	var winner string
	var receipt agent.CommitReceipt
	for result := range outcomes {
		if result.err != nil {
			if !errors.Is(result.err, status_error.ChatLogVersionConflict) {
				t.Fatal(result.err)
			}
			continue
		}
		if winner == "" {
			winner, receipt = result.key, result.receipt
		}
		if winner != result.key || receipt != result.receipt {
			t.Fatalf("competing commit returned success: %+v", result)
		}
	}
	page, err := store.ReadChanges(ctx, chatID, 0, 16)
	if err != nil || winner == "" || page.Version != 2 || len(page.Records) != 2 {
		t.Fatalf("concurrent history %+v winner %q err %v", page, winner, err)
	}
}

func TestDetachedTextAdmissionCanBeAcknowledgedWithoutPublicChat(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, root)
	k := agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "codex", Subject: "account", SessionID: "private-text"}, InvocationID: "P"}
	refs := []agent.HostInputRef{{HostInvocationID: "private-H"}}
	outcome := agent.NativeResult{Key: k, Turn: acpmeta.NativeTurn{InvocationID: "P", Origin: acpmeta.NativeTurnOriginHost,
		Status: acpmeta.NativeTurnCompleted, ConsumedInputIDs: []string{"private-input"}}}
	if err := store.CommitNativeResult(ctx, outcome); err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmNativeResult(ctx, k, refs); !errors.Is(err, status_error.ChatLogNativeOutcomePending) {
		t.Fatalf("detached ACK before admission = %v", err)
	}
	input := agent.HostInput{HostInvocationID: "private-H", AdapterInputID: "private-input", Session: k.Session}
	if err := store.CommitDetachedInput(ctx, input); err != nil {
		t.Fatal(err)
	}
	store = openTestStore(t, root)
	if err := store.ConfirmNativeResult(ctx, k, refs); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "chat")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private admission created a public directory: %v", err)
	}
}

func TestBoundedHistoryLocatesCompletedTurnsAndExactUserMessage(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, t.TempDir())
	created := func(execution, user string) agent.ConversationChange {
		return agent.ConversationChange{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: execution,
			Data: json.RawMessage(`{"userMessageId":"` + user + `"}`)}}
	}
	terminal := func(execution string, kind constants.ChatEventType) agent.ConversationChange {
		return agent.ConversationChange{Fact: &agent.ChatFact{Type: kind, ExecutionID: execution}}
	}
	writeBatch(t, store, "history", 0, fact(constants.ChatEventTypeCreated, "", ""),
		fact(constants.ChatEventTypeUserMessageCreated, "u1", `{"content":"one"}`), created("e1", "u1"),
		terminal("e1", constants.ChatEventTypeExecutionCompleted),
		fact(constants.ChatEventTypeUserMessageCreated, "u2", `{"content":"two"}`), created("e2", "u2"),
		terminal("e2", constants.ChatEventTypeExecutionFailed),
		fact(constants.ChatEventTypeUserMessageCreated, "u3", `{"content":"three"}`), created("e3", "u3"),
		terminal("e3", constants.ChatEventTypeExecutionCompleted))
	got, before, err := store.ReadHistoryCandidates(ctx, agent.MessageQuery{ChatID: chatID, Limit: 1})
	if err != nil || len(got) != 1 || got[0].UserMessageID != "u3" || got[0].MessageID != "e3" || before != 9 {
		t.Fatalf("recent history = %+v, before %d, err %v", got, before, err)
	}
	old, before, err := store.ReadHistoryCandidates(ctx, agent.MessageQuery{ChatID: chatID, Limit: 1, Before: before})
	if err != nil || len(old) != 1 || old[0].UserMessageID != "u1" || old[0].MessageID != "e1" || before != 0 {
		t.Fatalf("older history = %+v, before %d, err %v", old, before, err)
	}
	input, err := store.ReadMessage(ctx, chatID, old[0].UserMessageID)
	if err != nil || input.Text != "one" || input.Role != constants.MessageRoleUser {
		t.Fatalf("exact history input = %+v, err %v", input, err)
	}
}

func TestNativeInputReverseLookupAfterIndexLoss(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := openTestStore(t, root)
	session := agent.NativeSessionKey{Provider: "codex", Subject: "account", SessionID: "native"}
	public := agent.HostInput{HostInvocationID: "public-H", AdapterInputID: "adapter-public", ExecutionID: "turn", Session: session}
	detached := agent.HostInput{HostInvocationID: "detached-H", AdapterInputID: "adapter-detached", Session: session}
	writeBatch(t, s, "gate", 0, fact(constants.ChatEventTypeCreated, "", ""),
		agent.ConversationChange{Binding: &agent.NativeBinding{Session: session}},
		agent.ConversationChange{Input: &public})
	if err := s.CommitDetachedInput(ctx, detached); err != nil {
		t.Fatal(err)
	}
	path, err := s.IndexPath(chatID)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	s = openTestStore(t, root)
	got, err := s.ReadNativeInputs(ctx, session, []string{"adapter-detached", "adapter-public"})
	if err != nil || len(got) != 2 || got[0].Input != detached || got[0].Ref.ChatID != "" ||
		got[1].Input != public || got[1].Ref.ChatID != chatID {
		t.Fatalf("recovered inputs: %+v, %v", got, err)
	}
	if v, err := s.ReadVersion(ctx, chatID); err != nil || v != 3 {
		t.Fatalf("version=%d, %v", v, err)
	}
	other := session
	other.Subject = "other-account"
	if _, err := s.ReadNativeInputs(ctx, other, []string{"adapter-public"}); !errors.Is(err, status_error.ChatLogNativeOutcomePending) {
		t.Fatalf("cross-account input lookup = %v", err)
	}
}

func TestPageUsesFinalCorrectionWithoutReadingSupersededBody(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := openTestStore(t, root)
	k := agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "codex", Subject: "account", SessionID: "native"}, InvocationID: "P"}
	former := strings.Repeat("obsolete", 4096)
	rng, err := s.CommitNativeFragments(ctx, agent.NativeFragmentBatch{Key: k, CommitKey: "stream", MessageID: "native-message",
		Changes: []agent.MessageChange{{Kind: agent.MessageAppend, Text: former}}})
	if err != nil {
		t.Fatal(err)
	}
	result := agent.NativeResult{Key: k, Turn: acpmeta.NativeTurn{InvocationID: "P", Origin: acpmeta.NativeTurnOriginProvider, Status: acpmeta.NativeTurnCompleted},
		FinalMessage: &agent.NativeFinalMessage{ID: "native-message", Text: "final correction"}}
	if err := s.CommitNativeResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	writeBatch(t, s, "chat", 0, fact(constants.ChatEventTypeCreated, "", ""),
		agent.ConversationChange{Message: &agent.MessageChange{MessageID: "a", Kind: agent.MessageNativeFragment, Fragments: &rng}},
		agent.ConversationChange{Message: &agent.MessageChange{MessageID: "a", Kind: agent.MessageNativeFinal, Result: &k}})
	_, path := s.nativeStream(k)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	index := bytes.Index(encoded, []byte(former))
	if index < 0 {
		t.Fatal("fixture did not locate original body")
	}
	// Keep the derived coverage stamp so selected-read checks distinguish which body is fetched.
	encoded[index] = '!'
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	page, err := s.ReadMessages(ctx, agent.MessageQuery{ChatID: chatID, Limit: 1})
	if err != nil || len(page.Messages) != 1 || page.Messages[0].Text != "final correction" {
		t.Fatalf("latest projection consulted superseded body: %+v, %v", page, err)
	}
	if _, err := s.ReadNativeFragments(ctx, rng); !errors.Is(err, status_error.ChatLogCorrupted) {
		t.Fatalf("direct historical read should detect fixture corruption, got %v", err)
	}
}

func TestLocatorQueriesSeekByMessageCursorAndAdapter(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, t.TempDir())
	writeBatch(t, s, "chat", 0, fact(constants.ChatEventTypeCreated, "", ""),
		fact(constants.ChatEventTypeUserMessageCreated, "u", "{}"))
	plans := []struct {
		// SQL has the predicate shape of the bounded query under test.
		sql string
		// want identifies the expected compound index.
		want string
	}{
		{"SELECT * FROM conversation_messages WHERE stream=1 AND cursor<100 ORDER BY cursor DESC LIMIT 21", "conversation_anchors_cursor"},
		{"SELECT * FROM conversation_messages WHERE stream=1 AND cursor<100 AND history=1 ORDER BY cursor DESC LIMIT 21", "conversation_anchors_history"},
		{"SELECT * FROM conversation_positions INDEXED BY conversation_row_message WHERE stream=1 AND message_ref<>0 AND NOT display_link AND message_ref=2 ORDER BY cursor", "conversation_row_message"},
		{"SELECT * FROM conversation_positions INDEXED BY conversation_row_execution WHERE stream=1 AND execution_ref<>0 AND execution_ref=5 ORDER BY cursor", "conversation_row_execution"},
		{"SELECT * FROM conversation_positions WHERE adapter IS NOT NULL AND adapter IN (x'01',x'02')", "conversation_row_adapter"},
		{"SELECT * FROM conversation_positions INDEXED BY conversation_row_native WHERE stream=1 AND native_ref<>0 AND native_ref=1 AND kind=9 AND lane_ref=2", "conversation_row_native"},
	}
	err := s.indexes.with(ctx, func(db *gorm.DB) error {
		for _, p := range plans {
			var rows []struct {
				// Detail is the SQLite planner's access-path description.
				Detail string
			}
			if err := db.Raw("EXPLAIN QUERY PLAN " + p.sql).Scan(&rows).Error; err != nil {
				return err
			}
			if len(rows) == 0 || !strings.Contains(rows[0].Detail, "SEARCH") || !strings.Contains(rows[0].Detail, p.want) {
				t.Fatalf("query does not seek %s: %+v", p.want, rows)
			}
			if p.want == "conversation_row_message" && !strings.Contains(rows[0].Detail, "message_ref=?") {
				t.Fatalf("message query must seek the exact key: %+v", rows)
			}
			if p.want == "conversation_row_execution" && !strings.Contains(rows[0].Detail, "execution_ref=?") {
				t.Fatalf("execution query must seek the exact key: %+v", rows)
			}
			if p.want == "conversation_row_native" && !strings.Contains(rows[0].Detail, "native_ref=?") {
				t.Fatalf("native query must seek the exact key: %+v", rows)
			}
			t.Log(rows[0].Detail)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestIndexWriteFailureAfterFsyncKeepsOriginalCommit(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := openTestStore(t, root)
	writeBatch(t, s, "create", 0, fact(constants.ChatEventTypeCreated, "", ""))
	if err := s.indexes.with(ctx, func(db *gorm.DB) error {
		return db.Exec("CREATE TRIGGER reject_index_insert BEFORE INSERT ON conversation_positions BEGIN SELECT RAISE(ABORT, 'synthetic index failure'); END").Error
	}); err != nil {
		t.Fatal(err)
	}
	receipt := writeBatch(t, s, "after-create", 1, fact(constants.ChatEventTypeUserMessageCreated, "u", "{\"content\":\"durable\"}"))
	if _, dirty := s.indexes.reverseState(); len(dirty) != 1 {
		t.Fatalf("fixture did not leave index dirty: %v", dirty)
	}
	if err := s.indexes.with(ctx, func(db *gorm.DB) error { return db.Exec("DROP TRIGGER reject_index_insert").Error }); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = openTestStore(t, root)
	page, err := s.ReadChanges(ctx, chatID, 0, 10)
	if err != nil || page.Version != 2 || len(page.Records) != 2 {
		t.Fatalf("recovered authority: %+v, %v", page, err)
	}
	retry := writeBatch(t, s, "after-create", 1, fact(constants.ChatEventTypeUserMessageCreated, "u", "{\"content\":\"durable\"}"))
	if retry != receipt {
		t.Fatalf("receipt after index failure %+v != %+v", retry, receipt)
	}
}

func TestReadExactMessageIncludesLateFileFact(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, t.TempDir())
	writeBatch(t, s, "create", 0, fact(constants.ChatEventTypeCreated, "", ""),
		agent.ConversationChange{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "execution"}},
		agent.ConversationChange{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "execution"}},
		fact(constants.ChatEventTypeUserMessageCreated, "newer", "{\"content\":\"newer\"}"))
	writeBatch(t, s, "late", 4, agent.ConversationChange{Fact: &agent.ChatFact{Type: constants.ChatEventTypeFileChanges,
		ExecutionID: "execution", MessageID: "execution", Data: json.RawMessage("{\"recordId\":\"ref\"}")}})
	m, err := s.ReadMessage(ctx, chatID, "execution")
	if err != nil || len(m.Legacy) != 3 || m.Legacy[2].Type != constants.ChatEventTypeFileChanges {
		t.Fatalf("exact late file projection %+v, %v", m, err)
	}
}

func TestBindingTracksCreateAndClearWithoutCreatingChatOnRead(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, t.TempDir())
	b, err := s.ReadBinding(ctx, chatID)
	if err != nil || b != nil {
		t.Fatalf("fresh Chat binding %+v, %v", b, err)
	}
	session := agent.NativeSessionKey{Provider: "codex", Subject: "account", SessionID: "native"}
	writeBatch(t, s, "create", 0, fact(constants.ChatEventTypeCreated, "", ""),
		agent.ConversationChange{Binding: &agent.NativeBinding{Session: session}})
	b, err = s.ReadBinding(ctx, chatID)
	if err != nil || b == nil || b.Session != session {
		t.Fatalf("bound Chat %+v, %v", b, err)
	}
	writeBatch(t, s, "clear", 2, agent.ConversationChange{Binding: &agent.NativeBinding{Session: session, Cleared: true}})
	b, err = s.ReadBinding(ctx, chatID)
	if err != nil || b != nil {
		t.Fatalf("cleared Chat binding %+v, %v", b, err)
	}
}

func TestHistoryUsesExplicitAssistantIdentityOnce(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, t.TempDir())
	writeBatch(t, s, "begin", 0, fact(constants.ChatEventTypeCreated, "", ""),
		fact(constants.ChatEventTypeUserMessageCreated, "user", "{\"content\":\"question\"}"),
		agent.ConversationChange{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCreated, MessageID: "reply", ExecutionID: "run",
			Data: json.RawMessage("{\"userMessageId\":\"user\"}")}})
	k := agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "codex", Subject: "account", SessionID: "native"}, InvocationID: "P"}
	r, err := s.CommitNativeFragments(ctx, agent.NativeFragmentBatch{Key: k, CommitKey: "fragment", MessageID: "native-reply", Changes: []agent.MessageChange{
		{Kind: agent.MessageAppend, Text: "answer"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	writeBatch(t, s, "finish", 3,
		agent.ConversationChange{Message: &agent.MessageChange{MessageID: "reply", ExecutionID: "run", Kind: agent.MessageNativeFragment, Fragments: &r}},
		agent.ConversationChange{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "run"}})
	candidates, _, err := s.ReadHistoryCandidates(ctx, agent.MessageQuery{ChatID: chatID, Limit: 10})
	if err != nil || len(candidates) != 1 || candidates[0].MessageID != "reply" || candidates[0].UserMessageID != "user" {
		t.Fatalf("history candidates %+v, %v", candidates, err)
	}
	m, err := s.ReadMessage(ctx, chatID, "reply")
	if err != nil || m.Text != "answer" || !m.Complete {
		t.Fatalf("assistant reply %+v, %v", m, err)
	}
}

func TestRevisionLiveLocatorUnlink(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	s := openTestStore(t, root)
	writeBatch(t, s, "create", 0, fact(constants.ChatEventTypeCreated, "", ""))
	path, _ := s.IndexPath(chatID)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	first := writeBatch(t, s, "next", 1, fact(constants.ChatEventTypeUserMessageCreated, "user", `{"content":"hello"}`))
	for range 2 {
		page, err := s.ReadMessages(ctx, agent.MessageQuery{ChatID: chatID, Limit: 1})
		t.Logf("same owner: page %+v error=%v", page, err)
		if err != nil || len(page.Messages) != 1 || page.Messages[0].Text != "hello" {
			t.Fatalf("same owner lost durable page: %+v %v", page, err)
		}
	}
	second := writeBatch(t, s, "next", 1, fact(constants.ChatEventTypeUserMessageCreated, "user", `{"content":"hello"}`))
	if first != second {
		t.Fatal(first, second)
	}
	t.Logf("same owner: authoritative receipt recovered %+v", second)
}

func TestRevisionRejectsDuplicateAdapterAdmission(t *testing.T) {
	ctx := t.Context()
	s := openTestStore(t, t.TempDir())
	sess := agent.NativeSessionKey{Provider: "codex", Subject: "account", SessionID: "s"}
	a := agent.HostInput{Session: sess, HostInvocationID: "H1", AdapterInputID: "input", ExecutionID: "e1"}
	b := agent.HostInput{Session: sess, HostInvocationID: "H2", AdapterInputID: "input", ExecutionID: "e2"}
	writeBatch(t, s, "gate1", 0, fact(constants.ChatEventTypeCreated, "", ""),
		agent.ConversationChange{Binding: &agent.NativeBinding{Session: sess}}, agent.ConversationChange{Input: &a})
	if _, err := s.Commit(ctx, agent.ConversationBatch{ChatID: chatID, Key: "gate2", ExpectedVersion: 3, Changes: []agent.ConversationChange{{Input: &b}}}); !errors.Is(err, status_error.ChatLogIdempotencyConflict) {
		t.Fatalf("duplicate admission wrote authority: %v", err)
	}
	got, err := s.ReadNativeInputs(ctx, sess, []string{"input"})
	t.Logf("durable adapter owner=%+v err=%v", got, err)
	if err != nil || len(got) != 1 || got[0].Input != a {
		t.Fatal(got, err)
	}
}

func TestRevisionNativeMessageScopedReplace(t *testing.T) {
	ctx := t.Context()
	s := openTestStore(t, t.TempDir())
	k := agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "codex", Subject: "account", SessionID: "s"}, InvocationID: "p"}
	receipt := writeBatch(t, s, "create", 0, fact(constants.ChatEventTypeCreated, "", ""))
	for i, part := range []struct {
		lane, text string
		kind       agent.MessageChangeKind
	}{
		{"native-a", "kept", agent.MessageAppend},
		{"native-b", "obsolete", agent.MessageAppend},
		{"native-b", "new", agent.MessageReplace},
	} {
		id := strconv.Itoa(i)
		rng, err := s.CommitNativeFragments(ctx, agent.NativeFragmentBatch{Key: k, CommitKey: id, MessageID: part.lane, Changes: []agent.MessageChange{{Kind: part.kind, Text: part.text}}})
		if err != nil {
			t.Fatal(err)
		}
		receipt = writeBatch(t, s, "ref"+id, receipt.Version, agent.ConversationChange{Message: &agent.MessageChange{MessageID: "assistant", Kind: agent.MessageNativeFragment, Fragments: &rng}})
	}
	page, err := s.ReadMessages(ctx, agent.MessageQuery{ChatID: chatID, Limit: 1})
	if err != nil || len(page.Messages) != 1 || page.Messages[0].Text != "keptnew" {
		t.Fatal(page, err)
	}

}

func TestRevisionSuffixCorrectionReusesPrefix(t *testing.T) {
	ctx := t.Context()
	s := openTestStore(t, t.TempDir())
	k := agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "codex", Subject: "account", SessionID: "s"}, InvocationID: "p"}
	prefix := strings.Repeat("durable-prefix!", 2048)
	_, err := s.CommitNativeFragments(ctx, agent.NativeFragmentBatch{Key: k, CommitKey: "prefix", MessageID: "native", Changes: []agent.MessageChange{{Kind: agent.MessageAppend, Text: prefix}}})
	if err != nil {
		t.Fatal(err)
	}
	_, path := s.nativeStream(k)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result := agent.NativeResult{Key: k, Turn: acpmeta.NativeTurn{InvocationID: "p", Origin: acpmeta.NativeTurnOriginProvider, Status: acpmeta.NativeTurnCompleted}, FinalMessage: &agent.NativeFinalMessage{ID: "native", Text: prefix + "!"}}
	if err := s.CommitNativeResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	copies := bytes.Count(after, []byte(prefix))
	if copies != 1 || len(after)-len(before) > 1024 {
		t.Fatal(copies, len(after)-len(before))
	}
	got, err := s.ReadNativeResult(ctx, k)
	if err != nil || got.FinalMessage == nil || got.FinalMessage.Text != prefix+"!" {
		t.Fatal(got, err)
	}
	writeBatch(t, s, "public", 0, fact(constants.ChatEventTypeCreated, "", ""),
		agent.ConversationChange{Message: &agent.MessageChange{MessageID: "answer", Kind: agent.MessageNativeFinal, Result: &k}})
	page, err := s.ReadMessages(ctx, agent.MessageQuery{ChatID: chatID, Limit: 1})
	if err != nil || len(page.Messages) != 1 || page.Messages[0].Text != prefix+"!" {
		t.Fatalf("public suffix reconstruction: %d messages, %v", len(page.Messages), err)
	}
	t.Logf("1 new byte, 30 KiB known prefix stored %d times, authority grew by %d bytes", copies, len(after)-len(before))
}

func TestRevisionLateClassificationReachesEarlierDurableFragments(t *testing.T) {
	ctx := t.Context()
	s := openTestStore(t, t.TempDir())
	key := agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "codex", Subject: "account", SessionID: "native"}, InvocationID: "P"}
	receipt := writeBatch(t, s, "create", 0, fact(constants.ChatEventTypeCreated, "", ""))
	appendRange := func(command, lane string, changes ...agent.MessageChange) agent.NativeFragmentRange {
		t.Helper()
		ref, err := s.CommitNativeFragments(ctx, agent.NativeFragmentBatch{Key: key, CommitKey: command, MessageID: lane, Changes: changes})
		if err != nil {
			t.Fatal(err)
		}
		receipt = writeBatch(t, s, "link-"+command, receipt.Version, agent.ConversationChange{Message: &agent.MessageChange{
			MessageID: "assistant", Kind: agent.MessageNativeFragment, Fragments: &ref,
		}})
		return ref
	}
	early := strings.Repeat("a", 8192)
	late := strings.Repeat("b", 3072)
	appendRange("early", "native-progress", agent.MessageChange{Kind: agent.MessageAppend, Text: early})
	before, err := s.ReadMessage(ctx, chatID, "assistant")
	if err != nil || before.Text != early {
		t.Fatalf("initial persisted text %+v %v", before, err)
	}
	ref := appendRange("late", "native-progress",
		agent.MessageChange{Kind: agent.MessageClassify, ContentType: constants.RuntimeEventTypeCommentaryDelta, Revision: 11},
		agent.MessageChange{Kind: agent.MessageAppend, Text: late})
	appendRange("answer", "native-answer", agent.MessageChange{Kind: agent.MessageAppend, Text: "actual answer"})
	replay, err := s.ReadNativeFragments(ctx, ref)
	if err != nil || len(replay) != 2 || replay[0].Kind != agent.MessageClassify {
		t.Fatalf("durable phase replay %+v %v", replay, err)
	}
	index, err := s.IndexPath(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	message, err := s.ReadMessage(ctx, chatID, "assistant")
	if err != nil || message.Text != "actual answer" || message.Revision != 11 || len(message.Parts) != 3 {
		t.Fatalf("classification after rebuilding %+v %v", message, err)
	}
	if message.Parts[0].Type != constants.RuntimeEventTypeCommentaryDelta || message.Parts[0].Text != early ||
		message.Parts[1].Type != constants.RuntimeEventTypeCommentaryDelta || message.Parts[1].Text != late {
		t.Fatal("late classification did not reach both durable pieces")
	}
	appendRange("correct-progress", "native-progress", agent.MessageChange{
		Kind: agent.MessageReplace, ContentType: constants.RuntimeEventTypeCommentaryDelta, Text: "authoritative commentary",
	})
	message, err = s.ReadMessage(ctx, chatID, "assistant")
	if err != nil || message.Text != "actual answer" || len(message.Parts) != 2 ||
		message.Parts[0].Type != constants.RuntimeEventTypeCommentaryDelta || message.Parts[0].Text != "authoritative commentary" {
		t.Fatalf("classified replacement did not clear the same lane: %+v %v", message, err)
	}
}

func TestRevisionProjectionDoesNotVisitSupersededRelationships(t *testing.T) {
	ctx := t.Context()
	s := openTestStore(t, t.TempDir())
	key := agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "codex", Subject: "account", SessionID: "native"}, InvocationID: "P"}
	receipt := writeBatch(t, s, "create", 0, fact(constants.ChatEventTypeCreated, "", ""))
	for j := 0; j < 64; j++ {
		id := strconv.Itoa(j)
		ref, err := s.CommitNativeFragments(ctx, agent.NativeFragmentBatch{Key: key, CommitKey: id, MessageID: "native",
			Changes: []agent.MessageChange{{Kind: agent.MessageReplace, Text: "old-" + id}}})
		if err != nil {
			t.Fatal(err)
		}
		receipt = writeBatch(t, s, "link-"+id, receipt.Version, agent.ConversationChange{Message: &agent.MessageChange{
			MessageID: "assistant", Kind: agent.MessageNativeFragment, Fragments: &ref,
		}})
	}
	if err := s.CommitNativeResult(ctx, agent.NativeResult{Key: key,
		Turn:         acpmeta.NativeTurn{InvocationID: "P", Origin: acpmeta.NativeTurnOriginProvider, Status: acpmeta.NativeTurnCompleted},
		FinalMessage: &agent.NativeFinalMessage{ID: "native", Text: "current"},
	}); err != nil {
		t.Fatal(err)
	}
	writeBatch(t, s, "finish", receipt.Version, agent.ConversationChange{Message: &agent.MessageChange{
		MessageID: "assistant", Kind: agent.MessageNativeFinal, Result: &key,
	}})
	assertCurrent := func() {
		t.Helper()
		message, err := s.ReadMessage(ctx, chatID, "assistant")
		if err != nil || message.Text != "current" || len(message.Parts) != 1 {
			t.Fatalf("message %+v, %v", message, err)
		}
		r, err := s.openChat(ctx, chatID)
		if err != nil {
			t.Fatal(err)
		}
		defer r.close()
		anchor, err := r.index.anchor("assistant")
		if err != nil {
			t.Fatal(err)
		}
		facts, err := r.index.forMessages([]int64{anchor.Ref})
		if err != nil || len(facts[anchor.Ref]) != 0 {
			t.Fatalf("page traverses old references: %d %v", len(facts[anchor.Ref]), err)
		}
		display, err := r.index.displayForMessages([]int64{anchor.Ref})
		if err != nil || len(display[anchor.Ref]) != 1 {
			t.Fatalf("effective projection rows: %d %v", len(display[anchor.Ref]), err)
		}
	}
	assertCurrent()
	index, err := s.IndexPath(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	assertCurrent()
	history, err := s.ReadChanges(ctx, chatID, 1, 100)
	if err != nil || len(history.Records) != 65 || history.Records[0].Change.Message.Fragments == nil {
		t.Fatalf("raw replay %d %v", len(history.Records), err)
	}
}

func TestRevisionAdapterFenceSerializesPublicAndDetachedAdmission(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	s := openTestStore(t, root)
	session := agent.NativeSessionKey{Provider: "codex", Subject: "account", SessionID: "native"}
	public := agent.HostInput{Session: session, HostInvocationID: "public", AdapterInputID: "same", ExecutionID: "execution"}
	detached := agent.HostInput{Session: session, HostInvocationID: "detached", AdapterInputID: "same"}
	publicBatch := agent.ConversationBatch{ChatID: chatID, Key: "public", Changes: []agent.ConversationChange{
		fact(constants.ChatEventTypeCreated, "", ""), {Binding: &agent.NativeBinding{Session: session}}, {Input: &public},
	}}
	results := make(chan error, 2)
	var group sync.WaitGroup
	group.Go(func() { _, err := s.Commit(ctx, publicBatch); results <- err })
	group.Go(func() { results <- s.CommitDetachedInput(ctx, detached) })
	group.Wait()
	close(results)
	var accepted, conflicts int
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, status_error.ChatLogIdempotencyConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || conflicts != 1 {
		t.Fatalf("accepted %d, conflicts %d", accepted, conflicts)
	}
	inputs, err := s.ReadNativeInputs(ctx, session, []string{"same"})
	if err != nil || len(inputs) != 1 {
		t.Fatalf("durable owner %+v, %v", inputs, err)
	}
	path, _ := s.IndexPath(chatID)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	otherID := "fedcba9876543210fedcba9876543210"
	public.HostInvocationID = "different-chat"
	publicBatch.ChatID = otherID
	if _, err := s.Commit(ctx, publicBatch); !errors.Is(err, status_error.ChatLogIdempotencyConflict) {
		t.Fatalf("lost fence after locator rebuild: %v", err)
	}
	if version, err := s.ReadVersion(ctx, otherID); err != nil || version != 0 {
		t.Fatalf("failed claim published Chat %d %v", version, err)
	}
}

func TestRevisionNativeLocatorsInternLongIdentity(t *testing.T) {
	ctx := t.Context()
	s := openTestStore(t, t.TempDir())
	k := agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "codex", Subject: strings.Repeat("s", 128), SessionID: strings.Repeat("n", 128)}, InvocationID: "P"}
	id := strings.Repeat("m", 128)
	for _, text := range []string{"one", "two", "three"} {
		if _, err := s.CommitNativeFragments(ctx, agent.NativeFragmentBatch{Key: k, CommitKey: text, MessageID: id,
			Changes: []agent.MessageChange{{Kind: agent.MessageAppend, Text: text}}}); err != nil {
			t.Fatal(err)
		}
	}
	err := s.indexes.with(ctx, func(db *gorm.DB) error {
		var rows []rowIndex
		if err := db.Where("native_ref <> 0").Find(&rows).Error; err != nil {
			return err
		}
		var identities []nativeIdentityLocator
		var messages []nativeMessageLocator
		if err := db.Find(&identities).Error; err != nil {
			return err
		}
		if err := db.Find(&messages).Error; err != nil {
			return err
		}
		if len(rows) != 3 || len(identities) != 1 || len(messages) != 1 || string(messages[0].Message) != id {
			t.Fatalf("positions=%d identities=%d messages=%+v", len(rows), len(identities), messages)
		}
		for _, row := range rows {
			if len(row.Native) > 0 || row.Message != "" || row.NativeRef != identities[0].Ref || row.LaneRef != messages[0].Ref {
				t.Fatalf("noncompact position %+v", row)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRevisionHandoffClaimRequiresExactDurableInput(t *testing.T) {
	ctx := t.Context()
	s := openTestStore(t, t.TempDir())
	session := agent.NativeSessionKey{Provider: "codex", Subject: "account", SessionID: "native"}
	binding := agent.NativeBinding{Session: session, Handoff: &agent.NativeHandoff{History: []agent.HandoffPair{{UserMessageID: "u", AssistantMessageID: "a"}}}}
	input := agent.HostInput{Session: session, HostInvocationID: "H", AdapterInputID: "adapter", ExecutionID: "execution"}
	writeBatch(t, s, "create", 0, fact(constants.ChatEventTypeCreated, "", ""), agent.ConversationChange{Binding: &binding}, agent.ConversationChange{Input: &input})
	ref := agent.HostInputRef{ChatID: chatID, HostInvocationID: "H"}
	got, relation, err := s.ReadHostInputConsumption(ctx, ref)
	if err != nil || relation != nil || got != input {
		t.Fatalf("unconsumed input %+v %+v %v", got, relation, err)
	}
	binding.Handoff.Claim = &agent.HandoffClaim{HostInvocationID: "H", AdapterInputID: "wrong"}
	invalid := agent.ConversationBatch{ChatID: chatID, Key: "claim", ExpectedVersion: 3, Changes: []agent.ConversationChange{{Binding: &binding}}}
	if _, err := s.Commit(ctx, invalid); !errors.Is(err, status_error.ChatLogRecordInvalid) {
		t.Fatalf("wrong binding claim accepted: %v", err)
	}
	binding.Handoff.Claim.AdapterInputID = "adapter"
	writeBatch(t, s, "claim", 3, agent.ConversationChange{Binding: &binding})
	gotBinding, err := s.ReadBinding(ctx, chatID)
	if err != nil || gotBinding == nil || gotBinding.Handoff == nil || gotBinding.Handoff.Claim == nil ||
		*gotBinding.Handoff.Claim != *binding.Handoff.Claim {
		t.Fatalf("binding claim %+v %v", gotBinding, err)
	}
}

func TestRevisionDelayedReferenceDoesNotResurrectSupersededBody(t *testing.T) {
	ctx := t.Context()
	s := openTestStore(t, t.TempDir())
	key := agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "codex", Subject: "account", SessionID: "native"}, InvocationID: "P"}
	commit := func(command, body string, kind agent.MessageChangeKind) agent.NativeFragmentRange {
		t.Helper()
		r, err := s.CommitNativeFragments(ctx, agent.NativeFragmentBatch{Key: key, CommitKey: command, MessageID: "lane",
			Changes: []agent.MessageChange{{Kind: kind, Text: body}}})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	old := commit("old", "old", agent.MessageAppend)
	replacement := commit("replace", "corrected", agent.MessageReplace)
	following := commit("following", " tail", agent.MessageAppend)
	link := func(r agent.NativeFragmentRange) agent.ConversationChange {
		return agent.ConversationChange{Message: &agent.MessageChange{MessageID: "a", Kind: agent.MessageNativeFragment, Fragments: &r}}
	}
	writeBatch(t, s, "first", 0, fact(constants.ChatEventTypeCreated, "", ""), link(replacement), link(following))
	writeBatch(t, s, "late", 3, link(old), link(replacement))
	got, err := s.ReadMessage(ctx, chatID, "a")
	if err != nil || got.Text != "corrected tail" {
		t.Fatalf("late link changed current version: %+v %v", got, err)
	}
}

func TestRevisionCommentaryReplacementPreservesFinalText(t *testing.T) {
	ctx := t.Context()
	s := openTestStore(t, t.TempDir())
	key := agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "codex", Subject: "account", SessionID: "native"}, InvocationID: "P"}
	receipt := writeBatch(t, s, "create", 0, fact(constants.ChatEventTypeCreated, "", ""))
	link := func(command, lane string, change agent.MessageChange) {
		t.Helper()
		ref, err := s.CommitNativeFragments(ctx, agent.NativeFragmentBatch{Key: key, CommitKey: command, MessageID: lane, Changes: []agent.MessageChange{change}})
		if err != nil {
			t.Fatal(err)
		}
		receipt = writeBatch(t, s, "link-"+command, receipt.Version, agent.ConversationChange{Message: &agent.MessageChange{
			MessageID: "assistant", Kind: agent.MessageNativeFragment, Fragments: &ref,
		}})
	}
	link("answer", "answer-lane", agent.MessageChange{Kind: agent.MessageAppend, Text: "final-already-here"})
	link("provisional", "lane", agent.MessageChange{Kind: agent.MessageAppend, Text: "provisional old"})
	link("reason", "lane", agent.MessageChange{Kind: agent.MessageAppend, ContentType: constants.RuntimeEventTypeThinkingDelta, Text: "retained reasoning"})
	link("old", "lane", agent.MessageChange{Kind: agent.MessageAppend, ContentType: constants.RuntimeEventTypeCommentaryDelta, Text: "old commentary"})
	link("other", "another-lane", agent.MessageChange{Kind: agent.MessageAppend, Text: " independent"})
	link("correct", "lane", agent.MessageChange{Kind: agent.MessageReplace, ContentType: constants.RuntimeEventTypeCommentaryDelta, Text: "corrected commentary"})
	page, err := s.ReadMessages(ctx, agent.MessageQuery{ChatID: chatID, Limit: 1})
	if err != nil || len(page.Messages) != 1 || page.Messages[0].Text != "final-already-here independent" ||
		len(page.Messages[0].Parts) != 4 || page.Messages[0].Parts[1].Text != "corrected commentary" || page.Messages[0].Parts[2].Text != "retained reasoning" {
		t.Fatalf("commentary replacement %+v %v", page, err)
	}
	link("empty", "lane", agent.MessageChange{Kind: agent.MessageReplace, ContentType: constants.RuntimeEventTypeCommentaryDelta})
	if err := s.CommitNativeResult(ctx, agent.NativeResult{Key: key, Turn: acpmeta.NativeTurn{InvocationID: "P", Origin: acpmeta.NativeTurnOriginProvider,
		Status: acpmeta.NativeTurnCompleted}, FinalMessage: &agent.NativeFinalMessage{ID: "answer-lane", Text: "final-already-here"}}); err != nil {
		t.Fatal(err)
	}
	_, path := s.nativeStream(key)
	data, err := os.ReadFile(path)
	if err != nil || bytes.Count(data, []byte("final-already-here")) != 1 {
		t.Fatalf("commentary changed final prefix reuse: %v", err)
	}
	writeBatch(t, s, "terminal", receipt.Version, agent.ConversationChange{Message: &agent.MessageChange{
		MessageID: "assistant", Kind: agent.MessageNativeFinal, Result: &key,
	}})
	index, err := s.IndexPath(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	page, err = s.ReadMessages(ctx, agent.MessageQuery{ChatID: chatID, Limit: 1})
	if err != nil || len(page.Messages) != 1 || page.Messages[0].Text != "final-already-here independent" ||
		len(page.Messages[0].Parts) != 4 || page.Messages[0].Parts[1].Type != constants.RuntimeEventTypeCommentaryDelta ||
		page.Messages[0].Parts[1].Text != "" || page.Messages[0].Parts[2].Text != "retained reasoning" {
		t.Fatalf("explicit empty commentary or exact terminal %+v %v", page, err)
	}
}
