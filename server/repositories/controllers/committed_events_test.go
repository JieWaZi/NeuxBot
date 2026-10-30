package controllers

import (
	"context"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCommittedOrdinaryChatActivityOnlyRefreshesItsWaker(t *testing.T) {
	hub := events.New()
	defer hub.Close()
	database := WithEventPublisher(newTestDB(t), hub)
	if err := database.Create(&models.Chat{ID: "chat", WakerID: "waker"}).Error; err != nil {
		t.Fatal(err)
	}
	changes := hub.Subscribe("", events.Resource{Kind: "global"})
	defer changes.Close()
	wakerChanges := hub.Subscribe("entities", events.Resource{Kind: "waker", ID: "waker"})
	defer wakerChanges.Close()
	channelChanges := hub.Subscribe("entities", events.Resource{Kind: "channel", ID: "channel"})
	defer channelChanges.Close()
	states := []struct {
		// fields 是一次真实接纳、等待或结束投影。
		fields map[string]any
		// running 是投影生效后的运行计数。
		running int
		// waiting 是投影生效后的等待计数。
		waiting int
		// activity 是列表向用户显示的聚合活动状态。
		activity constants.WakerActivityState
	}{
		{map[string]any{"active_execution_id": "execution", "latest_sequence": 1}, 1, 0, constants.WakerActivityStateRunning},
		{map[string]any{"waiting_approval": true, "latest_sequence": 2}, 0, 1, constants.WakerActivityStateWaiting},
		{map[string]any{"active_execution_id": "", "waiting_approval": false, "latest_sequence": 3}, 0, 0, constants.WakerActivityStateIdle},
	}
	controller := NewChatController(database)
	for index, state := range states {
		if err := controller.AdvanceProjection(context.Background(), "chat", int64(index), state.fields); err != nil {
			t.Fatal(err)
		}
		seenWaker := false
		for len(changes.Events) > 0 {
			change := <-changes.Events
			switch change.Topic {
			case "entities":
				if change.Type != "entities.activity.changed" || change.Resource != (events.Resource{Kind: "waker", ID: "waker"}) {
					t.Fatalf("ordinary chat refreshed unrelated entity resources: %+v", change)
				}
				seenWaker = true
			case "automatic-tasks":
				t.Fatalf("ordinary chat refreshed automatic tasks: %+v", change)
			}
		}
		if !seenWaker || len(wakerChanges.Events) != 1 || len(channelChanges.Events) != 0 {
			t.Fatalf("activity event did not route exclusively to its waker: waker=%d channel=%d", len(wakerChanges.Events), len(channelChanges.Events))
		}
		<-wakerChanges.Events
		summaries, err := controller.ListActivitySummariesByWaker(context.Background(), []string{"waker"})
		summary := summaries["waker"]
		if err != nil || summary.RunningCount != state.running || summary.WaitingCount != state.waiting || summary.WakerActivityState() != state.activity {
			t.Fatalf("activity event did not expose the real committed counts: summary=%+v state=%s error=%v", summary, state.activity, err)
		}
	}
}

func TestCommittedDeletedChatActivityKeepsWakerScope(t *testing.T) {
	hub := events.New()
	defer hub.Close()
	database := WithEventPublisher(newTestDB(t), hub)
	if err := database.Create(&models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "execution"}).Error; err != nil {
		t.Fatal(err)
	}
	changes := hub.Subscribe("entities", events.Resource{Kind: "global"})
	defer changes.Close()
	if err := NewChatController(database).Delete(context.Background(), "chat"); err != nil {
		t.Fatal(err)
	}
	if len(changes.Events) != 1 {
		t.Fatalf("deleted chat lost its waker activity scope: %d", len(changes.Events))
	}
	if change := <-changes.Events; change.Type != "entities.activity.changed" || change.Resource != (events.Resource{Kind: "waker", ID: "waker"}) {
		t.Fatalf("deleted chat published unrelated entity scope: %+v", change)
	}
}

