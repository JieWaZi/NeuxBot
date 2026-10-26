package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/httpapi"
	"gorm.io/gorm"
	"sync/atomic"
	"testing"
	"time"
)

func regressionJob(t *testing.T, s *Service, lib *core.Library) *core.Job {
	t.Helper()
	ctx := context.Background()
	_, e := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "source", LibraryID: lib.ID, Filename: "a.md", Bytes: []byte("原始依据")})
	if e != nil {
		t.Fatal(e)
	}
	r, _, e := s.Organize(ctx, OrganizeCommand{RequestID: "job", LibraryID: lib.ID, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: constants.RuntimeIDCodex}})
	if e != nil {
		t.Fatal(e)
	}
	j, e := s.config.Engine.GetJob(ctx, r.ResourceID)
	if e != nil {
		t.Fatal(e)
	}
	return j
}
func TestDefaultOrganizeSelectsOnlyCurrentSourceHeads(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	r, e := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "v1", LibraryID: lib.ID, Filename: "a.md", Bytes: []byte("v1")})
	if e != nil {
		t.Fatal(e)
	}
	v, _ := s.config.Engine.GetSource(ctx, lib.ID, r.ResourceID)
	_, e = s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "v2", LibraryID: lib.ID, SourceID: v.SourceID, ExpectedVersion: v.Version, Filename: "a.md", Bytes: []byte("v2")})
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = s.Organize(ctx, OrganizeCommand{RequestID: "job", LibraryID: lib.ID, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: constants.RuntimeIDCodex}})
	if e != nil {
		t.Fatalf("default organize rejected replaced pending source: %v", e)
	}
}
func TestStartIgnoresHistoryOfDeletedLibrary(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	job := regressionJob(t, s, lib)
	if _, err := s.config.Engine.CancelOrganize(ctx, core.JobActionInput{RequestID: "cancel-before-delete", JobID: job.ID, ExpectedVersion: job.Version}); err != nil {
		t.Fatal(err)
	}
	current, err := s.config.Engine.GetLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.config.Engine.DeleteLibrary(ctx, core.DeleteLibraryInput{RequestID: "delete", LibraryID: lib.ID, ExpectedVersion: current.Version}); err != nil {
		t.Fatal(err)
	}
	if err = s.config.References.DeleteLibraryReferences(ctx, "local", "local", lib.ID); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Start(ctx); err != nil {
		t.Fatalf("deleted historical job blocked startup: %v", err)
	}
	security, err := httpapi.NewPolicyStore(httpapi.SecurityConfig{Tokens: map[string]httpapi.Principal{"test-token": {Subject: "test-user"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewRemoteAdapter(s, ProfileMap{"test": {RuntimeID: "codex"}}, security); err != nil {
		t.Fatalf("deleted history blocked remote adapter startup: %v", err)
	}

	if _, err = s.config.References.GetOperation(ctx, "job"); err != nil {
		t.Fatalf("historical audit anchor was removed: %v", err)
	}
}

// startupConfigurationReferences 用稳定错误模拟当前任务配置缺失与数据库故障。
type startupConfigurationReferences struct {
	// KnowledgeController 保留真实操作枚举与查询。
	controllers.KnowledgeController
	// configurationError 是任务配置读取结果。
	configurationError error
}

func (r startupConfigurationReferences) GetTaskConfiguration(context.Context, string) (models.KnowledgeTaskConfiguration, error) {
	return models.KnowledgeTaskConfiguration{}, r.configurationError
}
func TestStartRejectsMissingConfigurationAndPreservesDatabaseFailure(t *testing.T) {
	for _, expected := range []error{status_error.KnowledgeNotFound, status_error.DatabaseBusy} {
		t.Run(expected.Error(), func(t *testing.T) {
			s, lib := newServiceFixture(t)
			regressionJob(t, s, lib)
			s.config.References = startupConfigurationReferences{KnowledgeController: s.config.References, configurationError: expected}
			defer s.Close()
			err := s.Start(context.Background())
			if !errors.Is(err, expected) {
				t.Fatalf("task configuration failure swallowed: %v", err)
			}
			guardErr := s.SetRemoteGuard(func(context.Context, json.RawMessage) (func(), error) { return func() {}, nil })
			if !errors.Is(guardErr, expected) {
				t.Fatalf("remote guard swallowed task configuration failure: %v", guardErr)
			}
		})
	}
}

func TestRemoteGuardIgnoresDeletedJobWithRetainedConfiguration(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	if err := s.config.References.BeginOperation(ctx, models.KnowledgeOperationLink{RequestID: "stale-remote", Operation: "job.accept", Digest: "stale", Backend: "local", ConnectionID: "local", KnowledgeBaseID: lib.ID, ResourceID: "missing-job", ConfigurationID: "configuration", ReceiptConfirmed: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.config.References.FreezeTaskConfiguration(ctx, models.KnowledgeTaskConfiguration{ID: "configuration", KnowledgeBaseID: lib.ID, RuntimeID: "codex", RemoteSubject: "remote-user"}); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRemoteGuard(func(context.Context, json.RawMessage) (func(), error) { return func() {}, nil }); err != nil {
		t.Fatalf("missing remote job blocked startup: %v", err)
	}
}

func TestRecoveryReadsBoundedCandidatesDespiteHistoricalGrowth(t *testing.T) {
	for _, history := range []int{0, 1000} {
		t.Run(fmt.Sprintf("history-%d", history), func(t *testing.T) {
			s, lib, _, db := newServiceFixtureWithDB(t)
			ctx := context.Background()
			rows := make([]models.KnowledgeOperationLink, history)
			for i := range rows {
				rows[i] = models.KnowledgeOperationLink{RequestID: fmt.Sprintf("old-%d", i), Operation: "job.accept", Digest: "historical", Backend: "local", ConnectionID: "local", KnowledgeBaseID: lib.ID, ResourceID: fmt.Sprintf("deleted-job-%d", i), ReceiptConfirmed: true}
			}
			if history > 0 {
				if err := db.CreateInBatches(&rows, 100).Error; err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "raw", LibraryID: lib.ID, Filename: "a.md", Bytes: []byte("依据")}); err != nil {
				t.Fatal(err)
			}
			s.config.RemoteGuard = func(context.Context, json.RawMessage) (func(), error) { return func() {}, nil }
			recovery, _ := json.Marshal(httpapi.RecoveryIdentity{Subject: "user", Permission: core.PermissionSnapshot{PolicyVersion: lib.PolicyVersion, Operations: []string{"organize"}}})
			if _, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "active", LibraryID: lib.ID, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: "codex", RemoteSubject: "user", RemoteAuthorization: recovery}}); err != nil {
				t.Fatal(err)
			}
			s.config.RemoteGuard = nil
			var queries, readRows atomic.Int64
			if err := db.Callback().Query().After("gorm:query").Register("count-recovery-links", func(tx *gorm.DB) {
				if tx.Statement.Table == "knowledge_operation_links" {
					queries.Add(1)
					readRows.Add(tx.RowsAffected)
				}
			}); err != nil {
				t.Fatal(err)
			}
			s.config.Generator = func(ctx context.Context, _ models.LibraryExecutionDefaults) (core.Generator, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			defer s.Close()
			if err := s.Start(ctx); err != nil {
				t.Fatal(err)
			}
			startupRows, startupQueries := readRows.Load(), queries.Load()
			if err := s.SetRemoteGuard(func(context.Context, json.RawMessage) (func(), error) { return func() {}, nil }); err != nil {
				t.Fatal(err)
			}
			guardRows, guardQueries := readRows.Load()-startupRows, queries.Load()-startupQueries
			t.Logf("history=%d startup SQL=%d link rows=%d; guard SQL=%d link rows=%d", history, startupQueries, startupRows, guardQueries, guardRows)
			if startupRows > 1 || guardRows > 1 || startupQueries > 2 || guardQueries > 1 {
				t.Fatalf("recovery expanded completed history: startup=%d guard=%d", startupRows, guardRows)
			}
		})
	}
}

func TestRecoveryIgnoresActiveJobWithoutHostAcceptance(t *testing.T) {
	s, lib, _, db := newServiceFixtureWithDB(t)
	job := regressionJob(t, s, lib)
	if err := db.Where("request_id = ?", job.RequestID).Delete(&models.KnowledgeOperationLink{}).Error; err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Start(context.Background()); err != nil {
		t.Fatal("core activity owned by another caller blocked host startup", err)
	}
	current, err := s.config.Engine.GetJob(context.Background(), job.ID)
	if err != nil || current.State != "queued" {
		t.Fatal("foreign core job was driven", current, err)
	}
}

func TestRecoveryDefersRemoteQueueUntilGuardIsAssembled(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	s.config.RemoteGuard = func(context.Context, json.RawMessage) (func(), error) { return func() {}, nil }
	recovery, _ := json.Marshal(httpapi.RecoveryIdentity{Subject: "user", Permission: core.PermissionSnapshot{PolicyVersion: lib.PolicyVersion, Operations: []string{"organize"}}})
	for _, id := range []string{"first", "second"} {
		raw, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: id + "-raw", LibraryID: lib.ID, Filename: id + ".md", Bytes: []byte(id)})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = s.Organize(ctx, OrganizeCommand{RequestID: id + "-job", LibraryID: lib.ID, SourceVersionIDs: []string{raw.ResourceID}, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: "codex", Model: id, RemoteSubject: "user", RemoteAuthorization: recovery}}); err != nil {
			t.Fatal(err)
		}
	}
	s.config.RemoteGuard = nil
	// 真实宿主会先装配集成 API 闸门，它不能替代原独立远端授权。
	s.config.IntegrationGuard = func(context.Context, json.RawMessage) (func(), error) { return func() {}, nil }
	selected := make(chan string, 2)
	s.config.Generator = func(_ context.Context, in models.LibraryExecutionDefaults) (core.Generator, error) {
		selected <- in.Model
		return nil, status_error.RuntimeUnavailable
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.workers.Wait()
	select {
	case got := <-selected:
		t.Fatal("remote task ran before guard assembly", got)
	default:
	}
	if err := s.SetRemoteGuard(nil); err != nil {
		t.Fatal(err)
	}
	s.workers.Wait()
	select {
	case got := <-selected:
		t.Fatal("nil guard started remote task", got)
	default:
	}
	if err := s.SetRemoteGuard(func(context.Context, json.RawMessage) (func(), error) { return func() {}, nil }); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"first", "second"} {
		select {
		case got := <-selected:
			if got != expected {
				t.Fatal("remote queue configuration/order", got, expected)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("remote queue did not recover", expected)
		}
	}
	s.workers.Wait()
}

func TestRecoveryIncludesLibrariesBeyondFirstPage(t *testing.T) {
	s, _ := newServiceFixture(t)
	ctx := context.Background()
	for i := 0; i < 101; i++ {
		if _, err := s.config.Engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: fmt.Sprintf("lib-%d", i), Name: fmt.Sprintf("库%d", i), Kind: "general", WikiLanguage: "zh-CN"}); err != nil {
			t.Fatal(err)
		}
	}
	// 直接使用真实核心第二页，避免把默认库或同毫秒排序当作分页覆盖证据。
	page, _, err := s.config.Engine.ListLibraries(ctx, 100, 100)
	if err != nil || len(page) == 0 {
		t.Fatal("fixture has no second page", page, err)
	}
	library := page[0]
	raw, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "last-raw", LibraryID: library.ID, Filename: "a.md", Bytes: []byte("跨页依据")})
	if err != nil {
		t.Fatal(err)
	}
	receipt, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "last-job", LibraryID: library.ID, SourceVersionIDs: []string{raw.ResourceID}})
	if err != nil {
		t.Fatal(err)
	}
	s.config.Generator = func(context.Context, models.LibraryExecutionDefaults) (core.Generator, error) {
		return nil, status_error.RuntimeUnavailable
	}
	if err = s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.workers.Wait()
	job, err := s.config.Engine.GetJob(ctx, receipt.JobID)
	if err != nil || job.State != "failed" {
		t.Fatal("second-page library queue did not recover", job, err)
	}
}

