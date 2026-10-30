package channel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

const (
	// exhaustedTimelineSequence 标记一个 Chat 在当前游标快照中已没有更早消息。
	exhaustedTimelineSequence int64 = -1
)

// TimelineMessage 是团队主会话从一份权威 Chat JSONL 投影出的公开消息段。
type TimelineMessage struct {
	// Message 是原 Chat 重放得到的公开消息，不改变原始内容与执行状态。
	Message messageview.Message `json:"message"`
	// ChatID 是消息所属的权威 Chat 标识。
	ChatID string `json:"chatId"`
	// WakerID 是 Assistant 消息实际执行伙伴的标识；用户消息为空。
	WakerID string `json:"wakerId,omitempty"`
	// Coordinator 表示消息来自团队负责人使用的主 Chat；新线程中它同时是负责人成员 Chat。
	Coordinator bool `json:"coordinator"`
	// DirectDelivered 表示这条成员消息已直接作为完整答复交给用户，使用普通消息样式。
	DirectDelivered bool `json:"directDelivered,omitempty"`
}

// TimelineQuery 是团队公开时间线的一次游标读取条件。
type TimelineQuery struct {
	// ThreadID 是需要读取负责人主 Chat 与临时用户输入的团队对话标识。
	ThreadID string
	// Cursor 是上一页返回的不可解释游标；空值表示从最新消息读取。
	Cursor string
	// Limit 是本次最多返回的公开消息数，范围为一到一百。
	Limit int
}

// TimelinePage 是按时间升序返回的一页团队公开消息。
type TimelinePage struct {
	// LeadChatLatestSequence 是主 Chat 最新窗口实际覆盖的持久游标；旧页或未读取该来源时为空。
	LeadChatLatestSequence *int64
	// Resources 是本页从旧到新排列的公开消息段。
	Resources []TimelineMessage
	// NextCursor 是继续读取更早消息时原样回传的不透明游标。
	NextCursor string
	// HasMore 表示当前页之前仍可能存在更早的公开消息。
	HasMore bool
}

// timelineCursor 保存每份权威 Chat 的独立 JSONL 位置和临时输入位置。
type timelineCursor struct {
	// BeforeByChat 按 Chat 标识记录下一页读取使用的消息创建序号。
	BeforeByChat map[string]int64 `json:"beforeByChat"`
	// PendingBeforeCreatedAt 是下一页临时输入必须早于的 Unix 毫秒时间。
	PendingBeforeCreatedAt int64 `json:"pendingBeforeCreatedAt,omitempty"`
	// PendingBeforeRole 是同毫秒临时输入保持用户先于负责人排序的角色边界。
	PendingBeforeRole constants.MessageRole `json:"pendingBeforeRole,omitempty"`
	// PendingBeforeID 是临时输入时间和角色相同时使用的稳定标识边界。
	PendingBeforeID string `json:"pendingBeforeId,omitempty"`
	// PendingExhausted 表示当前游标快照已没有更早的临时用户输入。
	PendingExhausted bool `json:"pendingExhausted,omitempty"`
}

// timelineSourcePage 是一份 Chat JSONL 在本轮读取到的原始窗口和公开投影。
type timelineSourcePage struct {
	// ChatID 是原始窗口所属的权威 Chat 标识。
	ChatID string
	// Page 是 Chat 服务按创建序号返回的原始消息窗口。
	Page chatservice.MessageHistoryPage
	// Resources 是从原始窗口过滤出的公开团队消息。
	Resources []TimelineMessage
}

// timelineCandidate 标记一条公开消息来自 Chat JSONL 还是尚未提交的耐久输入。
type timelineCandidate struct {
	// Resource 是参与全局时间排序的公开团队消息。
	Resource TimelineMessage
	// Pending 表示消息是任务域对尚未提交 Chat JSONL 的用户输入临时投影。
	Pending bool
}

