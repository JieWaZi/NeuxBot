package im

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	protocol "github.com/JieWaZi/NeuxBot/server/internal/im"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

func TestResultProjectionIdleDoesNotPublishCheckpoint(t *testing.T) {
	service, _, _ := newIMFixture(t)
	ctx := t.Context()
	link := models.IMConversationLink{ID: "idle-im-link", BindingID: "b", ChatID: "idle-chat", EventCursor: 21}
	if err := service.config.Store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	service.config.Chats = replyProjectionChats{}
	subscription := service.config.Events.Subscribe("im", events.Resource{})
	defer subscription.Close()
	if _, err := service.projectLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-subscription.Events:
		t.Fatalf("idle result projection published an unchanged checkpoint: %+v", event)
	default:
	}
}

func TestResultProjectionBoundsLongReplyAndContinues(t *testing.T) {
	service, _, _ := newIMFixture(t)
	ctx := t.Context()
	link := models.IMConversationLink{ID: "long-replies", BindingID: "b", ChatID: "long-reply-chat"}
	if err := service.config.Store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	chats, projection := prepareScheduledProjection(t, service)
	longBody := strings.Repeat("团队🙂", 24000)
	chats.origins["long"] = link.ID
	chats.origins["follow-up"] = link.ID
	chats.events[link.ChatID] = []chat.PublicEvent{
		{Sequence: 1, Type: constants.ChatEventTypeExecutionFailed, ExecutionID: "long", ErrorCode: "RuntimeUnavailable"},
		{Sequence: 2, Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "follow-up"},
	}
	chats.messages[link.ChatID] = []messageview.Message{
		{ExecutionID: "long", Role: constants.MessageRoleAssistant, Content: longBody},
		{ExecutionID: "follow-up", Role: constants.MessageRoleAssistant, Content: "next answer"},
	}
	service.projectResults(projection)
	assertProjectionPreview(t, service, "result:"+link.ID+":long", longBody, "RuntimeUnavailable")
	assertProjectionDelivery(t, service, "result:"+link.ID+":follow-up", link.ID)
	updated, err := service.config.Store.Link(ctx, link.ID)
	if err != nil || updated.EventCursor != 2 {
		t.Fatalf("long reply blocked the next execution: cursor=%d err=%v", updated.EventCursor, err)
	}
	if chats.messages[link.ChatID][0].Content != longBody {
		t.Fatal("projection changed the stored Chat reply")
	}
	// A crash before checkpointing may replay both events. Their frozen previews must hash identically.
	if _, err := service.projectLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	if _, total, err := service.config.Store.Deliveries(ctx, constants.IMDeliveryStateUnspecified, 20, 0); err != nil || total != 2 {
		t.Fatalf("projection replay changed delivery identity: total=%d err=%v", total, err)
	}
	if _, err := service.CreateDelivery(ctx, DeliveryRequest{RequestID: "explicit-too-long", TargetID: link.BindingID, Content: longBody}); !hasCode(err, status_error.IMConfigurationInvalid) {
		t.Fatalf("explicit delivery accepted a body above the limit: %v", err)
	}
}

func assertProjectionPreview(t *testing.T, s *Service, requestID, body, terminalCode string) {
	t.Helper()
	delivery, exists, err := s.config.Store.FindDelivery(t.Context(), requestID)
	if err != nil || !exists {
		t.Fatalf("expected a queued preview for %s, exists=%t err=%v", requestID, exists, err)
	}
	preview, _, truncated := strings.Cut(delivery.Content, "\n\n回复内容较长")
	if !truncated || preview == "" || !strings.HasPrefix(body, preview) || !utf8.ValidString(delivery.Content) || len(delivery.Content) > constants.IMDeliveryTextMaximumBytes || !strings.Contains(delivery.Content, "请在 NeuxBot 查看完整内容") {
		t.Fatalf("invalid projection preview: truncated=%t valid_utf8=%t bytes=%d", truncated, utf8.ValidString(delivery.Content), len(delivery.Content))
	}
	if terminalCode != "" && !strings.Contains(delivery.Content, terminalCode) {
		t.Fatalf("preview dropped the terminal status %s", terminalCode)
	}
}

