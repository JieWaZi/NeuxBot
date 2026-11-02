package automatic_tasks

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// deletionAuthorizationScope 标记组合根授权回调传入的可重入 Context。
type deletionAuthorizationScope struct{}

// coordinatedDeletionBoundary 用真实 Chat 索引写入与事务验证最终聚合删除的锁序。
type coordinatedDeletionBoundary struct {
	// testExecutionBoundary 提供已经终结的首次执行快照。
	*testExecutionBoundary
	// chats 执行实际索引删除，使事务在任务 finalize 前持有数据库写锁。
	chats controllers.ChatController
	// entered 通知并发撤权已进入最终删除事务。
	entered chan struct{}
	// finish 由测试在并发撤权请求就绪后放行事务。
	finish chan struct{}
	// failFinalize 验证最终事务失败会回滚 Chat 索引和任务。
	failFinalize bool
}

func (b *coordinatedDeletionBoundary) DeleteChatsAtomically(ctx context.Context, ids []string, finalize func(context.Context) error) error {
	// 缺少外层授权屏障时立即失败，避免回归测试本身永久陷入 policy/SQLite 锁环。
	if ctx.Value(deletionAuthorizationScope{}) != true {
		return status_error.IntegrationForbidden
	}
	return b.transactions.Transaction(ctx, func(tx context.Context) error {
		for _, id := range ids {
			if err := b.chats.Delete(tx, id); err != nil {
				return err
			}
		}
		close(b.entered)
		<-b.finish
		if err := finalize(tx); err != nil {
			return err
		}
		if b.failFinalize {
			return status_error.DatabaseError
		}
		return nil
	})
}

func TestAutomaticDeleteSerializesFinalTransactionBeforeKeyRevocation(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[rollback], func(t *testing.T) {
			ctx := t.Context()
			db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "deletion.db"), Migrate: true})
			if err != nil {
				t.Fatal(err)
			}
			pool, _ := db.DB.DB()
			t.Cleanup(func() { _ = pool.Close() })
			tx := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB))
			tasks := controllers.NewAutomaticTaskController(db.DB)
			keys := controllers.NewIntegrationKeyController(db.DB)
			chats := controllers.NewChatController(db.DB)
			auth := integrationservice.New(integrationservice.Config{Keys: keys, Tasks: tasks})
			if err := tasks.Create(ctx, models.AutomaticTask{ID: "task", WakerID: "waker"}); err != nil {
				t.Fatal(err)
			}
			run := models.AutomaticRun{ID: "run", TaskID: "task", WakerID: "waker", ChatID: "chat", ExecutionID: "execution", Admission: constants.AutomaticRunAdmissionAccepted}
			if err := tasks.CreateRun(ctx, run); err != nil {
				t.Fatal(err)
			}
			if err := chats.Create(ctx, models.Chat{ID: "chat", WakerID: "waker"}); err != nil {
				t.Fatal(err)
			}
			mutation := models.IntegrationKeyMutation{RequestID: "key-create", Digest: "key-create", KeyID: "key", Action: "create"}
			if _, _, err := keys.ClaimMutation(ctx, mutation); err != nil {
				t.Fatal(err)
			}
			key, err := keys.CompleteMutation(ctx, mutation.RequestID, mutation.Digest, models.IntegrationKey{ID: "key", Name: "并发撤权", KeyHash: strings.Repeat("a", 64), Enabled: true}, 0)
			if err != nil {
				t.Fatal(err)
			}
			boundary := &coordinatedDeletionBoundary{testExecutionBoundary: &testExecutionBoundary{transactions: tx, states: map[string]chat.InitialExecutionState{
				"chat": {Chat: models.Chat{ID: "chat"}, InitialExecution: &models.Execution{ID: "execution", Status: constants.ExecutionStatusCompleted}},
			}}, chats: chats, entered: make(chan struct{}), finish: make(chan struct{}), failFinalize: rollback}
			config := schedule.Config{Tasks: auth.CoordinateTasks(tasks), Transactions: tx, Chats: boundary,
				AuthorizationWrite: func(ctx context.Context, change func(context.Context) error) error {
					return auth.WithAuthorizationWrite(ctx, func(locked context.Context) error {
						return change(context.WithValue(locked, deletionAuthorizationScope{}, true))
					})
				},
			}
			api := taskAPI{config: config, service: schedule.New(config)}
			deleted := make(chan error, 1)
			go func() { deleted <- api.Delete(ctx, "waker", "task") }()
			select {
			case err := <-deleted:
				t.Fatalf("final deletion bypassed authorization coordination: %v", err)
			case <-boundary.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("deletion did not reach final transaction")
			}
			requested := make(chan struct{})
			revoked := make(chan error, 1)
			go func() {
				close(requested)
				revoked <- auth.WithAuthorizationWrite(ctx, func(locked context.Context) error { return keys.Delete(locked, key.ID, key.Revision) })
			}()
			<-requested
			close(boundary.finish)
			select {
			case err := <-deleted:
				if rollback && !errors.Is(err, status_error.DatabaseError) {
					t.Fatalf("rollback error: %v", err)
				}
				if !rollback && err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("final deletion and revocation deadlocked")
			}
			select {
			case err := <-revoked:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("key revocation did not finish")
			}
			_, taskErr := tasks.Get(ctx, "task")
			_, chatErr := chats.Get(ctx, "chat")
			if rollback {
				if taskErr != nil || chatErr != nil {
					t.Fatalf("rollback lost aggregate: task=%v chat=%v", taskErr, chatErr)
				}
			} else if !errors.Is(taskErr, status_error.AutomaticTaskNotFound) || !util.HasStatusCode(chatErr, status_error.ChatNotFound) {
				t.Fatalf("aggregate survived commit: task=%v chat=%v", taskErr, chatErr)
			}
		})
	}
}
