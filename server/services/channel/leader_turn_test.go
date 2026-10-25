package channel

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	channeltool "github.com/JieWaZi/NeuxBot/server/internal/teamcapability"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// leaderTurnChatGroupFake 提供首发路由测试使用的成员 Chat，不启动真实 Runtime。
type leaderTurnChatGroupFake struct {
	// chats 保存成员 Chat 摘要，供负责人繁忙检查和重启恢复读取。
	chats *postTurnChatFake
}

// PrepareChatGroup 为每位成员返回独立 Chat，并模拟首发执行期间负责人繁忙。
func (f *leaderTurnChatGroupFake) PrepareChatGroup(
	_ context.Context,
	intent execution.PrepareChatGroupIntent,
) (execution.PreparedChatGroup, error) {
	f.chats.mutex.Lock()
	defer f.chats.mutex.Unlock()
	result := execution.PreparedChatGroup{}
	for _, member := range intent.Members {
		chat := models.Chat{
			ID: member.ChatID, WakerID: member.WakerID, Title: intent.Title,
			WorkspaceDirectory: intent.Scope.WorkingDirectory,
		}
		result.Chats = append(result.Chats, chat)
		if member.WakerID == intent.Scope.WakerID {
			chat.ActiveExecutionID = "initial-execution"
		}
		f.chats.summaries[chat.ID] = chatservice.ChatSummary{Chat: chat}
	}
	return result, nil
}

// TestLeaderTurnModeSurvivesFirstSendAndQueueRecovery 验证首发与重启恢复均向执行层传递本轮意图。
func TestLeaderTurnModeSurvivesFirstSendAndQueueRecovery(t *testing.T) {
	for _, initialDirect := range []bool{false, true} {
		t.Run(fmt.Sprintf("initial_direct=%t", initialDirect), func(t *testing.T) {
			database, err := dbhelper.NewDB(conf.DB{
				Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "leader-turn.db"), Migrate: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := database.DB.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })
			ctx := context.Background()
			channels := controllers.NewChannelController(database.DB)
			threads := controllers.NewChannelThreadController(database.DB)
			tasks := controllers.NewChannelTaskController(database.DB)
			wakers := controllers.NewWakerController(database.DB)
			leadID := "11111111111111111111111111111111"
			memberID := "22222222222222222222222222222222"
			channelID := "33333333333333333333333333333333"
			createChannelTestWakers(t, ctx, wakers, leadID, memberID)
			if err := channels.Create(ctx, models.Channel{
				ID: channelID, Name: "回合路由团队", LeadWakerID: leadID,
			}, []models.ChannelMember{
				{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", AccountID: "lead-account", PermissionLevel: constants.PermissionLevelAuto},
				{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
			}); err != nil {
				t.Fatal(err)
			}
			chats := &postTurnChatFake{summaries: make(map[string]chatservice.ChatSummary)}
			executions := &postTurnExecutionFake{}
			newService := func() *service {
				return &service{
					channels: channels, threads: threads, tasks: tasks, wakers: wakers,
					transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB)),
					chats:        chats, executions: executions, chatGroups: &leaderTurnChatGroupFake{chats: chats},
					watchers: make(map[string]struct{}), capabilities: channeltool.NewRegistry(),
				}
			}
			sut := newService()
			if err := sut.Start(ctx); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(sut.Close)
			mention := ""
			if initialDirect {
				mention = leadID
			}
			input := execution.WorkInput{Actor: "user", Content: "修改项目文件并验证"}
			created, err := sut.createThread(ctx, CreateThreadInput{
				ChannelID: channelID, ClientRequestID: "first-send", WorkingDirectory: t.TempDir(),
				MentionWakerID: mention, Input: input,
			})
			if err != nil {
				t.Fatal(err)
			}
			assertLeaderTurnIntent(t, executions.firstStart(), input.Content, initialDirect)
			if created.Members[0].AccountID != "lead-account" {
				t.Fatalf("account was not frozen in thread: %+v", created.Members)
			}
			if len(created.Tasks) != 0 {
				t.Fatalf("lead input created tasks: %+v", created.Tasks)
			}

			// 下一条使用相反模式且正文完全相同，必须只依赖结构化提及，不受历史回合影响。
			mention = leadID
			if initialDirect {
				mention = ""
			}
			queued, err := sut.sendMessage(ctx, SendMessageInput{
				ThreadID: created.Thread.ID, MentionWakerID: mention, Input: input,
			})
			if err != nil {
				t.Fatal(err)
			}
			if queued.QueuedInput == nil || queued.Execution != nil || queued.Task != nil || executions.startCount() != 1 {
				t.Fatalf("busy lead input was not queued: %+v", queued)
			}
			sut.Close()
			chats.mutex.Lock()
			summary := chats.summaries[created.Thread.LeadChatID]
			summary.Chat.ActiveExecutionID = ""
			chats.summaries[created.Thread.LeadChatID] = summary
			chats.mutex.Unlock()
			recovered := newService()
			if err := recovered.Start(ctx); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(recovered.Close)
			if executions.startCount() != 2 {
				t.Fatalf("recovered executions = %d, want 2", executions.startCount())
			}
			assertLeaderTurnIntent(t, executions.startAt(1), input.Content, !initialDirect)
		})
	}
}

