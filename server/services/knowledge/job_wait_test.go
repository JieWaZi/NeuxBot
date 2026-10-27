package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcli"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
)

func observedMaintenance(t *testing.T) (*Service, *core.Library, *core.Job, agentcapability.Binding) {
	t.Helper()
	s, lib := newServiceFixture(t)
	s.config.Events = events.New()
	t.Cleanup(s.config.Events.Close)
	current := selectedChat{chat: models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "execution"}}
	s.config.Chats = current
	receipt, err := s.config.Engine.AcceptJob(context.Background(), core.AcceptJobInput{Maintenance: true, RequestID: "maintenance", LibraryID: lib.ID, ExpectedVersion: lib.Version, StructureVersion: lib.StructureVersion, Permission: core.PermissionSnapshot{PolicyVersion: lib.PolicyVersion, Operations: []string{"maintain"}}, RulesVersion: "maintenance", ConfigDigest: "frozen"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.config.Engine.GetJob(context.Background(), receipt.JobID)
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range s.AgentBindings(context.Background(), current.chat) {
		if binding.Operation == "knowledge.jobs.wait" {
			return s, lib, job, binding
		}
	}
	t.Fatal("chat cannot wait for its accepted knowledge task")
	return nil, nil, nil, agentcapability.Binding{}
}

func TestKnowledgeJobWaitTimeoutCanResumeToSavedReport(t *testing.T) {
	s, lib, job, wait := observedMaintenance(t)
	ctx := context.Background()
	request := agentcapability.Request{Params: map[string]any{"libraryId": lib.ID, "jobId": job.ID, "timeoutMs": 1}}
	result, err := wait.Call(ctx, request)
	if err != nil || result.Data.(map[string]any)["completed"] != false {
		t.Fatalf("pending observation: %+v %v", result, err)
	}
	job, ready, err := s.config.Engine.ActivateQueuedJob(ctx, job.ID)
	if err != nil || !ready {
		t.Fatalf("observation cancelled or changed job: %+v %v", job, err)
	}
	report := "范围：两页。合并 duplicate → canonical，保留来源；canonical v2。遗留：一张过期图片，缺少可验证替代链接。"
	if _, err := s.config.Engine.CompleteMaintenanceJob(ctx, core.JobActionInput{RequestID: "complete", JobID: job.ID, ExpectedVersion: job.Version}, report); err != nil {
		t.Fatal(err)
	}
	result, err = wait.Call(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	data := result.Data.(map[string]any)
	resource := data["resource"].(map[string]any)
	if data["completed"] != true || resource["job"].(*core.Job).State != "completed" || resource["report"] != report {
		t.Fatalf("lost terminal state or durable report: %+v", data)
	}
}

func TestKnowledgeJobWaitWakesOnCommitWithoutHoldingPolicyLock(t *testing.T) {
	s, lib, job, _ := observedMaintenance(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var get agentcapability.Binding
	for _, binding := range s.AgentBindings(ctx, s.config.Chats.(selectedChat).chat) {
		if binding.Operation == "knowledge.jobs.get" {
			get = binding
		}
	}
	read := make(chan struct{}, 2)
	call := get.Call
	get.Call = func(ctx context.Context, request agentcapability.Request) (agentcapability.Result, error) {
		result, err := call(ctx, request)
		read <- struct{}{}
		return result, err
	}
	wait := s.jobWaitBinding(get)
	done := make(chan error, 1)
	go func() {
		result, err := wait.Call(ctx, agentcapability.Request{Params: map[string]any{"libraryId": lib.ID, "jobId": job.ID, "timeoutMs": 60000}})
		if err == nil && result.Data.(map[string]any)["completed"] != true {
			err = errors.New("event did not deliver terminal result")
		}
		done <- err
	}()
	select {
	case <-read:
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not read initial snapshot")
	}
	changed := make(chan error, 1)
	go func() {
		changed <- s.WithPolicyWrite(func() error {
			_, err := s.config.Engine.CancelOrganize(ctx, core.JobActionInput{RequestID: "cancel", JobID: job.ID, ExpectedVersion: job.Version})
			return err
		})
	}()
	select {
	case err := <-changed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait held policy lock while idle")
	}
	s.emitTaskChanged(job.ID, lib.ID)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("committed cancellation did not wake observer")
	}
}

func TestKnowledgeJobWaitFailureIsTerminalAndKeepsDiagnostic(t *testing.T) {
	s, lib, job, wait := observedMaintenance(t)
	ctx := context.Background()
	job, ready, err := s.config.Engine.ActivateQueuedJob(ctx, job.ID)
	if err != nil || !ready {
		t.Fatal(job, err)
	}
	if _, err := s.config.Engine.FailJob(ctx, core.FailJobInput{JobActionInput: core.JobActionInput{RequestID: "fail", JobID: job.ID, ExpectedVersion: job.Version}, Diagnostic: "model disconnected"}); err != nil {
		t.Fatal(err)
	}
	result, err := wait.Call(ctx, agentcapability.Request{Params: map[string]any{"libraryId": lib.ID, "jobId": job.ID}})
	if err != nil {
		t.Fatal(err)
	}
	data := result.Data.(map[string]any)
	resource := data["resource"].(map[string]any)
	got := resource["job"].(*core.Job)
	if data["completed"] != true || got.State != "failed" || got.Diagnostic != "model disconnected" || resource["report"] != nil {
		t.Fatalf("failure disguised as success or lost diagnostics: %+v", data)
	}
}

func TestKnowledgeJobWaitRejectsCrossLibraryRemoteAndCancelledObserver(t *testing.T) {
	s, lib, job, wait := observedMaintenance(t)
	ctx := context.Background()
	other, err := s.config.Engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "other", Name: "Other", Kind: "general", WikiLanguage: "en-US"})
	if err != nil {
		t.Fatal(err)
	}
	// 两库都可见时也不能用另一库读取原任务。
	for _, binding := range s.AgentBindings(ctx, s.config.Chats.(selectedChat).chat) {
		if binding.Operation == "knowledge.jobs.wait" {
			wait = binding
		}
	}
	for _, params := range []map[string]any{
		{"libraryId": "unseen", "jobId": job.ID},
		{"libraryId": other.ResourceID, "jobId": job.ID},
		{"libraryId": lib.ID, "backend": "custom", "connectionId": "remote", "jobId": job.ID},
	} {
		if _, err := wait.Call(ctx, agentcapability.Request{Params: params}); err == nil {
			t.Fatalf("unauthorized wait accepted: %v", params)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := wait.Call(cancelled, agentcapability.Request{Params: map[string]any{"libraryId": lib.ID, "jobId": job.ID}}); err == nil {
		t.Fatal("cancelled observer kept waiting")
	}
	got, err := s.config.Engine.GetJob(ctx, job.ID)
	if err != nil || got.State != "queued" {
		t.Fatalf("observer cancellation cancelled background work: %+v %v", got, err)
	}
	s.config.Events = nil
	if _, err := wait.Call(ctx, agentcapability.Request{Params: map[string]any{"libraryId": lib.ID, "jobId": job.ID}}); !errors.Is(err, status_error.AgentConnectionUnavailable) {
		t.Fatalf("missing event channel: %v", err)
	}
}

func TestKnowledgeJobWaitCLIResumesSameTaskAndReturnsReport(t *testing.T) {
	s, lib, job, wait := observedMaintenance(t)
	ctx := context.Background()
	registry := agentcapability.New(builtinskills.Guide)
	defer registry.Close()
	grant, activate, revoke, err := registry.Prepare(agentcapability.GrantSpec{ExecutionID: "execution", Bindings: []agentcapability.Binding{wait}, Validate: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer revoke()
	activate()
	server := httptest.NewServer(registry)
	defer server.Close()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	connection, err := agentcapability.WriteConnection(directory, "context.json", agentcapability.Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: server.URL + constants.AgentCLIPath, CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token})
	if err != nil {
		t.Fatal(err)
	}
	invoke := func() map[string]any {
		t.Helper()
		var output, stderr bytes.Buffer
		code := agentcli.Execute(ctx, []string{"knowledge", "jobs", "wait", "--library-id", lib.ID, "--job-id", job.ID, "--timeout", "1ms"}, agentcli.IO{Out: &output, Err: &stderr, ContextFile: connection})
		var response agentcapability.Response
		if err := json.Unmarshal(output.Bytes(), &response); err != nil || code != 0 || !response.OK {
			t.Fatalf("CLI wait: code=%d stdout=%s stderr=%s error=%v", code, &output, &stderr, err)
		}
		return response.Data.(map[string]any)
	}
	if data := invoke(); data["completed"] != false || data["resource"].(map[string]any)["job"].(map[string]any)["id"] != job.ID {
		t.Fatalf("CLI lost pending task: %+v", data)
	}
	job, ready, err := s.config.Engine.ActivateQueuedJob(ctx, job.ID)
	if err != nil || !ready {
		t.Fatal(job, err)
	}
	const report = "已合并 A 到 B v3，保留来源；遗留过期图片一张，尚无替代资料。"
	if _, err := s.config.Engine.CompleteMaintenanceJob(ctx, core.JobActionInput{RequestID: "complete", JobID: job.ID, ExpectedVersion: job.Version}, report); err != nil {
		t.Fatal(err)
	}
	if data := invoke(); data["completed"] != true || data["resource"].(map[string]any)["report"] != report {
		t.Fatalf("CLI lost saved report: %+v", data)
	}
}

func TestAutomaticIngestionJobWaitKeepsOriginalInvocationGuard(t *testing.T) {
	s, lib, job, _ := observedMaintenance(t)
	ctx := context.Background()
	current := s.config.Chats.(selectedChat)
	authorization := models.KnowledgeMaintenanceAuthorization{Backend: "local", ConnectionID: "local", KnowledgeBaseID: lib.ID, Operation: "organize"}
	run := models.AutomaticRun{ID: "run", WakerID: current.chat.WakerID, Configuration: models.AutomaticTaskConfiguration{Knowledge: &authorization}, IntegrationInvocation: &models.IntegrationInvocation{}}
	s.config.AutomaticRun = func(context.Context, string) (models.AutomaticRun, error) { return run, nil }
	revoked := false
	s.config.InvocationGuard = func(context.Context, string) (func(), error) {
		if revoked {
			return nil, status_error.IntegrationForbidden
		}
		return func() {}, nil
	}
	var wait agentcapability.Binding
	for _, binding := range s.AutomaticBindings(ctx, current.chat, run.ID) {
		if binding.Operation == "knowledge.jobs.wait" {
			wait = binding
		}
	}
	if wait.Call == nil {
		t.Fatal("automatic ingestion cannot follow the shared observation guide")
	}
	request := agentcapability.Request{Params: map[string]any{"libraryId": lib.ID, "jobId": job.ID, "timeoutMs": 1}}
	if result, err := wait.Call(ctx, request); err != nil || result.Data.(map[string]any)["completed"] != false {
		t.Fatalf("authorized observation failed: %+v %v", result, err)
	}
	revoked = true
	if _, err := wait.Call(ctx, request); !errors.Is(err, status_error.IntegrationForbidden) {
		t.Fatalf("observer detached from revoked invocation: %v", err)
	}
}