// ReadTimeline 按时间读取负责人主 Chat 与尚未持久投递输入的一页公开消息。
func (s *service) ReadTimeline(ctx context.Context, input TimelineQuery) (TimelinePage, error) {
	input.ThreadID = strings.TrimSpace(input.ThreadID)
	input.Cursor = strings.TrimSpace(input.Cursor)
	if input.ThreadID == "" || input.Limit < 1 || input.Limit > constants.ChannelTimelineMaxLimit {
		return TimelinePage{}, status_error.BadRequest
	}
	cursor, err := decodeTimelineCursor(input.Cursor)
	if err != nil {
		return TimelinePage{}, err
	}
	thread, err := s.threads.Get(ctx, input.ThreadID)
	if err != nil {
		return TimelinePage{}, err
	}
	members, err := s.threads.ListMembers(ctx, thread.ID)
	if err != nil {
		return TimelinePage{}, err
	}

	sources := make([]timelineSourcePage, 0, 1)
	candidates := make([]timelineCandidate, 0, input.Limit+len(members))
	var leadChatLatestSequence *int64
	readSource := func(chatID string, wakerID string, coordinator bool) error {
		beforeSequence := cursor.BeforeByChat[chatID]
		if beforeSequence == exhaustedTimelineSequence {
			return nil
		}
		page, readErr := s.chats.ReadMessageHistory(
			ctx,
			chatID,
			beforeSequence,
			input.Limit,
		)
		if readErr != nil {
			return readErr
		}
		if coordinator && beforeSequence == 0 {
			sequence := page.LatestSequence
			leadChatLatestSequence = &sequence
		}
		resources := appendTimelineMessages(nil, page.Resources, chatID, wakerID, coordinator)
		sources = append(sources, timelineSourcePage{
			ChatID:    chatID,
			Page:      page,
			Resources: resources,
		})
		for _, resource := range resources {
			candidates = append(candidates, timelineCandidate{Resource: resource})
		}
		return nil
	}
	if err := readSource(thread.LeadChatID, thread.LeadWakerID, true); err != nil {
		return TimelinePage{}, err
	}
	pendingResources, err := s.pendingTimelineMessages(ctx, thread, members, cursor, input.Limit+1)
	if err != nil {
		return TimelinePage{}, err
	}
	for _, resource := range pendingResources {
		candidates = append(candidates, timelineCandidate{Resource: resource, Pending: true})
	}
	sort.SliceStable(candidates, func(left int, right int) bool {
		return timelineMessageLess(candidates[left].Resource, candidates[right].Resource)
	})

	selectedStart := len(candidates) - input.Limit
	if selectedStart < 0 {
		selectedStart = 0
	}
	selected := candidates[selectedStart:]
	resources := make([]TimelineMessage, 0, len(selected))
	for _, candidate := range selected {
		resources = append(resources, candidate.Resource)
	}
	reportIDs := make([]string, 0)
	for _, resource := range resources {
		if resource.Message.Actor == string(constants.ChannelMessageActorMemberReport) {
			reportIDs = append(reportIDs, resource.Message.ID)
		}
	}
	directIDs, err := s.tasks.ListDirectDeliveredNotificationIDs(ctx, thread.ID, reportIDs)
	if err != nil {
		return TimelinePage{}, err
	}
	directByID := make(map[string]struct{}, len(directIDs))
	for _, id := range directIDs {
		directByID[id] = struct{}{}
	}
	for index := range resources {
		if resources[index].Message.Actor != string(constants.ChannelMessageActorMemberReport) {
			continue
		}
		if _, direct := directByID[resources[index].Message.ID]; direct {
			resources[index].DirectDelivered = true
		}
	}
	nextCursor, hasMore, err := nextTimelineCursor(cursor, sources, candidates, selected)
	if err != nil {
		return TimelinePage{}, err
	}
	return TimelinePage{
		LeadChatLatestSequence: leadChatLatestSequence,
		Resources:              resources,
		NextCursor:             nextCursor,
		HasMore:                hasMore,
	}, nil
}