// assertLeaderTurnIntent 检查执行边界保留原文和权限，并只包含当前回合的路由意图。
func assertLeaderTurnIntent(t *testing.T, intent execution.WorkIntent, content string, direct bool) {
	t.Helper()
	if intent.Input.Content != content || intent.Options.AccountID != "lead-account" || intent.Options.PermissionLevel != constants.PermissionLevelAuto || intent.Options.WorkspaceReadOnly {
		t.Fatalf("lead input or permission changed: %+v", intent)
	}
	instructions := intent.Options.SupplementalInstructions
	if len(instructions) != 1 || instructions[0].Name != constants.TeamInstructionFileName {
		t.Fatalf("lead instructions = %+v", instructions)
	}
	want, unwanted := "This is a coordination turn", "This is a direct-execution turn"
	if direct {
		want, unwanted = unwanted, want
	}
	if !strings.Contains(instructions[0].Content, want) || strings.Contains(instructions[0].Content, unwanted) {
		t.Fatalf("direct=%t instructions did not preserve turn mode: %s", direct, instructions[0].Content)
	}
}

// TestAutomaticTeamSourceFollowsOpenRun 验证自动运行封闭后负责人和成员的新工作不会复用历史调用身份。
func TestAutomaticTeamSourceFollowsOpenRun(t *testing.T) {
	for _, closed := range []bool{false, true} {
		for _, route := range []string{"lead", "direct_member", "lead_created_member", "notification"} {
			t.Run(fmt.Sprintf("closed=%t/%s", closed, route), func(t *testing.T) {
				ctx := t.Context()
				db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "source.db"), Migrate: true})
				if err != nil {
					t.Fatal(err)
				}
				pool, _ := db.DB.DB()
				t.Cleanup(func() { _ = pool.Close() })
				channels := controllers.NewChannelController(db.DB)
				threads := controllers.NewChannelThreadController(db.DB)
				tasks := controllers.NewChannelTaskController(db.DB)
				runs := controllers.NewAutomaticTaskController(db.DB)
				wakers := controllers.NewWakerController(db.DB)
				createChannelTestWakers(t, ctx, wakers, "lead-waker", "member-waker")
				if err := channels.Create(ctx, models.Channel{ID: "team", Name: "来源测试", LeadWakerID: "lead-waker"}, []models.ChannelMember{{ChannelID: "team", WakerID: "lead-waker"}, {ChannelID: "team", WakerID: "member-waker"}}); err != nil {
					t.Fatal(err)
				}
				thread := models.ChannelThread{ID: "thread", ChannelID: "team", LeadWakerID: "lead-waker", LeadChatID: "lead-chat", AutomaticRunID: "automatic-run", ReadyAt: testTimestamp(1), MaxParallel: 2, MaxTasks: 10}
				if err := threads.Create(ctx, thread, []models.ChannelThreadMember{
					{ThreadID: thread.ID, WakerID: "lead-waker", ChatID: "lead-chat", IsLead: true, RuntimeID: "codex"},
					{ThreadID: thread.ID, WakerID: "member-waker", ChatID: "member-chat", RuntimeID: "codex"},
				}); err != nil {
					t.Fatal(err)
				}
				if err := runs.CreateRun(ctx, models.AutomaticRun{ID: "automatic-run", TaskID: "automatic-task", ThreadID: thread.ID}); err != nil {
					t.Fatal(err)
				}
				chats := &postTurnChatFake{summaries: map[string]chatservice.ChatSummary{
					"lead-chat": {Chat: models.Chat{ID: "lead-chat"}}, "member-chat": {Chat: models.Chat{ID: "member-chat"}},
				}}
				executions := &postTurnExecutionFake{}
				sut := &service{channels: channels, threads: threads, tasks: tasks, wakers: wakers, chats: chats, executions: executions,
					transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)), capabilities: channeltool.NewRegistry(), automaticRun: runs.GetRun,
				}
				// 模拟原调度器在用户新输入前冻结已经结束的团队运行；实际来源读取仍访问真实仓库。
				sut.beforeUserInput = func(ctx context.Context, _ string) error {
					if closed {
						return runs.CloseTeamRun(ctx, "automatic-run", constants.AutomaticRunStateCompleted, testTimestamp(5))
					}
					return nil
				}
				switch route {
				case "lead", "direct_member":
					mention := ""
					if route == "direct_member" {
						mention = "member-waker"
					}
					if _, err := sut.sendMessage(ctx, SendMessageInput{ThreadID: thread.ID, MentionWakerID: mention, Input: execution.WorkInput{Content: "新的用户工作"}}); err != nil {
						t.Fatal(err)
					}
				case "lead_created_member":
					// 原自动内部的负责人派发与用户续聊负责人派发都走正式任务路径。
					if _, err := sut.CreateTask(ctx, CreateTaskInput{ThreadID: thread.ID, SenderWakerID: thread.LeadWakerID, AssigneeWakerID: "member-waker", Subject: "成员工作", Description: "读取知识并报告"}); err != nil {
						t.Fatal(err)
					}
				case "notification":
					if closed {
						if err := runs.CloseTeamRun(ctx, "automatic-run", constants.AutomaticRunStateCompleted, testTimestamp(5)); err != nil {
							t.Fatal(err)
						}
					}
					task := models.ChannelTask{ID: "reported-task", ThreadID: thread.ID, AssigneeWakerID: "member-waker", Subject: "成员结果", Status: constants.ChannelTaskStatusCompleted, Result: "已核验", CurrentFence: 1}
					if err := tasks.Create(ctx, task, nil); err != nil {
						t.Fatal(err)
					}
					attempt := models.ChannelTaskAttempt{ID: "reported-attempt", TaskID: task.ID, ChatID: "member-chat", ExecutionID: "reported-execution", AssigneeWakerID: "member-waker", Fence: 1, Reason: constants.ChannelAttemptReasonInitial, Status: constants.ExecutionStatusCompleted}
					if err := tasks.CreateAttempt(ctx, attempt); err != nil {
						t.Fatal(err)
					}
					if err := tasks.CreateNotification(ctx, models.ChannelLeadNotification{ID: "notification", ThreadID: thread.ID, TaskID: task.ID, AttemptID: attempt.ID, Payload: task.Result, Status: constants.ChannelNotificationStatusPending}); err != nil {
						t.Fatal(err)
					}
					if err := sut.drainNotifications(ctx, thread.ID); err != nil {
						t.Fatal(err)
					}
				}
				if executions.startCount() != 1 {
					t.Fatalf("execution count=%d", executions.startCount())
				}
				want := thread.AutomaticRunID
				if closed {
					want = ""
				}
				if got := executions.firstStart().AutomaticRunID; got != want {
					t.Fatalf("AutomaticRunID=%q, want %q", got, want)
				}
				stored, err := threads.Get(ctx, thread.ID)
				if err != nil || stored.AutomaticRunID != thread.AutomaticRunID {
					t.Fatalf("historical thread source changed: %+v %v", stored, err)
				}
			})
		}
	}
}
