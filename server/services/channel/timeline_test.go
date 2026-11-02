package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"gorm.io/gorm"
)

// timelineHistoryFake 为团队时间线测试返回指定的负责人 Chat 消息。
type timelineHistoryFake struct {
	// Service 补齐测试不涉及的聊天服务接口方法。
	chatservice.Service
	// resources 是测试中负责人 Chat 已持久化的公开消息。
	resources []messageview.Message
	// latestSequence 是负责人 Chat 最新窗口已有的持久尾游标。
	latestSequence int64
	// calls 记录真实时间线读取使用的历史页数量。
	calls int
}

// ReadMessageHistory 返回测试配置的完整消息窗口。
func (f *timelineHistoryFake) ReadMessageHistory(
	_ context.Context,
	_ string,
	before int64,
	limit int,
) (chatservice.MessageHistoryPage, error) {
	f.calls++
	resources := make([]messageview.Message, 0, len(f.resources))
	for _, message := range f.resources {
		if before == 0 || message.CreatedSequence < before {
			resources = append(resources, message)
		}
	}
	page := chatservice.MessageHistoryPage{HasMore: len(resources) > limit}
	if page.HasMore {
		resources = resources[len(resources)-limit:]
	}
	page.Resources = resources
	if len(resources) > 0 {
		page.NextBeforeSequence = resources[0].CreatedSequence
	}
	if before == 0 {
		page.LatestSequence = f.latestSequence
	}
	return page, nil
}