func TestResultProjectionWakesOldLinkAndContinuesFullChatPage(t *testing.T) {
	service, _, _ := newIMFixture(t)
	original := models.IMConversationLink{ID: "original", BindingID: "b", ChatID: "original-chat"}
	active := models.IMConversationLink{ID: "active", BindingID: "b", ChatID: "active-chat"}
	for _, link := range []models.IMConversationLink{original, active} {
		if err := service.config.Store.SaveLink(t.Context(), link); err != nil {
			t.Fatal(err)
		}
	}
	binding, err := service.config.Store.Binding(t.Context(), "b")
	if err != nil {
		t.Fatal(err)
	}
	binding.ActiveLinkID = active.ID
	if err := service.config.Store.SaveBinding(t.Context(), binding); err != nil {
		t.Fatal(err)
	}
	chats, projection := prepareScheduledProjection(t, service)
	service.projectResults(projection)
	chats.reads = nil
	for sequence := 1; sequence <= projectionEventPage; sequence++ {
		chats.events[original.ChatID] = append(chats.events[original.ChatID], chat.PublicEvent{
			Sequence: int64(sequence), Type: constants.ChatEventTypeUserMessageCreated,
		})
	}
	chats.complete(original, projectionEventPage+1)
	emitProjectionChange(t, service, events.Resource{Kind: "chat", ID: original.ChatID})
	emitProjectionChange(t, service, events.Resource{Kind: "chat", ID: original.ChatID})
	emitProjectionChange(t, service, events.Resource{Kind: "chat", ID: "unlinked-desktop-chat"})
	for range 3 {
		service.projectResults(projection)
	}
	if want := []string{original.ChatID, original.ChatID}; !reflect.DeepEqual(chats.reads, want) {
		t.Fatalf("projection read %v, want only the old link's two pages %v", chats.reads, want)
	}
	assertProjectedExecution(t, service, original)
}

func TestResultProjectionCatchesBeforeStartAndAfterLinkCommit(t *testing.T) {
	service, _, _ := newIMFixture(t)
	recovered := models.IMConversationLink{ID: "recovered", BindingID: "b", ChatID: "recovered-chat"}
	attached := models.IMConversationLink{ID: "attached", BindingID: "b"}
	for _, link := range []models.IMConversationLink{attached, recovered} {
		if err := service.config.Store.SaveLink(t.Context(), link); err != nil {
			t.Fatal(err)
		}
	}
	chats, projection := prepareScheduledProjection(t, service)
	chats.complete(recovered, 1)
	service.projectResults(projection)
	assertProjectedExecution(t, service, recovered)
	attached.ChatID = "attached-chat"
	chats.complete(attached, 1)
	emitProjectionChange(t, service, events.Resource{Kind: "chat", ID: attached.ChatID})
	service.projectResults(projection)
	if err := service.config.Store.UpdateLink(t.Context(), attached); err != nil {
		t.Fatal(err)
	}
	service.projectResults(projection)
	assertProjectedExecution(t, service, attached)
}

func TestResultProjectionSweepAdvancesDuringRepeatedRecoveryHints(t *testing.T) {
	service, _, _ := newIMFixture(t)
	chats, projection := prepareScheduledProjection(t, service)
	var last models.IMConversationLink
	for i := 0; i <= constants.IMPageSize; i++ {
		last = models.IMConversationLink{ID: fmt.Sprintf("link-%03d", i), BindingID: "b", ChatID: fmt.Sprintf("chat-%03d", i)}
		if err := service.config.Store.SaveLink(t.Context(), last); err != nil {
			t.Fatal(err)
		}
	}
	chats.complete(last, 1)
	service.projectResults(projection)
	// 缺口恢复期间收到的重复全局提示保留下一次扫描，不能重置本次页位置。
	if err := events.Emit(service.config.Events, "work-board", "work-board.changed", events.Resource{Kind: "global"}, map[string]string{"reason": constants.ChatReadingRecoveredReason}); err != nil {
		t.Fatal(err)
	}
	service.projectResults(projection)
	if len(chats.reads) != constants.IMPageSize+1 || chats.reads[len(chats.reads)-1] != last.ChatID {
		t.Fatalf("recovery hint restarted the incomplete sweep: %v", chats.reads)
	}
	assertProjectedExecution(t, service, last)
	for range 3 {
		service.projectResults(projection)
	}
	if len(chats.reads) != 2*(constants.IMPageSize+1) {
		t.Fatalf("wanted one additional bounded sweep after the hint, got %d reads", len(chats.reads))
	}
}

