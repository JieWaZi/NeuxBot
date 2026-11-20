package execution

import (
	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

func TestChatWorkflowSelectionPersistsThroughPublicStartAndContinuation(t *testing.T) {
	fixture := newMessageTestService(t, t.TempDir())
	ctx := t.Context()
	bound := map[string]string{}
	selection := workinput.WorkflowReference{ID: "review", RevisionID: "published-v1", Name: "评审"}
	fixture.execution.(*service).bindWorkflowTurn = func(_ context.Context, chat models.Chat, executionID string, ref *workinput.WorkflowReference) (WorkflowTurnBinding, error) {
		bound[executionID] = chat.ID
		if ref == nil {
			return WorkflowTurnBinding{}, nil
		}
		if ref.ID != selection.ID {
			t.Fatalf("wrong selected definition: %+v", ref)
		}
		return WorkflowTurnBinding{Reference: &selection, Instruction: "Use the selected review workflow."}, nil
	}
	intent := WorkIntent{
		ClientRequestID: "selection-only", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}},
		Input:   WorkInput{Actor: "user", Workflow: workinput.SelectWorkflow("review")},
		Options: WorkOptions{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
	}
	first, err := fixture.execution.Start(ctx, intent)
	if err != nil {
		t.Fatal("selection-only first send", err)
	}
	waitForChatTerminal(t, fixture, first.Chat.ID)
	second, err := fixture.execution.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", Content: "再执行一次"}, Options: intent.Options})
	if err != nil {
		t.Fatal("ordinary follow-up", err)
	}
	waitForChatTerminal(t, fixture, first.Chat.ID)
	history, err := fixture.chats.(chatservice.Module).ReadMessageHistory(ctx, first.Chat.ID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if first.Execution.ID == second.Execution.ID || len(bound) != 2 || bound[first.Execution.ID] != first.Chat.ID || bound[second.Execution.ID] != first.Chat.ID {
		t.Fatalf("incorrect turn binding: %+v", bound)
	}
	foundFirst, foundSecond := false, false
	for _, message := range history.Resources {
		switch message.ID {
		case first.Execution.UserMessageID:
			foundFirst = true
			if message.Workflow == nil || *message.Workflow != selection {
				t.Fatalf("lost frozen selection in message history: %+v", message.Workflow)
			}
		case second.Execution.UserMessageID:
			foundSecond = true
			if message.Workflow != nil || message.Content != "再执行一次" {
				t.Fatalf("ordinary follow-up inherited the earlier selection: %+v", message)
			}
		}
	}
	if !foundFirst || !foundSecond {
		t.Fatal("missing visible user messages", foundFirst, foundSecond)
	}
}