func TestCommittedActivityResourcesAreCapturedBeforeCommit(t *testing.T) {
	hub := events.New()
	defer hub.Close()
	database := WithEventPublisher(newTestDB(t), hub)
	transactions := NewTransactionController(dbhelper.NewGormTransactionManager(database))
	changes := hub.Subscribe("entities", events.Resource{Kind: "global"})
	defer changes.Close()
	err := transactions.Transaction(context.Background(), func(ctx context.Context) error {
		if err := NewChatController(database).Create(ctx, models.Chat{ID: "chat", WakerID: "waker"}); err != nil {
			return err
		}
		if len(changes.Events) != 0 {
			t.Fatal("activity event was published before commit")
		}
		return nil
	})
	if err != nil || len(changes.Events) != 1 {
		t.Fatalf("committed activity did not retain transaction identity: events=%d error=%v", len(changes.Events), err)
	}
	if change := <-changes.Events; change.Resource != (events.Resource{Kind: "waker", ID: "waker"}) {
		t.Fatalf("activity lookup did not use the transaction: %+v", change)
	}
}

func TestCommittedActivityRollbackPublishesNoScopes(t *testing.T) {
	hub := events.New()
	defer hub.Close()
	database := WithEventPublisher(newTestDB(t), hub)
	changes := hub.Subscribe("", events.Resource{Kind: "global"})
	defer changes.Close()
	transactions := NewTransactionController(dbhelper.NewGormTransactionManager(database))
	err := transactions.Transaction(context.Background(), func(ctx context.Context) error {
		if err := NewChatController(database).Create(ctx, models.Chat{ID: "chat", WakerID: "waker"}); err != nil {
			return err
		}
		return status_error.BadRequest
	})
	if err == nil || len(changes.Events) != 0 {
		t.Fatalf("rolled back activity published a successful scope: events=%d error=%v", len(changes.Events), err)
	}
}