// pendingTimelineMessages 投影尚未进入成员 Chat 的显式提及与任务补充。
func (s *service) pendingTimelineMessages(
	ctx context.Context,
	thread models.ChannelThread,
	members []models.ChannelThreadMember,
	cursor timelineCursor,
	limit int,
) ([]TimelineMessage, error) {
	if cursor.PendingExhausted {
		return nil, nil
	}
	tasks, err := s.tasks.ListByThread(ctx, thread.ID)
	if err != nil {
		return nil, err
	}
	membersByWakerID := make(map[string]models.ChannelThreadMember, len(members))
	for _, member := range members {
		membersByWakerID[member.WakerID] = member
	}
	result := make([]TimelineMessage, 0)
	tasksByID := make(map[string]models.ChannelTask, len(tasks))
	taskIDs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		tasksByID[task.ID] = task
		member, exists := membersByWakerID[task.AssigneeWakerID]
		if exists {
			taskIDs = append(taskIDs, task.ID)
		}
		if exists && task.CurrentFence == 0 && task.DirectInput != nil {
			result = appendPendingWorkInput(result, "channel-task-input:"+task.ID, member.ChatID, "", task.CreatedAt, *task.DirectInput)
		}
	}
	beforeAt, beforeID := cursor.PendingBeforeCreatedAt, cursor.PendingBeforeID
	if strings.HasPrefix(beforeID, "channel-task-follow-up:") {
		beforeID = strings.TrimPrefix(beforeID, "channel-task-follow-up:")
	} else {
		beforeID = ""
	}
	followUps, err := s.tasks.ListTimelineFollowUps(ctx, thread.ID, taskIDs, beforeAt, cursor.PendingBeforeRole, beforeID, limit)
	if err != nil {
		return nil, err
	}
	for _, followUp := range followUps {
		task, exists := tasksByID[followUp.TaskID]
		if !exists {
			continue
		}
		member, exists := membersByWakerID[task.AssigneeWakerID]
		if !exists {
			continue
		}
		actor := followUp.Actor
		if actor == "" {
			actor = constants.ChannelMessageActorUser
		}
		authorWakerID := ""
		if actor == constants.ChannelMessageActorLead {
			authorWakerID = thread.LeadWakerID
		}
		payload := followUp.Payload
		if strings.TrimSpace(payload.Content) == "" {
			payload = execution.WorkInput{Actor: string(actor), Content: followUp.Text}
		}
		result = appendPendingWorkInput(result, "channel-task-follow-up:"+followUp.ID, member.ChatID, authorWakerID, followUp.CreatedAt, payload)
	}
	if cursor.PendingBeforeCreatedAt == 0 {
		return result, nil
	}
	filtered := result[:0]
	for _, resource := range result {
		if resource.Message.CreatedAt < cursor.PendingBeforeCreatedAt ||
			(resource.Message.CreatedAt == cursor.PendingBeforeCreatedAt &&
				(resource.Message.Role == constants.MessageRoleUser && cursor.PendingBeforeRole == constants.MessageRoleAssistant ||
					resource.Message.Role == cursor.PendingBeforeRole && resource.Message.ID < cursor.PendingBeforeID)) {
			filtered = append(filtered, resource)
		}
	}
	return filtered, nil
}

