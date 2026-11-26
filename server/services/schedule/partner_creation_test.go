package schedule

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/cli"
	"github.com/JieWaZi/NeuxBot/server/internal/projectsource"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestPartnerAutomaticTaskCreation(t *testing.T) {
	ctx := t.Context()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "schedule.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	wakers := controllers.NewWakerController(db.DB)
	if err := wakers.Create(ctx, models.Waker{ID: "partner", Name: "Analyst"}); err != nil {
		t.Fatal(err)
	}
	tasks := controllers.NewAutomaticTaskController(db.DB)
	commands := controllers.NewAgentCommandController(db.DB)
	projects := controllers.NewProjectController(db.DB)
	service := New(Config{Tasks: tasks, Commands: commands, Wakers: wakers, Projects: projects,
		Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)),
		Executions:   &configurationAdmissionExecutions{},
		Now:          func() time.Time { return time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC) },
	})
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	current := models.Chat{ID: "chat", WakerID: "partner", AdditionalWorkingDirectories: []string{directory}}
	bindings := service.PartnerCreationBindings(current, "turn", execution.ExecutionConfiguration{
		RuntimeID: "codex", AccountID: "system", Model: "selected-model", PermissionLevel: constants.PermissionLevelDefault,
	})
	if len(bindings) != 1 || bindings[0].Operation != "automatic.tasks.create" {
		t.Fatalf("unexpected bindings: %+v", bindings)
	}
	binding := bindings[0]
	request := capability.Request{Operation: binding.Operation, RequestID: "once", Params: map[string]any{
		"name": "Weekly report", "instruction": "Summarize the release notes.", "schedule": map[string]any{
			"kind": "once", "timezone": "Asia/Shanghai", "onceAt": "2026-11-27T09:00",
		},
	}}
	op, ok := capability.Find(binding.Operation)
	if !ok || op.Validate(request.Params) != nil {
		t.Fatal("the scoped creation command does not accept a supported schedule")
	}
	request.Params["wakerId"] = "other"
	if op.Validate(request.Params) == nil {
		t.Fatal("the model could supply a different partner")
	}
	delete(request.Params, "wakerId")
	response := cliPartnerTask(t, binding, request, []string{directory})
	if !response.OK {
		t.Fatalf("could not create one-time schedule: %+v", response.Error)
	}
	result := response.Result
	taskID, _ := result.Data.(map[string]any)["taskId"].(string)
	task, err := tasks.Get(ctx, taskID)
	if err != nil || task.WakerID != "partner" || !task.Enabled || task.WorkingDirectory != directory ||
		task.RuntimeID != "codex" || task.Model != "selected-model" || task.PermissionLevel != constants.PermissionLevelDefault ||
		task.Instruction != "Summarize the release notes." || task.NextRunAt == nil ||
		*task.NextRunAt != time.Date(2026, 11, 27, 1, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("incorrect committed task: %+v, %v", task, err)
	}
	replayed, err := binding.Call(ctx, request)
	if err != nil || !replayed.Replayed || replayed.CommandID != result.CommandID || replayed.Data.(map[string]any)["taskId"] != taskID {
		t.Fatalf("incorrect replay: %+v, %v", replayed, err)
	}
	lookedUp, err := binding.Receipt(ctx, request.RequestID)
	if err != nil || lookedUp.Data.(map[string]any)["taskId"] != taskID {
		t.Fatalf("incorrect receipt lookup: %+v, %v", lookedUp, err)
	}
	request.Params["name"] = "Conflict"
	if _, err := binding.Call(ctx, request); !errors.Is(err, status_error.AgentCommandRequestConflict) {
		t.Fatalf("expected a request conflict, got %v", err)
	}

	request.RequestID = "weekly"
	request.Params["name"] = "Recurring report"
	request.Params["schedule"] = map[string]any{"kind": "weekly", "timezone": "Asia/Shanghai", "weekday": 5, "hour": 9, "minute": 0}
	if response := cliPartnerTask(t, binding, request, []string{directory}); !response.OK {
		t.Fatalf("could not create recurring schedule: %+v", response.Error)
	}
	request.RequestID = "ungranted-directory"
	if response := cliPartnerTask(t, binding, request, nil); response.OK ||
		response.Error == nil || response.Error.Code != status_error.AutomaticTaskWorkspaceInvalid.Key() {
		t.Fatalf("accepted the former working directory outside the active grant: %+v", response)
	}
	request.RequestID = "partner-workspace"
	managed := models.Chat{ID: "chat", WakerID: "partner", WorkspaceDirectory: directory, AdditionalWorkingDirectories: []string{directory}}
	managedBinding := service.PartnerCreationBindings(managed, "turn", execution.ExecutionConfiguration{
		RuntimeID: "codex", AccountID: "system", PermissionLevel: constants.PermissionLevelDefault,
	})[0]
	var published bool
	origin := capability.ReceiptOrigin{ChatID: managed.ID, ExecutionID: "turn", WakerID: managed.WakerID, Operation: managedBinding.Operation}
	publishCtx := capability.WithReceiptOrigin(ctx, origin, func(capability.Receipt) {
		published = true
		if !service.gate.TryLock() {
			t.Error("the chat receipt callback is holding the scheduler mutation lock")
			return
		}
		service.gate.Unlock()
	})
	managedResult, err := managedBinding.Call(publishCtx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !published {
		t.Fatal("the saved task did not publish a chat receipt")
	}
	managedID := managedResult.Data.(map[string]any)["taskId"].(string)
	managedTask, err := tasks.Get(ctx, managedID)
	if err != nil || managedTask.WorkingDirectory != "" {
		t.Fatalf("the managed workspace was promoted to an explicit local directory: %+v, %v", managedTask, err)
	}
	t.Run("edit original task", func(t *testing.T) { testPartnerTaskEditing(t, service, managed, managedTask) })

	project := models.Project{ID: "project", WakerID: "partner", Name: "Selected project"}
	if err := projects.Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	source, err := projectsource.Resolve(directory)
	if err != nil {
		t.Fatal(err)
	}
	extra, err := projectsource.Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sources := controllers.NewProjectSourceController(db.DB)
	for _, directory := range []models.ProjectSource{source, extra} {
		if err := sources.Ensure(ctx, directory); err != nil {
			t.Fatal(err)
		}
	}
	selected := []models.ProjectDirectory{{Source: source, IsDefault: true}}
	if err := projects.ReplaceDirectories(ctx, project.ID, selected); err != nil {
		t.Fatal(err)
	}
	projectChat := models.Chat{ID: "project-chat", WakerID: current.WakerID,
		ProjectID: project.ID, AdditionalWorkingDirectories: []string{source.Path}}
	projectBinding := service.PartnerCreationBindings(projectChat,
		"turn", execution.ExecutionConfiguration{RuntimeID: "codex", AccountID: "system", PermissionLevel: constants.PermissionLevelDefault})[0]
	create := projectBinding.Call
	projectBinding.Call = func(ctx context.Context, request capability.Request) (capability.Result, error) {
		origin := capability.ReceiptOrigin{ChatID: projectChat.ID, ExecutionID: "turn", WakerID: projectChat.WakerID, Operation: projectBinding.Operation}
		return create(capability.WithReceiptOrigin(ctx, origin, nil), request)
	}
	request.RequestID = "project"
	projectResponse := cliPartnerTask(t, projectBinding, request, []string{source.Path})
	if !projectResponse.OK {
		t.Fatalf("could not schedule within the granted project: %+v", projectResponse.Error)
	}
	projectTaskID := projectResponse.Result.Data.(map[string]any)["taskId"].(string)
	projectTask, err := tasks.Get(ctx, projectTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := projects.ReplaceDirectories(ctx, project.ID, append(selected, models.ProjectDirectory{Source: extra})); err != nil {
		t.Fatal(err)
	}
	request.RequestID = "expanded-project"
	if response := cliPartnerTask(t, projectBinding, request, []string{source.Path}); response.OK ||
		response.Error == nil || response.Error.Code != status_error.AutomaticTaskWorkspaceInvalid.Key() {
		t.Fatalf("accepted a project outside the active grant: %+v", response)
	}
	t.Run("edit after adding a project directory", func(t *testing.T) {
		bindings := service.PartnerEditBindings(projectChat, "project-edit-turn")
		list, update := bindings[0], bindings[1]
		edit := capability.Request{Operation: update.Operation, RequestID: "edit-expanded-project", Params: map[string]any{
			"taskId": projectTaskID, "revision": projectTask.ConfigurationVersion,
			"name": "Project report", "instruction": "Summarize project changes.",
			"schedule": map[string]any{"kind": "daily", "timezone": "Asia/Shanghai", "hour": 11, "minute": 0},
		}}
		if response := cliPartnerTask(t, update, edit, []string{source.Path}); !response.OK {
			t.Fatalf("could not edit an existing task after the project expanded: %+v", response.Error)
		}
		stored, err := tasks.Get(ctx, projectTaskID)
		if err != nil || stored.ProjectID != project.ID || stored.ConfigurationVersion != projectTask.ConfigurationVersion+1 ||
			stored.Name != "Project report" || stored.Instruction != "Summarize project changes." || stored.Schedule.Kind != constants.AutomaticScheduleKindDaily {
			t.Fatalf("the edit did not save the requested fields and keep the project: %+v, %v", stored, err)
		}
		read := capability.Request{Operation: list.Operation, Params: map[string]any{"taskId": projectTaskID}}
		listed := cliPartnerTask(t, list, read, []string{source.Path})
		if !listed.OK {
			t.Fatalf("could not read the daily schedule: %+v", listed.Error)
		}
		item := listed.Result.Data.(map[string]any)["items"].([]any)[0].(map[string]any)
		schedule := item["schedule"].(map[string]any)
		schedule["hour"] = 0
		edit.RequestID = "edit-daily-time"
		edit.Params = map[string]any{"taskId": projectTaskID, "revision": item["revision"], "schedule": schedule}
		if response := cliPartnerTask(t, update, edit, []string{source.Path}); !response.OK {
			t.Fatalf("could not update the listed daily schedule to midnight: %+v", response.Error)
		}
		stored, err = tasks.Get(ctx, projectTaskID)
		if err != nil || stored.Schedule.Hour != 0 || stored.NextRunAt == nil ||
			*stored.NextRunAt != time.Date(2026, 11, 26, 16, 0, 0, 0, time.UTC).UnixMilli() {
			t.Fatalf("the edit did not reschedule the task to midnight in its timezone: %+v, %v", stored, err)
		}
		if err := os.Remove(extra.Path); err != nil {
			t.Fatal(err)
		}
		edit.RequestID = "edit-unavailable-project"
		edit.Params = map[string]any{"taskId": projectTaskID, "revision": stored.ConfigurationVersion, "name": "Unavailable project"}
		if response := cliPartnerTask(t, update, edit, []string{source.Path}); response.OK ||
			response.Error == nil || response.Error.Code != status_error.AutomaticTaskWorkspaceInvalid.Key() {
			t.Fatalf("accepted an edit after a project directory became unavailable: %+v", response)
		}
	})

	request.RequestID = "rollback"
	service.config.Commands = rejectedPartnerCreationReceipts{Store: commands}
	binding = service.PartnerCreationBindings(managed, "turn", execution.ExecutionConfiguration{
		RuntimeID: "codex", AccountID: "system", PermissionLevel: constants.PermissionLevelDefault,
	})[0]
	if _, err := binding.Call(ctx, request); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("expected receipt commit failure, got %v", err)
	}
	_, total, err := tasks.List(ctx, controllers.AutomaticTaskFilter{WakerID: "partner"})
	if err != nil || total != 4 {
		t.Fatalf("replay, conflict or rollback changed the task count: %d, %v", total, err)
	}
}

