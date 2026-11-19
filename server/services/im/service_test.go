package im

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	protocol "github.com/JieWaZi/NeuxBot/server/internal/im"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

// registrationTransport 隔离飞书 SDK 的真实 HTTP 边界，避免扫码回归访问公网。
type registrationTransport func(*http.Request) (*http.Response, error)

func (f registrationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestFeishuExistingConnectionCanRestartQRWithoutChangingIdentity(t *testing.T) {
	s, _, _ := newIMFixture(t)
	hub := events.New()
	defer hub.Close()
	s.config.Events = hub
	progressEvents := hub.Subscribe("im", events.Resource{Kind: "global"})
	defer progressEvents.Close()
	ctx := context.Background()
	if err := s.config.Store.SaveConnection(ctx, models.IMConnection{ID: "feishu", Platform: constants.IMPlatformFeishu, Name: "飞书", AppID: "cli_existing", OwnerID: "owner", Credential: "old-secret"}, 0); err != nil {
		t.Fatal(err)
	}
	previousClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: registrationTransport(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, err
		}
		response := `{"client_id":"cli_existing","client_secret":"new-secret","user_info":{"open_id":"owner","tenant_brand":"feishu"}}`
		if form.Get("action") == "begin" {
			response = `{"device_code":"device","verification_uri_complete":"https://example.invalid/qr","expire_in":600}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header)}, nil
	})}
	s.ctx, s.cancel = context.WithCancel(ctx)
	t.Cleanup(func() { s.Close(); http.DefaultClient = previousClient })
	login, err := s.BeginLogin(ctx, "feishu")
	if err != nil {
		t.Fatalf("已有飞书应用不能返回扫码接入: %v", err)
	}
	deadline := time.After(2 * time.Second)
	for {
		var progress LoginProgress
		select {
		case event := <-progressEvents.Events:
			if event.Type != "im.login.updated" || event.Resource.Kind != "login" || event.Resource.ID != login.ID {
				t.Fatalf("wrong login event identity: %+v", event.Resource)
			}
			if strings.Contains(string(event.Payload), "new-secret") || strings.Contains(string(event.Payload), "old-secret") {
				t.Fatal("login event leaked connection credentials")
			}
			var payload struct {
				// Login 是扫码过程的原公开状态，不包含 SDK 密钥。
				Login LoginProgress `json:"login"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			progress = payload.Login
		case <-deadline:
			t.Fatal("扫码过程缺少真实进度事件")
		}
		if progress.Status == "confirmed" {
			qr, err := url.Parse(progress.QRContent)
			if err != nil || qr.Query().Get("clientID") != "cli_existing" || qr.Query().Get("name") != "" || qr.Query().Get("desc") != "" {
				t.Fatalf("二维码未固定原应用: %q", progress.QRContent)
			}
			connection, err := s.config.Store.Connection(ctx, "feishu")
			if err != nil || connection.AppID != "cli_existing" || connection.OwnerID != "owner" || connection.Credential != "new-secret" {
				t.Fatalf("重扫未正确更新原连接: %+v, %v", connection, err)
			}
			return
		}
		if progress.Status == "failed" {
			t.Fatalf("扫码失败: %s", progress.ErrorCode)
		}
	}
}

// recordingPlatform 记录已派发片段，模拟平台缺少确定发送收据。
type recordingPlatform struct {
	// ids 是依次交给平台的稳定发送身份。
	ids []string
	// failAt 是应返回结果不明的调用序号，零表示成功。
	failAt int
	// afterSend 在片段返回收据前模拟授权配置变化。
	afterSend func()
}

// capturingTeams 记录 IM 首发是否使用冻结范围及干净的工作输入。
type capturingTeams struct {
	// Service 复用未参与此用例的团队端口。
	channel.Service
	// input 保存一次真实路由交给团队的提交。
	input channel.SubmitMessageInput
}

func (c *capturingTeams) SubmitMessage(_ context.Context, input channel.SubmitMessageInput) (channel.SubmitMessageResult, error) {
	c.input = input
	return channel.SubmitMessageResult{Thread: &channel.ThreadResult{Thread: models.ChannelThread{ID: "thread", LeadChatID: "chat"}}}, nil
}

