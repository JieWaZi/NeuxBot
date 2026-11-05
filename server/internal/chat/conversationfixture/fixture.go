// Package conversationfixture writes test data through the current conversation authority.
package conversationfixture

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

func New(t testing.TB, root string) *conversation.Store {
	t.Helper()
	store, err := conversation.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

func Facts(inputs []eventlog.AppendInput) []agent.ConversationChange {
	changes := make([]agent.ConversationChange, len(inputs))
	for i, input := range inputs {
		changes[i].Fact = &agent.ChatFact{
			Type: input.Type, MessageID: input.MessageID, ExecutionID: input.ExecutionID,
			InvocationID: input.InvocationID, Data: input.Data,
		}
	}
	return changes
}

func CommitFacts(ctx context.Context, store *conversation.Store, chatID string, facts []eventlog.AppendInput) ([]eventlog.Record, error) {
	return Commit(ctx, store, chatID, Facts(facts))
}

func Commit(ctx context.Context, store *conversation.Store, chatID string, changes []agent.ConversationChange) ([]eventlog.Record, error) {
	version, err := store.ReadVersion(ctx, chatID)
	if err != nil {
		return nil, err
	}
	var records []eventlog.Record
	for len(changes) > 0 {
		n := min(len(changes), 64)
		batch := agent.ConversationBatch{ChatID: chatID, Key: util.New(), ExpectedVersion: version, Changes: changes[:n]}
		receipt, err := store.Commit(ctx, batch)
		if err != nil {
			return nil, err
		}
		for i, change := range batch.Changes {
			records = append(records, record(chatID, receipt.First+int64(i), receipt.RecordedAt, change))
		}
		version = receipt.Version
		changes = changes[n:]
	}
	return records, nil
}

func ReadFacts(ctx context.Context, store *conversation.Store, chatID string) ([]eventlog.Record, error) {
	var after int64
	var records []eventlog.Record
	for {
		page, err := store.ReadChanges(ctx, chatID, after, 128)
		if err != nil {
			return nil, err
		}
		for _, row := range page.Records {
			records = append(records, record(chatID, row.Cursor, row.RecordedAt, row.Change))
		}
		if page.Next == page.Version {
			return records, nil
		}
		after = page.Next
	}
}

func Records(chatID string, rows []agent.ConversationRecord) []eventlog.Record {
	records := make([]eventlog.Record, 0, len(rows))
	for _, row := range rows {
		records = append(records, record(chatID, row.Cursor, row.RecordedAt, row.Change))
	}
	return records
}

func NativeMessage(t testing.TB, store *conversation.Store, chatID, executionID string, parts ...agent.MessageChange) agent.ConversationChange {
	t.Helper()
	key := agent.NativeResultKey{
		Session:      agent.NativeSessionKey{Provider: "codex", Subject: "fixture", SessionID: chatID},
		InvocationID: executionID,
	}
	for i := range parts {
		parts[i].Revision = int64(i + 1)
	}
	positions, err := store.CommitNativeFragments(t.Context(), agent.NativeFragmentBatch{
		Key: key, CommitKey: util.New(), MessageID: executionID, Changes: parts,
	})
	if err != nil {
		t.Fatal(err)
	}
	return agent.ConversationChange{Message: &agent.MessageChange{
		MessageID: executionID, ExecutionID: executionID, Kind: agent.MessageNativeFragment,
		Fragments: &positions, Revision: int64(len(parts)),
	}}
}

func record(chatID string, sequence, at int64, change agent.ConversationChange) eventlog.Record {
	out := eventlog.Record{ChatID: chatID, Sequence: sequence, RecordedAt: at, SchemaVersion: constants.ChatLogSchemaVersion}
	if fact := change.Fact; fact != nil {
		out.Type, out.MessageID, out.ExecutionID, out.InvocationID, out.Data =
			fact.Type, fact.MessageID, fact.ExecutionID, fact.InvocationID, fact.Data
	}
	return out
}