func TestTimelineLatestWindowOwnsLeadChatSequenceWithoutChangingPagination(t *testing.T) {
	fixture := newDeletionFixture(t)
	_, threadID, _ := fixture.seedTeam(t)
	history := &timelineHistoryFake{latestSequence: 13, resources: []messageview.Message{
		{ID: "first", Role: constants.MessageRoleAssistant, CreatedAt: 100, CreatedSequence: 2},
		{ID: "second", Role: constants.MessageRoleAssistant, CreatedAt: 200, CreatedSequence: 5},
		{ID: "third", Role: constants.MessageRoleAssistant, CreatedAt: 300, CreatedSequence: 9},
	}}
	fixture.service.chats = history
	fixture.service.tasks = controllers.NewChannelTaskController(fixture.db)
	latest, err := fixture.service.ReadTimeline(context.Background(), TimelineQuery{ThreadID: threadID, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(latest)
	if err != nil {
		t.Fatal(err)
	}
	var public map[string]any
	if err := json.Unmarshal(encoded, &public); err != nil {
		t.Fatal(err)
	}
	if public["LeadChatLatestSequence"] != float64(13) || history.calls != 1 {
		t.Fatalf("main Chat tail was lost or reread: snapshot=%s calls=%d", encoded, history.calls)
	}
	cursor, err := decodeTimelineCursor(latest.NextCursor)
	if err != nil || !latest.HasMore || len(latest.Resources) != 2 || latest.Resources[0].Message.ID != "second" || cursor.BeforeByChat["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"] != 5 {
		t.Fatalf("latest page pagination changed: %+v cursor=%+v error=%v", latest, cursor, err)
	}
	older, err := fixture.service.ReadTimeline(context.Background(), TimelineQuery{ThreadID: threadID, Cursor: latest.NextCursor, Limit: 2})
	if err != nil || older.HasMore || older.NextCursor != "" || len(older.Resources) != 1 || older.Resources[0].Message.ID != "first" || history.calls != 2 {
		t.Fatalf("older page changed: %+v calls=%d error=%v", older, history.calls, err)
	}
	encoded, _ = json.Marshal(older)
	public = map[string]any{}
	if err := json.Unmarshal(encoded, &public); err != nil {
		t.Fatal(err)
	}
	if public["LeadChatLatestSequence"] != nil {
		t.Fatal("older window pretended that its zero cursor was the latest Chat tail")
	}
	exhausted, err := encodeTimelineCursor(timelineCursor{BeforeByChat: map[string]int64{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": exhaustedTimelineSequence}, PendingExhausted: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.ReadTimeline(context.Background(), TimelineQuery{ThreadID: threadID, Cursor: exhausted, Limit: 2}); err != nil || history.calls != 2 {
		t.Fatalf("exhausted source added a history query: calls=%d error=%v", history.calls, err)
	}
}

func TestAppendTimelineMessagesKeepsPublicChatSegments(t *testing.T) {
	t.Parallel()

	coordinator := appendTimelineMessages(nil, []messageview.Message{
		{ID: "user", SubmissionID: "coordinator-input", Role: constants.MessageRoleUser, Actor: string(constants.ChannelMessageActorUser)},
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
	if coordinator[0].Message.SubmissionID != "coordinator-input" {
		t.Fatal("coordinator timeline dropped the authoritative input identity")
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
		SubmissionID: "queued-request",
		Actor:        string(constants.ChannelMessageActorUser),
		Content:      "补充原任务并保留引用",
		Attachments:  []execution.AttachmentInput{{SourcePath: "/tmp/spec.md"}},
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
		timeline[0].Message.SubmissionID != "queued-request" ||
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

func TestTimelineLongFollowUpsPageSameMillisecondAcrossAuthorsWithoutGaps(t *testing.T) {
	fixture := newDeletionFixture(t)
	_, threadID, _ := fixture.seedTeam(t)
	ctx := context.Background()
	members, err := fixture.threads.ListMembers(ctx, threadID)
	if err != nil {
		t.Fatal(err)
	}
	tasks := controllers.NewChannelTaskController(fixture.db)
	fixture.service.tasks = tasks
	fixture.service.chats = &timelineHistoryFake{latestSequence: 1}
	expected := map[string]bool{}
	for index, member := range members {
		task := models.ChannelTask{ID: fmt.Sprintf("task-%d", index), ThreadID: threadID, AssigneeWakerID: member.WakerID, CurrentFence: 0, CreatedAt: 2000, DirectInput: &workinput.Input{Actor: string(constants.ChannelMessageActorUser), Content: "真实待投递输入"}}
		if err := tasks.Create(ctx, task, nil); err != nil {
			t.Fatal(err)
		}
		expected["channel-task-input:"+task.ID] = true
		rows := make([]models.ChannelTaskFollowUp, 0, 121)
		delivered := int64(1)
		for row := 0; row < 120; row++ {
			followUp := models.ChannelTaskFollowUp{ID: fmt.Sprintf("follow-%d-%03d", index, row), TaskID: task.ID, Actor: constants.ChannelMessageActorUser, Text: "已投递历史", CreatedAt: 1000, DeliveredAt: &delivered}
			if row < 21 {
				followUp.CreatedAt = 2000
				followUp.DeliveredAt = nil
				followUp.Text = "保留作者的补充"
				if row%2 == 0 {
					followUp.Actor = constants.ChannelMessageActorLead
				}
				expected["channel-task-follow-up:"+followUp.ID] = true
			}
			rows = append(rows, followUp)
		}
		if err := fixture.db.CreateInBatches(rows, 50).Error; err != nil {
			t.Fatal(err)
		}
	}
	followUpQueries := 0
	if err := fixture.db.Callback().Query().After("gorm:query").Register("timeline_page_bound", func(query *gorm.DB) {
		if strings.Contains(query.Statement.Table, "channel_task_follow_ups") {
			followUpQueries++
			if query.RowsAffected > 8 {
				t.Errorf("follow-up history materialized: rows=%d", query.RowsAffected)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer fixture.db.Callback().Query().Remove("timeline_page_bound")
	cursor := ""
	seen := map[string]bool{}
	pages := 0
	var assembled []TimelineMessage
	for {
		page, err := fixture.service.ReadTimeline(ctx, TimelineQuery{ThreadID: threadID, Limit: 7, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if pages > 20 {
			t.Fatal("pagination failed to converge")
		}
		for _, item := range page.Resources {
			if seen[item.Message.ID] || !expected[item.Message.ID] {
				t.Fatalf("unexpected/duplicate input=%s", item.Message.ID)
			}
			seen[item.Message.ID] = true
		}
		assembled = append(page.Resources, assembled...)
		if !page.HasMore {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != len(expected) || followUpQueries != pages*len(members) {
		t.Fatalf("lost inputs or per-task query: got=%d want=%d queries=%d pages=%d", len(seen), len(expected), followUpQueries, pages)
	}
	for index := 1; index < len(assembled); index++ {
		if timelineMessageLess(assembled[index], assembled[index-1]) {
			t.Fatalf("cursor order changed at %d: %s -> %s", index, assembled[index-1].Message.ID, assembled[index].Message.ID)
		}
	}
}
