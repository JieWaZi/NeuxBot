package main_conversations

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/apis/chats"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/uploads"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

// capturedTurns 允许本测试在真实业务接纳边界上控制模型何时主动发言。
type capturedTurns struct {
	// Service 只为送达用例替换 Start。
	execution.Service
	// failedExecutions 保存指定来源回合的测试终态。
	failedExecutions map[string]models.Execution
	// failureReadError 模拟从失败候选的原工作日志读取终态时的错误。
	failureReadError error
	// turns 保存各次执行拿到的来源、输入及授权工具。
	turns []execution.WorkIntent
	// decisions 保存原协调器收到的真实用户决定。
	decisions []string
	// projectedInputs 保存来源已接纳的富文本输入快照。
	projectedInputs map[[2]string]*messageview.Message
	// projectedReads 记录主会话读取富文本时访问的来源和消息身份。
	projectedReads [][2]string
	// projectedError 替换一次读取假件的来源日志错误。
	projectedError error
	// admissionError 模拟已写入公开交办后、写入来源日志前发生的一次错误。
	admissionError error
}

func (c *capturedTurns) Start(ctx context.Context, intent execution.WorkIntent) (execution.AcceptedExecution, error) {
	err := intent.AdmissionBoundary.Commit(ctx, func(context.Context) error {
		failure := c.admissionError
		c.admissionError = nil
		return failure
	})
	if err != nil {
		return execution.AcceptedExecution{}, err
	}
	c.turns = append(c.turns, intent)
	return execution.AcceptedExecution{Execution: models.Execution{ID: intent.ExecutionID}}, nil
}

func (c *capturedTurns) ResolveApproval(_ context.Context, chatID, approvalID, optionID string) error {
	c.decisions = append(c.decisions, chatID+"/"+approvalID+"/"+optionID)
	return nil
}
func (c *capturedTurns) ResolveElicitation(_ context.Context, chatID, elicitationID string, content map[string]any, declined bool) error {
	if content["answer"] != "accepted" || declined {
		return status_error.BadRequest
	}
	c.decisions = append(c.decisions, chatID+"/"+elicitationID)
	return nil
}

type capturedReceipts struct {
	// Module 提供此用例所需的执行接纳回执查询。
	chat.Module
	// runner 保存已经派发的原始提交。
	runner *capturedTurns
}

func (c capturedReceipts) ReadSubmission(_ context.Context, chatID, requestID string) (*models.Execution, error) {
	for _, turn := range c.runner.turns {
		if turn.Chat.ChatID == chatID && turn.Input.SubmissionID == requestID {
			return &models.Execution{ID: turn.ExecutionID, ChatID: chatID, SubmissionID: requestID}, nil
		}
	}
	return nil, nil
}

func (c capturedReceipts) ReadExecutionOutcome(_ context.Context, chatID, executionID string) (chat.ExecutionOutcome, error) {
	if c.runner.failureReadError != nil {
		return chat.ExecutionOutcome{}, c.runner.failureReadError
	}
	row := c.runner.failedExecutions[executionID]
	if row.ChatID != chatID {
		return chat.ExecutionOutcome{}, status_error.ExecutionNotFound
	}
	return chat.ExecutionOutcome{Execution: row}, nil
}

func (c capturedReceipts) GetChatSummary(_ context.Context, id string) (chat.ChatSummary, error) {
	for _, turn := range c.runner.turns {
		if turn.Chat.ChatID == id && turn.Chat.MainConversationSource != nil {
			return chat.ChatSummary{Chat: models.Chat{ID: id, WakerID: turn.Chat.MainConversationSource.WakerID, HiddenFromWakerHistory: true}}, nil
		}
	}
	return chat.ChatSummary{}, status_error.ChatNotFound
}

func (c capturedReceipts) PublicReadingMessage(_ context.Context, chatID, messageID string) (*messageview.Message, error) {
	c.runner.projectedReads = append(c.runner.projectedReads, [2]string{chatID, messageID})
	if c.runner.projectedError != nil {
		return nil, c.runner.projectedError
	}
	input := c.runner.projectedInputs[[2]string{chatID, messageID}]
	if input == nil {
		return nil, nil
	}
	result := *input
	result.ID = messageID
	return &result, nil
}

func (c capturedReceipts) ReadResolvedExecutionQuestions(context.Context, string, []string) (map[string][]messageview.MessageQuestion, error) {
	return nil, nil
}