func TestRecoveryWaitsForMatchingAuthorizationOrigin(t *testing.T) {
	for _, origin := range []string{"", "direct_api", "automatic_task"} {
		t.Run("origin-"+origin, func(t *testing.T) {
			s, lib := newServiceFixture(t)
			ctx := context.Background()
			allowed := func(context.Context, json.RawMessage) (func(), error) { return func() {}, nil }
			invocation := func(context.Context, string) (func(), error) { return func() {}, nil }
			s.config.RemoteGuard, s.config.IntegrationGuard, s.config.InvocationGuard = allowed, allowed, invocation
			recovery, _ := json.Marshal(map[string]any{"origin": origin, "automaticRunId": "original-run", "subject": "user", "permission": core.PermissionSnapshot{PolicyVersion: lib.PolicyVersion, Operations: []string{"organize"}}})
			if _, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "raw", LibraryID: lib.ID, Filename: "a.md", Bytes: []byte("授权来源")}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "job", LibraryID: lib.ID, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: "codex", RemoteSubject: "user", RemoteAuthorization: recovery}}); err != nil {
				t.Fatal(err)
			}
			switch origin {
			case "":
				s.config.RemoteGuard = nil
			case "direct_api":
				s.config.IntegrationGuard = nil
			case "automatic_task":
				s.config.InvocationGuard = nil
			}
			invoked := make(chan struct{}, 1)
			s.config.Generator = func(context.Context, models.LibraryExecutionDefaults) (core.Generator, error) {
				invoked <- struct{}{}
				return nil, status_error.RuntimeUnavailable
			}
			if err := s.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			s.workers.Wait()
			select {
			case <-invoked:
				t.Fatal("other authorization origin started deferred task")
			default:
			}
			s.config.RemoteGuard, s.config.IntegrationGuard, s.config.InvocationGuard = allowed, allowed, invocation
			if err := s.SetRemoteGuard(allowed); err != nil {
				t.Fatal(err)
			}
			select {
			case <-invoked:
			case <-time.After(5 * time.Second):
				t.Fatal("task did not resume after matching guard assembly")
			}
			s.workers.Wait()
		})
	}
}