func testPartnerTaskEditing(t *testing.T, service *Service, chat models.Chat, original models.AutomaticTask) {
	t.Helper()
	ctx := t.Context()
	bindings := service.PartnerEditBindings(chat, "later-turn")
	list, update := bindings[0], bindings[1]
	read := capability.Request{Operation: "automatic.tasks.list", Params: map[string]any{}}
	response := cliPartnerTask(t, list, read, nil)
	if !response.OK {
		t.Fatalf("cannot find the task from a later turn: %+v", response.Error)
	}
	items := response.Result.Data.(map[string]any)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["taskId"] != original.ID {
		t.Fatalf("expected only the task created by this chat: %+v", items)
	}
	read.Params["taskId"] = original.ID
	response = cliPartnerTask(t, list, read, nil)
	if !response.OK || response.Result.Data.(map[string]any)["items"].([]any)[0].(map[string]any)["instruction"] != original.Instruction {
		t.Fatalf("could not read the task's full instruction: %+v", response)
	}
	readTask := response.Result.Data.(map[string]any)["items"].([]any)[0].(map[string]any)
	edit := capability.Request{Operation: "automatic.tasks.update", RequestID: "move-to-11", Params: map[string]any{
		"taskId": original.ID, "revision": original.ConfigurationVersion,
		"schedule": map[string]any{"kind": "weekly", "timezone": "Asia/Shanghai", "weekday": 5, "hour": 11, "minute": 0},
	}}
	wrongChat := models.Chat{ID: "other-chat", WakerID: chat.WakerID}
	other := service.PartnerEditBindings(wrongChat, "other-turn")
	for _, checked := range []struct {
		// binding 是跨对话调用的读取或更新能力。
		binding capability.Binding
		// request 引用实际存在、但归属另一对话的任务。
		request capability.Request
	}{{other[0], read}, {other[1], edit}} {
		response := cliPartnerTask(t, checked.binding, checked.request, nil)
		if response.OK || response.Error == nil || response.Error.Code != status_error.AutomaticTaskNotFound.Key() {
			t.Fatalf("accepted an edit or read by another chat: %+v", response)
		}
	}
	schedule := readTask["schedule"].(map[string]any)
	schedule["hour"] = 11
	edit.Params["schedule"] = schedule
	response = cliPartnerTask(t, update, edit, nil)
	if !response.OK || response.Result.Data.(map[string]any)["taskId"] != original.ID {
		t.Fatalf("could not reschedule the original task using the listed rule: %+v", response.Error)
	}
	replayed := cliPartnerTask(t, update, edit, nil)
	if !replayed.OK || !replayed.Result.Replayed || replayed.Result.CommandID != response.Result.CommandID {
		t.Fatalf("a repeated update did not recover the original receipt: %+v", replayed)
	}
	afterTime, err := service.config.Tasks.Get(ctx, original.ID)
	if err != nil || afterTime.Instruction != original.Instruction || afterTime.Schedule.Hour != 11 ||
		afterTime.ConfigurationVersion != original.ConfigurationVersion+1 || afterTime.NextRunAt == nil ||
		*afterTime.NextRunAt != time.Date(2026, 11, 27, 3, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("rescheduling did not preserve the task and recalculate its next run: %+v, %v", afterTime, err)
	}
	edit.RequestID = "revise-instruction"
	edit.Params = map[string]any{"taskId": original.ID, "revision": original.ConfigurationVersion, "name": "Release summary", "instruction": "Cover the last 24 hours."}
	if stale := cliPartnerTask(t, update, edit, nil); stale.OK || stale.Error == nil || stale.Error.Code != status_error.AutomaticTaskRevisionConflict.Key() {
		t.Fatalf("accepted a stale configuration revision: %+v", stale)
	}
	clock := service.config.Now
	service.config.Now = func() time.Time { return time.UnixMilli(*afterTime.NextRunAt).Add(time.Minute) }
	defer func() { service.config.Now = clock }()
	edit.Params["revision"] = afterTime.ConfigurationVersion
	if edited := cliPartnerTask(t, update, edit, nil); !edited.OK {
		t.Fatalf("could not update the name and instruction: %+v", edited.Error)
	}
	afterInstruction, err := service.config.Tasks.Get(ctx, original.ID)
	if err != nil || afterInstruction.Instruction != "Cover the last 24 hours." || afterInstruction.Name != "Release summary" ||
		afterInstruction.Schedule != afterTime.Schedule || afterInstruction.WorkingDirectory != original.WorkingDirectory || afterInstruction.Enabled != original.Enabled ||
		afterInstruction.NextRunAt == nil || *afterInstruction.NextRunAt != *afterTime.NextRunAt {
		t.Fatalf("editing the instruction changed unrelated configuration or failed: %+v, %v", afterInstruction, err)
	}

	commands := service.config.Commands
	service.config.Commands = rejectedPartnerCreationReceipts{Store: commands}
	defer func() { service.config.Commands = commands }()
	edit.RequestID = "rollback-edit"
	edit.Params = map[string]any{"taskId": original.ID, "revision": afterInstruction.ConfigurationVersion, "name": "Should roll back"}
	if rejected := cliPartnerTask(t, update, edit, nil); rejected.OK || rejected.Error == nil || rejected.Error.Code != status_error.BadRequest.Key() {
		t.Fatalf("expected an update receipt failure: %+v", rejected)
	}
	stored, err := service.config.Tasks.Get(ctx, original.ID)
	if err != nil || stored.Name != afterInstruction.Name || stored.ConfigurationVersion != afterInstruction.ConfigurationVersion {
		t.Fatalf("update did not roll back with its receipt: %+v, %v", stored, err)
	}
}

