package mainconversation

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestWorkflowOriginAndConclusionStayWithTheOriginalMainInput(t *testing.T) {
	ctx := t.Context()
	service, wakers, _, _, _ := newTestService(t)
	for _, id := range []string{"partner", "other"} {
		if err := wakers.Create(ctx, models.Waker{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	work, err := service.ReserveWork(ctx, "waker", "partner", "partner", "scope")
	if err != nil {
		t.Fatal(err)
	}
	other, err := service.Ensure(ctx, "waker", "other")
	if err != nil {
		t.Fatal(err)
	}
	input, err := service.AdmitUser(ctx, "waker", "partner", "submission", "Run the approval flow", "", work.WorkChatID, "execution", "message", "", "", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	conv, origin, err := service.ResolveWorkflowOrigin(ctx, work.WorkChatID, "execution")
	if err != nil || conv != work.ConversationID || origin != input.ID {
		t.Fatalf("original workflow address: %q %q %v", conv, origin, err)
	}
	if _, _, err := service.ResolveWorkflowOrigin(ctx, work.WorkChatID, "another-execution"); !errors.Is(err, status_error.ChatReplyTargetUnavailable) {
		t.Fatalf("wrong execution resolved the input: %v", err)
	}
	run := models.WorkflowRun{
		ID: "actual-run", DefinitionID: "definition", Status: constants.WorkflowRunStatusCancelled,
		ConclusionRequested: true, ConclusionMainID: conv, ConclusionReplyID: origin,
		SourceChatID: work.WorkChatID, SourceExecutionID: "execution", SourceWakerID: "partner",
	}
	for range 2 {
		if err := service.PublishWorkflowConclusion(ctx, run, "用户拒绝了审批。"); err != nil {
			t.Fatal(err)
		}
	}
	rows, _, err := service.Messages(ctx, conv, 0, 10)
	if err != nil || len(rows) != 2 || rows[1].ReplyToMessageID != input.ID || rows[1].WorkflowRun == nil || rows[1].WorkflowRun.RunID != "actual-run" || rows[1].Content != "用户拒绝了审批。" {
		t.Fatalf("incorrect main timeline: %+v %v", rows, err)
	}
	otherRows, _, err := service.Messages(ctx, other.ID, 0, 10)
	if err != nil || len(otherRows) != 0 {
		t.Fatalf("report sent to a different subject: %+v %v", otherRows, err)
	}
}
