package im

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func (s *Service) projectResults() {
	links, err := s.config.Store.Links(s.ctx, "")
	if err != nil {
		return
	}
	for _, link := range links {
		if s.ctx.Err() != nil {
			return
		}
		if link.ChatID == "" {
			continue
		}
		binding, err := s.config.Store.Binding(s.ctx, link.BindingID)
		if err != nil || binding.State != constants.IMBindingStateApproved {
			continue
		}
		if err := s.projectLink(s.ctx, link); err != nil {
			logx.Errorf("im result projection failed: link_id=%s error=%v", link.ID, err)
		}
	}
}

func (s *Service) projectLink(ctx context.Context, link models.IMConversationLink) error {
	events, err := s.config.Chats.ListEvents(ctx, link.ChatID, link.EventCursor, 100)
	// 删除内部对话后仍保留 IM 关联及投递收据；明确不存在的源不再产生结果。
	if hasCode(err, status_error.ChatNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	cursor := link.EventCursor
	for _, event := range events {
		if event.Transient || event.Sequence <= cursor {
			continue
		}
		switch event.Type {
		case constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionFailed, constants.ChatEventTypeExecutionCanceled:
			message, err := s.resultMessage(ctx, link.ChatID, event.ExecutionID, "")
			if err != nil {
				return err
			}
			content := message.Content
			if event.Type == constants.ChatEventTypeExecutionFailed {
				content += "\n\n本次执行失败，请在 NeuxBot 查看详情。原因：" + event.ErrorCode
			}
			if event.Type == constants.ChatEventTypeExecutionCanceled {
				content += "\n\n本次工作已停止。"
			}
			if strings.TrimSpace(content) != "" {
				if _, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "result:" + link.ID + ":" + event.ExecutionID, TargetID: link.BindingID, LinkID: link.ID, ChatID: link.ChatID, ExecutionID: event.ExecutionID, Content: content}); err != nil {
					return err
				}
			}
		case constants.ChatEventTypeUserMessageCreated:
			if event.Message != nil && event.Message.Actor == string(constants.ChannelMessageActorMemberReport) {
				link.PendingReportIDs = append(link.PendingReportIDs, event.Message.ID)
			}
		}
		cursor = event.Sequence
	}
	link.EventCursor = cursor
	if len(link.PendingReportIDs) > 0 && link.ThreadID != "" {
		notifications, err := s.config.TeamTasks.ReadNotifications(ctx, link.ThreadID, link.PendingReportIDs)
		if err != nil {
			return err
		}
		settled := map[string]bool{}
		for _, notice := range notifications {
			if notice.DirectDelivered {
				message, err := s.resultMessage(ctx, link.ChatID, "", notice.ID)
				if err != nil {
					return err
				}
				if _, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "member-result:" + link.ID + ":" + notice.ID, TargetID: link.BindingID, LinkID: link.ID, ChatID: message.SourceChatID, Content: message.Content}); err != nil {
					return err
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
	if err := s.config.Store.CheckpointLink(ctx, link); err != nil {
		return err
	}
	return s.projectInteractions(ctx, link)
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

func (s *Service) projectInteractions(ctx context.Context, link models.IMConversationLink) error {
	ids, err := s.interactionChats(ctx, link)
	if err != nil {
		return err
	}
	for _, chatID := range ids {
		summary, err := s.config.Chats.GetChatSummary(ctx, chatID)
		if err != nil {
			return err
		}
		for _, approval := range summary.Chat.PendingApprovals {
			content := "需要工具审批：" + approval.Title + "\n" + approval.Summary + "\n编号：" + approval.ApprovalID[:8]
			for index, option := range approval.Options {
				content += "\n" + strconv.Itoa(index+1) + ". " + option.Name
			}
			content += "\n回复 /approve " + approval.ApprovalID[:8] + " <选项编号>，或在 NeuxBot 处理。"
			if _, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "approval:" + link.ID + ":" + chatID + ":" + approval.ApprovalID, TargetID: link.BindingID, LinkID: link.ID, ChatID: chatID, Content: content}); err != nil {
				return err
			}
		}
		for _, question := range summary.Chat.PendingElicitations {
			content := "需要补充信息：" + question.Message + "\n编号：" + question.ElicitationID[:8] + "\n请在 NeuxBot 回答。简单表单也可回复 /answer " + question.ElicitationID[:8] + " <JSON答案>。"
			if _, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "question:" + link.ID + ":" + chatID + ":" + question.ElicitationID, TargetID: link.BindingID, LinkID: link.ID, ChatID: chatID, Content: content}); err != nil {
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
		summary, err := s.config.Chats.GetChatSummary(ctx, chatID)
		if err != nil {
			return err
		}
		if command == "/approve" {
			for _, approval := range summary.Chat.PendingApprovals {
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
			for _, question := range summary.Chat.PendingElicitations {
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