func TestStartFailsInterruptedDocumentAndWikiJobs(t *testing.T) {
	for _, maintenance := range []bool{false, true} {
		t.Run(fmt.Sprintf("maintenance-%t", maintenance), func(t *testing.T) {
			s, lib := newServiceFixture(t)
			ctx := context.Background()
			var job *core.Job
			if maintenance {
				receipt, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "wiki-job", LibraryID: lib.ID, Maintenance: true})
				if err != nil {
					t.Fatal(err)
				}
				job, err = s.config.Engine.GetJob(ctx, receipt.JobID)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				job = regressionJob(t, s, lib)
			}
			job, ready, err := s.config.Engine.ActivateQueuedJob(ctx, job.ID)
			if err != nil || !ready || job.State != "processing" {
				t.Fatalf("job did not start before interruption: %+v %v", job, err)
			}
			// 新宿主没有旧模型调用；启动不能把原执行重新接回模型服务。
			var calls atomic.Int64
			s.config.Generator = func(ctx context.Context, _ models.LibraryExecutionDefaults) (core.Generator, error) {
				calls.Add(1)
				<-ctx.Done()
				return nil, ctx.Err()
			}
			s.config.RunMaintenance = func(ctx context.Context, _ models.LibraryExecutionDefaults, _ MaintenanceInvocation) (string, error) {
				calls.Add(1)
				<-ctx.Done()
				return "", ctx.Err()
			}
			restarted := New(s.config)
			t.Cleanup(restarted.Close)
			if err := restarted.Start(ctx); err != nil {
				t.Fatal(err)
			}
			current, err := s.config.Engine.GetJob(ctx, job.ID)
			if err != nil || current.State != "failed" || current.Diagnostic == "" {
				t.Fatalf("interrupted job was not settled: %+v %v", current, err)
			}
			if calls.Load() != 0 {
				t.Fatal("startup reran the interrupted model call")
			}
			active, err := s.config.Engine.GetActiveJob(ctx, lib.ID)
			if err != nil || active != nil {
				t.Fatalf("interrupted job still blocks library actions: %+v %v", active, err)
			}
			for _, id := range job.SourceVersionIDs {
				source, err := s.config.Engine.GetSource(ctx, lib.ID, id)
				if err != nil || source.State != "failed" {
					t.Fatalf("interrupted source remains busy: %+v %v", source, err)
				}
			}
			if maintenance {
				stages, err := s.config.Engine.ListStages(ctx, job.ID)
				if err != nil || len(stages) != 1 || stages[0].State != "finished" || stages[0].Outcome != "failed" {
					t.Fatalf("interrupted Wiki stage remains running: %+v %v", stages, err)
				}
			}
			if _, err := restarted.Retry(ctx, "retry-interrupted", current.ID, current.Version); err != nil {
				t.Fatalf("interrupted job cannot be retried: %v", err)
			}
		})
	}
}

func TestStartSettlesInterruptedJobBeforeConfigurationRecovery(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	job := regressionJob(t, s, lib)
	if _, ready, err := s.config.Engine.ActivateQueuedJob(ctx, job.ID); err != nil || !ready {
		t.Fatalf("activate: ready=%t error=%v", ready, err)
	}
	// 旧任务配置不可恢复，也必须释放中断任务的占用。
	s.config.References = startupConfigurationReferences{KnowledgeController: s.config.References, configurationError: status_error.KnowledgeNotFound}
	restarted := New(s.config)
	t.Cleanup(restarted.Close)
	if err := restarted.Start(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := s.config.Engine.GetJob(ctx, job.ID)
	if err != nil || current.State != "failed" {
		t.Fatalf("missing configuration left interrupted job active: %+v %v", current, err)
	}
}