func TestResultProjectionResubscribesAndReplaysAfterOverflow(t *testing.T) {
	service, _, _ := newIMFixture(t)
	link := models.IMConversationLink{ID: "overflow-recovery", BindingID: "b", ChatID: "overflow-chat"}
	if err := service.config.Store.SaveLink(t.Context(), link); err != nil {
		t.Fatal(err)
	}
	chats, projection := prepareScheduledProjection(t, service)
	service.projectResults(projection)
	for range events.BufferSize {
		emitProjectionChange(t, service, events.Resource{Kind: "channel", ID: "unrelated"})
	}
	chats.complete(link, 1)
	emitProjectionChange(t, service, events.Resource{Kind: "chat", ID: link.ChatID})
	for range 3 {
		service.projectResults(projection)
	}
	assertProjectedExecution(t, service, link)
	chats.reads = nil
	emitProjectionChange(t, service, events.Resource{Kind: "chat", ID: link.ChatID})
	service.projectResults(projection)
	if !reflect.DeepEqual(chats.reads, []string{link.ChatID}) {
		t.Fatalf("new events were lost after recovery: %v", chats.reads)
	}
}

func TestResultProjectionReplaysAfterReenableAndRetriesOnlyFailedLink(t *testing.T) {
	service, _, _ := newIMFixture(t)
	ctx := t.Context()
	link := models.IMConversationLink{ID: "retry", BindingID: "b", ChatID: "retry-chat"}
	if err := service.config.Store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	connection, err := service.config.Store.Connection(ctx, "c")
	if err != nil {
		t.Fatal(err)
	}
	connection.Enabled = false
	if err := service.config.Store.SaveConnection(ctx, connection, connection.Revision); err != nil {
		t.Fatal(err)
	}
	chats, projection := prepareScheduledProjection(t, service)
	chats.complete(link, 1)
	service.projectResults(projection)
	if len(chats.reads) != 0 {
		t.Fatalf("disabled target still read the Chat: %v", chats.reads)
	}
	connection.Enabled = true
	if err := service.config.Store.SaveConnection(ctx, connection, connection.Revision+1); err != nil {
		t.Fatal(err)
	}
	chats.failFirst = true
	service.projectResults(projection)
	service.projectResults(projection)
	service.projectResults(projection)
	if !reflect.DeepEqual(chats.reads, []string{link.ChatID, link.ChatID}) {
		t.Fatalf("failed link was not retried exactly once after re-enable: %v", chats.reads)
	}
	assertProjectedExecution(t, service, link)
}

func TestResultProjectionReplaysOnReapproval(t *testing.T) {
	service, _, _ := newIMFixture(t)
	ctx := t.Context()
	link := models.IMConversationLink{ID: "approved-again", BindingID: "b", ChatID: "approved-again-chat"}
	if err := service.config.Store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	binding, err := service.config.Store.Binding(ctx, link.BindingID)
	if err != nil {
		t.Fatal(err)
	}
	binding.State = constants.IMBindingStateRejected
	if err := service.config.Store.SaveBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}
	chats, projection := prepareScheduledProjection(t, service)
	chats.complete(link, 1)
	service.projectResults(projection)
	if len(chats.reads) != 0 {
		t.Fatalf("revoked target still read the Chat: %v", chats.reads)
	}
	binding.State = constants.IMBindingStateApproved
	if err := service.config.Store.SaveBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}
	service.projectResults(projection)
	assertProjectedExecution(t, service, link)
}

