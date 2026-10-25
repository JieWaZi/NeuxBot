package memory

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
)

func TestAgentMemoryReceiptsSurviveReopenForgetAndRejectChangedPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	service, err := NewService(ServiceConfig{DBPath: path})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	owner := newContext("partner", "project")
	bindings := map[string]agentcapability.Binding{}
	bind := func() {
		for _, value := range service.AgentBindings(owner) {
			bindings[value.Operation] = value
		}
	}
	bind()
	request := agentcapability.Request{Operation: "memory.entries.create", RequestID: "stable", Params: map[string]any{"scope": "partner", "content": "用户使用中文沟通"}}
	first, err := bindings[request.Operation].Call(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	id := first.Data.(map[string]any)["memoryId"].(string)
	service.Close()
	service, err = NewService(ServiceConfig{DBPath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	bind()
	replay, err := bindings[request.Operation].Call(ctx, request)
	if err != nil || !replay.Replayed || first.CommandID != replay.CommandID {
		t.Fatalf("durable replay = %+v %v", replay, err)
	}
	changed := request
	changed.Params = map[string]any{"scope": "partner", "content": "其他内容"}
	if _, err := bindings[request.Operation].Call(ctx, changed); !errors.Is(err, status_error.AgentCommandRequestConflict) {
		t.Fatalf("changed request accepted: %v", err)
	}
	forget := bindings["memory.entries.forget"]
	if _, err := forget.Call(ctx, agentcapability.Request{Operation: forget.Operation, RequestID: "forget", Params: map[string]any{"memoryId": id}}); err != nil {
		t.Fatal(err)
	}
	if _, err := bindings["memory.entries.get"].Call(ctx, agentcapability.Request{Params: map[string]any{"memoryId": id}}); err == nil {
		t.Fatal("forgotten memory remained readable")
	}
	if receipt, err := bindings[request.Operation].Receipt(ctx, "stable"); err != nil || receipt.CommandID != first.CommandID {
		t.Fatalf("forget destroyed receipt: %+v %v", receipt, err)
	}
	other := owner
	other.ChatID = "another-chat"
	for _, binding := range service.AgentBindings(other) {
		if binding.Operation == request.Operation {
			if _, err := binding.Receipt(ctx, "stable"); err == nil {
				t.Fatal("receipt leaked across chats")
			}
		}
	}
}

func TestAgentMemoryReceiptFailureRollsBackKnowledgeAndIndex(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	if _, err := service.store.db.Exec("CREATE TRIGGER reject_cli_receipt BEFORE INSERT ON memory_command_receipts BEGIN SELECT RAISE(ABORT, 'receipt denied'); END"); err != nil {
		t.Fatal(err)
	}
	var create agentcapability.Binding
	for _, binding := range service.AgentBindings(newContext("partner", "")) {
		if binding.Operation == "memory.entries.create" {
			create = binding
		}
	}
	if _, err := create.Call(context.Background(), agentcapability.Request{Operation: create.Operation, RequestID: "rollback", Params: map[string]any{"scope": "partner", "content": "这条事实必须回滚"}}); err == nil {
		t.Fatal("write succeeded without receipt")
	}
	var count int
	if err := service.store.db.QueryRow("SELECT COUNT(*) FROM memories").Scan(&count); err != nil || count != 0 {
		t.Fatalf("half-committed memory: %d %v", count, err)
	}
}