// nextTimelineCursor 推进已返回来源的游标，并保留尚未入选来源的快照上界。
func nextTimelineCursor(
	current timelineCursor,
	sources []timelineSourcePage,
	all []timelineCandidate,
	selected []timelineCandidate,
) (string, bool, error) {
	next := cloneTimelineCursor(current)
	selectedSequenceByChat := make(map[string]int64)
	selectedPending := make([]TimelineMessage, 0)
	for _, candidate := range selected {
		if candidate.Pending {
			selectedPending = append(selectedPending, candidate.Resource)
			continue
		}
		sequence := candidate.Resource.Message.CreatedSequence
		if sequence <= 0 {
			return "", false, status_error.ChatLogError
		}
		previous := selectedSequenceByChat[candidate.Resource.ChatID]
		if previous == 0 || sequence < previous {
			selectedSequenceByChat[candidate.Resource.ChatID] = sequence
		}
	}

	hasMore := len(all) > len(selected)
	for _, source := range sources {
		if source.Page.HasMore {
			hasMore = true
		}
		if sequence := selectedSequenceByChat[source.ChatID]; sequence > 0 {
			next.BeforeByChat[source.ChatID] = sequence
			continue
		}
		if len(source.Resources) > 0 {
			// 该 Chat 有较旧的可见候选但未入选本页；首次读取时固定当前 JSONL 上界，
			// 避免后续新消息插入“加载更早”结果。
			if _, exists := next.BeforeByChat[source.ChatID]; !exists {
				upper := source.Page.LatestSequence + 1
				if upper <= 1 {
					upper = newestCreatedSequence(source.Resources) + 1
				}
				next.BeforeByChat[source.ChatID] = upper
			}
			continue
		}
		if source.Page.HasMore && source.Page.NextBeforeSequence > 0 {
			// 当前原始窗口只有内部输入；跳过该窗口后仍可继续寻找更早公开消息。
			next.BeforeByChat[source.ChatID] = source.Page.NextBeforeSequence
		} else {
			next.BeforeByChat[source.ChatID] = exhaustedTimelineSequence
		}
	}

	if len(selectedPending) > 0 {
		oldest := selectedPending[0]
		for _, resource := range selectedPending[1:] {
			if timelineMessageLess(resource, oldest) {
				oldest = resource
			}
		}
		next.PendingBeforeCreatedAt = oldest.Message.CreatedAt
		next.PendingBeforeRole = oldest.Message.Role
		next.PendingBeforeID = oldest.Message.ID
	} else if !hasPendingCandidate(all) {
		next.PendingExhausted = true
	}
	if !hasMore {
		return "", false, nil
	}
	encoded, err := encodeTimelineCursor(next)
	if err != nil {
		return "", false, err
	}
	return encoded, true, nil
}

// newestCreatedSequence 返回一个公开 Chat 窗口中最大的消息创建序号。
func newestCreatedSequence(resources []TimelineMessage) int64 {
	var newest int64
	for _, resource := range resources {
		if resource.Message.CreatedSequence > newest {
			newest = resource.Message.CreatedSequence
		}
	}
	return newest
}

// hasPendingCandidate 判断当前候选集合是否仍含临时用户输入。
func hasPendingCandidate(candidates []timelineCandidate) bool {
	for _, candidate := range candidates {
		if candidate.Pending {
			return true
		}
	}
	return false
}

// cloneTimelineCursor 复制游标映射，避免分页计算修改调用方状态。
func cloneTimelineCursor(cursor timelineCursor) timelineCursor {
	result := cursor
	result.BeforeByChat = make(map[string]int64, len(cursor.BeforeByChat))
	for chatID, beforeSequence := range cursor.BeforeByChat {
		result.BeforeByChat[chatID] = beforeSequence
	}
	return result
}

// decodeTimelineCursor 校验并解析客户端原样回传的不透明游标。
func decodeTimelineCursor(encoded string) (timelineCursor, error) {
	if encoded == "" {
		return timelineCursor{BeforeByChat: make(map[string]int64)}, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		logx.Errorf("decode channel timeline cursor failed: error=%v", err)
		return timelineCursor{}, status_error.BadRequest
	}
	var cursor timelineCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		logx.Errorf("unmarshal channel timeline cursor failed: error=%v", err)
		return timelineCursor{}, status_error.BadRequest
	}
	if cursor.BeforeByChat == nil {
		return timelineCursor{}, status_error.BadRequest
	}
	for chatID, sequence := range cursor.BeforeByChat {
		if strings.TrimSpace(chatID) == "" || sequence < exhaustedTimelineSequence {
			return timelineCursor{}, status_error.BadRequest
		}
	}
	if cursor.PendingBeforeCreatedAt < 0 ||
		(cursor.PendingBeforeCreatedAt == 0 && (cursor.PendingBeforeID != "" || cursor.PendingBeforeRole != "")) ||
		(cursor.PendingBeforeCreatedAt > 0 && (cursor.PendingBeforeID == "" || (cursor.PendingBeforeRole != constants.MessageRoleUser && cursor.PendingBeforeRole != constants.MessageRoleAssistant))) {
		return timelineCursor{}, status_error.BadRequest
	}
	return cursor, nil
}