func TestResultProjectionPublishesMemberInteractionAndSettlesPendingReport(t *testing.T) {
	service, db, _ := newIMFixture(t)
	ctx := t.Context()
	link := models.IMConversationLink{ID: "original-team-link", BindingID: "b", ChatID: "lead-chat", ThreadID: "thread"}
	if err := service.config.Store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	member := models.ChannelThreadMember{ThreadID: link.ThreadID, WakerID: "member", ChatID: "member-chat"}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	service.config.TeamTasks = controllers.NewChannelTaskController(db)
	notice := models.ChannelLeadNotification{ID: "member-report", ThreadID: link.ThreadID, AttemptID: "attempt", IMReplyLinkID: link.ID, Status: constants.ChannelNotificationStatusPending}
	if err := service.config.TeamTasks.CreateNotification(ctx, notice); err != nil {
		t.Fatal(err)
	}
	chats, projection := prepareScheduledProjection(t, service)
	service.config.Teams = scheduledProjectionTeam{}
	service.projectResults(projection)

	chats.summaries[member.ChatID] = models.Chat{ActiveExecutionID: "member-execution", PendingApprovals: []models.PendingApproval{{ApprovalID: "member-approval"}}}
	chats.origins["member-execution"] = link.ID
	service.config.Teams = scheduledProjectionTeam{pendingChat: member.ChatID}
	report := messageview.Message{ID: notice.ID, Actor: string(constants.ChannelMessageActorMemberReport), Content: strings.Repeat("成员🙂", 24000), SourceChatID: member.ChatID}
	chats.events[link.ChatID] = []chat.PublicEvent{{Sequence: 1, Type: constants.ChatEventTypeUserMessageCreated, Message: &report}}
	chats.messages[link.ChatID] = []messageview.Message{report}
	emitProjectionChange(t, service, events.Resource{Kind: "chat", ID: member.ChatID})
	service.projectResults(projection)
	assertProjectionDelivery(t, service, "approval:"+link.ID+":"+member.ChatID+":member-approval", link.ID)
	pending, err := service.config.Store.Link(ctx, link.ID)
	if err != nil || !reflect.DeepEqual(pending.PendingReportIDs, []string{notice.ID}) {
		t.Fatalf("undecided report was not checkpointed: %+v, %v", pending, err)
	}
	// 直接消费不发布 work-board.live，测试不再添加 Chat 事件。
	if consumed, err := service.config.TeamTasks.ConsumePendingNotification(ctx, notice.ID, 1); err != nil || !consumed {
		t.Fatalf("set direct delivery decision: %v, %v", consumed, err)
	}
	service.projectResults(projection)
	assertProjectionDelivery(t, service, "member-result:"+link.ID+":"+notice.ID, link.ID)
	assertProjectionPreview(t, service, "member-result:"+link.ID+":"+notice.ID, report.Content, "")
	pending, err = service.config.Store.Link(ctx, link.ID)
	if err != nil || len(pending.PendingReportIDs) != 0 || pending.EventCursor != 1 {
		t.Fatalf("processed report remained pending or advanced the event cursor: %+v, %v", pending, err)
	}
}

