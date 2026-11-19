package im

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const projectionEventPage = 100

// resultProjection 保存本次 Core 需要补读的来源，始终从持久的 IM 游标恢复。
type resultProjection struct {
	// hub 提供提交后变化提示以及订阅溢出的恢复信号。
	hub *events.Hub
	// subscriptions 随 IM 后台循环释放。
	subscriptions map[string]events.Subscription
	// pending 合并同一来源的提示和重试，无任何连接配置时为空引用。
	pending map[events.Resource]projectionSweep
}

type projectionSweep struct {
	// afterID 是已处理页面的最后一个会话关联标识。
	afterID string
	// restart 保存扫描开始后收到的新提示，待本次分页结束后再次扫描。
	restart bool
}

// setActive 只在存在 IM 连接时接收工作看板变化；停用的连接仍有历史需要刷新。
func (p *resultProjection) setActive(active bool) {
	if active == (p.pending != nil) {
		return
	}
	if !active {
		p.close()
		p.subscriptions = nil
		p.pending = nil
		return
	}
	if p.hub != nil {
		p.subscriptions = map[string]events.Subscription{
			"im":         p.hub.Subscribe("im", events.Resource{}),
			"work-board": p.hub.Subscribe("work-board", events.Resource{}),
		}
	}
	p.pending = map[events.Resource]projectionSweep{{Kind: "global"}: {}}
}

func (p *resultProjection) close() {
	for _, subscription := range p.subscriptions {
		subscription.Close()
	}
}

func (p *resultProjection) wake(source events.Resource) {
	sweep := p.pending[source]
	sweep.restart = sweep.afterID != ""
	p.pending[source] = sweep
}

func (p *resultProjection) collect(topic string, subscription events.Subscription) ([]string, bool) {
	var changedChats []string
	refreshHistory := false
	for range events.BufferSize {
		select {
		case event, ok := <-subscription.Events:
			if !ok {
				subscription.Close()
				select {
				case <-p.hub.Done():
					delete(p.subscriptions, topic)
				default:
					p.subscriptions[topic] = p.hub.Subscribe(topic, events.Resource{})
					p.wake(events.Resource{Kind: "global"})
					refreshHistory = refreshHistory || topic == "work-board"
				}
				return changedChats, refreshHistory
			}
			if source, ok := projectionSource(event); ok {
				p.wake(source)
				if topic == "work-board" && event.Type == "work-board.changed" && !event.Transient {
					switch source.Kind {
					case "chat":
						changedChats = append(changedChats, source.ID)
					case "global":
						refreshHistory = true
					}
				}
			}
		default:
			return changedChats, refreshHistory
		}
	}
	return changedChats, refreshHistory
}

func projectionSource(event events.Event) (events.Resource, bool) {
	if event.Topic == "im" {
		var source events.Resource
		if json.Unmarshal(event.Payload, &source) == nil {
			switch source.Kind {
			case "binding", "conversation", constants.IMConnectionConfigurationResourceKind:
				return source, source.ID != ""
			}
		}
		return events.Resource{}, false
	}
	switch event.Resource.Kind {
	case "chat", "thread", "team_thread":
		return event.Resource, event.Resource.ID != ""
	case "global":
		var change struct {
			// Reason 是原 Chat 查询投影恢复后发布的变化原因。
			Reason string `json:"reason"`
		}
		if json.Unmarshal(event.Payload, &change) == nil && change.Reason == constants.ChatReadingRecoveredReason {
			return event.Resource, true
		}
	}
	return events.Resource{}, false
}

