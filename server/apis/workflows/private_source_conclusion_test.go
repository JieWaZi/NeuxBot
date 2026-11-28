package workflows

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
)

func TestWorkflowRepeatFromPrivateMainSourceDeliversOnlyToThatChat(t *testing.T) {
	ctx := t.Context()
	service, _ := openCoreWorkflowPorts(t, t.TempDir(), &fixtureRuntime{calls: map[string]int{}}, fixtureEnvironment{}, true)
	definition := savedTestWorkflow(t, service, humanSource)
	source, _, firstRunID := startWaitingWorkflowFromChat(t, service, definition, mainconversation.DefaultWorkScope)
	waitAndApprove := func(runID string) {
		t.Helper()
		state := awaitFixtureState(t, service, runID, func(state controllers.WorkflowRunState) bool {
			return state.Run.Status == constants.WorkflowRunStatusWaiting && len(state.Humans) == 1
		})
		if err := answerHumanFixture(ctx, workflowAPIDependencies(service), AnswerHumanCommand{
			RunID: runID, HumanID: state.Humans[0].ID, RequestID: "approve", Value: "approve",
		}); err != nil {
			t.Fatal(err)
		}
		awaitFixtureState(t, service, runID, func(state controllers.WorkflowRunState) bool {
			return state.Run.Status == constants.WorkflowRunStatusSucceeded
		})
	}
	waitAndApprove(firstRunID)
	if err := service.RetryConclusion(ctx, firstRunID); err != nil {
		t.Fatal(err)
	}
	main, err := service.main.Ensure(ctx, "waker", source.WakerID)
	if err != nil {
		t.Fatal(err)
	}
	const submissionID = "private-repeat"
	actual := models.Execution{ID: "private-execution", UserMessageID: "private-user", SubmissionID: submissionID, ChatID: source.ID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault, Status: constants.ExecutionStatusRunning}
	queue := controllers.NewMainConversationInputController(service.db)
	queued, err := queue.Insert(ctx, models.MainConversationInput{
		ConversationID: main.ID, WorkChatID: source.ID, PublicMessageID: mainconversation.UserMessageID(main.ID, submissionID),
		ExecutionID: actual.ID, SourceMessageID: actual.UserMessageID, PrivateSource: true, EntryPoint: execution.MainConversationSourceEntryPoint,
		Input: workinput.Input{SubmissionID: submissionID, Content: "Repeat the last workflow"}, RequestHash: "private-repeat",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.main.ResolveWorkflowOrigin(ctx, source.ID, actual.ID); !errors.Is(err, status_error.ChatReplyTargetUnavailable) {
		t.Fatalf("an uncommitted private input was accepted as workflow provenance: %v", err)
	}
	if claimed, err := queue.Claim(ctx, queued.Sequence); err != nil || !claimed {
		t.Fatalf("claim private input: %v %v", claimed, err)
	}
	if marked, err := queue.MarkCommitting(ctx, queued.Sequence); err != nil || !marked {
		t.Fatalf("mark private input: %v %v", marked, err)
	}
	encode := func(value any) json.RawMessage {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	accepted, err := service.Chats.CommitAcceptedTurn(ctx, chatservice.StartedTurn{Chat: source, Execution: actual, Facts: []eventlog.AppendInput{
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: actual.UserMessageID, Data: encode(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Content: "Repeat the last workflow"})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: actual.ID, Data: encode(eventlog.ExecutionCreatedData{UserMessageID: actual.UserMessageID, SubmissionID: submissionID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault})},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: actual.ID},
	}})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := service.BindWorkflowTurn(ctx, accepted.Chat, actual.ID, nil)
	if err != nil || bound.Capability == nil {
		t.Fatalf("bind private repeat: %+v %v", bound, err)
	}
	repeatID := ""
	for _, binding := range bound.Capability.Bindings {
		if binding.Operation != "workflow.runs.repeat" {
			continue
		}
		result, err := binding.Call(ctx, capability.Request{Operation: binding.Operation, RequestID: "repeat-private-flow", Params: map[string]any{"runId": firstRunID}})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := result.Data.(map[string]any)
		repeatID, _ = data["runId"].(string)
	}
	if repeatID == "" || repeatID == firstRunID {
		t.Fatalf("private repeat returned no independent run: %q", repeatID)
	}
	known, found, err := queue.Get(ctx, main.ID, queued.PublicMessageID)
	if err != nil || !found {
		t.Fatalf("read committed queue input: %v %v", found, err)
	}
	if settled, err := queue.Settle(ctx, known, constants.MainConversationInputAccepted, ""); err != nil || !settled {
		t.Fatalf("settle private continuation: %v %v", settled, err)
	}
	if conversationID, replyID, err := service.main.ResolveWorkflowOrigin(ctx, source.ID, actual.ID); err != nil || conversationID != "" || replyID != "" {
		t.Fatalf("private provenance changed after settlement: %q %q %v", conversationID, replyID, err)
	}
	waitAndApprove(repeatID)
	terminal, err := service.Chats.CommitTerminal(ctx, chatservice.TerminalFact{Execution: actual, Type: constants.ChatEventTypeExecutionCompleted, Status: constants.ExecutionStatusCompleted, Data: encode(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})})
	if err != nil {
		t.Fatal(err)
	}
	service.Chats.PublishTerminal(terminal)
	if err := service.RetryConclusion(ctx, repeatID); err != nil {
		t.Fatal(err)
	}
	if reports := workflowExecutionConclusions(t, service, source.ID, actual.ID, repeatID); len(reports) != 1 {
		t.Fatalf("private source received wrong conclusion count: %+v", reports)
	}
	messages, _, err := service.main.Messages(ctx, main.ID, 0, 10)
	if err != nil || len(messages) != 2 {
		t.Fatalf("private repeat published to main: %+v %v", messages, err)
	}
}