func TestResultProjectionSettlesAbandonedReportsAndKeepsLateNotification(t *testing.T) {
	service, db, _ := newIMFixture(t)
	ctx := t.Context()
	link := models.IMConversationLink{ID: "abandoned-team-link", BindingID: "b", ChatID: "abandoned-lead", ThreadID: "abandoned-team"}
	service.config.TeamTasks = controllers.NewChannelTaskController(db)
	service.config.Teams = scheduledProjectionTeam{}
	if err := service.config.Store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	chats, projection := prepareScheduledProjection(t, service)
	for index, report := range []struct {
		// id 标识报告及其所属的成员尝试。
		id string
		// status 为报告对应成员尝试的持久状态。
		status constants.ExecutionStatus
	}{
		{"stopped", constants.ExecutionStatusCanceled},
		{"failed", constants.ExecutionStatusFailed},
		{"awaiting", constants.ExecutionStatusRunning},
		{"notified", constants.ExecutionStatusCompleted},
	} {
		task := models.ChannelTask{ID: "task-" + report.id, ThreadID: link.ThreadID, Status: constants.ChannelTaskStatusInProgress, CurrentFence: 1, ChangedPaths: []string{}}
		if err := service.config.TeamTasks.Create(ctx, task, nil); err != nil {
			t.Fatal(err)
		}
		if err := service.config.TeamTasks.CreateAttempt(ctx, models.ChannelTaskAttempt{ID: report.id, TaskID: task.ID, ChatID: "member-chat", Fence: 1, Status: report.status, IMReplyLinkID: link.ID}); err != nil {
			t.Fatal(err)
		}
		message := messageview.Message{ID: report.id, Actor: string(constants.ChannelMessageActorMemberReport), Content: "member report", SourceChatID: "member-chat"}
		chats.events[link.ChatID] = append(chats.events[link.ChatID], chat.PublicEvent{Sequence: int64(index + 1), Type: constants.ChatEventTypeUserMessageCreated, Message: &message})
		chats.messages[link.ChatID] = append(chats.messages[link.ChatID], message)
	}
	// 监听失败的通知使用独立消息身份，不属于此前提前上报的正文。
	if err := service.config.TeamTasks.CreateNotification(ctx, models.ChannelLeadNotification{ID: "failed-notification", ThreadID: link.ThreadID, TaskID: "task-failed", AttemptID: "failed", IMReplyLinkID: link.ID, Status: constants.ChannelNotificationStatusPending}); err != nil {
		t.Fatal(err)
	}
	if err := service.config.TeamTasks.CreateNotification(ctx, models.ChannelLeadNotification{ID: "notified", ThreadID: link.ThreadID, TaskID: "task-notified", AttemptID: "notified", IMReplyLinkID: link.ID, Status: constants.ChannelNotificationStatusPending}); err != nil {
		t.Fatal(err)
	}
	service.projectResults(projection)
	updated, err := service.config.Store.Link(ctx, link.ID)
	if err != nil || updated.EventCursor != 4 || !reflect.DeepEqual(updated.PendingReportIDs, []string{"awaiting", "notified"}) {
		t.Fatalf("projection kept abandoned reports or dropped a live attempt: %+v %v", updated, err)
	}
	if err := service.config.Transactions.Transaction(ctx, func(tx context.Context) error {
		if err := service.config.TeamTasks.UpdateAttemptFields(tx, "awaiting", map[string]any{"status": constants.ExecutionStatusCompleted}); err != nil {
			return err
		}
		return service.config.TeamTasks.CreateNotification(tx, models.ChannelLeadNotification{ID: "awaiting", ThreadID: link.ThreadID, TaskID: "task-awaiting", AttemptID: "awaiting", IMReplyLinkID: link.ID, Status: constants.ChannelNotificationStatusPending})
	}); err != nil {
		t.Fatal(err)
	}
	// 重启只从持久待处理报告恢复，仍等待真实通知确定交付方式。
	recovered := &resultProjection{hub: service.config.Events}
	recovered.setActive(true)
	defer recovered.close()
	service.projectResults(recovered)
	updated, err = service.config.Store.Link(ctx, link.ID)
	if err != nil || !reflect.DeepEqual(updated.PendingReportIDs, []string{"awaiting", "notified"}) {
		t.Fatalf("pending notification was discarded after restarting projection: %+v %v", updated, err)
	}
	for _, id := range updated.PendingReportIDs {
		if consumed, err := service.config.TeamTasks.ConsumePendingNotification(ctx, id, 1); err != nil || !consumed {
			t.Fatalf("set direct delivery decision for %s: %v, %v", id, consumed, err)
		}
	}
	service.projectResults(recovered)
	assertProjectionDelivery(t, service, "member-result:"+link.ID+":awaiting", link.ID)
	assertProjectionDelivery(t, service, "member-result:"+link.ID+":notified", link.ID)
	chats.reads = nil
	service.projectResults(recovered)
	if len(chats.reads) != 0 {
		t.Fatalf("settled reports still caused history reads: %v", chats.reads)
	}
}

func prepareScheduledProjection(t *testing.T, s *Service) (*scheduledProjectionChats, *resultProjection) {
	t.Helper()
	s.ctx = t.Context()
	chats := &scheduledProjectionChats{
		events:    map[string][]chat.PublicEvent{},
		messages:  map[string][]messageview.Message{},
		summaries: map[string]models.Chat{},
		origins:   map[string]string{},
	}
	s.config.Chats = chats
	projection := &resultProjection{hub: s.config.Events}
	projection.setActive(true)
	t.Cleanup(projection.close)
	return chats, projection
}

func emitProjectionChange(t *testing.T, s *Service, resource events.Resource) {
	t.Helper()
	if err := events.Emit(s.config.Events, "work-board", "work-board.changed", resource, resource); err != nil {
		t.Fatal(err)
	}
}

func assertProjectionDelivery(t *testing.T, s *Service, requestID, linkID string) {
	t.Helper()
	delivery, exists, err := s.config.Store.FindDelivery(t.Context(), requestID)
	if err != nil || !exists || delivery.LinkID != linkID {
		t.Fatalf("expected dispatch %q for %q, got %+v, %v, %v", requestID, linkID, delivery, exists, err)
	}
}

func assertProjectedExecution(t *testing.T, s *Service, link models.IMConversationLink) {
	t.Helper()
	assertProjectionDelivery(t, s, "result:"+link.ID+":"+link.ID+"-execution", link.ID)
}