func (s *Service) projectResults(projection *resultProjection) {
	var changedChats []string
	refreshHistory := false
	for topic, subscription := range projection.subscriptions {
		chats, refresh := projection.collect(topic, subscription)
		changedChats = append(changedChats, chats...)
		refreshHistory = refreshHistory || refresh
	}
	var connections []string
	if !refreshHistory && len(changedChats) > 0 {
		slices.Sort(changedChats)
		changedChats = slices.Compact(changedChats)
		var err error
		connections, err = s.config.Store.ConversationConnections(s.ctx, changedChats)
		if err != nil {
			refreshHistory = true
		} else if len(connections) == 0 {
			// 批量查找已确认这些 Chat 均未关联 IM，无需再逐个查找投递关系。
			for _, id := range changedChats {
				delete(projection.pending, events.Resource{Kind: "chat", ID: id})
			}
		}
	}
	s.publishConversationChanges(connections, refreshHistory)
	if len(projection.pending) == 0 {
		return
	}
	pending := projection.pending
	projection.pending = map[events.Resource]projectionSweep{}
	seen := map[string]bool{}
	for source, sweep := range pending {
		if s.ctx.Err() != nil {
			return
		}
		links, err := s.config.Store.ProjectionLinks(s.ctx, source, sweep.afterID, constants.IMPageSize)
		if err != nil {
			projection.pending[source] = sweep
			logx.Errorf("im projection lookup failed: resource=%s id=%s error=%v", source.Kind, source.ID, err)
			continue
		}
		if len(links) == constants.IMPageSize {
			sweep.afterID = links[len(links)-1].ID
			projection.pending[source] = sweep
		} else if sweep.restart {
			projection.pending[source] = projectionSweep{}
		}
		for _, link := range links {
			if s.ctx.Err() != nil {
				return
			}
			if seen[link.ID] {
				continue
			}
			seen[link.ID] = true
			again, err := s.projectLink(s.ctx, link)
			if err != nil {
				logx.Errorf("im result projection failed: link_id=%s error=%v", link.ID, err)
			}
			if err != nil || again {
				projection.wake(events.Resource{Kind: "conversation", ID: link.ID})
			}
		}
	}
}

// publishConversationChanges 让已提交的聊天变化刷新对应连接的会话统计，覆盖停用的连接。
func (s *Service) publishConversationChanges(connections []string, refreshAll bool) {
	if refreshAll {
		resource := events.Resource{Kind: "global"}
		_ = events.Emit(s.config.Events, "im", constants.IMConversationsChangedEventType, resource, resource)
		return
	}
	for _, connectionID := range connections {
		resource := events.Resource{Kind: "connection", ID: connectionID}
		_ = events.Emit(s.config.Events, "im", constants.IMConversationsChangedEventType, resource, resource)
	}
}

func (s *Service) projectLink(ctx context.Context, link models.IMConversationLink) (bool, error) {
	events, err := s.config.Chats.ListEvents(ctx, link.ChatID, link.EventCursor, projectionEventPage)
	// 删除内部对话后仍保留 IM 关联及投递收据；明确不存在的源不再产生结果。
	if hasCode(err, status_error.ChatNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	cursor := link.EventCursor
	previousReports := link.PendingReportIDs
	for _, event := range events {
		if event.Transient || event.Sequence <= cursor {
			continue
		}
		switch event.Type {
		case constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionFailed, constants.ChatEventTypeExecutionCanceled:
			matches, err := s.executionRepliesTo(ctx, link, link.ChatID, event.ExecutionID)
			if err != nil {
				return false, err
			}
			if !matches {
				break
			}
			message, err := s.resultMessage(ctx, link.ChatID, event.ExecutionID, "")
			if err != nil {
				return false, err
			}
			status := ""
			if event.Type == constants.ChatEventTypeExecutionFailed {
				status = "\n\n本次执行失败，请在 NeuxBot 查看详情。原因：" + event.ErrorCode
			}
			if event.Type == constants.ChatEventTypeExecutionCanceled {
				status = "\n\n本次工作已停止。"
			}
			if content := projectedReplyContent(message.Content, status); content != "" {
				if _, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "result:" + link.ID + ":" + event.ExecutionID, TargetID: link.BindingID, LinkID: link.ID, ChatID: link.ChatID, ExecutionID: event.ExecutionID, Content: content}); err != nil {
					return false, err
				}
			}
		case constants.ChatEventTypeUserMessageCreated:
			if event.Message != nil && event.Message.Actor == string(constants.ChannelMessageActorMemberReport) {
				link.PendingReportIDs = append(link.PendingReportIDs, event.Message.ID)
			}
		}
		cursor = event.Sequence
	}
	if len(link.PendingReportIDs) > 0 && link.ThreadID != "" {
		notifications, err := s.config.TeamTasks.ReadNotifications(ctx, link.ThreadID, link.PendingReportIDs)
		if err != nil {
			return false, err
		}
		settled := map[string]bool{}
		if len(notifications) < len(link.PendingReportIDs) {
			abandoned, err := s.config.TeamTasks.ListAbandonedReportIDs(ctx, link.ThreadID, link.PendingReportIDs)
			if err != nil {
				return false, err
			}
			for _, id := range abandoned {
				settled[id] = true
			}
		}
		for _, notice := range notifications {
			if notice.IMReplyLinkID != link.ID {
				settled[notice.ID] = true
			} else if notice.DirectDelivered {
				message, err := s.resultMessage(ctx, link.ChatID, "", notice.ID)
				if err != nil {
					return false, err
				}
				if _, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "member-result:" + link.ID + ":" + notice.ID, TargetID: link.BindingID, LinkID: link.ID, ChatID: message.SourceChatID, Content: projectedReplyContent(message.Content, "")}); err != nil {
					return false, err
				}
				settled[notice.ID] = true
			} else if notice.Status == constants.ChannelNotificationStatusConsumed {
				settled[notice.ID] = true
			}
		}
		pending := []string{}
		for _, id := range link.PendingReportIDs {
			if !settled[id] {
				pending = append(pending, id)
			}
		}
		link.PendingReportIDs = pending
	}
	if cursor != link.EventCursor || !slices.Equal(previousReports, link.PendingReportIDs) {
		link.EventCursor = cursor
		if err := s.config.Store.CheckpointLink(ctx, link); err != nil {
			return false, err
		}
	}
	// 成员报告是否直达可在主 Chat 的最后一个事件之后才由团队事务决定。
	return len(events) == projectionEventPage || (link.ThreadID != "" && len(link.PendingReportIDs) > 0), s.projectInteractions(ctx, link)
}

