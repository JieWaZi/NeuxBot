package automatic_tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// configurationBoundary 只替代外部 CLI 可用性，CRUD 使用真实持久化和配置校验。
type configurationBoundary struct{}

func (configurationBoundary) ValidateConfiguration(context.Context, execution.ExecutionConfiguration) error {
	return nil
}
func (configurationBoundary) Start(context.Context, execution.WorkIntent) (execution.AcceptedExecution, error) {
	return execution.AcceptedExecution{}, status_error.RuntimeUnavailable
}

func bindAndOutput(t *testing.T, operator ginx.HandleOperator, wakerID, taskID, query string, body interface{}) (interface{}, error) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Params = gin.Params{{Key: "wakerId", Value: wakerID}, {Key: "taskId", Value: taskID}}
	ctx.Request = httptest.NewRequest(operator.Method(), "/automatic-tasks"+query, bytes.NewReader(data))
	ctx.Request.Header.Set("Content-Type", "application/json")
	if err := ginx.ParameterBinding(ctx, operator, ginx.GetOperatorTypeInfo(reflect.TypeOf(operator))); err != nil {
		return nil, err
	}
	if err := operator.Validate(ctx); err != nil {
		return nil, err
	}
	return operator.Output(ctx)
}

func TestManagementBindingsCRUDAndTaskReferences(t *testing.T) {
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "management.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB.DB()
	t.Cleanup(func() { _ = sql.Close() })
	wakers := controllers.NewWakerController(db.DB)
	wakerID := util.New()
	if err := wakers.Create(context.Background(), models.Waker{ID: wakerID, Name: "自动伙伴"}); err != nil {
		t.Fatal(err)
	}
	previous := global.ScheduleService
	dependencies := schedule.Config{Tasks: controllers.NewAutomaticTaskController(db.DB), Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)), Wakers: wakers, Projects: controllers.NewProjectController(db.DB), Executions: configurationBoundary{}}
	global.ScheduleService = schedule.New(dependencies)
	previousTasks, previousTransactions, previousWakers := global.AutomaticTaskControllerManager, global.TransactionControllerManager, global.WakerControllerManager
	global.AutomaticTaskControllerManager, global.TransactionControllerManager, global.WakerControllerManager = dependencies.Tasks, dependencies.Transactions, dependencies.Wakers
	t.Cleanup(func() {
		global.ScheduleService = previous
		global.AutomaticTaskControllerManager, global.TransactionControllerManager, global.WakerControllerManager = previousTasks, previousTransactions, previousWakers
	})
	config := models.AutomaticTaskConfiguration{Name: "归档", Instruction: "整理资料", Trigger: constants.AutomaticTaskTriggerAPI, WorkingDirectory: t.TempDir(), RuntimeID: "codex", AccountID: "system", Model: "model", PermissionLevel: constants.PermissionLevelDefault, Enabled: true}
	result, err := bindAndOutput(t, &CreateAutomaticTask{}, wakerID, "", "", config)
	if err != nil {
		t.Fatal(err)
	}
	created := result.(AutomaticTaskReference)
	if created.TaskID == "" {
		t.Fatalf("missing creation receipt: %+v", created)
	}
	result, err = bindAndOutput(t, &GetAutomaticTask{}, wakerID, created.TaskID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	task := result.(schedule.TaskView)
	encoded, _ := json.Marshal(task)
	if bytes.Contains(encoded, []byte(`"token"`)) || bytes.Contains(encoded, []byte("tokenHash")) {
		t.Fatal("read leaked token")
	}
	config.Name = "更新归档"
	config.Enabled = false
	_, err = bindAndOutput(t, &UpdateAutomaticTask{}, wakerID, created.TaskID, "", UpdateAutomaticTaskRequest{AutomaticTaskConfiguration: config, Revision: task.ConfigurationVersion})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bindAndOutput(t, &UpdateAutomaticTask{}, wakerID, created.TaskID, "", UpdateAutomaticTaskRequest{AutomaticTaskConfiguration: config, Revision: task.ConfigurationVersion}); !errors.Is(err, status_error.AutomaticTaskRevisionConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	result, err = bindAndOutput(t, &ListAutomaticTasks{}, wakerID, "", "?enabled=false&query=更新&pageNum=1&pageSize=20", nil)
	if err != nil {
		t.Fatal(err)
	}
	list := result.(ListAutomaticTasksResponse)
	if len(list.Resources) != 1 || list.TotalSize != 1 || list.Counts.Total != 1 || list.Counts.Enabled != 0 {
		t.Fatalf("false filter or counts: %+v", list)
	}
	task = list.Resources[0]
	if _, err := bindAndOutput(t, &GetAutomaticTask{}, "another-waker", created.TaskID, "", nil); !errors.Is(err, status_error.AutomaticTaskNotFound) {
		t.Fatalf("scope: %v", err)
	}
	if _, err := bindAndOutput(t, &ListAutomaticTasks{}, wakerID, "", "?trigger=invalid", nil); err == nil {
		t.Fatal("invalid enum accepted")
	}

	otherID := util.New()
	if err := wakers.Create(context.Background(), models.Waker{ID: otherID, Name: "另一伙伴"}); err != nil {
		t.Fatal(err)
	}
	config.Name = "另一伙伴的任务"
	result, err = bindAndOutput(t, &CreateAutomaticTask{}, otherID, "", "", config)
	if err != nil {
		t.Fatal(err)
	}
	otherTask := result.(AutomaticTaskReference)
	result, err = bindAndOutput(t, &ListGlobalAutomaticTasks{}, "", "", "?pageNum=1&pageSize=20", nil)
	if err != nil {
		t.Fatal(err)
	}
	globalList := result.(ListAutomaticTasksResponse)
	if globalList.TotalSize != 2 || globalList.Counts.Total != 2 || len(globalList.Resources) != 2 {
		t.Fatalf("全局列表遗漏执行者: %+v", globalList)
	}
	result, err = bindAndOutput(t, &GetGlobalAutomaticTask{}, "", otherTask.TaskID, "", nil)
	if err != nil || result.(schedule.TaskView).ExecutorName != "另一伙伴" {
		t.Fatalf("全局详情: %+v %v", result, err)
	}
	if _, err := bindAndOutput(t, &ListGlobalAutomaticTasks{}, "", "", "?executorKind=invalid", nil); err == nil {
		t.Fatal("无效执行者类型未拒绝")
	}
	if _, err := bindAndOutput(t, &DeleteAutomaticTask{}, wakerID, created.TaskID, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := bindAndOutput(t, &GetAutomaticTask{}, wakerID, created.TaskID, "", nil); !errors.Is(err, status_error.AutomaticTaskNotFound) {
		t.Fatalf("deleted task remained: %v", err)
	}
}