// scheduledProjectionChats 返回各个 Chat 相互独立的公开权威事件。
type scheduledProjectionChats struct {
	// Module 提供与本测试无关的会话端口。
	chat.Module
	// events 保存按权威游标依次返回的持久事实。
	events map[string][]chat.PublicEvent
	// messages 以 Chat 为范围保存最终答复或成员报告。
	messages map[string][]messageview.Message
	// summaries 保存各 Chat 当前待处理的交互。
	summaries map[string]models.Chat
	// origins 保存活动及已完成执行的 IM 来源。
	origins map[string]string
	// reads 捕获实际读取的所有 Chat，用来确认闲置历史不再被遍历。
	reads []string
	// failFirst 模拟一次可恢复的权威日志读取故障。
	failFirst bool
}

func (c *scheduledProjectionChats) complete(link models.IMConversationLink, sequence int64) {
	executionID := link.ID + "-execution"
	c.events[link.ChatID] = append(c.events[link.ChatID], chat.PublicEvent{
		Sequence: sequence, Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: executionID,
	})
	c.messages[link.ChatID] = []messageview.Message{{ExecutionID: executionID, Role: constants.MessageRoleAssistant, Content: "answer"}}
	c.origins[executionID] = link.ID
}

func (c *scheduledProjectionChats) ListEvents(_ context.Context, chatID string, after int64, limit int) ([]chat.PublicEvent, error) {
	c.reads = append(c.reads, chatID)
	if c.failFirst {
		c.failFirst = false
		return nil, status_error.ChatLogError
	}
	var page []chat.PublicEvent
	for _, event := range c.events[chatID] {
		if event.Sequence > after {
			page = append(page, event)
			if len(page) == limit {
				break
			}
		}
	}
	return page, nil
}

func (c *scheduledProjectionChats) CountUserMessages(_ context.Context, chatID string) (int64, error) {
	var count int64
	for _, event := range c.events[chatID] {
		if event.Type == constants.ChatEventTypeUserMessageCreated {
			count++
		}
	}
	return count, nil
}

func (c *scheduledProjectionChats) ReadExecution(_ context.Context, chatID, executionID string) (models.Execution, error) {
	return models.Execution{ID: executionID, ChatID: chatID, IMReplyLinkID: c.origins[executionID]}, nil
}

func (c *scheduledProjectionChats) ReadMessageHistory(_ context.Context, chatID string, _ int64, _ int) (chat.MessageHistoryPage, error) {
	return chat.MessageHistoryPage{Resources: c.messages[chatID]}, nil
}

func (c *scheduledProjectionChats) GetChatSummary(_ context.Context, chatID string) (chat.ChatSummary, error) {
	return chat.ChatSummary{Chat: c.summaries[chatID]}, nil
}

// scheduledProjectionTeam 暴露当前有待办的成员，与发送平台独立。
type scheduledProjectionTeam struct {
	// Service 提供与本测试无关的团队端口。
	channel.Service
	// pendingChat 是团队当前有待办的成员 Chat。
	pendingChat string
}

func (s scheduledProjectionTeam) ListPendingInteractions(context.Context, string) ([]channel.PendingTeamInteraction, []channel.TeamMemberActivity, error) {
	if s.pendingChat == "" {
		return nil, nil, nil
	}
	return []channel.PendingTeamInteraction{{ChatID: s.pendingChat}}, nil, nil
}

func TestResultProjectionKeepsDesktopWorkPrivate(t *testing.T) {
	service, _, _ := newIMFixture(t)
	ctx := t.Context()
	link := models.IMConversationLink{ID: "original-im-link", BindingID: "b", ChatID: "shared-chat"}
	if err := service.config.Store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	service.config.Chats = replyProjectionChats{}
	if _, err := service.projectLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	deliveries, total, err := service.config.Store.Deliveries(ctx, constants.IMDeliveryStateUnspecified, 20, 0)
	if err != nil || total != 0 {
		t.Fatalf("desktop response or interaction was queued for IM delivery: %+v, %v", deliveries, err)
	}
	updated, err := service.config.Store.Link(ctx, link.ID)
	if err != nil || updated.EventCursor != 21 {
		t.Fatalf("desktop work stopped the IM cursor: %+v, %v", updated, err)
	}
}