func TestTeamManagedScopeAndCleanInputSurviveIMRouting(t *testing.T) {
	s, db, _ := newIMFixture(t)
	ctx := context.Background()
	s.config.Threads = controllers.NewChannelThreadController(db)
	teams := &capturingTeams{}
	s.config.Teams = teams
	binding, err := s.config.Store.Binding(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	binding.WakerID = ""
	binding.ChannelID = "team"
	if err := s.config.Store.SaveBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}
	input := models.IMInbox{ID: "message", BindingID: "b", ConnectionID: "c", Content: "你是谁", State: constants.IMInboxStatePending}
	if err := s.config.Store.SaveInbox(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := s.route(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).First(&input, "id = ?", input.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.accept(ctx, &input); err != nil {
		t.Fatal(err)
	}
	if teams.input.Create == nil || teams.input.Create.WorkingDirectory != "" {
		t.Fatal("IM must let the common team executor allocate its owned workspace")
	}
	work := teams.input.Create.Input
	if work.Content != "你是谁" || len(work.ContextInstructions) != 0 || work.IMReplyLinkID != input.LinkID {
		t.Fatalf("用户消息被执行说明污染: %+v", work)
	}
}

func (*recordingPlatform) Run(ctx context.Context, _ models.IMConnection, _ protocol.BatchCommit, _ protocol.StateChanged) error {
	<-ctx.Done()
	return nil
}
func (p *recordingPlatform) Send(_ context.Context, _ models.IMConnection, _ models.IMBinding, id, text string, _ *models.IMFile) (string, bool, error) {
	p.ids = append(p.ids, id)
	if p.afterSend != nil {
		p.afterSend()
	}
	if len(p.ids) == p.failAt {
		return "", false, status_error.IMPlatformUnavailable
	}
	return id, true, nil
}
func (*recordingPlatform) Download(context.Context, models.IMConnection, string, models.IMFile) ([]byte, error) {
	return []byte("attachment"), nil
}

func newIMFixture(t *testing.T) (*Service, *gorm.DB, *recordingPlatform) {
	t.Helper()
	directory := t.TempDir()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(directory, "im.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	hub := events.New()
	t.Cleanup(hub.Close)
	db := controllers.WithEventPublisher(database.DB, hub)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	platform := &recordingPlatform{}
	store := controllers.NewIMController(db)
	service := New(Config{Events: hub, Store: store, Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db)), DataDirectory: directory, Platforms: map[constants.IMPlatform]protocol.Platform{constants.IMPlatformWeixin: platform}})
	ctx := context.Background()
	if err := store.SaveConnection(ctx, models.IMConnection{ID: "c", Platform: constants.IMPlatformWeixin, Name: "测试微信", Enabled: true, Credential: "test-token", OwnerID: "owner", KeepAwake: true}, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.DiscoverBinding(ctx, models.IMBinding{ID: "b", ConnectionID: "c", ExternalChatID: "owner", Kind: constants.IMChatKindDirect, Name: "自己", State: constants.IMBindingStateApproved, WakerID: "waker", ContextToken: "context", AllowWorkflowDelivery: true}); err != nil {
		t.Fatal(err)
	}
	return service, db, platform
}

func TestIMInteractionCommandUsesQuotedConversation(t *testing.T) {
	for _, tc := range []struct {
		// name 标识当前命令的路由情形。
		name string
		// selectCommand 模拟回复前切换当前对话。
		selectCommand string
		// content 是收到的审批或问答命令。
		content string
		// quoteReceipt 表示命令引用平台已送达的交互消息。
		quoteReceipt bool
		// unresolvedQuote 是无法在当前聊天定位的外部引用。
		unresolvedQuote string
		// interactionID 是应提交到现有执行的交互标识。
		interactionID string
	}{
		{name: "quoted approval after use", selectCommand: "/use current-link", content: "/approve approval-id 1", quoteReceipt: true, interactionID: "approval-id"},
		{name: "quoted answer after new", selectCommand: "/new", content: `/answer question-id {"answer":"ready"}`, quoteReceipt: true, interactionID: "question-id"},
		{name: "unquoted approval", content: "/approve approval-id 1", interactionID: "approval-id"},
		{name: "unresolved quote", content: "/approve approval-id 1", unresolvedQuote: "unknown-receipt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, db, _ := newIMFixture(t)
			ctx := t.Context()
			original := models.IMConversationLink{ID: "original-link", BindingID: "b", ChatID: "original-chat"}
			current := models.IMConversationLink{ID: "current-link", BindingID: "b", ChatID: "current-chat"}
			for _, link := range []models.IMConversationLink{original, current} {
				if err := s.config.Store.SaveLink(ctx, link); err != nil {
					t.Fatal(err)
				}
			}
			s.config.Chats = quotedInteractionChats{replyProjectionChats{replyLinkID: original.ID}}
			executions := &quotedInteractionExecutions{}
			s.config.Executions = executions
			// 通过实际路由与 SQLite 收件身份保存命令。
			route := func(id, content, quote string) models.IMInbox {
				t.Helper()
				input := models.IMInbox{ID: id, ConnectionID: "c", BindingID: "b", ExternalMessageID: id, Content: content, QuoteMessageID: quote, State: constants.IMInboxStatePending}
				if err := s.config.Store.SaveInbox(ctx, input); err != nil {
					t.Fatal(err)
				}
				if err := s.route(ctx, input); err != nil {
					t.Fatal(err)
				}
				if err := db.First(&input, "id = ?", id).Error; err != nil {
					t.Fatal(err)
				}
				return input
			}
			route("use-original", "/use "+original.ID, "")
			quote := tc.unresolvedQuote
			if tc.quoteReceipt {
				delivery, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "original-interaction", TargetID: "b", LinkID: original.ID, ChatID: original.ChatID, ExecutionID: "active", Content: "原对话的交互消息"})
				if err != nil {
					t.Fatal(err)
				}
				if err := s.sendDelivery(ctx, delivery); err != nil {
					t.Fatal(err)
				}
				delivery, err = s.config.Store.Delivery(ctx, delivery.ID)
				if err != nil || len(delivery.Receipts) != 1 {
					t.Fatalf("缺少交互投递回执: %+v %v", delivery, err)
				}
				quote = delivery.Receipts[0]
			}
			if tc.selectCommand != "" {
				route("select-conversation", tc.selectCommand, "")
			}
			binding, err := s.config.Store.Binding(ctx, "b")
			if err != nil {
				t.Fatal(err)
			}
			input := route("interaction-command", tc.content, quote)
			if tc.interactionID == "" {
				if input.State != constants.IMInboxStateProcessed || input.LinkID != "" {
					t.Fatalf("无法定位的引用仍被路由到当前对话: %+v", input)
				}
				return
			}
			if input.State != constants.IMInboxStateRouted || input.LinkID != original.ID {
				t.Fatalf("命令未路由到交互所在的对话: %+v", input)
			}
			if err := s.accept(ctx, &input); err != nil {
				t.Fatal(err)
			}
			if executions.chatID != original.ChatID || executions.interactionID != tc.interactionID {
				t.Fatalf("交互提交给了错误的执行: %+v", executions)
			}
			after, err := s.config.Store.Binding(ctx, "b")
			if err != nil || after.ActiveLinkID != binding.ActiveLinkID {
				t.Fatalf("引用交互改变了当前对话: %+v %v", after, err)
			}
		})
	}
}

