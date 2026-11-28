package global

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/eventsource"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestAutomaticReportsPublishToMainAndReuseOnlyTheirOwnTopic(t *testing.T) {
	dir := t.TempDir()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(dir, "reports.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := database.DB.DB()
	t.Cleanup(func() { _ = sql.Close() })
	tx := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	wakers := controllers.NewWakerController(database.DB)
	for _, owner := range []string{"report-owner", "second-owner"} {
		if err := wakers.Create(t.Context(), models.Waker{ID: owner, Name: owner}); err != nil {
			t.Fatal(err)
		}
	}
	streams, err := conversation.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(streams.Close)
	chats, err := chat.New(chat.Config{StorageDirectory: dir, Conversations: streams, Projects: controllers.NewProjectController(database.DB),
		QueryIndexes: chat.QueryIndexDependencies{Chats: controllers.NewChatController(database.DB), Transactions: tx, DeletionIntents: controllers.NewChatDeletionIntentController(database.DB)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := chats.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chats.Close)
	previousConfig, previousChat, previousMain := Config, ChatService, MainConversationService
	t.Cleanup(func() { Config, ChatService, MainConversationService = previousConfig, previousChat, previousMain })
	Config = &Configuration{ManagedFilesDirectory: dir}
	ChatService = chats
	MainConversationService = mainconversation.New(controllers.NewMainConversationController(database.DB), tx, wakers, controllers.NewChannelController(database.DB))
	layout, err := chatstorage.NewLayout(dir)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := layout.AgentWorkspace("report-owner")
	if err != nil {
		t.Fatal(err)
	}
	origin := "work-origin"
	if _, err := chats.CreateDormantChats(t.Context(), []chat.DormantChat{{Chat: models.Chat{
		ID: origin, WakerID: "report-owner", Title: "Original turn", WorkspaceDirectory: workspace,
		AdditionalWorkingDirectories: []string{workspace},
	}}}); err != nil {
		t.Fatal(err)
	}
	run := models.AutomaticRun{
		ID: "run-1", WakerID: "report-owner", TaskID: "weekly-report", ChatID: "run-chat", Source: constants.AutomaticRunSourceEventTest,
		SourceEvent:   &eventsource.Event{URL: "https://github.com/example/demo/pull/42"},
		Configuration: models.AutomaticTaskConfiguration{ReportDestination: constants.AutomaticReportDestinationMainConversation, Name: "Ops report"},
	}
	publish := func(text string) string {
		t.Helper()
		href, err := publishAutomaticTaskReport(t.Context(), run, text)
		if err != nil {
			t.Fatal(err)
		}
		return href
	}
	mainHref := publish("Pull request summary")
	if replay := publish("Pull request summary"); mainHref != replay || !strings.Contains(mainHref, mainconversation.AutomaticReportMessageID(run.ID)) {
		t.Fatalf("main report receipt changed on replay: %s / %s", mainHref, replay)
	}
	subject, err := MainConversationService.Ensure(t.Context(), "waker", run.WakerID)
	if err != nil {
		t.Fatal(err)
	}
	page, _, err := MainConversationService.Messages(t.Context(), subject.ID, 0, 10)
	if err != nil || len(page) != 1 || !strings.Contains(page[0].Content, "Test run") ||
		!strings.Contains(page[0].Content, run.SourceEvent.URL) || strings.Contains(page[0].Content, "/automatic-tasks/weekly-report") {
		t.Fatalf("main conversation report = %+v, %v", page, err)
	}
	run.ID, run.Source = "run-2", constants.AutomaticRunSourceSchedule
	run.SourceEvent = nil
	run.Configuration.ReportDestination = constants.AutomaticReportDestinationTopic
	topicHref := publish("Second run")
	if replay := publish("Second run"); replay != topicHref {
		t.Fatalf("topic replay changed destination: %s / %s", topicHref, replay)
	}
	run.ID = "run-3"
	if next := publish("Third run"); next != topicHref {
		t.Fatalf("same rule no longer reuses its dedicated topic: %s", next)
	}
	run.ID, run.TaskID = "run-4", "daily-report"
	if other := publish("Independent report"); other == topicHref {
		t.Fatal("different rules shared one topic")
	}
	topic, _, _ := strings.Cut(strings.TrimPrefix(topicHref, "/chats/"), "?")
	history, err := chats.ReadMessageHistory(t.Context(), topic, 0, 10)
	if err != nil || len(history.Resources) != 2 || history.Resources[0].Role != constants.MessageRoleAssistant ||
		history.Resources[0].Content != "Second run" || history.Resources[1].Content != "Third run" {
		t.Fatalf("topic messages are not durable and ordered: %+v, %v", history, err)
	}
	summary, err := chats.ReadMessageHistory(t.Context(), origin, 0, 10)
	if err != nil || len(summary.Resources) != 0 {
		t.Fatalf("scheduled report was posted back to the originating ordinary chat: %+v %v", summary, err)
	}

	t.Run("team report uses the frozen destination and only the lead can send", func(t *testing.T) {
		ctx := t.Context()
		channels := controllers.NewChannelController(database.DB)
		threads := controllers.NewChannelThreadController(database.DB)
		tasks := controllers.NewAutomaticTaskController(database.DB)
		team := models.Channel{ID: "report-team", Name: "Report team", LeadWakerID: "report-owner"}
		if err := channels.Create(ctx, team, []models.ChannelMember{
			{ChannelID: team.ID, WakerID: team.LeadWakerID}, {ChannelID: team.ID, WakerID: "second-owner", Position: 1},
		}); err != nil {
			t.Fatal(err)
		}
		run := models.AutomaticRun{ID: "team-agent-report", TaskID: "team-weekly", OccurrenceKey: "first", WakerID: team.LeadWakerID,
			ExecutionID: "run-lead-execution", ReportByAgent: true, ReportState: constants.AutomaticReportStatePending,
			Admission:     constants.AutomaticRunAdmissionPending,
			Configuration: models.AutomaticTaskConfiguration{ChannelID: team.ID, ReportDestination: constants.AutomaticReportDestinationMainConversation}}
		if err := tasks.CreateRun(ctx, run); err != nil {
			t.Fatal(err)
		}
		thread := models.ChannelThread{ID: "report-thread", ChannelID: team.ID, LeadWakerID: team.LeadWakerID, LeadChatID: "lead-work", AutomaticRunID: run.ID}
		if err := threads.Create(ctx, thread, []models.ChannelThreadMember{
			{ThreadID: thread.ID, WakerID: team.LeadWakerID, ChatID: thread.LeadChatID},
			{ThreadID: thread.ID, WakerID: "second-owner", ChatID: "member-work", Position: 1},
		}); err != nil {
			t.Fatal(err)
		}
		service := schedule.New(schedule.Config{Tasks: tasks, Threads: threads, PublishReport: publishAutomaticTaskReport})
		denied, err := service.AgentReportBinding(ctx, models.Chat{ID: "member-work", WakerID: "second-owner"}, "member-execution", run.ID)
		if err != nil || denied.Call != nil {
			t.Fatalf("member acquired the lead's publishing tool: %+v %v", denied, err)
		}
		grant, err := service.AgentReportBinding(ctx, models.Chat{ID: thread.LeadChatID, WakerID: team.LeadWakerID}, run.ExecutionID, run.ID)
		if err != nil || grant.Call == nil {
			t.Fatalf("lead did not receive its publishing tool: %+v %v", grant, err)
		}
		request := capability.Request{RequestID: "explicit-report", Params: map[string]any{"content": "Verified team report"}}
		for i := range 2 {
			receipt, err := grant.Call(ctx, request)
			if err != nil || receipt.CommandID != mainconversation.AutomaticReportMessageID(run.ID) || receipt.Replayed != (i == 1) {
				t.Fatalf("actual team publication = %+v, %v", receipt, err)
			}
		}
		teamMain, err := MainConversationService.Ensure(ctx, "team", team.ID)
		if err != nil {
			t.Fatal(err)
		}
		rows, _, err := MainConversationService.Messages(ctx, teamMain.ID, 0, 10)
		if err != nil || len(rows) != 1 || rows[0].Content != "Verified team report" {
			t.Fatalf("agent report did not reach its frozen team: %+v %v", rows, err)
		}
	})
}
