package channel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
)

const (
	channelInteractionProjectionBatchSize  = 500
	channelInteractionProjectionShardCount = 32
)

// PendingTeamInteraction 是 Team 页面处理所有成员 Runtime 交互的统一安全投影。
type PendingTeamInteraction struct {
	// ID 是审批或结构化输入在 Chat 范围内的稳定标识。
	ID string `json:"id"`
	// ThreadID 是交互所属团队对话标识。
	ThreadID string `json:"threadId"`
	// ChatID 是实际等待 Runtime 响应的成员 Chat 标识。
	ChatID string `json:"chatId"`
	// WakerID 是发起交互的团队成员伙伴标识。
	WakerID string `json:"wakerId"`
	// ExecutionID 是交互所属的当前成员执行标识。
	ExecutionID string `json:"executionId"`
	// Type 区分 approval 与 elicitation。
	Type string `json:"type"`
	// CreatedAt 是请求进入 Chat 权威日志的 Unix 毫秒时间。
	CreatedAt int64 `json:"createdAt"`
	// CreatedSequence 是请求在来源 Chat 权威日志中的创建序号。
	CreatedSequence int64 `json:"createdSequence"`
	// Approval 是审批请求的安全载荷，其他类型为空。
	Approval *models.PendingApproval `json:"approval,omitempty"`
	// Elicitation 是结构化输入请求的安全载荷，其他类型为空。
	Elicitation *models.PendingElicitation `json:"elicitation,omitempty"`
}

// TeamMemberActivity 是 Team 页面展示成员运行或等待用户操作状态的统一投影。
type TeamMemberActivity struct {
	// WakerID 是成员伙伴标识。
	WakerID string `json:"wakerId"`
	// ChatID 是成员在当前团队对话中的固定 Chat 标识。
	ChatID string `json:"chatId"`
	// ExecutionID 是当前活跃执行标识；空表示 idle。
	ExecutionID string `json:"executionId,omitempty"`
	// State 是 idle、running 或 requires_action。
	State constants.TeamMemberActivityState `json:"state"`
}

// teamInteractionEventPayload 是 Chat 交互事实写入团队事件流的来源信息和安全载荷。
type teamInteractionEventPayload struct {
	// ThreadID 是交互所属团队对话标识。
	ThreadID string `json:"threadId"`
	// ChatID 是来源成员 Chat 标识。
	ChatID string `json:"chatId"`
	// WakerID 是来源成员伙伴标识。
	WakerID string `json:"wakerId"`
	// ExecutionID 是来源执行标识。
	ExecutionID string `json:"executionId"`
	// ChatSequence 是来源 Chat 的权威事件序号。
	ChatSequence int64 `json:"chatSequence"`
	// ApprovalRequest 是审批请求安全载荷。
	ApprovalRequest *chat.PublicApprovalRequest `json:"approvalRequest,omitempty"`
	// ApprovalResolution 是审批处理安全载荷。
	ApprovalResolution *chat.PublicApprovalResolution `json:"approvalResolution,omitempty"`
	// ElicitationRequest 是结构化输入请求安全载荷。
	ElicitationRequest *chat.PublicElicitationRequest `json:"elicitationRequest,omitempty"`
	// ElicitationResolution 是结构化输入处理安全载荷。
	ElicitationResolution *chat.PublicElicitationResolution `json:"elicitationResolution,omitempty"`
}

// ListPendingInteractions 聚合负责人和成员 Chat 尚待用户处理的审批与结构化输入。
func (s *service) ListPendingInteractions(
	ctx context.Context,
	threadID string,
) ([]PendingTeamInteraction, []TeamMemberActivity, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, nil, status_error.BadRequest
	}
	members, err := s.threads.ListMembers(ctx, threadID)
	if err != nil {
		return nil, nil, err
	}
	interactions := make([]PendingTeamInteraction, 0)
	activities := make([]TeamMemberActivity, 0, len(members))
	for _, member := range members {
		summary, summaryErr := s.chats.GetChatSummary(ctx, member.ChatID)
		if summaryErr != nil {
			return nil, nil, summaryErr
		}
		state := constants.TeamMemberActivityStateIdle
		if summary.Chat.ActiveExecutionID != "" {
			state = constants.TeamMemberActivityStateRunning
		}
		if len(summary.Chat.PendingApprovals) > 0 || len(summary.Chat.PendingElicitations) > 0 {
			state = constants.TeamMemberActivityStateRequiresAction
		}
		activities = append(activities, TeamMemberActivity{
			WakerID:     member.WakerID,
			ChatID:      member.ChatID,
			ExecutionID: summary.Chat.ActiveExecutionID,
			State:       state,
		})
		for index := range summary.Chat.PendingApprovals {
			approval := summary.Chat.PendingApprovals[index]
			interactions = append(interactions, PendingTeamInteraction{
				ID:              approval.ApprovalID,
				ThreadID:        threadID,
				ChatID:          member.ChatID,
				WakerID:         member.WakerID,
				ExecutionID:     summary.Chat.ActiveExecutionID,
				Type:            "approval",
				CreatedAt:       approval.RequestedAt,
				CreatedSequence: approval.RequestedSequence,
				Approval:        &approval,
			})
		}
		for index := range summary.Chat.PendingElicitations {
			elicitation := summary.Chat.PendingElicitations[index]
			interactions = append(interactions, PendingTeamInteraction{
				ID:              elicitation.ElicitationID,
				ThreadID:        threadID,
				ChatID:          member.ChatID,
				WakerID:         member.WakerID,
				ExecutionID:     summary.Chat.ActiveExecutionID,
				Type:            "elicitation",
				CreatedAt:       elicitation.RequestedAt,
				CreatedSequence: elicitation.RequestedSequence,
				Elicitation:     &elicitation,
			})
		}
	}
	sort.SliceStable(interactions, func(i, j int) bool {
		if interactions[i].CreatedAt != interactions[j].CreatedAt {
			return interactions[i].CreatedAt < interactions[j].CreatedAt
		}
		return interactions[i].ID < interactions[j].ID
	})
	return interactions, activities, nil
}