func TestIMReplyProjectionAndApprovalRequireExactLink(t *testing.T) {
	service, _, _ := newIMFixture(t)
	ctx := t.Context()
	original := models.IMConversationLink{ID: "original-im-link", BindingID: "b", ChatID: "shared-chat"}
	unrelated := models.IMConversationLink{ID: "another-im-link", BindingID: "b", ChatID: original.ChatID}
	for _, link := range []models.IMConversationLink{original, unrelated} {
		if err := service.config.Store.SaveLink(ctx, link); err != nil {
			t.Fatal(err)
		}
	}
	service.config.Chats = replyProjectionChats{replyLinkID: original.ID}
	for _, link := range []models.IMConversationLink{unrelated, original, original} {
		if _, err := service.projectLink(ctx, link); err != nil {
			t.Fatal(err)
		}
	}
	deliveries, total, err := service.config.Store.Deliveries(ctx, constants.IMDeliveryStateUnspecified, 20, 0)
	if err != nil || total != 3 {
		t.Fatalf("wanted a result, approval and question only on the original IM link: %+v, %v", deliveries, err)
	}
	for _, delivery := range deliveries {
		if delivery.LinkID != original.ID || delivery.ExecutionID == "" {
			t.Fatalf("delivery has the wrong origin: %+v", delivery)
		}
	}
	binding, err := service.config.Store.Binding(ctx, original.BindingID)
	if err != nil {
		t.Fatal(err)
	}
	executions := &replyProjectionExecutions{}
	service.config.Executions = executions
	if err := service.resolveInteraction(ctx, models.IMInbox{ID: "approval-foreign-link", LinkID: unrelated.ID}, binding, unrelated, "/approve", "approval-id 1"); err != nil || executions.approvalID != "" {
		t.Fatalf("another link resolved the approval: %+v, %v", executions, err)
	}
	if err := service.resolveInteraction(ctx, models.IMInbox{ID: "approval-original-link", LinkID: original.ID}, binding, original, "/approve", "approval-id 1"); err != nil || executions.approvalID != "approval-id" {
		t.Fatalf("original link could not resolve the approval: %+v, %v", executions, err)
	}
}

// replyProjectionChats 提供同一 Chat 中已完成的答复和另一活动回合的交互。
type replyProjectionChats struct {
	// Module 提供当前回归不应触及的其他会话端口。
	chat.Module
	// replyLinkID 是这些测试回合的受信 IM 来源，桌面回合留空。
	replyLinkID string
}

func (replyProjectionChats) ListEvents(context.Context, string, int64, int) ([]chat.PublicEvent, error) {
	return []chat.PublicEvent{{Sequence: 21, Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "completed"}}, nil
}

func (replyProjectionChats) CountUserMessages(context.Context, string) (int64, error) {
	return 1, nil
}

func (c replyProjectionChats) ReadExecution(_ context.Context, chatID, id string) (models.Execution, error) {
	return models.Execution{ID: id, ChatID: chatID, IMReplyLinkID: c.replyLinkID}, nil
}

func (replyProjectionChats) ReadMessageHistory(context.Context, string, int64, int) (chat.MessageHistoryPage, error) {
	return chat.MessageHistoryPage{Resources: []messageview.Message{{ExecutionID: "completed", Role: constants.MessageRoleAssistant, Content: "answer"}}}, nil
}

func (replyProjectionChats) GetChatSummary(context.Context, string) (chat.ChatSummary, error) {
	return chat.ChatSummary{Chat: models.Chat{
		ActiveExecutionID:   "active",
		PendingApprovals:    []models.PendingApproval{{ApprovalID: "approval-id", Title: "approval", Options: []models.PendingApprovalOption{{ID: "ok", Name: "Approve"}}}},
		PendingElicitations: []models.PendingElicitation{{ElicitationID: "question-id", Message: "question"}},
	}}, nil
}

// replyProjectionExecutions 记录真正到达执行内核的 IM 审批命令。
type replyProjectionExecutions struct {
	// Service 提供当前回归不应触及的其他执行端口。
	execution.Service
	// approvalID 是收到的审批标识。
	approvalID string
}

func (f *replyProjectionExecutions) ResolveApproval(_ context.Context, _, approvalID, _ string) error {
	f.approvalID = approvalID
	return nil
}