// quotedInteractionChats 提供引用对话中的活动审批和可通过 IM 回答的表单。
type quotedInteractionChats struct {
	// replyProjectionChats 提供已有交互与执行来源。
	replyProjectionChats
}

func (quotedInteractionChats) CountUserMessages(context.Context, string) (int64, error) {
	return 1, nil
}

func (c quotedInteractionChats) GetChatSummary(ctx context.Context, chatID string) (chat.ChatSummary, error) {
	summary, err := c.replyProjectionChats.GetChatSummary(ctx, chatID)
	summary.Chat.PendingElicitations[0].Mode = "form"
	return summary, err
}

// quotedInteractionExecutions 记录命令解析后实际送达的目标。
type quotedInteractionExecutions struct {
	// Service 复用该回归不触发的执行端口。
	execution.Service
	// chatID 是提交审批或回答的会话。
	chatID string
	// interactionID 是提交审批或回答的目标。
	interactionID string
}

func (e *quotedInteractionExecutions) ResolveApproval(_ context.Context, chatID, approvalID, _ string) error {
	e.chatID, e.interactionID = chatID, approvalID
	return nil
}

func (e *quotedInteractionExecutions) ResolveElicitation(_ context.Context, chatID, questionID string, _ map[string]any, _ bool) error {
	e.chatID, e.interactionID = chatID, questionID
	return nil
}