func TestCommittedActivityLookupsStayIndexedAndSkipStreamingContent(t *testing.T) {
	hub := events.New()
	defer hub.Close()
	database := WithEventPublisher(newTestDB(t), hub)
	for _, fixture := range []any{
		&models.Chat{ID: "chat", WakerID: "waker"},
		&models.Chat{ID: "automatic-chat", WakerID: "historic-waker", HiddenFromWakerHistory: true},
		&models.AutomaticTask{ID: "automatic-task", WakerID: "current-waker", AutomaticTaskConfiguration: models.AutomaticTaskConfiguration{ChannelID: "channel"}},
		&models.AutomaticRun{ID: "run", TaskID: "automatic-task", ChatID: "automatic-chat", OccurrenceKey: "run"},
	} {
		if err := database.Create(fixture).Error; err != nil {
			t.Fatal(err)
		}
	}
	log := &notificationQueryLog{Interface: logger.Discard}
	observed := database.Session(&gorm.Session{Logger: log})
	for _, example := range []struct {
		// resource 是需要捕获依赖范围的原工作身份。
		resource events.Resource
		// queries 是当前路径的固定查询预算。
		queries int
	}{
		{eventsResource("chat", "chat"), 3},
		{eventsResource("chat", "automatic-chat"), 4},
		{eventsResource("channel", "channel"), 1},
	} {
		log.Statements = nil
		if err := observed.Transaction(func(tx *gorm.DB) error {
			_, _, err := workBoardActivityResources(context.Background(), tx, example.resource)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if len(log.Statements) != example.queries {
			t.Fatalf("%+v used %d scope queries, want %d indexed lookups", example.resource, len(log.Statements), example.queries)
		}
		for _, statement := range log.Statements {
			type planRow struct {
				// Detail 是 SQLite 对当前查询步骤的索引与扫描说明。
				Detail string
			}
			var plan []planRow
			if err := database.Raw("EXPLAIN QUERY PLAN " + statement).Scan(&plan).Error; err != nil {
				t.Fatal(err)
			}
			for _, step := range plan {
				if strings.HasPrefix(step.Detail, "SCAN ") {
					t.Fatalf("activity scope lookup scanned a table: %s (%s)", step.Detail, statement)
				}
			}
		}
	}
	changes := hub.Subscribe("", events.Resource{Kind: "global"})
	defer changes.Close()
	log.Statements = nil
	if err := observed.Transaction(func(tx *gorm.DB) error {
		return NewChatController(tx).AdvanceProjection(context.Background(), "chat", 0, map[string]any{"latest_sequence": 1})
	}); err != nil {
		t.Fatal(err)
	}
	if len(changes.Events) != 0 || len(log.Statements) != 1 {
		t.Fatalf("streaming cursor triggered activity reads/events: statements=%d events=%d", len(log.Statements), len(changes.Events))
	}
}

func TestCommittedMemberChatActivityRefreshesItsChannelAndAutomaticTask(t *testing.T) {
	hub := events.New()
	defer hub.Close()
	database := WithEventPublisher(newTestDB(t), hub)
	for _, fixture := range []any{
		&models.Chat{ID: "member-chat", WakerID: "member", HiddenFromWakerHistory: true},
		&models.ChannelThread{ID: "thread", ChannelID: "channel", LeadChatID: "member-chat"},
		&models.ChannelThreadMember{ThreadID: "thread", WakerID: "member", ChatID: "member-chat"},
		&models.AutomaticRun{ID: "run", TaskID: "automatic-task", ThreadID: "thread", Admission: constants.AutomaticRunAdmissionAccepted},
	} {
		if err := database.Create(fixture).Error; err != nil {
			t.Fatal(err)
		}
	}
	changes := hub.Subscribe("", events.Resource{Kind: "global"})
	defer changes.Close()
	if err := NewChatController(database).AdvanceProjection(context.Background(), "member-chat", 0, map[string]any{"active_execution_id": "execution", "latest_sequence": 1}); err != nil {
		t.Fatal(err)
	}
	seenChannel, seenTask := false, false
	for len(changes.Events) > 0 {
		change := <-changes.Events
		switch change.Topic {
		case "entities":
			if change.Type != "entities.activity.changed" || change.Resource != (events.Resource{Kind: "channel", ID: "channel"}) {
				t.Fatalf("hidden member chat refreshed unrelated entity resources: %+v", change)
			}
			seenChannel = true
		case "automatic-tasks":
			if change.Resource != (events.Resource{Kind: "automatic_task", ID: "automatic-task"}) {
				t.Fatalf("member chat refreshed unrelated automatic tasks: %+v", change)
			}
			seenTask = true
		}
	}
	if !seenChannel || !seenTask {
		t.Fatalf("missing activity scopes: channel=%v task=%v", seenChannel, seenTask)
	}
}

func TestCommittedWorkActivityKeepsAutomaticRunSummariesAndGlobalConfirmations(t *testing.T) {
	hub := events.New()
	defer hub.Close()
	database := WithEventPublisher(newTestDB(t), hub)
	for _, fixture := range []any{
		&models.Chat{ID: "automatic-chat", WakerID: "waker", HiddenFromWakerHistory: true},
		&models.ChannelThread{ID: "automatic-thread", ChannelID: "channel"},
		&models.ChannelTask{ID: "team-task", ThreadID: "automatic-thread"},
		&models.AutomaticRun{ID: "partner-run", TaskID: "partner-task", ChatID: "automatic-chat", OccurrenceKey: "partner"},
		&models.AutomaticRun{ID: "team-run", TaskID: "team-task", ThreadID: "automatic-thread", OccurrenceKey: "team"},
		&models.AutomaticRun{ID: "workflow-run", TaskID: "workflow-task", WorkflowRunID: "workflow", OccurrenceKey: "workflow"},
	} {
		if err := database.Create(fixture).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, example := range []struct {
		// resource 是实际提交后的工作身份。
		resource events.Resource
		// task 是必须刷新的原自动任务；空值表示全局阅读确认。
		task string
		// channel 表示本次工作同时改变团队活动摘要。
		channel bool
	}{
		{eventsResource("chat", "automatic-chat"), "partner-task", false},
		{eventsResource("thread", "automatic-thread"), "team-task", true},
		{eventsResource("task", "team-task"), "team-task", true},
		{eventsResource("workflow_run", "workflow"), "workflow-task", false},
		{eventsResource("global", ""), "", false},
	} {
		t.Run(example.resource.Kind, func(t *testing.T) {
			changes := hub.Subscribe("automatic-tasks", events.Resource{Kind: "global"})
			defer changes.Close()
			entities := hub.Subscribe("entities", events.Resource{Kind: "global"})
			defer entities.Close()
			publishCommittedChange(context.Background(), database, "work-board", example.resource)
			if len(changes.Events) != 1 {
				t.Fatalf("automatic work lost its summary refresh: %d", len(changes.Events))
			}
			want := eventsResource("automatic_task", example.task)
			if example.task == "" {
				want = eventsResource("global", "")
			}
			if change := <-changes.Events; change.Resource != want {
				t.Fatalf("automatic work refreshed the wrong task: got=%+v want=%+v", change.Resource, want)
			}
			if example.channel {
				if len(entities.Events) != 1 || (<-entities.Events).Resource != (events.Resource{Kind: "channel", ID: "channel"}) {
					t.Fatal("team work lost its channel activity refresh")
				}
			} else if len(entities.Events) != 0 {
				t.Fatal("hidden automatic work or global reading confirmation refreshed entity directories")
			}
		})
	}
}

func TestCommittedAutomaticRunWaitingRefreshesCurrentTaskWaker(t *testing.T) {
	hub := events.New()
	defer hub.Close()
	database := WithEventPublisher(newTestDB(t), hub)
	for _, fixture := range []any{
		&models.Chat{ID: "automatic-chat", WakerID: "historic-waker", HiddenFromWakerHistory: true, ActiveExecutionID: "execution"},
		&models.AutomaticTask{ID: "automatic-task", WakerID: "current-waker"},
		&models.AutomaticRun{ID: "run", TaskID: "automatic-task", WakerID: "historic-waker", ChatID: "automatic-chat", OccurrenceKey: "run"},
	} {
		if err := database.Create(fixture).Error; err != nil {
			t.Fatal(err)
		}
	}
	waker := hub.Subscribe("automatic-tasks", eventsResource("waker", "current-waker"))
	defer waker.Close()
	historic := hub.Subscribe("automatic-tasks", eventsResource("waker", "historic-waker"))
	defer historic.Close()
	task := hub.Subscribe("automatic-tasks", eventsResource("automatic_task", "automatic-task"))
	defer task.Close()
	controller := NewChatController(database)
	for index, fields := range []map[string]any{
		{"waiting_approval": true, "latest_sequence": 1},
		{"waiting_approval": false, "waiting_elicitation": true, "latest_sequence": 2},
		{"waiting_elicitation": false, "latest_sequence": 3},
	} {
		if err := controller.AdvanceProjection(context.Background(), "automatic-chat", int64(index), fields); err != nil {
			t.Fatal(err)
		}
		if len(task.Events) != 1 || len(waker.Events) != 1 || len(historic.Events) != 0 {
			t.Fatalf("waiting change lost current task ownership: task=%d current-waker=%d historic-waker=%d", len(task.Events), len(waker.Events), len(historic.Events))
		}
		<-task.Events
		<-waker.Events
	}
}

func TestCommittedDeletingChannelRefreshesConfiguredTasksWithoutRuns(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[rollback], func(t *testing.T) {
			hub := events.New()
			defer hub.Close()
			database := WithEventPublisher(newTestDB(t), hub)
			for _, task := range []models.AutomaticTask{
				{ID: "first-task", WakerID: "owner", AutomaticTaskConfiguration: models.AutomaticTaskConfiguration{ChannelID: "deleted-channel"}},
				{ID: "second-task", WakerID: "owner", AutomaticTaskConfiguration: models.AutomaticTaskConfiguration{ChannelID: "deleted-channel"}},
				{ID: "unrelated-task", WakerID: "unrelated-owner", AutomaticTaskConfiguration: models.AutomaticTaskConfiguration{ChannelID: "other-channel"}},
			} {
				if err := database.Create(&task).Error; err != nil {
					t.Fatal(err)
				}
			}
			changes := hub.Subscribe("automatic-tasks", eventsResource("global", ""))
			defer changes.Close()
			transactions := NewTransactionController(dbhelper.NewGormTransactionManager(database))
			err := transactions.Transaction(context.Background(), func(ctx context.Context) error {
				if err := NewChannelThreadController(database).DeleteByChannel(ctx, "deleted-channel"); err != nil {
					return err
				}
				if len(changes.Events) != 0 {
					t.Fatal("dependency invalidation escaped before commit")
				}
				if rollback {
					return status_error.BadRequest
				}
				return nil
			})
			if rollback {
				if err == nil || len(changes.Events) != 0 {
					t.Fatalf("rolled back channel deletion published scopes: events=%d error=%v", len(changes.Events), err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := map[events.Resource]bool{
				eventsResource("automatic_task", "first-task"):  true,
				eventsResource("automatic_task", "second-task"): true,
				eventsResource("waker", "owner"):                true,
			}
			for len(changes.Events) > 0 {
				change := <-changes.Events
				if !want[change.Resource] {
					t.Fatalf("channel deletion refreshed unrelated or duplicate automatic-task scope: %+v", change.Resource)
				}
				delete(want, change.Resource)
			}
			if len(want) != 0 {
				t.Fatalf("configured tasks lost deleted channel dependency: missing=%+v", want)
			}
		})
	}
}

func TestCommittedTaskOwnershipLookupFailureKeepsCommitAndRecoveryBoundary(t *testing.T) {
	hub := events.New()
	defer hub.Close()
	database := WithEventPublisher(newTestDB(t), hub)
	for _, id := range []string{"committed-chat", "rollback-chat"} {
		if err := database.Create(&models.AutomaticRun{ID: id, TaskID: "task", ChatID: id, OccurrenceKey: id}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Migrator().DropTable(&models.AutomaticTask{}); err != nil {
		t.Fatal(err)
	}
	changes := hub.Subscribe("", eventsResource("global", ""))
	defer changes.Close()
	transactions := NewTransactionController(dbhelper.NewGormTransactionManager(database))
	if err := transactions.Transaction(context.Background(), func(ctx context.Context) error {
		if err := NewChatController(database).Create(ctx, models.Chat{ID: "committed-chat", WakerID: "waker"}); err != nil {
			return err
		}
		if len(changes.Events) != 0 {
			t.Fatal("failed scope lookup published recovery before commit")
		}
		return nil
	}); err != nil {
		t.Fatalf("scope lookup failure changed the successful write: %v", err)
	}
	if _, err := NewChatController(database).Get(context.Background(), "committed-chat"); err != nil {
		t.Fatal(err)
	}
	recovery := map[string]bool{"entities": false, "automatic-tasks": false}
	for len(changes.Events) > 0 {
		change := <-changes.Events
		if _, expected := recovery[change.Topic]; expected {
			if change.Resource != (eventsResource("global", "")) {
				t.Fatalf("failed ownership lookup did not require a full snapshot: %+v", change)
			}
			recovery[change.Topic] = true
		}
	}
	if !recovery["entities"] || !recovery["automatic-tasks"] {
		t.Fatalf("committed write lost scope recovery: %+v", recovery)
	}
	err := transactions.Transaction(context.Background(), func(ctx context.Context) error {
		if err := NewChatController(database).Create(ctx, models.Chat{ID: "rollback-chat", WakerID: "waker"}); err != nil {
			return err
		}
		return status_error.BadRequest
	})
	if err == nil || len(changes.Events) != 0 {
		t.Fatalf("rolled back ownership lookup published success: events=%d error=%v", len(changes.Events), err)
	}
}