func TestSendMainConversationMessagePublishesAndRepliesInOriginalProject(t *testing.T) {
	ctx := context.Background()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "core.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	oldMain, oldWakers, oldTeams, oldProjects := global.MainConversationService, global.WakerControllerManager, global.ChannelControllerManager, global.ProjectControllerManager
	oldMainStore := global.MainConversationControllerManager
	oldTransactions := global.TransactionControllerManager
	oldRuntimes, oldExecution, oldChats := global.RuntimeEnvironmentControllerManager, global.ExecutionService, global.ChatService
	t.Cleanup(func() {
		global.TransactionControllerManager = oldTransactions
		global.MainConversationControllerManager = oldMainStore
		global.MainConversationService, global.WakerControllerManager, global.ChannelControllerManager, global.ProjectControllerManager = oldMain, oldWakers, oldTeams, oldProjects
		global.RuntimeEnvironmentControllerManager, global.ExecutionService, global.ChatService = oldRuntimes, oldExecution, oldChats
	})
	wakers, teams := controllers.NewWakerController(db.DB), controllers.NewChannelController(db.DB)
	global.WakerControllerManager, global.ChannelControllerManager = wakers, teams
	global.ProjectControllerManager = controllers.NewProjectController(db.DB)
	global.RuntimeEnvironmentControllerManager = controllers.NewRuntimeEnvironmentController(db.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB))
	global.MainConversationControllerManager = controllers.NewMainConversationController(db.DB)
	global.MainConversationService = mainconversation.New(global.MainConversationControllerManager, controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)), wakers, teams)
	runner := &capturedTurns{}
	global.ExecutionService, global.ChatService = runner, capturedReceipts{runner: runner}
	if err = wakers.Create(ctx, models.Waker{ID: "partner", Name: "伙伴"}); err != nil {
		t.Fatal(err)
	}
	if err = teams.Create(ctx, models.Channel{ID: "team", Name: "团队", LeadWakerID: "partner"}, []models.ChannelMember{{ChannelID: "team", WakerID: "partner", RuntimeID: "team-runtime", PermissionLevel: constants.PermissionLevelAuto}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err = global.ProjectControllerManager.Create(ctx, models.Project{ID: id, Name: id, WakerID: "partner"}); err != nil {
			t.Fatal(err)
		}
	}
	if err = global.RuntimeEnvironmentControllerManager.Ensure(ctx, models.RuntimeEnvironment{ID: "runtime", RuntimeID: "partner-runtime", Enabled: true, IsDefault: true, DefaultPermissionLevel: constants.PermissionLevelAuto}); err != nil {
		t.Fatal(err)
	}

	t.Run("timeline and activity share one read snapshot", func(t *testing.T) {
		// 两次查询共用一个事务连接，期间新接纳的回合只能出现在下次 GET。
		var messages, activity gorm.ConnPool
		callback := db.DB.Callback().Query()
		const probe = "test:main_timeline_snapshot"
		if err := callback.After("gorm:query").Register(probe, func(q *gorm.DB) {
			switch q.Statement.Table {
			case (models.MainConversationMessage{}).TableName():
				messages = q.Statement.ConnPool
			case (models.Chat{}).TableName():
				activity = q.Statement.ConnPool
			}
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = callback.Remove(probe) })
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/api/v1/main-conversations/waker/partner", nil)
		if _, err := (&GetMainConversation{SubjectType: "waker", SubjectID: "partner"}).Output(c); err != nil {
			t.Fatal(err)
		}
		if _, inTransaction := messages.(gorm.TxCommitter); !inTransaction || messages != activity {
			t.Fatalf("timeline and activity must share one transaction, got %T and %T", messages, activity)
		}
	})

	invokeAs := func(kind, subject, auth string, body MainConversationInput) (MainConversationAcceptance, error) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/api/v1/main-conversations/"+kind+"/"+subject+"/messages", nil)
		c.Set("bot.auth.subject", auth)
		op := SendMainConversationMessage{SubjectType: kind, SubjectID: subject, Body: body}
		value, err := op.Output(c)
		if err != nil {
			return MainConversationAcceptance{}, err
		}
		return value.(MainConversationAcceptance), nil
	}
	invoke := func(kind, subject string, body MainConversationInput) (MainConversationAcceptance, error) {
		return invokeAs(kind, subject, "", body)
	}
	readMainPage := func(t *testing.T) MainConversationResponse {
		t.Helper()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/api/v1/main-conversations/waker/partner", nil)
		response, err := (&GetMainConversation{SubjectType: "waker", SubjectID: "partner"}).Output(c)
		if err != nil {
			t.Fatal(err)
		}
		return response.(MainConversationResponse)
	}
	a := MainConversationInput{Content: "检查项目 A", ProjectID: "a", SubmissionID: "a-first"}
	sent, err := invoke("waker", "partner", a)
	if err != nil {
		t.Fatal(err)
	}
	turn := runner.turns[0]
	if turn.Options.RuntimeID != "partner-runtime" || turn.Options.EntryPoint != execution.MainConversationEntryPoint || turn.Chat.MainConversationSource.ProjectID != "a" {
		t.Fatalf("first dispatch: %#v", turn)
	}
	var post capability.Binding
	for _, tool := range turn.Options.ToolCapabilities[0].Bindings {
		if tool.Operation == constants.ConversationMessagePostOperation {
			post = tool
		}
	}
	if post.Call == nil {
		t.Fatal("the worker has no publication grant")
	}
	publish := func(key, content string) string {
		res, err := post.Call(ctx, capability.Request{RequestID: key, Params: map[string]any{"content": content}})
		if err != nil {
			t.Fatal(err)
		}
		return res.CommandID
	}
	// 长前导空白之后的多字节正文应进入引用，超出摘录范围的内容只存在于原发言。
	excerpt := "发现二" + strings.Repeat("界", constants.ChatReplyExcerptLimit-3)
	longMessage := strings.Repeat("　", constants.ChatReplyExcerptLimit) + excerpt + "超出摘录"
	first, second := publish("one", "发现一"), publish("two", longMessage)
	if first == second || publish("one", "发现一") != first {
		t.Fatal("independent posts or replays lost their message identity")
	}
	b, err := invoke("waker", "partner", MainConversationInput{Content: "检查 B", ProjectID: "b", SubmissionID: "b-first"})
	if err != nil || sent.Message.SourceChatID == b.Message.SourceChatID || b.Message.ConversationID != sent.Message.ConversationID {
		t.Fatalf("project B: %#v, %v", b, err)
	}
	reply := MainConversationInput{Content: "继续第二项", ReplyToMessageID: second, SubmissionID: "quoted"}
	quoted, err := invoke("waker", "partner", reply)
	if err != nil || quoted.Message.SourceChatID != sent.Message.SourceChatID || quoted.Message.ReplyToMessageID != second {
		t.Fatalf("quoted: %#v, %v", quoted, err)
	}
	if runner.turns[2].Input.Content != reply.Content || len(runner.turns[2].Input.ContextInstructions) != 1 || runner.turns[2].Chat.MainConversationSource != nil {
		t.Fatal("quote altered user input or project")
	}
	quote := runner.turns[2].Input.ContextInstructions[0].Content
	if !strings.Contains(quote, excerpt) || strings.Contains(quote, "超出摘录") || !strings.Contains(quote, `"truncated":true`) || !strings.Contains(quote, `"role":"assistant"`) {
		t.Fatal("CLI received the wrong quoted input or project")
	}
	replayed, err := invoke("waker", "partner", a)
	if err != nil || replayed.ExecutionID != sent.ExecutionID || len(runner.turns) != 3 {
		t.Fatalf("duplicate admission: %#v, %v", replayed, err)
	}
	changed := a
	changed.ProjectID = "b"
	if _, err = invoke("waker", "partner", changed); !util.HasStatusCode(err, status_error.CreateRequestConflict) {
		t.Fatalf("changed project on replay: %v", err)
	}
	reply.ProjectID = "b"
	if _, err = invoke("waker", "partner", reply); !util.HasStatusCode(err, status_error.BadRequest) {
		t.Fatalf("quoted scope override: %v", err)
	}
	team, err := invoke("team", "team", MainConversationInput{Content: "团队任务", SubmissionID: "team-first"})
	if err != nil || team.Message.ConversationID == sent.Message.ConversationID || runner.turns[3].Options.RuntimeID != "team-runtime" {
		t.Fatalf("team dispatch: %#v %v", team, err)
	}
	if _, err = invoke("team", "team", MainConversationInput{Content: "其他会话", ReplyToMessageID: first, SubmissionID: "cross-subject"}); err == nil {
		t.Fatal("team accepted a quote belonging to the private partner conversation")
	}
	messages, _, err := global.MainConversationService.Messages(ctx, sent.Message.ConversationID, 0, 50)
	if err != nil || len(messages) != 5 || messages[0].ID != sent.Message.ID || messages[1].ID != first || messages[2].ID != second {
		t.Fatalf("public timeline: %#v, %v", messages, err)
	}
	// 已经接纳的原请求，即使原临时路径消失也可取回同一回执。
	tempSelection := filepath.Join(t.TempDir(), "selected")
	if err := os.Mkdir(tempSelection, 0700); err != nil {
		t.Fatal(err)
	}
	request := MainConversationInput{Content: "检查临时目录", SubmissionID: "once", WorkingDirectory: tempSelection}
	original, err := invoke("waker", "partner", request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(tempSelection); err != nil {
		t.Fatal(err)
	}
	again, err := invoke("waker", "partner", request)
	if err != nil || again.ExecutionID != original.ExecutionID {
		t.Fatalf("replay after deleting the selected path: %#v %v", again, err)
	}

	// 新交办交给现任负责人，引用旧交办由仍在队的原执行伙伴在原范围续办。
	if err := wakers.Create(ctx, models.Waker{ID: "new-lead", Name: "新负责人"}); err != nil {
		t.Fatal(err)
	}
	member := models.ChannelMember{ChannelID: "team", WakerID: "partner", RuntimeID: "team-runtime", PermissionLevel: constants.PermissionLevelAuto}
	lead := models.ChannelMember{ChannelID: "team", WakerID: "new-lead", RuntimeID: "new-runtime", PermissionLevel: constants.PermissionLevelAuto}
	edited := models.Channel{ID: "team", Name: "团队", LeadWakerID: lead.WakerID}
	if err := teams.Update(ctx, edited, []models.ChannelMember{lead, member}); err != nil {
		t.Fatal(err)
	}
	oldReply := MainConversationInput{Content: "继续原交办", SubmissionID: "old-lead", ReplyToMessageID: team.Message.ID}
	continued, err := invoke("team", "team", oldReply)
	if err != nil || continued.Message.SourceChatID != team.Message.SourceChatID || runner.turns[len(runner.turns)-1].Options.RuntimeID != member.RuntimeID {
		t.Fatalf("reply to an existing member: %#v %v", continued, err)
	}
	fresh, err := invoke("team", "team", MainConversationInput{Content: "新交办", SubmissionID: "new-lead"})
	if err != nil || fresh.Message.SourceChatID == team.Message.SourceChatID || runner.turns[len(runner.turns)-1].Options.RuntimeID != lead.RuntimeID {
		t.Fatalf("new team assignment: %#v %v", fresh, err)
	}
	t.Run("failed team assignment keeps the original member after a lead change", func(t *testing.T) {
		if err := db.DB.Create(&models.ChatReadableMessage{ChatID: team.Message.SourceChatID, MessageID: team.ExecutionID, ExecutionID: team.ExecutionID, Kind: "failure", OrderSequence: 100}).Error; err != nil {
			t.Fatal(err)
		}
		runner.failedExecutions = map[string]models.Execution{team.ExecutionID: {ID: team.ExecutionID, ChatID: team.Message.SourceChatID, SubmissionID: "team-first", Status: constants.ExecutionStatusFailed, ErrorCode: "RuntimeAuthRequired"}}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/api/v1/main-conversations/team/team", nil)
		value, err := (&GetMainConversation{SubjectType: "team", SubjectID: "team"}).Output(c)
		if err != nil {
			t.Fatal(err)
		}
		rows := value.(MainConversationResponse).Messages
		if len(rows) != 3 || rows[0].ID != team.Message.ID || rows[0].FailureCode != "RuntimeAuthRequired" || rows[0].SourceWakerID != member.WakerID || rows[0].Role != "user" {
			t.Fatalf("failed team assignment lost its actual partner: %+v", rows)
		}
		for _, row := range rows[1:] {
			if row.SourceWakerID != "" || row.FailureCode != "" {
				t.Fatalf("normal user input has a partner failure identity: %+v", row)
			}
		}
	})
	if err := teams.Update(ctx, edited, []models.ChannelMember{lead}); err != nil {
		t.Fatal(err)
	}
	oldReply.SubmissionID = "removed-member"
	if _, err := invoke("team", "team", oldReply); !util.HasStatusCode(err, status_error.ChatReplyTargetUnavailable) {
		t.Fatalf("removed member could accept new work: %v", err)
	}

	var rich MainConversationInput
	if err := json.Unmarshal([]byte(`{"submissionId":"full","message":{"text":"请检查文件","runtimeId":"custom","accountId":"account","model":"model","reasoning":"high","fast":true,"contextWindow":"long","permissionLevel":"auto","attachments":[{"sourcePath":"/private/review/file.md"}],"codeReferences":[{"path":"/private/review/go.mod","startLine":1,"endLine":2,"content":"module example"}],"skillIds":["s"],"pluginIds":["p"],"connectorIds":["c"],"workflowId":"flow"}}`), &rich); err != nil {
		t.Fatal(err)
	}
	full, err := invoke("waker", "partner", rich)
	if err != nil {
		t.Fatal(err)
	}
	actual := runner.turns[len(runner.turns)-1]
	if actual.Options.RuntimeID != "custom" || actual.Options.AccountID != "account" || actual.Options.Model != "model" || actual.Options.Fast == nil || !*actual.Options.Fast || actual.Options.ContextWindow != "long" || actual.Input.Content != "请检查文件" || actual.Input.Attachments[0].SourcePath != "/private/review/file.md" || actual.Input.Workflow.ID != "flow" || len(actual.Input.PluginIDs) != 1 || len(actual.Input.SkillIDs) != 1 || len(actual.Input.ConnectorIDs) != 1 || actual.Input.CodeReferences[0].Content != "module example" {
		t.Fatalf("partner selections lost: %+v", actual)
	}
	if !strings.Contains(full.Message.Content, "file.md") || !strings.Contains(full.Message.Content, "go.mod") || strings.Contains(full.Message.Content, "/private") || strings.Contains(full.Message.Content, "module example") {
		t.Fatalf("public file context: %s", full.Message.Content)
	}
	if again, err := invoke("waker", "partner", rich); err != nil || again.ExecutionID != full.ExecutionID {
		t.Fatalf("rich replay: %+v %v", again, err)
	}
	t.Run("the timeline adds only the original user rendering fields", func(t *testing.T) {
		key := [2]string{full.Message.SourceChatID, full.Message.SourceMessageID}
		if key[1] == "" || key[1] != actual.UserMessageID || key[1] == actual.ExecutionID {
			t.Fatalf("public row does not name the frozen input: %+v", full.Message)
		}
		runner.projectedInputs = map[[2]string]*messageview.Message{
			key: {
				Role:           constants.MessageRoleUser,
				Actor:          "user",
				Content:        "请检查文件",
				Attachments:    []eventlog.AttachmentReference{{Name: "file.md", Path: "attachments/file.md", Managed: true, MediaType: "text/markdown"}},
				CodeReferences: []eventlog.CodeReference{{Path: "go.mod", StartLine: 1, EndLine: 2, Content: "module example"}},
			},
		}
		t.Cleanup(func() { runner.projectedInputs = nil })
		encoded, err := json.Marshal(readMainPage(t))
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range []string{`"inputContent":"请检查文件"`, `"path":"attachments/file.md"`, `"content":"module example"`} {
			if !strings.Contains(string(encoded), value) {
				t.Fatalf("public timeline missing %s", value)
			}
		}
		if strings.Contains(string(encoded), "/private/review") || len(runner.projectedReads) != 1 || runner.projectedReads[0] != key {
			t.Fatalf("source reads or paths exceeded the rich admission: %v", runner.projectedReads)
		}
	})
	t.Run("an unavailable source leaves the public history readable", func(t *testing.T) {
		t.Cleanup(func() { runner.projectedError = nil })
		for _, unavailable := range []error{status_error.ChatNotFound, status_error.ChatLogIndexCorrupted} {
			runner.projectedError = unavailable
			page := readMainPage(t)
			var kept MainConversationTimelineMessage
			for _, row := range page.Messages {
				if row.ID == full.Message.ID {
					kept = row
				}
			}
			if kept.Content != full.Message.Content || kept.InputContent != nil || len(kept.Attachments) != 0 {
				t.Fatalf("unavailable input did not keep only its public summary: %+v", kept)
			}
		}
		runner.projectedError = status_error.DatabaseError
		if _, err := timelineMessage(ctx, full.Message); !util.HasStatusCode(err, status_error.DatabaseError) {
			t.Fatalf("a storage outage must not be treated as an unavailable source: %v", err)
		}
	})
	var changedRich MainConversationInput
	if err := json.Unmarshal([]byte(`{"submissionId":"full","message":{"text":"请检查文件","runtimeId":"custom","permissionLevel":"auto","model":"different"}}`), &changedRich); err != nil {
		t.Fatal(err)
	}
	if _, err := invoke("waker", "partner", changedRich); !util.HasStatusCode(err, status_error.CreateRequestConflict) {
		t.Fatalf("changed message options on replay: %v", err)
	}
	teamFull, err := invoke("team", "team", rich)
	if err != nil {
		t.Fatal(err)
	}
	teamTurn := runner.turns[len(runner.turns)-1]
	if teamTurn.Options.RuntimeID != lead.RuntimeID || teamTurn.Options.AccountID == "account" || teamTurn.Options.Model == "model" || teamTurn.Input.Attachments[0].SourcePath != "/private/review/file.md" || teamFull.Message.Content != full.Message.Content {
		t.Fatalf("team input: %+v", teamTurn)
	}

	beforeSource, _, _ := global.MainConversationService.Messages(ctx, full.Message.ConversationID, 0, 100)
	source := SendMainConversationSourceMessage{SubjectType: "waker", SubjectID: "partner", WorkChatID: full.Message.SourceChatID}
	if err := json.Unmarshal([]byte(`{"submissionId":"source-input","text":"work here","runtimeId":"source-runtime","permissionLevel":"auto","attachments":[{"sourcePath":"/some/other/second.png"}]}`), &source.Body); err != nil {
		t.Fatal(err)
	}
	sendSource := func() (interface{}, error) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/sources/"+source.WorkChatID+"/messages", nil)
		return source.Output(c)
	}
	runner.admissionError = status_error.ChatLogError
	if _, err := sendSource(); !util.HasStatusCode(err, status_error.ChatLogError) {
		t.Fatalf("source log failure should remain retryable: %v", err)
	}
	if pending, _, err := global.MainConversationService.Messages(ctx, full.Message.ConversationID, 0, 100); err != nil || len(pending) != len(beforeSource) {
		t.Fatalf("source input exposed before durable acceptance: %d %v", len(pending), err)
	}
	if _, err := sendSource(); err != nil {
		t.Fatal(err)
	}
	sourceTurn := runner.turns[len(runner.turns)-1]
	afterSource, _, _ := global.MainConversationService.Messages(ctx, full.Message.ConversationID, 0, 100)
	if len(afterSource) != len(beforeSource)+1 || afterSource[len(afterSource)-1].Content != "work here\n\nsecond.png" || afterSource[len(afterSource)-1].SourceChatID != source.WorkChatID || sourceTurn.Input.Content != "work here" || sourceTurn.Chat.ChatID != full.Message.SourceChatID || len(sourceTurn.Options.ToolCapabilities) != 1 || sourceTurn.Options.RuntimeID != "source-runtime" {
		t.Fatalf("source input did not reach the public timeline and its original work history: %+v", sourceTurn)
	}
	sourceRow := afterSource[len(afterSource)-1]
	if sourceRow.SourceMessageID == "" || sourceRow.SourceMessageID != sourceTurn.UserMessageID || sourceRow.SourceMessageID == actual.UserMessageID {
		t.Fatalf("source continuation must link its own input: %+v", sourceRow)
	}
	if _, err := sendSource(); err != nil {
		t.Fatal(err)
	}
	againRows, _, _ := global.MainConversationService.Messages(ctx, full.Message.ConversationID, 0, 100)
	if len(againRows) != len(afterSource) || againRows[len(againRows)-1].ID != sourceRow.ID {
		t.Fatal("source retry duplicated the published user message")
	}
	t.Run("active work identifies its public input and replies beyond the current page", func(t *testing.T) {
		if sourceRow.ID == sourceTurn.ExecutionID || sourceRow.SourceChatID != full.Message.SourceChatID {
			t.Fatalf("continuation must use its own execution on the same work: %+v", sourceRow)
		}
		chatStore := controllers.NewChatController(db.DB)
		if err := chatStore.Create(ctx, models.Chat{
			ID: sourceRow.SourceChatID, WakerID: "partner",
			HiddenFromWakerHistory: true, ActiveExecutionID: sourceTurn.ExecutionID,
		}); err != nil {
			t.Fatal(err)
		}
		readPage := func(before int64) MainConversationResponse {
			t.Helper()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/api/v1/main-conversations/waker/partner", nil)
			result, err := (&GetMainConversation{SubjectType: "waker", SubjectID: "partner", BeforeSequence: before, Limit: 1}).Output(c)
			if err != nil {
				t.Fatal(err)
			}
			return result.(MainConversationResponse)
		}
		checkActive := func(page MainConversationResponse, answered bool) {
			t.Helper()
			body, err := json.Marshal(page.ActiveWork)
			if err != nil {
				t.Fatal(err)
			}
			var active []map[string]any
			if err := json.Unmarshal(body, &active); err != nil {
				t.Fatal(err)
			}
			if len(active) != 1 || active[0]["workChatId"] != sourceRow.SourceChatID ||
				active[0]["executionId"] != sourceTurn.ExecutionID || active[0]["inputMessageId"] != sourceRow.ID ||
				active[0]["hasPublicReply"] != answered {
				t.Fatalf("active continuation has the wrong public facts: %s", body)
			}
		}

		// 即使旧轮发言后进入公开记录，也不能算作本轮的公开进展。
		stale, _, err := global.MainConversationService.PublishWithReceipt(ctx, sourceRow.SourceChatID, full.ExecutionID, "old-reply", "older turn", full.Message.ID)
		if err != nil {
			t.Fatal(err)
		}
		latest := readPage(0)
		if len(latest.Messages) != 1 || latest.Messages[0].ID != stale.ID {
			t.Fatalf("old reply should be the only row in the latest window: %+v", latest.Messages)
		}
		checkActive(latest, false)

		var postSource capability.Binding
		for _, binding := range sourceTurn.Options.ToolCapabilities[0].Bindings {
			if binding.Operation == constants.ConversationMessagePostOperation {
				postSource = binding
			}
		}
		if postSource.Call == nil {
			t.Fatal("source continuation has no publication grant")
		}
		var lastReplyID string
		for _, content := range []string{"first answer", "second answer"} {
			receipt, err := postSource.Call(ctx, capability.Request{RequestID: content, Params: map[string]any{"content": content}})
			if err != nil {
				t.Fatal(err)
			}
			latest := readPage(0)
			if len(latest.Messages) != 1 || latest.Messages[0].ID != receipt.CommandID ||
				latest.Messages[0].ID == lastReplyID || latest.Messages[0].ReplyToMessageID != sourceRow.ID ||
				latest.Messages[0].SourceChatID != sourceRow.SourceChatID {
				t.Fatalf("source reply lost its own identity or input reference: %+v", latest.Messages)
			}
			lastReplyID = receipt.CommandID
			checkActive(latest, true)
		}
		another, err := invoke("waker", "partner", MainConversationInput{SubmissionID: "after-source-replies", Content: "different work", ProjectID: "b"})
		if err != nil {
			t.Fatal(err)
		}
		latest = readPage(0)
		if len(latest.Messages) != 1 || latest.Messages[0].ID != another.Message.ID {
			t.Fatalf("latest window should no longer include either reply: %+v", latest.Messages)
		}
		checkActive(latest, true)
		checkActive(readPage(sourceRow.Sequence), true)

		if err := chatStore.AdvanceProjection(ctx, sourceRow.SourceChatID, 0, map[string]any{
			"latest_sequence": int64(1), "active_execution_id": "",
		}); err != nil {
			t.Fatal(err)
		}
		if active := readPage(0).ActiveWork; len(active) != 0 {
			t.Fatalf("ended execution stayed active: %+v", active)
		}
	})
	source.Body.Text = "changed"
	if _, err := sendSource(); !util.HasStatusCode(err, status_error.CreateRequestConflict) {
		t.Fatalf("source replay changed the accepted public content: %v", err)
	}
	source.SubjectType = "team"
	source.SubjectID = "team"
	if _, err := sendSource(); !util.HasStatusCode(err, status_error.ChatNotFound) {
		t.Fatalf("foreign source accepted input: %v", err)
	}

	store, err := uploads.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldUploads := global.Uploads
	global.Uploads = store
	t.Cleanup(func() { global.Uploads = oldUploads; store.Close() })
	const browser = "web:test"
	upload, err := store.Stage(ctx, browser, "proof.txt", strings.NewReader("verified bytes"))
	if err != nil {
		t.Fatal(err)
	}
	fileInput := MainConversationInput{SubmissionID: "file-only", Message: &chats.MessageRequest{Text: "请查看附件。", AttachmentOnly: true, RuntimeID: "runtime", PermissionLevel: constants.PermissionLevelAuto, Attachments: []chats.MessageAttachment{{SourcePath: upload}}}}
	if _, err := invokeAs("waker", "partner", "web:other", fileInput); !util.HasStatusCode(err, status_error.BadRequest) {
		t.Fatalf("foreign upload accepted: %v", err)
	}
	runner.admissionError = status_error.ChatLogError
	if _, err := invokeAs("waker", "partner", browser, fileInput); !util.HasStatusCode(err, status_error.ChatLogError) {
		t.Fatalf("failed admission should be reported: %v", err)
	}
	pendingPage := readMainPage(t)
	fileID := mainconversation.UserMessageID(sent.Message.ConversationID, fileInput.SubmissionID)
	for _, row := range pendingPage.Messages {
		if row.ID == fileID {
			t.Fatalf("an unaccepted submission became visible: %+v", row)
		}
	}
	fileReceipt, err := invokeAs("waker", "partner", browser, fileInput)
	if err != nil {
		t.Fatal(err)
	}
	fileTurn := runner.turns[len(runner.turns)-1]
	fileMessage := fileReceipt.Message
	if fileMessage.Content != "proof.txt" || !fileTurn.Input.AttachmentOnly || fileMessage.ID != fileID || fileTurn.UserMessageID != fileMessage.SourceMessageID {
		t.Fatalf("attachment-only receipt: %+v", fileReceipt)
	}
	runner.projectedInputs = map[[2]string]*messageview.Message{
		{fileMessage.SourceChatID, fileMessage.SourceMessageID}: {
			Role: constants.MessageRoleUser, Actor: "user", AttachmentOnly: true,
			Attachments: []eventlog.AttachmentReference{{Name: "proof.txt", Path: "attachments/proof.txt", Managed: true}},
		},
		{sourceRow.SourceChatID, sourceRow.SourceMessageID}: {
			Role: constants.MessageRoleUser, Actor: "user", Content: "work here",
			Attachments: []eventlog.AttachmentReference{{Name: "second.png", Path: "attachments/second.png", MediaType: "image/png", Managed: true}},
		},
	}
	readyPage := readMainPage(t)
	file := readyPage.Messages[len(readyPage.Messages)-1]
	if len(readyPage.Messages) != len(pendingPage.Messages)+1 || file.ID != fileID || file.InputContent == nil || *file.InputContent != "" || !file.AttachmentOnly || len(file.Attachments) != 1 || !file.Attachments[0].Managed {
		t.Fatalf("replay did not hydrate the same user message: %+v", file)
	}
	var image MainConversationTimelineMessage
	for _, row := range readyPage.Messages {
		if row.ID == sourceRow.ID {
			image = row
			break
		}
	}
	if image.InputContent == nil || *image.InputContent != "work here" || len(image.Attachments) != 1 || image.Attachments[0].MediaType != "image/png" {
		t.Fatalf("wrong source image projection: %+v", image)
	}
	store.Close()
	if replay, err := invokeAs("waker", "partner", browser, fileInput); err != nil || replay.ExecutionID != fileReceipt.ExecutionID {
		t.Fatalf("lost replay after upload expiry: %+v %v", replay, err)
	}
	fileInput.Message.Attachments[0].SourcePath = "/local/proof.txt"
	if _, err := invokeAs("waker", "partner", browser, fileInput); !util.HasStatusCode(err, status_error.CreateRequestConflict) {
		t.Fatalf("different attachment with the same label was replayed: %v", err)
	}
	fileInput.SubmissionID = "local-file"
	if _, err := invokeAs("waker", "partner", browser, fileInput); !util.HasStatusCode(err, status_error.BadRequest) {
		t.Fatalf("browser opened a host file: %v", err)
	}

	t.Run("accepted failure survives reload and links the original assignment", func(t *testing.T) {
		failed, err := invoke("waker", "partner", MainConversationInput{SubmissionID: "failed-first", Content: "failed request", ProjectID: "a"})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.DB.Create(&models.ChatReadableMessage{ChatID: failed.Message.SourceChatID, MessageID: failed.ExecutionID, ExecutionID: failed.ExecutionID, Kind: "failure", OrderSequence: 100}).Error; err != nil {
			t.Fatal(err)
		}
		runner.failedExecutions = map[string]models.Execution{failed.ExecutionID: {ID: failed.ExecutionID, ChatID: failed.Message.SourceChatID, SubmissionID: "failed-first", Status: constants.ExecutionStatusFailed, ErrorCode: "RuntimeAuthRequired", ErrorMessage: "private native diagnostic"}}
		check := func(page MainConversationResponse, want string) {
			t.Helper()
			seen := 0
			for _, row := range page.Messages {
				data, _ := json.Marshal(row)
				if strings.Contains(string(data), "private native diagnostic") {
					t.Fatalf("private failure in public message %s", data)
				}
				var annotation map[string]any
				if err := json.Unmarshal(data, &annotation); err != nil {
					t.Fatal(err)
				}
				if row.ID == failed.Message.ID {
					seen++
					if row.Role != "user" || row.SourceChatID != failed.Message.SourceChatID || row.FailureCode != want {
						t.Fatalf("wrong failed input projection: %s", data)
					}
					if (want != "" && annotation["sourceWakerId"] != "partner") || (want == "" && annotation["sourceWakerId"] != nil) {
						t.Errorf("wrong partner identity for the failed input: %s", data)
					}
				} else if annotation["failureCode"] != nil {
					t.Fatalf("failure moved to different row %s", data)
				} else if row.Role == "user" && annotation["sourceWakerId"] != nil {
					t.Fatalf("normal user input has a partner failure identity: %s", data)
				}
			}
			if seen != 1 {
				t.Fatalf("expected exactly one original failed input, got %d", seen)
			}
		}
		check(readMainPage(t), "RuntimeAuthRequired")
		check(readMainPage(t), "RuntimeAuthRequired")
		terminal := runner.failedExecutions[failed.ExecutionID]
		unknown := terminal
		unknown.ErrorCode = "private native diagnostic"
		runner.failedExecutions[failed.ExecutionID] = unknown
		check(readMainPage(t), status_error.RuntimeExecutionFailed.Key())
		for _, status := range []constants.ExecutionStatus{constants.ExecutionStatusCompleted, constants.ExecutionStatusCanceled} {
			nonFailure := terminal
			nonFailure.Status = status
			runner.failedExecutions[failed.ExecutionID] = nonFailure
			check(readMainPage(t), "")
		}
		runner.failedExecutions[failed.ExecutionID] = terminal
		runner.failureReadError = status_error.ChatLogCorrupted
		check(readMainPage(t), "")
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/api/v1/main-conversations/waker/partner", nil)
		for _, code := range []status_error.StatusError{status_error.DatabaseError, status_error.OperationCanceled} {
			runner.failureReadError = code
			if _, err := (&GetMainConversation{SubjectType: "waker", SubjectID: "partner"}).Output(c); !util.HasStatusCode(err, code) {
				t.Fatalf("failure annotation swallowed %v: %v", code, err)
			}
		}
		runner.failureReadError = nil
		continued, err := invoke("waker", "partner", MainConversationInput{SubmissionID: "after-failure", Content: "continue", ReplyToMessageID: failed.Message.ID})
		if err != nil || continued.Message.SourceChatID != failed.Message.SourceChatID {
			t.Fatalf("continuation lost failed source: %+v %v", continued, err)
		}
		if _, _, err := global.MainConversationService.PublishWithReceipt(ctx, continued.Message.SourceChatID, continued.ExecutionID, "new-turn", "later turn", failed.Message.ID); err != nil {
			t.Fatal(err)
		}
		check(readMainPage(t), "RuntimeAuthRequired")
		if _, _, err := global.MainConversationService.PublishWithReceipt(ctx, failed.Message.SourceChatID, failed.ExecutionID, "published", "already answered", failed.Message.ID); err != nil {
			t.Fatal(err)
		}
		c.Request = httptest.NewRequest("GET", "/api/v1/main-conversations/waker/partner", nil)
		response, err := (&GetMainConversation{SubjectType: "waker", SubjectID: "partner", BeforeSequence: continued.Message.Sequence, Limit: 1}).Output(c)
		if err != nil {
			t.Fatal(err)
		}
		check(response.(MainConversationResponse), "")
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("PUT", "/sources/"+full.Message.SourceChatID+"/approvals/approve", nil)
	approval := ResolveMainConversationSourceApproval{SubjectType: "waker", SubjectID: "partner", WorkChatID: full.Message.SourceChatID, ApprovalID: "approve", Body: chats.ResolveApprovalRequest{OptionID: "allow_once"}}
	elicitation := ResolveMainConversationSourceElicitation{SubjectType: "waker", SubjectID: "partner", WorkChatID: full.Message.SourceChatID, ElicitationID: "question", Body: chats.ResolveElicitationRequest{Content: map[string]any{"answer": "accepted"}}}
	if _, err := approval.Output(c); err != nil {
		t.Fatal(err)
	}
	if _, err := elicitation.Output(c); err != nil {
		t.Fatal(err)
	}
	approval.SubjectType = "team"
	approval.SubjectID = "team"
	if _, err := approval.Output(c); !util.HasStatusCode(err, status_error.ChatNotFound) {
		t.Fatalf("foreign subject answered an approval: %v", err)
	}
	approval.WorkChatID = team.Message.SourceChatID
	if _, err := approval.Output(c); !util.HasStatusCode(err, status_error.AgentOperationForbidden) {
		t.Fatalf("former team member retained approval authority: %v", err)
	}
	direct := chats.ResolveApproval{ChatID: full.Message.SourceChatID, ApprovalID: "approve", Body: approval.Body}
	if _, err := direct.Output(c); !util.HasStatusCode(err, status_error.AgentOperationForbidden) {
		t.Fatalf("ordinary route answered a bound source: %v", err)
	}
	directInput := chats.ResolveElicitation{ChatID: full.Message.SourceChatID, ElicitationID: "question", Body: elicitation.Body}
	if _, err := directInput.Output(c); !util.HasStatusCode(err, status_error.AgentOperationForbidden) {
		t.Fatalf("ordinary route answered a bound source: %v", err)
	}
	if len(runner.decisions) != 2 || runner.decisions[0] != full.Message.SourceChatID+"/approve/allow_once" || runner.decisions[1] != full.Message.SourceChatID+"/question" {
		t.Fatalf("original interaction requests: %#v", runner.decisions)
	}

}