// projectedReplyContent 为自动回复保留有界预览，稳定生成相同的 Outbox 正文。
func projectedReplyContent(body, status string) string {
	content := strings.TrimSpace(body + status)
	if len(content) <= constants.IMDeliveryTextMaximumBytes {
		return content
	}
	ending := "\n\n回复内容较长，已省略部分。请在 NeuxBot 查看完整内容。" + status
	end := constants.IMDeliveryTextMaximumBytes - len(ending)
	for !utf8.RuneStart(content[end]) {
		end--
	}
	return strings.TrimSpace(content[:end]) + ending
}

func (s *Service) resultMessage(ctx context.Context, chatID, executionID, messageID string) (messageview.Message, error) {
	cursor := int64(0)
	for {
		page, err := s.config.Chats.ReadMessageHistory(ctx, chatID, cursor, 100)
		if err != nil {
			return messageview.Message{}, err
		}
		for _, message := range page.Resources {
			if messageID != "" && message.ID == messageID {
				return message, nil
			}
			if executionID != "" && message.ExecutionID == executionID && message.Role == constants.MessageRoleAssistant {
				return message, nil
			}
		}
		if !page.HasMore {
			return messageview.Message{}, nil
		}
		cursor = page.NextBeforeSequence
	}
}

func (s *Service) interactionChats(ctx context.Context, link models.IMConversationLink) ([]string, error) {
	ids := []string{link.ChatID}
	if link.ThreadID != "" {
		interactions, _, err := s.config.Teams.ListPendingInteractions(ctx, link.ThreadID)
		if err != nil {
			return nil, err
		}
		ids = []string{}
		seen := map[string]bool{}
		for _, interaction := range interactions {
			if !seen[interaction.ChatID] {
				ids = append(ids, interaction.ChatID)
				seen[interaction.ChatID] = true
			}
		}
	}
	return ids, nil
}

// executionRepliesTo 从已接纳回合读取投递来源，关联 Chat 本身不授予回传权限。
func (s *Service) executionRepliesTo(ctx context.Context, link models.IMConversationLink, chatID, executionID string) (bool, error) {
	if executionID == "" || link.ID == "" {
		return false, nil
	}
	execution, err := s.config.Chats.ReadExecution(ctx, chatID, executionID)
	return err == nil && execution.IMReplyLinkID == link.ID, err
}

// pendingReplyInteractions 把交互展示和 IM 命令统一限制在本会话发起的活动回合。
func (s *Service) pendingReplyInteractions(ctx context.Context, link models.IMConversationLink, chatID string) (*models.Chat, error) {
	summary, err := s.config.Chats.GetChatSummary(ctx, chatID)
	if err != nil || len(summary.Chat.PendingApprovals)+len(summary.Chat.PendingElicitations) == 0 {
		return nil, err
	}
	matches, err := s.executionRepliesTo(ctx, link, chatID, summary.Chat.ActiveExecutionID)
	if err != nil || !matches {
		return nil, err
	}
	return &summary.Chat, nil
}

