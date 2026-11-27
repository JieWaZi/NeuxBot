package main_conversations

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/apis/chats"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// teamPreparationExecution 在占位接纳和提交之间暂停，模拟慢原生会话准备。
type teamPreparationExecution struct {
	// Service 补齐当前测试不关心的执行操作。
	execution.Service
	// prepare 通知已有执行可以接收用户答复，并允许真实团队配置变更。
	prepare func()
	// committed 表明是否写入了执行接纳。
	committed bool
}

func (e *teamPreparationExecution) Start(ctx context.Context, intent execution.WorkIntent) (execution.AcceptedExecution, error) {
	e.prepare()
	err := intent.AdmissionBoundary.Commit(ctx, func(context.Context) error {
		e.committed = true
		return nil
	})
	return execution.AcceptedExecution{}, err
}

func TestTeamMainAdmissionAllowsOtherRequestsDuringPreparationAndRechecksActor(t *testing.T) {
	for _, fromSource := range []bool{false, true} {
		name := "new assignment"
		if fromSource {
			name = "source continuation"
		}
		t.Run(name, func(t *testing.T) {
			database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "team.db"), Migrate: true})
			if err != nil {
				t.Fatal(err)
			}
			pool, err := database.DB.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = pool.Close() })
			ctx := t.Context()
			channels, wakers := controllers.NewChannelController(database.DB), controllers.NewWakerController(database.DB)
			tx := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
			store := controllers.NewMainConversationController(database.DB)
			main := mainconversation.New(store, tx, wakers, channels)
			oldMain, oldChannels, oldExecution, oldChat := global.MainConversationService, global.ChannelControllerManager, global.ExecutionService, global.ChatService
			t.Cleanup(func() {
				global.MainConversationService, global.ChannelControllerManager, global.ExecutionService, global.ChatService = oldMain, oldChannels, oldExecution, oldChat
			})
			global.MainConversationService, global.ChannelControllerManager = main, channels

			team := models.Channel{ID: "team", Name: "Team", LeadWakerID: "lead"}
			lead := models.ChannelMember{ChannelID: team.ID, WakerID: "lead", RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}
			other := models.ChannelMember{ChannelID: team.ID, WakerID: "other", RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1}
			if err := channels.Create(ctx, team, []models.ChannelMember{lead, other}); err != nil {
				t.Fatal(err)
			}
			work, err := main.ReserveWork(ctx, "team", team.ID, lead.WakerID, "managed")
			if err != nil {
				t.Fatal(err)
			}
			global.ChatService = sourceHistoryChat{
				summary: chat.ChatSummary{Chat: models.Chat{
					ID: work.WorkChatID, WakerID: lead.WakerID, HiddenFromWakerHistory: true,
					WaitingApproval: true, PendingApprovals: []models.PendingApproval{{ApprovalID: "approval"}},
					WaitingElicitation: true, PendingElicitations: []models.PendingElicitation{{ElicitationID: "question"}},
				}},
				page: chat.MessageHistoryPage{Resources: []messageview.Message{{ID: "history", Role: constants.MessageRoleUser, Content: "retained"}}},
			}
			runner := &teamPreparationExecution{prepare: func() {
				// 另一个 HTTP 请求不能继承当前接纳的重入标记。
				wait, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
				defer cancel()
				locked, release, err := teamwork.Acquire(wait, team.ID)
				if err != nil {
					t.Fatalf("another team action was blocked by native preparation: %v", err)
				}
				defer release()
				team.LeadWakerID = other.WakerID
				members := []models.ChannelMember{other}
				if !fromSource {
					// 已不再是负责人时，仍在队的原伙伴不能接纳这条全新的交办。
					members = append(members, lead)
				}
				if err := tx.Transaction(locked, func(tx context.Context) error { return channels.Update(tx, team, members) }); err != nil {
					t.Fatal(err)
				}
			}}
			global.ExecutionService = runner
			httpCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
			httpCtx.Request = httptest.NewRequest("POST", "/", nil).WithContext(ctx)
			const submission = "recheck"
			if fromSource {
				_, err = (&SendMainConversationSourceMessage{SubjectType: "team", SubjectID: team.ID, WorkChatID: work.WorkChatID,
					Body: chats.SendMessageRequest{SubmissionID: submission, MessageRequest: chats.MessageRequest{Text: "continue"}},
				}).Output(httpCtx)
			} else {
				_, err = (&SendMainConversationMessage{SubjectType: "team", SubjectID: team.ID,
					Body: MainConversationInput{SubmissionID: submission, Content: "new assignment"},
				}).Output(httpCtx)
			}
			if !util.HasStatusCode(err, status_error.ChatReplyTargetUnavailable) || runner.committed {
				t.Fatalf("obsolete team role admitted: committed=%t error=%v", runner.committed, err)
			}
			if fromSource {
				value, err := (&GetMainConversationSource{SubjectType: "team", SubjectID: team.ID, WorkChatID: work.WorkChatID}).Output(httpCtx)
				if err != nil {
					t.Fatal(err)
				}
				source := value.(MainConversationSourceResponse)
				if source.Chat.WaitingApproval || source.Chat.WaitingElicitation || len(source.Chat.PendingApprovals) != 0 || len(source.Chat.PendingElicitations) != 0 {
					t.Fatalf("revoked source still returns actionable requests: %+v", source.Chat)
				}
				if len(source.Resources) != 1 || source.Resources[0].ID != "history" {
					t.Fatalf("revocation removed the original work history: %+v", source.Resources)
				}
			}
			_, found, err := store.FindMessage(ctx, work.ConversationID, mainconversation.UserMessageID(work.ConversationID, submission))
			if err != nil || found {
				t.Fatalf("obsolete team role left a public input: found=%t error=%v", found, err)
			}
		})
	}
}
