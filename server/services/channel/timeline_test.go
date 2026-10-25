package channel

import (
	"context"
	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// timelineHistoryFake 为团队时间线测试返回指定的负责人 Chat 消息。
type timelineHistoryFake struct {
	// Service 补齐测试不涉及的聊天服务接口方法。
	chatservice.Service
	// resources 是测试中负责人 Chat 已持久化的公开消息。
	resources []messageview.Message
}

// ReadMessageHistory 返回测试配置的完整消息窗口。
func (f *timelineHistoryFake) ReadMessageHistory(
	_ context.Context,
	_ string,
	_ int64,
	_ int,
) (chatservice.MessageHistoryPage, error) {
	return chatservice.MessageHistoryPage{Resources: f.resources}, nil
}

func TestAppendTimelineMessagesKeepsPublicChatSegments(t *testing.T) {
	t.Parallel()

	coordinator := appendTimelineMessages(nil, []messageview.Message{
		{ID: "user", Role: constants.MessageRoleUser, Actor: string(constants.ChannelMessageActorUser)},
		{ID: "system", Role: constants.MessageRoleUser, Actor: string(constants.ChannelMessageActorSystem)},
		{ID: "unknown", Role: constants.MessageRoleUser},
		{ID: "lead", Role: constants.MessageRoleAssistant},
	}, "coordinator-chat", "lead", true)
	if len(coordinator) != 2 || coordinator[0].Message.ID != "user" || coordinator[1].Message.ID != "lead" {
		t.Fatalf("coordinator timeline = %#v", coordinator)
	}
	if coordinator[0].WakerID != "" || coordinator[1].WakerID != "lead" {
		t.Fatalf("coordinator authors = %#v", coordinator)
	}

	member := appendTimelineMessages(nil, []messageview.Message{
		{ID: "direct-user", Role: constants.MessageRoleUser, Actor: string(constants.ChannelMessageActorUser)},
		{ID: "assignment", Role: constants.MessageRoleUser, Actor: string(constants.ChannelMessageActorLead)},
		{ID: "execution", Role: constants.MessageRoleAssistant},
	}, "member-chat", "member", false)
	if len(member) != 2 || member[0].Message.ID != "direct-user" ||
		member[1].Message.ID != "execution" || member[1].WakerID != "member" {
		t.Fatalf("member timeline = %#v", member)
	}
}

func TestAppendPendingWorkInputKeepsQueuedDirectMentionVisible(t *testing.T) {
	t.Parallel()

	payload, err := normalizeTaskInput(&execution.WorkInput{
		Actor:       string(constants.ChannelMessageActorUser),
		Content:     "补充原任务并保留引用",
		Attachments: []execution.AttachmentInput{{SourcePath: "/tmp/spec.md"}},
		CodeReferences: []execution.CodeReferenceInput{{
			Path:      "server/main.go",
			StartLine: 1,
			EndLine:   2,
		}},
		SkillIDs:     []string{"skill-a"},
		ConnectorIDs: []string{"connector-a"},
	})
	if err != nil {
		t.Fatalf("marshalTaskInput() error = %v", err)
	}
	timeline := appendPendingWorkInput(nil, "queued-input", "member-chat", "", 100, *payload)
	if len(timeline) != 1 || timeline[0].Message.Content != "补充原任务并保留引用" ||
		len(timeline[0].Message.Attachments) != 1 ||
		len(timeline[0].Message.CodeReferences) != 1 ||
		len(timeline[0].Message.Skills) != 1 ||
		len(timeline[0].Message.Connectors) != 1 {
		t.Fatalf("queued direct mention timeline = %#v", timeline)
	}
	internalPayload, err := normalizeTaskInput(&execution.WorkInput{
		Actor:   string(constants.ChannelMessageActorLead),
		Content: "内部任务输入",
	})
	if err != nil {
		t.Fatalf("marshal internal task input: %v", err)
	}
	if internal := appendPendingWorkInput(nil, "internal", "member-chat", "", 100, *internalPayload); len(internal) != 0 {
		t.Fatalf("internal task input leaked into timeline: %#v", internal)
	}
	leadFollowUp := appendPendingWorkInput(
		nil,
		"lead-follow-up",
		"member-chat",
		"lead-waker",
		100,
		*internalPayload,
	)
	if len(leadFollowUp) != 1 ||
		leadFollowUp[0].Message.Actor != string(constants.ChannelMessageActorLead) ||
		leadFollowUp[0].Message.Role != constants.MessageRoleAssistant ||
		leadFollowUp[0].WakerID != "lead-waker" {
		t.Fatalf("lead follow-up author projection = %#v", leadFollowUp)
	}
}

func TestNextTimelineCursorKeepsPerChatPositionsWithoutDroppingOlderMessages(t *testing.T) {
	t.Parallel()

	a1 := timelineTestMessage("a-1", "chat-a", 100, 2)
	b1 := timelineTestMessage("b-1", "chat-b", 200, 3)
	a2 := timelineTestMessage("a-2", "chat-a", 300, 4)
	b2 := timelineTestMessage("b-2", "chat-b", 400, 5)
	all := []timelineCandidate{
		{Resource: a1},
		{Resource: b1},
		{Resource: a2},
		{Resource: b2},
	}
	sources := []timelineSourcePage{
		{
			ChatID: "chat-a",
			Page: chatservice.MessageHistoryPage{
				NextBeforeSequence: 2,
				LatestSequence:     8,
				HasMore:            true,
			},
			Resources: []TimelineMessage{a1, a2},
		},
		{
			ChatID: "chat-b",
			Page: chatservice.MessageHistoryPage{
				LatestSequence: 9,
			},
			Resources: []TimelineMessage{b1, b2},
		},
	}
	encoded, hasMore, err := nextTimelineCursor(
		timelineCursor{BeforeByChat: make(map[string]int64)},
		sources,
		all,
		all[2:],
	)
	if err != nil {
		t.Fatalf("nextTimelineCursor() error = %v", err)
	}
	if !hasMore || encoded == "" {
		t.Fatalf("nextTimelineCursor() = (%q, %v), want earlier page", encoded, hasMore)
	}
	cursor, err := decodeTimelineCursor(encoded)
	if err != nil {
		t.Fatalf("decodeTimelineCursor() error = %v", err)
	}
	if !reflect.DeepEqual(cursor.BeforeByChat, map[string]int64{"chat-a": 4, "chat-b": 5}) {
		t.Fatalf("per-chat cursor = %#v", cursor.BeforeByChat)
	}
}

func TestNextTimelineCursorSkipsInternalOnlyRawWindow(t *testing.T) {
	t.Parallel()

	encoded, hasMore, err := nextTimelineCursor(
		timelineCursor{BeforeByChat: make(map[string]int64)},
		[]timelineSourcePage{{
			ChatID: "member-chat",
			Page: chatservice.MessageHistoryPage{
				NextBeforeSequence: 42,
				HasMore:            true,
			},
		}},
		nil,
		nil,
	)
	if err != nil || !hasMore {
		t.Fatalf("nextTimelineCursor() = (%q, %v, %v)", encoded, hasMore, err)
	}
	cursor, err := decodeTimelineCursor(encoded)
	if err != nil {
		t.Fatalf("decodeTimelineCursor() error = %v", err)
	}
	if cursor.BeforeByChat["member-chat"] != 42 {
		t.Fatalf("internal-only cursor = %#v, want 42", cursor.BeforeByChat)
	}
}

func TestDecodeTimelineCursorRejectsMalformedInput(t *testing.T) {
	t.Parallel()
	if _, err := decodeTimelineCursor("not-base64!!!"); err == nil {
		t.Fatal("decodeTimelineCursor() error = nil, want bad request")
	}
}

// timelineTestMessage 建立带权威创建序号的测试团队消息。
func timelineTestMessage(id string, chatID string, createdAt int64, sequence int64) TimelineMessage {
	return TimelineMessage{
		Message: messageview.Message{
			ID:              id,
			Role:            constants.MessageRoleAssistant,
			CreatedSequence: sequence,
			CreatedAt:       createdAt,
		},
		ChatID: chatID,
	}
}
