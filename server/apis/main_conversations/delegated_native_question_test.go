package main_conversations

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/apis/chats"
	"github.com/JieWaZi/NeuxBot/server/apis/work_board"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
	"github.com/JieWaZi/acp-go/pkg/codex/protocol"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

const delegatedNativeQuestionFixture = "NEUX_DELEGATED_NATIVE_QUESTION_FIXTURE"

func TestDelegatedNativeQuestionUsesOriginalSourceAndAttention(t *testing.T) {
	t.Setenv("NEUX_DELEGATION_FIXTURE", "1")
	t.Setenv(delegatedNativeQuestionFixture, "1")
	root := t.TempDir()
	configureNativeMainConversationAt(t, root, "", true)
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(root, "main-delivery.db")})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	previous := global.WorkBoardControllerManager
	global.WorkBoardControllerManager = controllers.NewWorkBoardController(database.DB)
	defer func() { global.WorkBoardControllerManager = previous }()
	ctx, cancel := context.WithTimeout(t.Context(), 18*time.Second)
	defer cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("PUT", "/", nil).WithContext(ctx)

	rootRun, err := global.ExecutionService.Start(ctx, execution.WorkIntent{
		ClientRequestID: "fixture-native-question-root", Chat: execution.ChatTarget{Create: &execution.CreateChatSpec{WakerID: "native-main-partner"}},
		Input: execution.WorkInput{Actor: "user", Content: "fixture delegate team"},
		Options: execution.WorkOptions{EntryPoint: execution.WorkbenchEntryPoint, RuntimeID: constants.RuntimeIDCodex,
			PermissionLevel: constants.PermissionLevelAuto}, Ticket: execution.WorkTicketInput{Start: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	const child = "fixture-c"
	owner, err := global.MainConversationService.Ensure(ctx, "waker", child)
	if err != nil {
		t.Fatal(err)
	}
	var question models.WorkBoardAttention
	waitMainQueueFixture(t, ctx, func() bool {
		items, count, _, err := global.WorkBoardControllerManager.Attention(ctx, controllers.WorkBoardQuery{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			return false
		}
		if count != 1 || len(items) != 1 {
			t.Fatalf("child question had competing or duplicated owners: %+v", items)
		}
		question = items[0]
		return true
	})
	if question.OwnerKind != constants.WorkOwnerKindMainConversation || question.OwnerID != owner.ID ||
		question.WakerID != child || question.ChatID == rootRun.Chat.ID || question.RequestID == "" ||
		question.ExecutionID == "" || question.ExecutionID == rootRun.Execution.ID || question.Kind != constants.WorkBoardRequestKindInput {
		t.Fatalf("work board lost the exact native child request: %+v", question)
	}
	ticketStore := controllers.NewWorkTicketController(database.DB)
	childTicket, err := ticketStore.Ticket(ctx, question.ExecutionID)
	if err != nil || childTicket.WorkChatID != question.ChatID || childTicket.CurrentExecutionID != question.ExecutionID ||
		childTicket.ParentTicketID == "" || childTicket.Generation != 1 {
		t.Fatalf("native question lost its executing child attempt: %+v, %v", childTicket, err)
	}
	parent, err := ticketStore.Ticket(ctx, childTicket.ParentTicketID)
	if err != nil || parent.OwnerKind != "team" || parent.OwnerID != "fixture-team-b" || parent.WorkChatID == "" {
		t.Fatalf("missing direct team parent: %+v, %v", parent, err)
	}
	source := GetMainConversationSource{SubjectType: "waker", SubjectID: child, WorkChatID: question.ChatID}
	page, err := source.Output(c)
	if err != nil {
		t.Fatal(err)
	}
	summary := page.(MainConversationSourceResponse).Chat
	if pending := summary.PendingElicitations; len(pending) != 1 || pending[0].ElicitationID != question.RequestID {
		t.Fatalf("source does not expose the exact question from the work board: %+v", pending)
	}
	target := work_board.GetWorkBoardLiveTarget{Operation: "request_opened", WorkKind: question.OwnerKind, WorkID: question.OwnerID,
		ChatID: question.ChatID, ExecutionID: question.ExecutionID, RequestKind: question.Kind, RequestID: question.RequestID}
	assertTarget := func(op work_board.GetWorkBoardLiveTarget, want bool) {
		t.Helper()
		result, err := op.Output(c)
		if err != nil {
			t.Fatal(err)
		}
		projection := result.(work_board.WorkBoardLiveTarget)
		if projection.Valid != want || want && (projection.ChatID != question.ChatID || projection.WakerID != child) {
			t.Fatalf("pending target = %+v, want valid=%v", projection, want)
		}
	}
	assertTarget(target, true)
	old := target
	old.ExecutionID = rootRun.Execution.ID
	assertTarget(old, false)

	validAnswer := chats.ResolveElicitationRequest{Content: map[string]any{"choice": "Verified"}}
	resolve := ResolveMainConversationSourceElicitation{SubjectType: "waker", SubjectID: child, WorkChatID: question.ChatID,
		ElicitationID: question.RequestID, Body: validAnswer}
	wrongOwner := resolve
	wrongOwner.SubjectType, wrongOwner.SubjectID = parent.OwnerKind, parent.OwnerID
	if _, err := wrongOwner.Output(c); !util.HasStatusCode(err, status_error.ChatNotFound) {
		t.Fatalf("another subject could answer the child's request: %v", err)
	}
	parentSource := wrongOwner
	parentSource.WorkChatID = parent.WorkChatID
	if _, err := parentSource.Output(c); !util.HasStatusCode(err, status_error.ElicitationAlreadyResolved) {
		t.Fatalf("direct parent's legitimate source could answer a request originating in its child's Chat: %v", err)
	}
	ordinary := chats.ResolveElicitation{ChatID: question.ChatID, ElicitationID: question.RequestID, Body: validAnswer}
	if _, err := ordinary.Output(c); !util.HasStatusCode(err, status_error.AgentOperationForbidden) {
		t.Fatalf("ordinary Chat endpoint could answer a hidden source request: %v", err)
	}
	invalid := resolve
	invalid.Body = chats.ResolveElicitationRequest{Content: map[string]any{"choice": "Invalid"}}
	if _, err := invalid.Output(c); !util.HasStatusCode(err, status_error.BadRequest) {
		t.Fatalf("invalid native form value was not refused: %v", err)
	}
	assertTarget(target, true)
	if _, err := resolve.Output(c); err != nil {
		t.Fatal(err)
	}
	assertTarget(target, false)
	if _, err := resolve.Output(c); !util.HasStatusCode(err, status_error.ElicitationAlreadyResolved) {
		t.Fatalf("settled native request accepted another answer: %v", err)
	}
	waitMainQueueFixture(t, ctx, func() bool {
		childRun, err := global.ChatService.ReadExecution(ctx, question.ChatID, question.ExecutionID)
		if err != nil {
			return false
		}
		if childRun.Status == constants.ExecutionStatusFailed || childRun.Status == constants.ExecutionStatusCanceled {
			t.Fatalf("native model did not receive the answer in the original child turn: %+v", childRun)
		}
		return childRun.Status == constants.ExecutionStatusCompleted
	})
	waitMainQueueFixture(t, ctx, func() bool {
		root, err := global.ChatService.GetChatSummary(ctx, rootRun.Chat.ID)
		if err != nil || root.Chat.ActiveExecutionID != "" || root.Chat.LastEndedExecutionID == "" || root.Chat.LastEndedExecutionID == rootRun.Execution.ID {
			return false
		}
		records, err := global.ChatService.ReadExecutionRecords(rootRun.Chat.ID, root.Chat.LastEndedExecutionID)
		if err != nil {
			return false
		}
		reports, err := workticket.OrdinaryWorkReportReceipts(records, root.Chat.LastEndedExecutionID)
		if err != nil || len(reports) != 1 || reports[0].Content != "A received B's verified result." {
			t.Fatalf("wrong root result after the user answered C: %+v, %v", reports, err)
		}
		return true
	})
}

func requireDelegatedNativeQuestionAnswer(t *testing.T, read *bufio.Scanner, write *json.Encoder, threadID, turnID string) {
	t.Helper()
	const rpcID = "delegated-question-rpc"
	params := protocol.ToolRequestUserInputParams{
		ThreadID: threadID, TurnID: turnID, ItemID: turnID + "-native-question", IsBlocking: true,
		Questions: []protocol.ToolRequestUserInputQuestion{{
			ID: "choice", Header: "Evidence", Question: "How should C verify the evidence?",
			Options: []protocol.ToolRequestUserInputOption{{Label: "Verified"}, {Label: "Review"}},
		}},
	}
	if err := write.Encode(map[string]any{"id": rpcID, "method": protocol.MethodToolRequestUserInput, "params": params}); err != nil {
		os.Exit(4)
	}
	for read.Scan() {
		var response struct {
			// ID is the native server request identifier echoed by the client.
			ID json.RawMessage `json:"id"`
			// Result carries the native model's accepted structured answer.
			Result protocol.ToolRequestUserInputResponse `json:"result"`
		}
		if err := json.Unmarshal(read.Bytes(), &response); err != nil {
			os.Exit(4)
		}
		if string(response.ID) != `"`+rpcID+`"` {
			continue
		}
		if !slices.Equal(response.Result.Answers["choice"].Answers, []string{"Verified"}) {
			os.Exit(4)
		}
		return
	}
	os.Exit(4)
}
