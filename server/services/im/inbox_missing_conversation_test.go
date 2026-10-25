package im

import (
	"context"
	"io"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	protocol "github.com/JieWaZi/NeuxBot/server/internal/im"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"gorm.io/gorm"
)

// missingConversationExecutions 隔离 Runtime，但沿真实 Chat 查询边界验证续发目标。
type missingConversationExecutions struct {
	// Service 提供当前回归未调用的执行端口。
	execution.Service
	// chats 读取与 IM 共用临时数据库的权威会话。
	chats chat.Module
	// inputs 记录到达执行接纳边界的真实工作意图。
	inputs []execution.WorkIntent
	// directory 是替身建立可安全删除的受管 Chat 工作区时使用的数据根。
	directory string
}

func (e *missingConversationExecutions) Start(ctx context.Context, input execution.WorkIntent) (execution.AcceptedExecution, error) {
	e.inputs = append(e.inputs, input)
	if input.Chat.ChatID != "" {
		summary, err := e.chats.GetChatSummary(ctx, input.Chat.ChatID)
		if err != nil {
			return execution.AcceptedExecution{}, err
		}
		return execution.AcceptedExecution{Chat: summary.Chat, Execution: models.Execution{ID: input.ExecutionID}}, nil
	}
	created, err := createAdmissionChat(ctx, e.chats, e.directory, protocol.StableID("chat", input.ClientRequestID), input.Chat.Create.WakerID)
	if err != nil {
		return execution.AcceptedExecution{}, err
	}
	return execution.AcceptedExecution{Chat: created, Execution: models.Execution{ID: input.ExecutionID}}, nil
}

func createAdmissionChat(ctx context.Context, module chat.Module, root, id, wakerID string) (models.Chat, error) {
	layout, err := chatstorage.NewLayout(root)
	if err != nil {
		return models.Chat{}, err
	}
	directory, err := layout.ChatWorkspace(id)
	if err != nil {
		return models.Chat{}, err
	}
	if err := workspace.EnsureExecutionWorkspace(directory, id, wakerID, nil); err != nil {
		return models.Chat{}, err
	}
	created, err := module.CreateDormantChats(ctx, []chat.DormantChat{{Chat: models.Chat{ID: id, WakerID: wakerID, WorkspaceDirectory: directory, AdditionalWorkingDirectories: []string{directory}}}})
	if err != nil {
		return models.Chat{}, err
	}
	return created[0], nil
}

// missingConversationTeams 隔离团队派发，并使用真实 Repository 验证续发对话。
type missingConversationTeams struct {
	// Service 提供当前回归未调用的团队端口。
	channel.Service
	// threads 从真实临时数据库读取团队对话。
	threads controllers.ChannelThreadController
	// inputs 记录实际交给团队接纳的首发或续发。
	inputs []channel.SubmitMessageInput
}

func (e *missingConversationTeams) SubmitMessage(ctx context.Context, input channel.SubmitMessageInput) (channel.SubmitMessageResult, error) {
	e.inputs = append(e.inputs, input)
	if input.Continue != nil {
		if _, err := e.threads.Get(ctx, input.Continue.ThreadID); err != nil {
			return channel.SubmitMessageResult{}, err
		}
	}
	return channel.SubmitMessageResult{Thread: &channel.ThreadResult{Thread: models.ChannelThread{ID: "replacement-thread", LeadChatID: "replacement-chat"}}}, nil
}