type rejectedPartnerCreationReceipts struct {
	// Store supplies existing receipt reads while the test rejects inserts.
	capability.Store
}

func (rejectedPartnerCreationReceipts) Create(context.Context, capability.Receipt) error {
	return status_error.BadRequest
}

func cliPartnerTask(t *testing.T, binding capability.Binding, request capability.Request, roots []string) capability.Response {
	t.Helper()
	registry := capability.New(nil)
	t.Cleanup(registry.Close)
	grant, activate, revoke, err := registry.Prepare(capability.GrantSpec{
		ExecutionID: "turn", Bindings: []capability.Binding{binding}, FileRoots: roots, Validate: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(revoke)
	activate()
	endpoint := httptest.NewServer(registry)
	t.Cleanup(endpoint.Close)
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	connection, err := capability.WriteConnection(directory, "context.json", capability.Connection{
		ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: endpoint.URL + constants.AgentCLIPath,
		CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(constants.AgentCLIContextEnvironment, connection)
	encoded, err := json.Marshal(request.Params)
	if err != nil {
		t.Fatal(err)
	}
	var output, failure bytes.Buffer
	args := append(strings.Split(request.Operation, "."), "--input", "-")
	if request.RequestID != "" {
		args = append(args, "--request-id", request.RequestID)
	}
	code := cli.Execute(t.Context(), args,
		cli.IO{In: bytes.NewReader(encoded), Out: &output, Err: &failure})
	var response capability.Response
	body := output.Bytes()
	if code != 0 {
		body = failure.Bytes()
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("botcli response is not JSON: %s %s", &output, &failure)
	}
	return response
}