// SyncInteractionEvents 把成员 Chat 的交互事实幂等投影到团队持久事件流。
func (s *service) SyncInteractionEvents(ctx context.Context, threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return status_error.BadRequest
	}
	thread, err := s.threads.Get(ctx, threadID)
	if err != nil {
		return err
	}
	members, err := s.threads.ListMembers(ctx, threadID)
	if err != nil {
		return err
	}
	for _, member := range members {
		unlock := s.lockInteractionProjection(member.ChatID)
		after := s.interactionCursor(member.ChatID)
		for {
			events, listErr := s.chats.ListEvents(ctx, member.ChatID, after, channelInteractionProjectionBatchSize)
			if listErr != nil {
				unlock()
				return listErr
			}
			for _, event := range events {
				if event.Sequence <= after {
					continue
				}
				if mirrorErr := s.mirrorInteractionEvent(ctx, thread, member, event); mirrorErr != nil {
					unlock()
					return mirrorErr
				}
				after = event.Sequence
			}
			s.setInteractionCursor(member.ChatID, after)
			if len(events) < channelInteractionProjectionBatchSize {
				break
			}
		}
		unlock()
	}
	return nil
}

// mirrorInteractionEvent 只持久化审批和结构化输入请求/处理事实。
func (s *service) mirrorInteractionEvent(
	ctx context.Context,
	thread models.ChannelThread,
	member models.ChannelThreadMember,
	event chat.PublicEvent,
) error {
	eventType := constants.ChannelEventTypeUnspecified
	payload := teamInteractionEventPayload{
		ThreadID:              thread.ID,
		ChatID:                member.ChatID,
		WakerID:               member.WakerID,
		ExecutionID:           interactionExecutionID(event),
		ChatSequence:          event.Sequence,
		ApprovalRequest:       event.ApprovalRequest,
		ApprovalResolution:    event.ApprovalResolution,
		ElicitationRequest:    event.ElicitationRequest,
		ElicitationResolution: event.ElicitationResolution,
	}
	switch event.Type {
	case constants.ChatEventTypeApprovalRequested:
		eventType = constants.ChannelEventApprovalRequested
	case constants.ChatEventTypeApprovalResolved:
		eventType = constants.ChannelEventApprovalResolved
	case constants.ChatEventTypeElicitationRequested:
		eventType = constants.ChannelEventElicitationRequested
	case constants.ChatEventTypeElicitationResolved:
		eventType = constants.ChannelEventElicitationResolved
	default:
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return status_error.InternalServerError
	}
	_, err = s.appendPreparedEvent(ctx, models.ChannelEvent{
		ID:        interactionEventID(thread.ID, member.ChatID, event.Sequence, eventType),
		ChannelID: thread.ChannelID,
		ThreadID:  thread.ID,
		Type:      eventType,
		Payload:   data,
		CreatedAt: event.RecordedAt,
	})
	return err
}

// interactionExecutionID 从公开事件的消息或交互所在执行状态提取安全执行标识。
func interactionExecutionID(event chat.PublicEvent) string {
	return event.ExecutionID
}

// interactionEventID 从来源游标生成稳定 32 字节十六进制事件标识，保证重放幂等。
func interactionEventID(threadID string, chatID string, sequence int64, eventType constants.ChannelEventType) string {
	digest := sha256.Sum256([]byte(threadID + "\x00" + chatID + "\x00" + string(eventType) + "\x00" + strconv.FormatInt(sequence, 10)))
	return hex.EncodeToString(digest[:16])
}