func TestResultProjectionSkipsDeletedChatWithoutChangingDeliveryHistory(t *testing.T) {
	service, db, _ := newIMFixture(t)
	attachConversationReaders(t, service, db)
	ctx := context.Background()
	chatID := protocol.StableID("chat", "deleted-result")
	if _, err := createAdmissionChat(ctx, service.config.Chats, service.config.DataDirectory, chatID, "waker"); err != nil {
		t.Fatal(err)
	}
	binding, err := service.config.Store.Binding(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	link := models.IMConversationLink{ID: "deleted-result-link", BindingID: binding.ID, ChatID: chatID, Configuration: models.IMConversationConfiguration{WakerID: binding.WakerID, ChannelID: binding.ChannelID, ProjectID: binding.ProjectID, WorkingDirectory: binding.WorkingDirectory, Options: binding.Options}, EventCursor: 17, PendingReportIDs: []string{"previous-report"}}
	if err := service.config.Store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	delivery, err := service.CreateDelivery(ctx, DeliveryRequest{RequestID: "previous-result", TargetID: binding.ID, LinkID: link.ID, ChatID: chatID, Content: "已经确定的结果"})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err = service.config.Store.Delivery(ctx, delivery.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.config.Chats.DeleteChat(ctx, chatID); err != nil {
		t.Fatal(err)
	}
	for tick := 0; tick < 3; tick++ {
		if _, err := service.projectLink(ctx, link); err != nil {
			t.Errorf("第 %d 轮结果同步仍将已删除对话作为可重试错误: %v", tick+1, err)
		}
	}
	retained, err := service.config.Store.Link(ctx, link.ID)
	if err != nil || retained.ChatID != link.ChatID || retained.EventCursor != link.EventCursor || !reflect.DeepEqual(retained.PendingReportIDs, link.PendingReportIDs) {
		t.Fatalf("已删除会话的关联或投递游标被改写: %+v %v", retained, err)
	}
	retainedDelivery, err := service.config.Store.Delivery(ctx, delivery.ID)
	if err != nil || !reflect.DeepEqual(retainedDelivery, delivery) {
		t.Fatalf("原冻结投递事实被改写: %+v %v", retainedDelivery, err)
	}
	deliveries, total, err := service.config.Store.Deliveries(ctx, constants.IMDeliveryStateUnspecified, 20, 0)
	if err != nil || total != 1 || len(deliveries) != 1 {
		t.Fatalf("结果同步重复创建投递: %+v %d %v", deliveries, total, err)
	}
}

func TestResultProjectionDoesNotSkipAuthorityWhenQueryIndexIsMissing(t *testing.T) {
	service, db, _ := newIMFixture(t)
	attachConversationReaders(t, service, db)
	ctx := context.Background()
	chatID := protocol.StableID("chat", "existing-result-authority")
	if _, err := createAdmissionChat(ctx, service.config.Chats, service.config.DataDirectory, chatID, "waker"); err != nil {
		t.Fatal(err)
	}
	link := models.IMConversationLink{ID: "existing-result-link", BindingID: "b", ChatID: chatID}
	if err := service.config.Store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", chatID).Delete(&models.Chat{}).Error; err != nil {
		t.Fatal(err)
	}
	// 事件来自权威日志，摘要来自查询投影；后者缺失仍需报告，不能静默跳过整条关联。
	if _, err := service.projectLink(ctx, link); !hasCode(err, status_error.ChatNotFound) {
		t.Fatalf("查询摘要缺失被忽略或返回了其他故障: %v", err)
	}
	retained, err := service.config.Store.Link(ctx, link.ID)
	if err != nil || retained.EventCursor == 0 || retained.ChatID != chatID {
		t.Fatalf("真实对话未继续同步权威事件: %+v %v", retained, err)
	}
}

// failingResultProjectionChats 在公开事件读取边界模拟明确的日志与数据库故障。
type failingResultProjectionChats struct {
	// Module 提供当前回归不应触及的其他会话端口。
	chat.Module
	// err 是本轮公开事件读取返回的稳定错误。
	err error
}

func (c failingResultProjectionChats) ListEvents(context.Context, string, int64, int) ([]chat.PublicEvent, error) {
	return nil, c.err
}

func TestResultProjectionPreservesReadFailures(t *testing.T) {
	for _, failure := range []status_error.StatusError{status_error.DatabaseError, status_error.ChatLogError, status_error.ChatLogIndexCorrupted} {
		t.Run(failure.Key(), func(t *testing.T) {
			service := New(Config{Chats: failingResultProjectionChats{err: failure}})
			if _, err := service.projectLink(context.Background(), models.IMConversationLink{ChatID: "existing-chat"}); !hasCode(err, failure) {
				t.Fatalf("真实读取故障被当作会话删除: %v", err)
			}
		})
	}
}
