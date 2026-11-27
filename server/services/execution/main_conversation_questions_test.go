package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/cli"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestMainConversationQuestionSurvivesWaitTimeoutAndColdRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	hub := events.New()
	t.Cleanup(hub.Close)
	registry := capability.New(nil)
	t.Cleanup(registry.Close)
	endpoint := httptest.NewServer(registry)
	t.Cleanup(endpoint.Close)
	binary := filepath.Join(t.TempDir(), "botcli")
	if err := os.WriteFile(binary, []byte("fixture CLI"), 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := newMessageTestService(t, t.TempDir(), messageTestOptions{agent: registry, agentBinary: binary, events: hub})
	core := fixture.execution.(*service)
	core.agentEndpoint = endpoint.URL + constants.AgentCLIPath

	main := controllers.NewMainConversationController(fixture.db)
	transactions := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(fixture.db))
	channel := controllers.NewChannelController(fixture.db)
	conversation := mainconversation.New(main, transactions, fixture.wakers, channel)
	const actor = "waker-1"
	work, err := conversation.ReserveWork(ctx, "waker", actor, actor, "decision")
	if err != nil {
		t.Fatal(err)
	}
	foreign := models.Waker{ID: util.New(), Name: "其他伙伴"}
	if err := fixture.wakers.Create(ctx, foreign); err != nil {
		t.Fatal(err)
	}
	foreignMain, err := conversation.Ensure(ctx, "waker", foreign.ID)
	if err != nil {
		t.Fatal(err)
	}
	executionID := util.New()
	const input = "hold-for-stream 请选择交付形式"
	message, err := conversation.AdmitUser(ctx, "waker", actor, "report", input, "", work.WorkChatID, executionID, "", "", "", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := core.Start(ctx, WorkIntent{
		Chat:        ChatTarget{ChatID: work.WorkChatID, MainConversationSource: &CreateChatSpec{WakerID: actor, WorkingDirectory: t.TempDir()}},
		Input:       WorkInput{Actor: "user", Content: input, SubmissionID: message.ID},
		Options:     WorkOptions{RuntimeID: "codex", EntryPoint: MainConversationEntryPoint, PermissionLevel: constants.PermissionLevelAuto, ToolCapabilities: MainConversationTools(work.WorkChatID, executionID, message.ID, conversation)},
		ExecutionID: executionID, Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
	})
	if err != nil || accepted.Execution.ID != executionID {
		t.Fatalf("admit original execution: %+v %v", accepted, err)
	}
	waitNativeProductLive(t, ctx, fixture.chats.(chatservice.Module), accepted, "fixture partial")
	root := filepath.Dir(filepath.Dir(core.systemSkillDirectory))
	paths, err := filepath.Glob(filepath.Join(root, constants.AgentCLIContextDirectory, "*", "*", "connection.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("want one managed connection, got %v: %v", paths, err)
	}
	t.Setenv(constants.AgentCLIContextEnvironment, paths[0])
	call := func(input string, args ...string) capability.Response {
		t.Helper()
		var out, stderr bytes.Buffer
		if code := cli.Execute(ctx, args, cli.IO{In: strings.NewReader(input), Out: &out, Err: &stderr}); code != 0 {
			t.Fatalf("botcli %s exited %d: %s", strings.Join(args, " "), code, &stderr)
		}
		var response capability.Response
		if err := json.Unmarshal(out.Bytes(), &response); err != nil || !response.OK {
			t.Fatalf("botcli %s returned %s: %v", strings.Join(args, " "), &out, err)
		}
		return response
	}
	created := call(`{"message":"选择交付方式","requestedSchema":{"type":"object","properties":{"choice":{"type":"string","enum":["file","text"]}},"required":["choice"],"additionalProperties":false}}`,
		"interaction", "questions", "create", "--request-id", "format", "--input", "-")
	questionID := created.CommandID
	if questionID == "" || created.Data.(map[string]any)["questionId"] != questionID {
		t.Fatalf("missing question identity: %+v", created)
	}
	wait := func() map[string]any {
		t.Helper()
		return call("", "interaction", "questions", "wait", "--question-id", questionID, "--timeout", "25ms").Data.(map[string]any)
	}
	pending := wait()
	resource := pending["resource"].(map[string]any)
	if pending["completed"] != false || resource["questionId"] != questionID || resource["status"] != "pending" {
		t.Fatalf("wait timeout changed the open question: %+v", pending)
	}
	if guidance, _ := pending["nextAction"].(string); !strings.Contains(guidance, "interaction.questions.wait") || !strings.Contains(guidance, questionID) {
		t.Errorf("wait timeout omitted the instruction to continue waiting: %+v", pending)
	}

	// 冷读从同一个 SQLite 权威投影恢复，不依赖最初的工作 Chat 订阅。
	board := controllers.NewWorkBoardController(fixture.db)
	items, total, _, err := board.Attention(ctx, controllers.WorkBoardQuery{Limit: 1})
	if err != nil || total != 1 || len(items) != 1 || items[0].OwnerKind != constants.WorkOwnerKindMainConversation ||
		items[0].OwnerID != work.ConversationID || items[0].ChatID != work.WorkChatID || items[0].ExecutionID != executionID || items[0].RequestID != questionID {
		t.Fatalf("cold board read lost the original decision: %+v, total=%d, err=%v", items, total, err)
	}
	active, err := main.ActiveWork(ctx, work.ConversationID)
	if err != nil || len(active) != 1 || !active[0].WaitingElicitation || active[0].ExecutionID != executionID {
		t.Fatalf("cold main conversation read lost the wait: %+v %v", active, err)
	}
	if other, err := main.ActiveWork(ctx, foreignMain.ID); err != nil || len(other) != 0 {
		t.Fatalf("the pending decision reached another main conversation: %+v %v", other, err)
	}

	if err := conversation.AuthorizeWorkActor(ctx, accepted.Chat); err != nil {
		t.Fatal(err)
	}
	if err := core.ResolveElicitation(ctx, work.WorkChatID, questionID, map[string]any{"choice": "file"}, false); err != nil {
		t.Fatal(err)
	}
	answered := wait()
	resource = answered["resource"].(map[string]any)
	answer, _ := resource["content"].(map[string]any)
	if answered["completed"] != true || resource["questionId"] != questionID || resource["status"] != "accepted" || answer["choice"] != "file" {
		t.Fatalf("original execution did not get the user's answer: %+v", answered)
	}
	if _, exists := answered["nextAction"]; exists {
		t.Fatalf("an answered question still asked the execution to wait: %+v", answered)
	}
	if items, total, _, err := board.Attention(ctx, controllers.WorkBoardQuery{Limit: 1}); err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("answered decision remained pending: %+v, total=%d, err=%v", items, total, err)
	}
	publication := call("", "conversations", "messages", "post", "--request-id", "publish-choice", "--content", "交付形式："+answer["choice"].(string))
	public, _, err := conversation.Messages(ctx, work.ConversationID, 0, 10)
	if err != nil || len(public) != 2 || public[1].ID != publication.CommandID || public[1].Content != "交付形式：file" ||
		public[1].SourceExecutionID != executionID || public[1].ReplyToMessageID != message.ID {
		t.Fatalf("the original execution could not use the accepted choice: %+v %v", public, err)
	}
	if err := core.CancelExecutionAndWait(ctx, work.WorkChatID, executionID); err != nil {
		t.Fatal(err)
	}
}