func (s *Service) projectInteractions(ctx context.Context, link models.IMConversationLink) error {
	ids, err := s.interactionChats(ctx, link)
	if err != nil {
		return err
	}
	for _, chatID := range ids {
		pending, err := s.pendingReplyInteractions(ctx, link, chatID)
		if err != nil {
			return err
		}
		if pending == nil {
			continue
		}
		for _, approval := range pending.PendingApprovals {
			content := "需要工具审批：" + approval.Title + "\n" + approval.Summary + "\n编号：" + approval.ApprovalID[:8]
			for index, option := range approval.Options {
				content += "\n" + strconv.Itoa(index+1) + ". " + option.Name
			}
			content += "\n回复 /approve " + approval.ApprovalID[:8] + " <选项编号>，或在 NeuxBot 处理。"
			if _, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "approval:" + link.ID + ":" + chatID + ":" + approval.ApprovalID, TargetID: link.BindingID, LinkID: link.ID, ChatID: chatID, ExecutionID: pending.ActiveExecutionID, Content: content}); err != nil {
				return err
			}
		}
		for _, question := range pending.PendingElicitations {
			content := "需要补充信息：" + question.Message + "\n编号：" + question.ElicitationID[:8] + "\n请在 NeuxBot 回答。简单表单也可回复 /answer " + question.ElicitationID[:8] + " <JSON答案>。"
			if _, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "question:" + link.ID + ":" + chatID + ":" + question.ElicitationID, TargetID: link.BindingID, LinkID: link.ID, ChatID: chatID, ExecutionID: pending.ActiveExecutionID, Content: content}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) resolveInteraction(ctx context.Context, input models.IMInbox, binding models.IMBinding, link models.IMConversationLink, command, args string) error {
	parts := strings.SplitN(strings.TrimSpace(args), " ", 2)
	if len(parts) != 2 || len(parts[0]) < 8 {
		return s.finishInput(ctx, input, binding, "请提供完整命令、审批或问答编号，以及要提交的选项或答案。")
	}
	ids, err := s.interactionChats(ctx, link)
	if err != nil {
		return err
	}
	matched := 0
	targetChat := ""
	interactionID := ""
	optionID := ""
	for _, chatID := range ids {
		pending, err := s.pendingReplyInteractions(ctx, link, chatID)
		if err != nil {
			return err
		}
		if pending == nil {
			continue
		}
		if command == "/approve" {
			for _, approval := range pending.PendingApprovals {
				if strings.HasPrefix(approval.ApprovalID, parts[0]) {
					index, err := strconv.Atoi(parts[1])
					if err != nil || index < 1 || index > len(approval.Options) {
						return s.finishInput(ctx, input, binding, "选项编号无效，请按审批消息中的编号回复。")
					}
					matched++
					targetChat = chatID
					interactionID = approval.ApprovalID
					optionID = approval.Options[index-1].ID
				}
			}
		} else {
			for _, question := range pending.PendingElicitations {
				if strings.HasPrefix(question.ElicitationID, parts[0]) && question.Mode == "form" {
					matched++
					targetChat = chatID
					interactionID = question.ElicitationID
				}
			}
		}
	}
	if matched != 1 {
		return s.finishInput(ctx, input, binding, "该交互已结束或编号无法唯一匹配，请在 NeuxBot 查看当前待处理项。")
	}
	if command == "/approve" {
		err = s.config.Executions.ResolveApproval(ctx, targetChat, interactionID, optionID)
	} else {
		var answer map[string]any
		if err := json.Unmarshal([]byte(parts[1]), &answer); err != nil || answer == nil {
			return s.finishInput(ctx, input, binding, "答案需为符合表单的 JSON 对象。复杂表单请在 NeuxBot 回答。")
		}
		err = s.config.Executions.ResolveElicitation(ctx, targetChat, interactionID, answer, false)
	}
	if err != nil {
		return err
	}
	return s.finishInput(ctx, input, binding, "已提交。工作将按所选结果继续。")
}
