package execution_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/cli"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// TestBoundConversationCLICommitsIndependentMessagesAndReplaysTheirReceipts 通过真实 CLI、执行授权及数据库核验发言、重投与跨来源公开历史。
func TestBoundConversationCLICommitsIndependentMessagesAndReplaysTheirReceipts(t *testing.T) {
	ctx := t.Context()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "main.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	wakers := controllers.NewWakerController(db.DB)
	if err := wakers.Create(ctx, models.Waker{ID: "p", Name: "partner"}); err != nil {
		t.Fatal(err)
	}
	publisher := mainconversation.New(controllers.NewMainConversationController(db.DB), controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)), wakers, controllers.NewChannelController(db.DB))
	work, err := publisher.ReserveWork(ctx, "waker", "p", "p", "project")
	if err != nil {
		t.Fatal(err)
	}
	inbound, err := publisher.AdmitUser(ctx, "waker", "p", "user-input", "请检查并报告结果。", "", work.WorkChatID, "", "", "", "", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	grants := execution.MainConversationTools(work.WorkChatID, "execution", inbound.ID, publisher)
	registry := capability.New(nil)
	t.Cleanup(registry.Close)
	grant, activate, finish, err := registry.Prepare(capability.GrantSpec{
		ExecutionID: "execution", Bindings: grants[0].Bindings,
		Validate: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	defer finish()
	server := httptest.NewServer(registry)
	t.Cleanup(server.Close)
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	contextFile, err := capability.WriteConnection(directory, "worker.json", capability.Connection{
		ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: server.URL + constants.AgentCLIPath,
		CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(constants.AgentCLIContextEnvironment, contextFile)
	run := func(wantError string, args ...string) capability.Response {
		t.Helper()
		var out, errOut bytes.Buffer
		code := cli.Execute(ctx, args, cli.IO{Out: &out, Err: &errOut})
		output := out.Bytes()
		if code != 0 {
			output = errOut.Bytes()
		}
		var result capability.Response
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("invalid CLI result: %s (%v)", output, err)
		}
		if wantError != "" {
			if result.Error == nil || result.Error.Code != wantError || code == 0 {
				t.Fatalf("wanted %s, got %s", wantError, output)
			}
		} else if code != 0 || !result.OK {
			t.Fatalf("CLI call failed: %s", output)
		}
		return result
	}
	post := func(key, content, wantError string) capability.Response {
		t.Helper()
		return run(wantError, "conversations", "messages", "post", "--request-id", key, "--content", content)
	}
	receipts := func(key, wantError string) capability.Response {
		t.Helper()
		return run(wantError, "receipts", "get", "--operation", constants.ConversationMessagePostOperation, "--request-id", key)
	}
	req := httptest.NewRequest(http.MethodPost, constants.AgentCLIPath+"/commands",
		strings.NewReader(`{"operation":"conversations.messages.post","requestId":"late-message","params":{"content":"来自旧执行"}}`))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer "+grant.Token)
	req.Header.Set(constants.AgentCLIExecutionHeader, "old-execution")
	response := httptest.NewRecorder()
	registry.ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("stale execution reached the publisher: %d %s", response.Code, response.Body)
	}
	receipts("update", "AgentReceiptNotFound")
	const update = "第一项已检查。\n\n正文第二段。"
	first := post("update", update, "")
	inProgress, _, err := publisher.Messages(ctx, work.ConversationID, 0, 10)
	if err != nil || len(inProgress) != 2 || inProgress[0].ID != inbound.ID || inProgress[1].ID != first.CommandID || inProgress[1].Content != update {
		t.Fatalf("first publication must be readable while the execution is active: %#v, %v", inProgress, err)
	}
	replay := post("update", update, "")
	receipt := receipts("update", "")
	if first.CommandID == "" || first.Replayed || !replay.Replayed || !receipt.Replayed || replay.CommandID != first.CommandID || receipt.CommandID != first.CommandID {
		t.Fatalf("inconsistent public receipts: %+v %+v %+v", first.Result, replay.Result, receipt.Result)
	}
	post("update", "另一段正文", "AgentCommandRequestConflict")
	const answer = "整理完成，详见交付内容。"
	last := post("reply", answer, "")
	messages, _, err := publisher.Messages(ctx, work.ConversationID, 0, 10)
	if err != nil || len(messages) != 3 {
		t.Fatalf("public messages: %#v, %v", messages, err)
	}
	expected := map[string]string{first.CommandID: update, last.CommandID: answer}
	for _, msg := range messages {
		if msg.ID == inbound.ID {
			continue
		}
		if msg.Content != expected[msg.ID] || msg.Role != "assistant" || msg.SourceChatID != work.WorkChatID || msg.SourceWakerID != "p" || msg.ReplyToMessageID != inbound.ID {
			t.Fatalf("wrong public message or origin: %+v", msg)
		}
		delete(expected, msg.ID)
	}
	if len(expected) != 0 {
		t.Fatalf("missing explicit publications: %v", expected)
	}
	otherScope, err := publisher.ReserveWork(ctx, "waker", "p", "p", "private-project-directory")
	if err != nil {
		t.Fatal(err)
	}
	otherInput, err := publisher.AdmitUser(ctx, "waker", "p", "second-project", "补充：另一个项目需要相同检查。", "", otherScope.WorkChatID, "", "", "", "", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := wakers.Create(ctx, models.Waker{ID: "other", Name: "other"}); err != nil {
		t.Fatal(err)
	}
	foreign, err := publisher.ReserveWork(ctx, "waker", "other", "other", "foreign-project")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.AdmitUser(ctx, "waker", "other", "another-person", "outside this conversation", "", foreign.WorkChatID, "", "", "", "", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	latest := run("", "conversations", "messages", "list", "--limit", "2").Data.(map[string]any)
	recent := latest["messages"].([]any)
	if len(recent) != 2 || recent[0].(map[string]any)["id"] != last.CommandID || recent[1].(map[string]any)["id"] != otherInput.ID || latest["hasMore"] != true {
		t.Fatalf("latest public history: %+v", latest)
	}
	cursor := strconv.FormatInt(int64(latest["nextBeforeSequence"].(float64)), 10)
	older := run("", "conversations", "messages", "list", "--before-sequence", cursor).Data.(map[string]any)
	prior := older["messages"].([]any)
	if len(prior) != 2 || prior[0].(map[string]any)["id"] != inbound.ID || prior[1].(map[string]any)["id"] != first.CommandID || older["hasMore"] != false {
		t.Fatalf("older public history: %+v", older)
	}
	for _, item := range append(prior, recent...) {
		for key := range item.(map[string]any) {
			switch key {
			case "id", "role", "content", "replyToMessageId", "createdAt":
			default:
				t.Errorf("private message metadata leaked in history: %s", key)
			}
		}
	}
	finish()
	post("late", "已经结束", "AgentContextUnauthorized")
}
