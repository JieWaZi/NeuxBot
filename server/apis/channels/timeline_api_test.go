package channels

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	channelservice "github.com/JieWaZi/NeuxBot/server/services/channel"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// timelineAPIService 只观测元数据与消息端点是否读取同一时间线事实。
type timelineAPIService struct {
	// Service 补齐本测试不调用的团队能力。
	channelservice.Service
	// page 是独立消息端点拥有的历史页。
	page channelservice.TimelinePage
	// reads 记录时间线快照的读取次数。
	reads int
}

func (s *timelineAPIService) ReadTimeline(context.Context, channelservice.TimelineQuery) (channelservice.TimelinePage, error) {
	s.reads++
	return s.page, nil
}
func (*timelineAPIService) SyncInteractionEvents(context.Context, string) error { return nil }
func (*timelineAPIService) ListPendingInteractions(context.Context, string) ([]channelservice.PendingTeamInteraction, []channelservice.TeamMemberActivity, error) {
	return nil, nil, nil
}

// timelineAPIChat 返回元数据入口原本需要的执行摘要，不读取消息历史。
type timelineAPIChat struct {
	// Module 补齐本测试不调用的聊天日志与事件能力。
	chatservice.Module
}

func (*timelineAPIChat) GetChatSummary(context.Context, string) (chatservice.ChatSummary, error) {
	return chatservice.ChatSummary{Chat: models.Chat{ID: "lead-chat", ActiveExecutionID: "lead-active"}}, nil
}

func TestTeamMetadataAndTimelineHaveOneMessageSnapshotOwner(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "timeline-owner.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	oldThreads, oldTasks, oldTeams, oldChats := global.ChannelThreadControllerManager, global.ChannelTaskControllerManager, global.ChannelService, global.ChatService
	t.Cleanup(func() {
		global.ChannelThreadControllerManager, global.ChannelTaskControllerManager, global.ChannelService, global.ChatService = oldThreads, oldTasks, oldTeams, oldChats
	})
	global.ChannelThreadControllerManager = controllers.NewChannelThreadController(database.DB)
	global.ChannelTaskControllerManager = controllers.NewChannelTaskController(database.DB)
	teams := &timelineAPIService{}
	if err := json.Unmarshal([]byte(`{"leadChatLatestSequence":37,"nextCursor":"opaque-page","hasMore":true}`), &teams.page); err != nil {
		t.Fatal(err)
	}
	global.ChannelService, global.ChatService = teams, &timelineAPIChat{}
	ready := int64(1)
	if err := database.DB.Create(&models.ChannelThread{ID: "thread", ChannelID: "channel", LeadChatID: "lead-chat", ReadyAt: &ready}).Error; err != nil {
		t.Fatal(err)
	}
	ctx, _ := gin.CreateTestContext(nil)
	metadata, err := (&GetChannelThread{ChannelID: "channel", ThreadID: "thread"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if teams.reads != 0 {
		t.Fatalf("metadata request secretly read %d full timeline pages", teams.reads)
	}
	encoded, _ := json.Marshal(metadata)
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"timeline", "timelineNextCursor", "timelineHasMore", "leadChatLatestSequence"} {
		if _, found := fields[key]; found {
			t.Fatalf("metadata retained a second message snapshot field %q", key)
		}
	}
	if fields["leadActiveExecutionId"] != "lead-active" {
		t.Fatal("metadata lost the original execution summary")
	}
	page, err := (&ListChannelTimeline{ChannelID: "channel", ThreadID: "thread", Limit: 2}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ = json.Marshal(page)
	fields = map[string]any{}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if teams.reads != 1 || fields["leadChatLatestSequence"] != float64(37) || fields["nextCursor"] != "opaque-page" || fields["hasMore"] != true {
		t.Fatalf("timeline lost its own Chat tail or paging contract: %s reads=%d", encoded, teams.reads)
	}
}