func TestIMResultProjectionWakesFromCommittedChatTerminal(t *testing.T) {
	s, db, _ := newIMFixture(t)
	attachConversationReaders(t, s, db)
	ctx := t.Context()
	s.ctx = ctx
	link := models.IMConversationLink{ID: "terminal-link", BindingID: "b", ChatID: protocol.StableID("chat", "committed-terminal")}
	conversation, err := createAdmissionChat(ctx, s.config.Chats, s.config.DataDirectory, link.ChatID, "waker")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.config.Store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	execution := models.Execution{ID: "terminal-execution", ChatID: link.ChatID, UserMessageID: "user", IMReplyLinkID: link.ID}
	created, err := json.Marshal(eventlog.ExecutionCreatedData{UserMessageID: execution.UserMessageID, IMReplyLinkID: link.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.config.Chats.CommitAcceptedTurn(ctx, chat.StartedTurn{
		Chat:      conversation,
		Execution: execution,
		Facts: []eventlog.AppendInput{
			{Type: constants.ChatEventTypeUserMessageCreated, MessageID: execution.UserMessageID, Data: json.RawMessage(`{"role":"user","content":"test"}`)},
			{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: execution.ID, Data: created},
			{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: execution.ID},
		},
	}); err != nil {
		t.Fatal(err)
	}
	projection := &resultProjection{hub: s.config.Events}
	projection.setActive(true)
	defer projection.close()
	s.projectResults(projection)
	if len(projection.pending) != 0 {
		t.Fatalf("启动对账未结束: %v", projection.pending)
	}
	commit, err := s.config.Chats.CommitTerminal(ctx, chat.TerminalFact{
		Execution: execution,
		Type:      constants.ChatEventTypeExecutionCanceled,
		Status:    constants.ExecutionStatusCanceled,
		Data:      json.RawMessage(`{"status":"canceled"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	s.config.Chats.PublishTerminal(commit)
	s.projectResults(projection)
	delivery, found, err := s.config.Store.FindDelivery(ctx, "result:"+link.ID+":"+execution.ID)
	if err != nil || !found || delivery.LinkID != link.ID || delivery.State != constants.IMDeliveryStatePending {
		t.Fatalf("已提交的 Chat 终态未触发对该 IM 会话的投递: %+v %v", delivery, err)
	}
}

func TestIMWithoutConnectionsSkipsOrdinaryChatProjection(t *testing.T) {
	s, db, _ := newIMFixture(t)
	ctx := t.Context()
	connection, err := s.config.Store.Connection(ctx, "c")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.config.Store.DeleteConnection(ctx, connection.ID, connection.Revision, ""); err != nil {
		t.Fatal(err)
	}
	linkQueries := 0
	if err := db.Callback().Query().After("gorm:query").Register("test:im-link-reads", func(query *gorm.DB) {
		if strings.Contains(query.Statement.SQL.String(), "im_conversation_links") {
			linkQueries++
		}
	}); err != nil {
		t.Fatal(err)
	}
	s.ctx = ctx
	projection := &resultProjection{hub: s.config.Events}
	defer projection.close()
	s.tick(projection)
	for _, id := range []string{"ordinary-chat-a", "ordinary-chat-b"} {
		emitProjectionChange(t, s, events.Resource{Kind: "chat", ID: id})
	}
	s.tick(projection)
	if linkQueries != 0 {
		t.Fatalf("ordinary chats triggered %d IM link queries without any connections", linkQueries)
	}
	if _, subscribed := projection.subscriptions["work-board"]; subscribed {
		t.Fatal("IM still subscribed to ordinary chat changes without any connections")
	}
}

func TestIMBatchesRelationshipLookupsForUnlinkedChats(t *testing.T) {
	s, db, _ := newIMFixture(t)
	ctx := t.Context()
	s.ctx = ctx
	projection := &resultProjection{hub: s.config.Events}
	defer projection.close()
	projection.setActive(true)
	s.projectResults(projection)
	linkQueries := 0
	if err := db.Callback().Query().After("gorm:query").Register("test:im-unlinked-chat-reads", func(query *gorm.DB) {
		if strings.Contains(query.Statement.SQL.String(), "im_conversation_links") {
			linkQueries++
		}
	}); err != nil {
		t.Fatal(err)
	}
	for i := range constants.IMPageSize {
		emitProjectionChange(t, s, events.Resource{Kind: "chat", ID: fmt.Sprintf("ordinary-chat-%d", i)})
	}
	emitProjectionChange(t, s, events.Resource{Kind: "chat", ID: "ordinary-chat-0"})
	s.projectResults(projection)
	if linkQueries != 1 {
		t.Fatalf("ordinary chats triggered %d IM link queries, want one batch lookup", linkQueries)
	}
}

func TestIMConversationHistoryRefreshesForMemberOfDisabledConnection(t *testing.T) {
	s, db, _ := newIMFixture(t)
	ctx := t.Context()
	s.ctx = ctx
	connection, err := s.config.Store.Connection(ctx, "c")
	if err != nil {
		t.Fatal(err)
	}
	connection.Enabled = false
	if err := s.config.Store.SaveConnection(ctx, connection, connection.Revision); err != nil {
		t.Fatal(err)
	}
	link := models.IMConversationLink{ID: "team-link", BindingID: "b", ChatID: "lead-chat", ThreadID: "thread"}
	if err := s.config.Store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	member := models.ChannelThreadMember{ThreadID: link.ThreadID, WakerID: "member", ChatID: "member-chat"}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	projection := &resultProjection{hub: s.config.Events}
	s.tick(projection)
	defer projection.close()
	subscription := s.config.Events.Subscribe("im", events.Resource{})
	defer subscription.Close()
	publishChatChange := func(chatID string) {
		t.Helper()
		source := events.Resource{Kind: "chat", ID: chatID}
		if err := events.Emit(s.config.Events, "work-board", "work-board.changed", source, source); err != nil {
			t.Fatal(err)
		}
	}
	publishChatChange("unrelated-chat")
	s.projectResults(projection)
	select {
	case change := <-subscription.Events:
		t.Fatalf("an unlinked chat invalidated IM history: %+v", change)
	default:
	}
	publishChatChange(member.ChatID)
	publishChatChange(member.ChatID)
	s.projectResults(projection)
	select {
	case change := <-subscription.Events:
		if change.Type != constants.IMConversationsChangedEventType || change.Resource != (events.Resource{Kind: "connection", ID: connection.ID}) {
			t.Fatalf("member change invalidated the wrong IM resource: %+v", change)
		}
	default:
		t.Fatal("member change did not invalidate its linked connection history")
	}
	select {
	case change := <-subscription.Events:
		t.Fatalf("duplicate member changes were not coalesced: %+v", change)
	default:
	}
}

func TestFrozenDeliveryUnknownRetryOnlyResendsUnconfirmedParts(t *testing.T) {
	s, _, platform := newIMFixture(t)
	ctx := context.Background()
	platform.failAt = 2
	delivery, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "request", TargetID: "b", Content: strings.Repeat("汉", 2000), ChatID: "chat", ExecutionID: "execution"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.sendDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	recorded, err := s.config.Store.Delivery(ctx, delivery.ID)
	if err != nil || recorded.State != constants.IMDeliveryStateUnknown || recorded.NextPart != 1 || len(recorded.Receipts) != 1 {
		t.Fatalf("unknown receipt: %+v %v", recorded, err)
	}
	if err := s.RetryDelivery(ctx, delivery.ID, false); !errors.Is(err, status_error.IMDeliveryRetryUnsafe) {
		t.Fatalf("unknown was silently resent: %v", err)
	}
	if err := s.RetryDelivery(ctx, delivery.ID, true); err != nil {
		t.Fatal(err)
	}
	recorded, _ = s.config.Store.Delivery(ctx, delivery.ID)
	if err := s.sendDelivery(ctx, recorded); err != nil {
		t.Fatal(err)
	}
	final, _ := s.config.Store.Delivery(ctx, delivery.ID)
	if final.State != constants.IMDeliveryStateSent || final.Attempts != 2 || len(platform.ids) != 3 || platform.ids[1] != platform.ids[2] || platform.ids[0] == platform.ids[2] {
		t.Fatalf("retry reran confirmed part: %+v %+v", final, platform.ids)
	}
	replay, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "request", TargetID: "b", Content: strings.Repeat("汉", 2000), ChatID: "chat", ExecutionID: "execution"})
	if err != nil || replay.ID != final.ID {
		t.Fatal("duplicate created new delivery")
	}
}

func TestFrozenFileSurvivesSourceChangeAndRejectsScopeEscape(t *testing.T) {
	s, _, _ := newIMFixture(t)
	ctx := context.Background()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "result.md")
	if err := os.WriteFile(source, []byte("first result"), 0600); err != nil {
		t.Fatal(err)
	}
	request := DeliveryRequest{RequestID: "file", TargetID: "b", Files: []string{source}, AllowedRoots: []string{root}}
	delivery, err := s.CreateDelivery(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("different result"), 0600); err != nil {
		t.Fatal(err)
	}
	replay, err := s.CreateDelivery(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(replay.Files[0].Path)
	if err != nil || string(data) != "first result" || replay.ID != delivery.ID {
		t.Fatalf("frozen file changed: %s %v", data, err)
	}
	request.Content = "changed"
	if _, err := s.CreateDelivery(ctx, request); !errors.Is(err, status_error.IMConfigurationConflict) {
		t.Fatalf("changed payload reused id: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "escape", TargetID: "b", Files: []string{link}, AllowedRoots: []string{root}}); !errors.Is(err, status_error.IMFileInvalid) {
		t.Fatalf("symlink escaped approved root: %v", err)
	}
}

// rejectInbox 在平台 ACK 边界注入一次持久化失败。
type rejectInbox struct {
	// IMController 保留真实 SQLite 事务和其他写入。
	controllers.IMController
}

func (rejectInbox) SaveInbox(context.Context, models.IMInbox) error {
	return status_error.IMServiceUnavailable
}

func TestReceiveCommitsCursorAndInputAtomicallyBeforeAcknowledgment(t *testing.T) {
	s, db, _ := newIMFixture(t)
	ctx := context.Background()
	connection, err := s.config.Store.Connection(ctx, "c")
	if err != nil {
		t.Fatal(err)
	}
	batch := []protocol.Message{{ID: "1", ChatID: "new-chat", SenderID: "member", Kind: constants.IMChatKindDirect, Content: "开始工作", ContextToken: "new-context", SentAt: time.Now().UnixMilli()}}
	real := s.config.Store
	s.config.Store = rejectInbox{IMController: real}
	if err := s.Receive(ctx, connection, batch, "next-cursor"); err == nil {
		t.Fatal("ACK boundary swallowed durability failure")
	}
	var count int64
	if err := db.Model(&models.IMBinding{}).Where("external_chat_id = ?", "new-chat").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("partial chat discovery survived rollback")
	}
	actual, _ := real.Connection(ctx, "c")
	if actual.Cursor != "" {
		t.Fatal("cursor advanced without input")
	}
	s.config.Store = real
	if err := s.Receive(ctx, connection, batch, "next-cursor"); err != nil {
		t.Fatal(err)
	}
	if err := s.Receive(ctx, connection, batch, "same-cursor"); err != nil {
		t.Fatal(err)
	}
	db.Model(&models.IMInbox{}).Count(&count)
	actual, _ = real.Connection(ctx, "c")
	if count != 1 || actual.Cursor != "same-cursor" {
		t.Fatalf("dedup/cursor: %d %s", count, actual.Cursor)
	}
}

func TestNewConversationFreezesQueuedWorkAndDurableInputDoesNotInheritScheduleExpiry(t *testing.T) {
	s, _, _ := newIMFixture(t)
	ctx := context.Background()
	input := models.IMInbox{ID: "first", ConnectionID: "c", BindingID: "b", Content: "原工作", ReceivedAt: time.Now().UnixMilli(), State: constants.IMInboxStatePending}
	if err := s.config.Store.SaveInbox(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := s.route(ctx, input); err != nil {
		t.Fatal(err)
	}
	binding, _ := s.config.Store.Binding(ctx, "b")
	original := binding.ActiveLinkID
	if original == "" {
		t.Fatal("task did not freeze a link")
	}
	link, _ := s.config.Store.Link(ctx, original)
	if link.ChatID != "" {
		t.Fatal("routing manufactured an empty internal Chat")
	}
	command := models.IMInbox{ID: "new", ConnectionID: "c", BindingID: "b", Content: "/new", ReceivedAt: time.Now().UnixMilli(), State: constants.IMInboxStatePending}
	if err := s.config.Store.SaveInbox(ctx, command); err != nil {
		t.Fatal(err)
	}
	if err := s.route(ctx, command); err != nil {
		t.Fatal(err)
	}
	binding, _ = s.config.Store.Binding(ctx, "b")
	queued, _ := s.config.Store.Inbox(ctx, constants.IMInboxStateRouted, 50)
	if binding.ActiveLinkID != "" || len(queued) != 1 || queued[0].LinkID != original {
		t.Fatal("new command rerouted existing work")
	}
	oldInput := models.IMInbox{ID: "old-input", ConnectionID: "c", BindingID: "b", Content: "旧工作", ReceivedAt: time.Now().Add(-time.Hour).UnixMilli(), State: constants.IMInboxStatePending}
	s.config.Store.SaveInbox(ctx, oldInput)
	if err := s.route(ctx, oldInput); err != nil {
		t.Fatal(err)
	}
	pending, _ := s.config.Store.Inbox(ctx, constants.IMInboxStatePending, 50)
	if len(pending) != 0 {
		t.Fatal("old input did not route")
	}
	routed, _ := s.config.Store.Inbox(ctx, constants.IMInboxStateRouted, 50)
	if len(routed) != 2 {
		t.Fatal("durable IM input inherited schedule expiry")
	}
}

func TestTargetsDoNotExposeOtherPartnerOrUnapprovedChats(t *testing.T) {
	s, _, _ := newIMFixture(t)
	ctx := context.Background()
	targets, err := s.Targets(ctx, "waker", false)
	if err != nil || len(targets) != 1 {
		t.Fatal(err)
	}
	targets, err = s.Targets(ctx, "other", false)
	if err != nil || len(targets) != 0 {
		t.Fatal("target scope escaped")
	}
	targets, err = s.Targets(ctx, "", true)
	if err != nil || len(targets) != 1 {
		t.Fatal("workflow authorization was ignored")
	}
	binding, _ := s.config.Store.Binding(ctx, "b")
	binding.AllowWorkflowDelivery = false
	if err := s.config.Store.SaveBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}
	targets, err = s.Targets(ctx, "", true)
	if err != nil || len(targets) != 0 {
		t.Fatal("workflow exposed disabled target")
	}
	binding, _ = s.config.Store.Binding(ctx, "b")
	binding.State = constants.IMBindingStateRejected
	s.config.Store.SaveBinding(ctx, binding)
	if _, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "rejected", TargetID: "b", Content: "result"}); !errors.Is(err, status_error.IMChatNotAuthorized) {
		t.Fatalf("unapproved target accepted: %v", err)
	}
}

func TestConnectionRemovalIsAtomicAndPreservesOtherConnections(t *testing.T) {
	service, db, _ := newIMFixture(t)
	ctx := context.Background()
	for _, value := range []any{
		&models.Chat{ID: "internal-chat"},
		&models.IMConnection{ID: "other", Revision: 1},
		&models.IMBinding{ID: "other-binding", ConnectionID: "other", ExternalChatID: "other"},
		&models.IMConversationLink{ID: "link", BindingID: "b", ChatID: "internal-chat"},
		&models.IMInbox{ID: "input", ConnectionID: "c", BindingID: "b", ExternalMessageID: "message"},
		&models.IMDelivery{ID: "delivery", ConnectionID: "c", BindingID: "b", RequestID: "request"},
	} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	remove := func(revision int64) error {
		return service.WithConnectionRemoval(ctx, "c", func(tx context.Context) error { return service.config.Store.DeleteConnection(tx, "c", revision, "") })
	}
	if err := remove(99); !errors.Is(err, status_error.IMConfigurationConflict) {
		t.Fatalf("stale deletion: %v", err)
	}
	var count int64
	db.Model(&models.IMBinding{}).Where("connection_id = ?", "c").Count(&count)
	if count != 1 {
		t.Fatal("stale deletion modified associations")
	}
	service.dispatchConnections["c"] = 1
	if err := remove(1); !errors.Is(err, status_error.IMConnectionInUse) {
		t.Fatalf("deleted active input: %v", err)
	}
	delete(service.dispatchConnections, "c")
	service.delivering["target:b"] = true
	if err := remove(1); !errors.Is(err, status_error.IMConnectionInUse) {
		t.Fatalf("deleted active send: %v", err)
	}
	delete(service.delivering, "target:b")
	workerCtx, cancel := context.WithCancel(ctx)
	service.connections["c"] = &connectionWorker{cancel: cancel}
	loginCtx, cancelLogin := context.WithCancel(ctx)
	service.logins["login"] = &loginSession{progress: LoginProgress{ConnectionID: "c", Status: "wait"}, cancel: cancelLogin}
	if err := remove(1); err != nil {
		t.Fatal(err)
	}
	if workerCtx.Err() == nil || loginCtx.Err() == nil {
		t.Fatal("connection or login worker survived deletion")
	}
	for _, model := range []any{&models.IMConnection{}, &models.IMBinding{}, &models.IMConversationLink{}, &models.IMInbox{}, &models.IMDelivery{}} {
		if err := db.Model(model).Where("id IN ?", []string{"c", "b", "link", "input", "delivery"}).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("association retained: %T", model)
		}
	}
	if err := db.Model(&models.Chat{}).Where("id = ?", "internal-chat").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("internal conversation removed: %v", err)
	}
	if _, err := service.config.Store.Connection(ctx, "other"); err != nil {
		t.Fatalf("other connection removed: %v", err)
	}
	if _, err := service.config.Store.Binding(ctx, "other-binding"); err != nil {
		t.Fatalf("other binding removed: %v", err)
	}
}

func TestIMLoginCancellationPublishesSafeTerminalProgressOnce(t *testing.T) {
	service, _, _ := newIMFixture(t)
	hub := events.New()
	defer hub.Close()
	service.config.Events = hub
	started := make(chan struct{})
	exited := make(chan struct{})
	service.config.Platforms[constants.IMPlatformWeixin] = &protocol.Weixin{Client: &http.Client{Transport: registrationTransport(func(request *http.Request) (*http.Response, error) {
		if strings.Contains(request.URL.Path, "get_bot_qrcode") {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"qrcode":"secret-server-device-code","qrcode_img_content":"https://example.invalid/qr"}`)), Header: make(http.Header)}, nil
		}
		close(started)
		<-request.Context().Done()
		close(exited)
		return nil, request.Context().Err()
	})}}
	service.ctx, service.cancel = context.WithCancel(context.Background())
	t.Cleanup(service.Close)
	subscription := hub.Subscribe("im", events.Resource{Kind: "global"})
	defer subscription.Close()
	login, err := service.BeginLogin(context.Background(), "c")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("Core did not start official QR request")
	}
	for len(subscription.Events) > 0 {
		<-subscription.Events
	}
	if err := service.CancelLogin(login.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.CancelLogin(login.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not stop official QR request")
	}
	service.workers.Wait()
	if len(subscription.Events) != 1 {
		t.Fatalf("cancellation emitted duplicate events: %d", len(subscription.Events))
	}
	event := <-subscription.Events
	if strings.Contains(string(event.Payload), "secret-server-device-code") || strings.Contains(string(event.Payload), "test-token") {
		t.Fatal("cancel event exposed private authentication state")
	}
	var payload struct {
		// Login 是安全取消事件里的扫码状态。
		Login LoginProgress `json:"login"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Login.ID != login.ID || payload.Login.Status != "cancelled" {
		t.Fatalf("cancelled state was replaced by delayed failure: %+v", payload.Login)
	}
}

func TestIMRepeatedWaitingProgressDoesNotPublishUnchangedState(t *testing.T) {
	service, _, _ := newIMFixture(t)
	hub := events.New()
	defer hub.Close()
	service.config.Events = hub
	subscription := hub.Subscribe("im", events.Resource{Kind: "global"})
	defer subscription.Close()
	session := &loginSession{progress: LoginProgress{ID: "login", ConnectionID: "c", Status: "wait", QRContent: "https://example.invalid/qr"}}
	service.loginProgress(session, "wait", "", "")
	if len(subscription.Events) != 0 {
		t.Fatal("unchanged official waiting state created repeated push")
	}
	service.loginProgress(session, "need_verifycode", "", "")
	if len(subscription.Events) != 1 {
		t.Fatal("real verification requirement did not publish")
	}
	session.progress.Status = "cancelled"
	service.loginProgress(session, "expired", "", "")
	if len(subscription.Events) != 1 || session.progress.Status != "cancelled" {
		t.Fatal("delayed progress changed cancelled login")
	}
}