// encodeTimelineCursor 把内部多 Chat 游标编码为客户端不可解释的 URL 安全文本。
func encodeTimelineCursor(cursor timelineCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		logx.Errorf("marshal channel timeline cursor failed: error=%v", err)
		return "", status_error.InternalServerError
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// timelineMessageLess 按公开时间、角色、来源与标识建立稳定全序。
func timelineMessageLess(left TimelineMessage, right TimelineMessage) bool {
	if left.Message.CreatedAt != right.Message.CreatedAt {
		return left.Message.CreatedAt < right.Message.CreatedAt
	}
	if left.Message.Role != right.Message.Role {
		return left.Message.Role == constants.MessageRoleUser
	}
	if left.Message.ID != right.Message.ID {
		return left.Message.ID < right.Message.ID
	}
	return left.ChatID < right.ChatID
}

// appendPendingWorkInput 把尚未提交到 Chat JSONL 的用户或负责人输入投影为真实作者消息。
func appendPendingWorkInput(
	timeline []TimelineMessage,
	messageID string,
	chatID string,
	wakerID string,
	createdAt int64,
	input execution.WorkInput,
) []TimelineMessage {
	if input.Actor != string(constants.ChannelMessageActorUser) &&
		input.Actor != string(constants.ChannelMessageActorLead) {
		return timeline
	}
	if input.Actor == string(constants.ChannelMessageActorLead) && wakerID == "" {
		return timeline
	}
	role := constants.MessageRoleUser
	if input.Actor == string(constants.ChannelMessageActorLead) {
		role = constants.MessageRoleAssistant
	}
	message := messageview.Message{
		ID:           messageID,
		SubmissionID: input.SubmissionID,
		Role:         role,
		Actor:        input.Actor,
		Content:      input.Content,
		CreatedAt:    createdAt,
		UpdatedAt:    createdAt,
	}
	if input.Content != "" {
		message.DisplayParts = []messageview.MessageDisplayPart{{
			ID:   messageID + ":text",
			Type: constants.MessageDisplayPartText,
			Text: input.Content,
		}}
	}
	for _, attachment := range input.Attachments {
		message.Attachments = append(message.Attachments, eventlog.AttachmentReference{
			Name: filepath.Base(attachment.SourcePath),
		})
	}
	for _, id := range input.PluginIDs {
		message.Plugins = append(message.Plugins, plugins.Snapshot{ID: id, Name: id})
	}
	for _, skillID := range input.SkillIDs {
		message.Skills = append(message.Skills, eventlog.MessageSkillReference{
			ID:   skillID,
			Name: skillID,
		})
	}
	for _, connectorID := range input.ConnectorIDs {
		message.Connectors = append(message.Connectors, eventlog.MessageConnectorReference{
			ID:   connectorID,
			Name: connectorID,
		})
	}
	for _, reference := range input.CodeReferences {
		message.CodeReferences = append(message.CodeReferences, eventlog.CodeReference{
			Path:      reference.Path,
			StartLine: reference.StartLine,
			EndLine:   reference.EndLine,
			Content:   reference.Content,
		})
	}
	return append(timeline, TimelineMessage{
		Message: message,
		ChatID:  chatID,
		WakerID: wakerID,
	})
}

// appendTimelineMessages 保留负责人 Chat 的真实用户输入、成员报告与全部负责人回复。
func appendTimelineMessages(
	timeline []TimelineMessage,
	messages []messageview.Message,
	chatID string,
	wakerID string,
	coordinator bool,
) []TimelineMessage {
	for _, message := range messages {
		authorWakerID := ""
		if message.Role == constants.MessageRoleUser {
			switch message.Actor {
			case string(constants.ChannelMessageActorUser):
			case string(constants.ChannelMessageActorMemberReport):
				authorWakerID = message.SourceWakerID
			default:
				continue
			}
		} else if message.Role != constants.MessageRoleAssistant {
			continue
		} else {
			authorWakerID = wakerID
		}
		timeline = append(timeline, TimelineMessage{
			Message:     message,
			ChatID:      chatID,
			WakerID:     authorWakerID,
			Coordinator: coordinator,
		})
	}
	return timeline
}