func attachConversationReaders(t *testing.T, service *Service, db *gorm.DB) {
	t.Helper()
	module, err := chat.New(chat.Config{
		StorageDirectory: service.config.DataDirectory,
		Projects:         controllers.NewProjectController(db),
		QueryIndexes: chat.QueryIndexDependencies{
			Chats:           controllers.NewChatController(db),
			RuntimeSessions: controllers.NewRuntimeSessionController(db),
			Transactions:    service.config.Transactions,
			DeletionIntents: controllers.NewChatDeletionIntentController(db),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.config.Chats = module
	service.config.Threads = controllers.NewChannelThreadController(db)
	t.Cleanup(module.Close)
}

func TestIncomingMessageCreatesConversationWhenCurrentPartnerChatWasDeleted(t *testing.T) {
	service, db, _ := newIMFixture(t)
	attachConversationReaders(t, service, db)
	ctx := context.Background()
	executions := &missingConversationExecutions{chats: service.config.Chats, directory: service.config.DataDirectory}
	service.config.Executions = executions
	binding, err := service.config.Store.Binding(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	previous := binding
	previous.Options.Model = "previous-model"
	binding.Options.Model = "current-model"
	binding.ActiveLinkID = "deleted-link"
	if err := service.config.Store.SaveBinding(ctx, binding, binding.Revision); err != nil {
		t.Fatal(err)
	}
	if err := service.config.Store.SaveLink(ctx, models.IMConversationLink{ID: "deleted-link", BindingID: "b", ChatID: protocol.StableID("chat", "deleted"), Title: "已删除对话", Configuration: previous}); err != nil {
		t.Fatal(err)
	}
	input := models.IMInbox{ID: "new-work", ConnectionID: "c", BindingID: "b", Content: "开始新的工作", State: constants.IMInboxStatePending}
	if err := service.config.Store.SaveInbox(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := service.route(ctx, input); err != nil {
		t.Fatalf("当前会话删除后新消息路由失败: %v", err)
	}
	if err := db.First(&input, "id = ?", input.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.accept(ctx, input); err != nil {
		t.Fatalf("当前会话删除后新消息应新建对话，而非续发失效会话: %v", err)
	}
	if len(executions.inputs) != 1 || executions.inputs[0].Chat.Create == nil || executions.inputs[0].Chat.ChatID != "" {
		t.Fatalf("新消息未通过首次接纳: %+v", executions.inputs)
	}
	if executions.inputs[0].Options.Model != "current-model" || input.LinkID == "deleted-link" {
		t.Fatalf("新对话未冻结当前配置或覆盖了旧关联: %+v", executions.inputs[0])
	}
	current, err := service.config.Store.Link(ctx, input.LinkID)
	if err != nil {
		t.Fatal(err)
	}
	followup := models.IMInbox{ID: "followup-work", ConnectionID: "c", BindingID: "b", Content: "继续新工作", State: constants.IMInboxStatePending}
	if err := service.config.Store.SaveInbox(ctx, followup); err != nil {
		t.Fatal(err)
	}
	if err := service.route(ctx, followup); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&followup, "id = ?", followup.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.accept(ctx, followup); err != nil {
		t.Fatal(err)
	}
	if len(executions.inputs) != 2 || executions.inputs[1].Chat.Create != nil || executions.inputs[1].Chat.ChatID != current.ChatID || followup.LinkID != current.ID {
		t.Fatalf("后续新消息未延续已经新建的当前对话: %+v", executions.inputs)
	}
}

func TestIncomingMessageCreatesConversationWhenCurrentTeamThreadWasDeleted(t *testing.T) {
	service, db, _ := newIMFixture(t)
	attachConversationReaders(t, service, db)
	ctx := context.Background()
	teams := &missingConversationTeams{threads: service.config.Threads}
	service.config.Teams = teams
	binding, err := service.config.Store.Binding(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	binding.WakerID = ""
	binding.ChannelID = "team"
	binding.ActiveLinkID = "deleted-link"
	if err := service.config.Store.SaveBinding(ctx, binding, binding.Revision); err != nil {
		t.Fatal(err)
	}
	if err := service.config.Store.SaveLink(ctx, models.IMConversationLink{ID: "deleted-link", BindingID: "b", ChatID: "deleted-chat", ThreadID: "deleted-thread", Title: "已删除团队对话", Configuration: binding}); err != nil {
		t.Fatal(err)
	}
	input := models.IMInbox{ID: "new-team-work", ConnectionID: "c", BindingID: "b", Content: "开始新的团队工作", State: constants.IMInboxStatePending}
	if err := service.config.Store.SaveInbox(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := service.route(ctx, input); err != nil {
		t.Fatalf("当前团队对话删除后新消息路由失败: %v", err)
	}
	if err := db.First(&input, "id = ?", input.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.accept(ctx, input); err != nil {
		t.Fatalf("当前团队对话删除后新消息应新建对话，而非续发失效线程: %v", err)
	}
	if len(teams.inputs) != 1 || teams.inputs[0].Create == nil || teams.inputs[0].Continue != nil || input.LinkID == "deleted-link" {
		t.Fatalf("新消息未进入独立团队首发: %+v", teams.inputs)
	}
}

func TestQueuedMessageKeepsFrozenConfigurationAfterConversationDeletionAndNewSelection(t *testing.T) {
	service, db, _ := newIMFixture(t)
	attachConversationReaders(t, service, db)
	ctx := context.Background()
	chatID := protocol.StableID("chat", "previous")
	if _, err := createAdmissionChat(ctx, service.config.Chats, service.config.DataDirectory, chatID, "waker"); err != nil {
		t.Fatal(err)
	}
	binding, err := service.config.Store.Binding(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	binding.Options.Model = "frozen-model"
	binding.ActiveLinkID = "previous-link"
	if err := service.config.Store.SaveBinding(ctx, binding, binding.Revision); err != nil {
		t.Fatal(err)
	}
	previous := models.IMConversationLink{ID: "previous-link", BindingID: "b", ChatID: chatID, Configuration: binding, EventCursor: 17, PendingReportIDs: []string{"already-observed-report"}}
	if err := service.config.Store.SaveLink(ctx, previous); err != nil {
		t.Fatal(err)
	}
	input := models.IMInbox{ID: "queued-work", ConnectionID: "c", BindingID: "b", Content: "已排队的新输入", State: constants.IMInboxStatePending}
	if err := service.config.Store.SaveInbox(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := service.route(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&input, "id = ?", input.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.config.Chats.DeleteChat(ctx, chatID); err != nil {
		t.Fatal(err)
	}
	if err := service.SelectConversation(ctx, "b", ""); err != nil {
		t.Fatal(err)
	}
	binding, err = service.config.Store.Binding(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	binding.Options.Model = "future-model"
	if err := service.config.Store.SaveBinding(ctx, binding, binding.Revision); err != nil {
		t.Fatal(err)
	}
	executions := &missingConversationExecutions{chats: service.config.Chats, directory: service.config.DataDirectory}
	service.config.Executions = executions
	if err := service.accept(ctx, input); err != nil {
		t.Fatalf("接纳前失效的排队输入未建立独立对话: %v", err)
	}
	if len(executions.inputs) != 1 || executions.inputs[0].Chat.Create == nil || executions.inputs[0].Options.Model != "frozen-model" || executions.inputs[0].Input.SubmissionID != input.ID {
		t.Fatalf("排队工作丢失原冻结配置或输入身份: %+v", executions.inputs)
	}
	actual, err := service.config.Store.Binding(ctx, "b")
	if err != nil || actual.ActiveLinkID != "" {
		t.Fatalf("原排队输入覆盖了用户准备的新对话: %+v %v", actual, err)
	}
	retained, err := service.config.Store.Link(ctx, previous.ID)
	if err != nil || retained.ChatID != chatID || retained.EventCursor != previous.EventCursor || len(retained.PendingReportIDs) != 1 {
		t.Fatalf("失效原关联的历史或投递游标被覆盖: %+v %v", retained, err)
	}
}

func TestConversationDatabaseFailureDoesNotCreateReplacement(t *testing.T) {
	service, db, _ := newIMFixture(t)
	attachConversationReaders(t, service, db)
	ctx := context.Background()
	binding, err := service.config.Store.Binding(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	binding.ActiveLinkID = "existing-link"
	if err := service.config.Store.SaveBinding(ctx, binding, binding.Revision); err != nil {
		t.Fatal(err)
	}
	if err := service.config.Store.SaveLink(ctx, models.IMConversationLink{ID: "existing-link", BindingID: "b", ChatID: "actual-chat", Configuration: binding}); err != nil {
		t.Fatal(err)
	}
	failQueries := true
	if err := db.Callback().Query().Before("gorm:query").Register("im_test_chat_query_failure", func(tx *gorm.DB) {
		if failQueries && tx.Statement.Table == "im_conversation_links" {
			_ = tx.AddError(io.ErrUnexpectedEOF)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove("im_test_chat_query_failure") })
	input := models.IMInbox{ID: "new-work", ConnectionID: "c", BindingID: "b", Content: "新的输入", State: constants.IMInboxStatePending}
	if err := service.config.Store.SaveInbox(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := service.route(ctx, input); !hasCode(err, status_error.DatabaseError) {
		t.Fatalf("数据库故障被当作会话删除或被忽略: %v", err)
	}
	failQueries = false
	links, err := service.config.Store.Links(ctx, "b")
	if err != nil || len(links) != 1 {
		t.Fatalf("数据库查询失败仍建立了新对话: %+v %v", links, err)
	}
	actual, err := service.config.Store.Binding(ctx, "b")
	if err != nil || actual.ActiveLinkID != binding.ActiveLinkID {
		t.Fatalf("数据库查询失败改变了当前指针: %+v %v", actual, err)
	}
}

func TestExistingConversationWithMissingQueryIndexIsNotReplaced(t *testing.T) {
	service, db, _ := newIMFixture(t)
	attachConversationReaders(t, service, db)
	ctx := context.Background()
	chatID := protocol.StableID("chat", "existing-authority")
	if _, err := createAdmissionChat(ctx, service.config.Chats, service.config.DataDirectory, chatID, "waker"); err != nil {
		t.Fatal(err)
	}
	binding, err := service.config.Store.Binding(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	binding.ActiveLinkID = "existing-link"
	if err := service.config.Store.SaveBinding(ctx, binding, binding.Revision); err != nil {
		t.Fatal(err)
	}
	if err := service.config.Store.SaveLink(ctx, models.IMConversationLink{ID: "existing-link", BindingID: "b", ChatID: chatID, Configuration: binding}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", chatID).Delete(&models.Chat{}).Error; err != nil {
		t.Fatal(err)
	}
	input := models.IMInbox{ID: "next-input", ConnectionID: "c", BindingID: "b", Content: "继续真实存在的对话", State: constants.IMInboxStatePending}
	if err := service.config.Store.SaveInbox(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := service.route(ctx, input); err != nil {
		t.Fatalf("可重建查询索引缺失阻止了正常路由: %v", err)
	}
	if err := db.First(&input, "id = ?", input.ID).Error; err != nil || input.LinkID != binding.ActiveLinkID {
		t.Fatalf("仍存在的权威会话被当作删除而新建: %+v %v", input, err)
	}
	links, err := service.config.Store.Links(ctx, "b")
	if err != nil || len(links) != 1 {
		t.Fatalf("投影缺失产生额外 IM 对话关联: %+v %v", links, err)
	}
}

func TestQuotingDeletedConversationStartsOnlyTheNewInput(t *testing.T) {
	service, db, _ := newIMFixture(t)
	attachConversationReaders(t, service, db)
	ctx := context.Background()
	binding, err := service.config.Store.Binding(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.config.Store.SaveLink(ctx, models.IMConversationLink{ID: "deleted-link", BindingID: "b", ChatID: protocol.StableID("chat", "deleted"), Configuration: binding}); err != nil {
		t.Fatal(err)
	}
	previous := models.IMInbox{ID: "completed-work", ConnectionID: "c", BindingID: "b", ExternalMessageID: "quoted-message", Content: "之前已完成的工作", LinkID: "deleted-link", ExecutionID: "completed-execution", State: constants.IMInboxStateProcessed}
	if err := service.config.Store.SaveInbox(ctx, previous); err != nil {
		t.Fatal(err)
	}
	input := models.IMInbox{ID: "quoted-new-work", ConnectionID: "c", BindingID: "b", QuoteMessageID: "quoted-message", Content: "这是新的要求", State: constants.IMInboxStatePending}
	if err := service.config.Store.SaveInbox(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := service.route(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&input, "id = ?", input.ID).Error; err != nil {
		t.Fatal(err)
	}
	executions := &missingConversationExecutions{chats: service.config.Chats, directory: service.config.DataDirectory}
	service.config.Executions = executions
	if err := service.accept(ctx, input); err != nil {
		t.Fatalf("引用已删会话未能新建独立对话: %v", err)
	}
	if len(executions.inputs) != 1 || executions.inputs[0].Input.Content != input.Content || executions.inputs[0].Input.SubmissionID != input.ID || executions.inputs[0].Chat.Create == nil {
		t.Fatalf("引用删除会话重放了旧工作或未创建独立对话: %+v", executions.inputs)
	}
	retained := models.IMInbox{}
	if err := db.First(&retained, "id = ?", previous.ID).Error; err != nil || retained.State != previous.State || retained.ExecutionID != previous.ExecutionID || retained.LinkID != previous.LinkID {
		t.Fatalf("旧工作被重新排队或改写: %+v %v", retained, err)
	}
}

func TestDuplicateReceivedMessageDoesNotReplayAcceptedWorkAfterDeletion(t *testing.T) {
	service, db, _ := newIMFixture(t)
	attachConversationReaders(t, service, db)
	ctx := context.Background()
	connection, err := service.config.Store.Connection(ctx, "c")
	if err != nil {
		t.Fatal(err)
	}
	message := protocol.Message{ID: "external-message", ChatID: "another-user", SenderID: "owner", Kind: constants.IMChatKindDirect, Content: "只执行一次"}
	if err := service.Receive(ctx, connection, []protocol.Message{message}, "first-cursor"); err != nil {
		t.Fatal(err)
	}
	bindingID := protocol.StableID("binding", connection.ID, message.ChatID)
	binding, err := service.config.Store.Binding(ctx, bindingID)
	if err != nil {
		t.Fatal(err)
	}
	binding.State = constants.IMBindingStateApproved
	binding.WakerID = "waker"
	if err := service.config.Store.SaveBinding(ctx, binding, binding.Revision); err != nil {
		t.Fatal(err)
	}
	inputs, err := service.config.Store.Inbox(ctx, constants.IMInboxStatePending, 20)
	if err != nil || len(inputs) != 1 {
		t.Fatalf("首次消息未正常进入待路由状态: %+v %v", inputs, err)
	}
	if err := service.route(ctx, inputs[0]); err != nil {
		t.Fatal(err)
	}
	inputs, err = service.config.Store.Inbox(ctx, constants.IMInboxStateRouted, 20)
	if err != nil || len(inputs) != 1 {
		t.Fatalf("首次消息未正常冻结路由: %+v %v", inputs, err)
	}
	executions := &missingConversationExecutions{chats: service.config.Chats, directory: service.config.DataDirectory}
	service.config.Executions = executions
	if err := service.accept(ctx, inputs[0]); err != nil {
		t.Fatal(err)
	}
	link, err := service.config.Store.Link(ctx, inputs[0].LinkID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.config.Chats.DeleteChat(ctx, link.ChatID); err != nil {
		t.Fatal(err)
	}
	if err := service.Receive(ctx, connection, []protocol.Message{message}, "repeated-cursor"); err != nil {
		t.Fatal(err)
	}
	for _, state := range []constants.IMInboxState{constants.IMInboxStatePending, constants.IMInboxStateRouted} {
		work, err := service.config.Store.Inbox(ctx, state, 20)
		if err != nil || len(work) != 0 {
			t.Fatalf("删除会话后重复平台消息重新进入执行队列: %+v %v", work, err)
		}
	}
	accepted, err := service.config.Store.Inbox(ctx, constants.IMInboxStateAccepted, 20)
	if err != nil || len(accepted) != 1 || len(executions.inputs) != 1 {
		t.Fatalf("原接纳事实丢失或重复工作: %+v %v", accepted, err)
	}
}
