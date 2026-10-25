package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// TestSourceProcessingUsesDurableWorkAndKeepsIndividualResults 核对真实阅读事实、逐来源终态和失败原因，不依赖聊天执行。
func TestSourceProcessingUsesDurableWorkAndKeepsIndividualResults(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "knowledge.db")
	engine, err := core.Open(ctx, core.Options{DBPath: path, RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	previous := global.KnowledgeEngine
	global.KnowledgeEngine = engine
	t.Cleanup(func() { global.KnowledgeEngine = previous })
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	created, err := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "library", Name: "Queue", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, name := range []string{"reading", "completed", "failed", "outside"} {
		rec, e := engine.ImportSource(ctx, core.ImportSourceInput{RequestID: name, LibraryID: created.ResourceID, Filename: name + ".md", Bytes: []byte(name)})
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, rec.ResourceID)
	}
	assertStates := func(want ...string) map[string]Source {
		t.Helper()
		output, e := (&ListSources{LibraryID: created.ResourceID, Limit: 50}).Output(&gin.Context{})
		if e != nil {
			t.Fatal(e)
		}
		actual := map[string]Source{}
		for _, source := range output.(SourceList).Items {
			actual[source.ID] = source
		}
		for i, id := range ids {
			if actual[id].State != want[i] {
				t.Fatalf("source %d state=%s want=%s", i, actual[id].State, want[i])
			}
		}
		return actual
	}
	assertStates("pending", "pending", "pending", "pending")
	if _, err = database.Exec("UPDATE knowledge_source_versions SET state='failed',diagnostic='previous failure' WHERE id=?", ids[0]); err != nil {
		t.Fatal(err)
	}
	library, err := engine.GetLibrary(ctx, created.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := engine.AcceptJob(ctx, core.AcceptJobInput{RequestID: "job", LibraryID: library.ID, ExpectedVersion: library.Version, StructureVersion: library.StructureVersion, SourceVersionIDs: ids[:3], Permission: core.PermissionSnapshot{PolicyVersion: library.PolicyVersion, Operations: []string{"organize"}}, RulesVersion: "rules", ConfigDigest: "config"})
	if err != nil {
		t.Fatal(err)
	}
	assertStates("queued", "queued", "queued", "pending")
	if err = engine.EnsureOrganizeSourceWork(ctx, receipt.JobID); err != nil {
		t.Fatal(err)
	}
	// 投影测试直接写入已经持久化的处理事实；不需要创建旧阶段、候选或Execution。
	if _, err = database.Exec("UPDATE knowledge_organize_source_work SET started_at=100 WHERE job_id=? AND source_version_id=?", receipt.JobID, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec("UPDATE knowledge_source_versions SET state='processed' WHERE id=?", ids[1]); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec("UPDATE knowledge_organize_source_work SET state='done' WHERE job_id=? AND source_version_id=?", receipt.JobID, ids[1]); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec("UPDATE knowledge_organize_source_work SET state='dead',diagnostic='model_generation_failed: upstream unavailable' WHERE job_id=? AND source_version_id=?", receipt.JobID, ids[2]); err != nil {
		t.Fatal(err)
	}
	actual := assertStates("processing", "processed", "failed", "pending")
	for _, id := range ids[:3] {
		if !actual[id].OrganizeBlocked {
			t.Fatalf("active task released source reservation: %+v", actual[id])
		}
	}
	if actual[ids[3]].OrganizeBlocked {
		t.Fatal("unrelated source was reserved")
	}
	if actual[ids[2]].Diagnostic != "model_generation_failed: upstream unavailable" {
		t.Fatalf("missing hover diagnostic: %+v", actual[ids[2]])
	}
	raw, err := (&GetSource{LibraryID: library.ID, SourceID: ids[0]}).Output(&gin.Context{})
	if err != nil || raw.(Source).State != "processing" {
		t.Fatalf("single source=%+v error=%v", raw, err)
	}
	job, err := engine.GetJob(ctx, receipt.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = engine.CancelOrganize(ctx, core.JobActionInput{RequestID: "cancel", JobID: job.ID, ExpectedVersion: job.Version}); err != nil {
		t.Fatal(err)
	}
	job, err = engine.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = engine.RetryJob(ctx, core.JobActionInput{RequestID: "retry", JobID: job.ID, ExpectedVersion: job.Version}); err != nil {
		t.Fatal(err)
	}
	assertStates("queued", "processed", "queued", "pending")
}

// TestListJobsActiveJobDoesNotDependOnHistoryOrder 验证重试旧任务时活动事实不被更新的已完成记录遮蔽。
func TestListJobsActiveJobDoesNotDependOnHistoryOrder(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "knowledge.db")
	engine, err := core.Open(ctx, core.Options{DBPath: path, RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	host, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "host.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	hostSQL, _ := host.DB.DB()
	defer hostSQL.Close()
	previous, previousRefs := global.KnowledgeEngine, global.KnowledgeControllerManager
	global.KnowledgeEngine = engine
	global.KnowledgeControllerManager = controllers.NewKnowledgeController(host.DB)
	t.Cleanup(func() { global.KnowledgeEngine = previous; global.KnowledgeControllerManager = previousRefs })
	created, err := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "history-library", Name: "History", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	source, err := engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "history-source", LibraryID: created.ResourceID, Filename: "source.md", Bytes: []byte("source")})
	if err != nil {
		t.Fatal(err)
	}
	library, err := engine.GetLibrary(ctx, created.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	accept := func(request string) *core.Job {
		t.Helper()
		result, err := engine.AcceptJob(ctx, core.AcceptJobInput{RequestID: request, LibraryID: library.ID, ExpectedVersion: library.Version, StructureVersion: library.StructureVersion, SourceVersionIDs: []string{source.ResourceID}, Permission: core.PermissionSnapshot{PolicyVersion: library.PolicyVersion, Operations: []string{"organize"}}, RulesVersion: "rules", ConfigDigest: "config"})
		if err != nil {
			t.Fatal(err)
		}
		job, err := engine.GetJob(ctx, result.JobID)
		if err != nil {
			t.Fatal(err)
		}
		return job
	}
	old := accept("old")
	_, err = engine.CancelOrganize(ctx, core.JobActionInput{RequestID: "cancel-old", JobID: old.ID, ExpectedVersion: old.Version})
	if err != nil {
		t.Fatal(err)
	}
	newer := accept("newer")
	// 读取投影测试直接固定历史终态及排序，不通过模型运行制造测试夹具。
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err = database.Exec("UPDATE knowledge_jobs SET state='completed',created_at=200 WHERE id=?", newer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec("UPDATE knowledge_jobs SET created_at=100 WHERE id=?", old.ID); err != nil {
		t.Fatal(err)
	}
	old, err = engine.GetJob(ctx, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = engine.RetryJob(ctx, core.JobActionInput{RequestID: "retry-old", JobID: old.ID, ExpectedVersion: old.Version}); err != nil {
		t.Fatal(err)
	}
	output, err := (&ListJobs{LibraryID: library.ID, Limit: 1}).Output(&gin.Context{})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	var active Job
	if err = json.Unmarshal(fields["activeJob"], &active); err != nil {
		t.Fatalf("missing activeJob in %s: %v", payload, err)
	}
	if active.ID != old.ID || active.State != "queued" || output.(JobList).Items[0].ID != newer.ID {
		t.Fatalf("active=%+v history=%+v", active, output)
	}
	current, err := engine.GetJob(ctx, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = engine.CancelOrganize(ctx, core.JobActionInput{RequestID: "cancel-retry", JobID: old.ID, ExpectedVersion: current.Version}); err != nil {
		t.Fatal(err)
	}
	output, err = (&ListJobs{LibraryID: library.ID, Limit: 1}).Output(&gin.Context{})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ = json.Marshal(output)
	if err = json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["activeJob"]) != "null" {
		t.Fatalf("terminal job retained active slot: %s", payload)
	}
}

// TestSourceListFiltersBeforePagination 验证接口按真实排队状态先筛选，再排序、计数与分页。
func TestSourceListFiltersBeforePagination(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "query.db")
	engine, err := core.Open(ctx, core.Options{DBPath: path, RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	previous := global.KnowledgeEngine
	global.KnowledgeEngine = engine
	t.Cleanup(func() { global.KnowledgeEngine = previous })
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	created, err := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "filtered-library", Name: "筛选", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for i := 1; i <= 44; i++ {
		name := fmt.Sprintf("batch-%d.md", i)
		r, e := engine.ImportSource(ctx, core.ImportSourceInput{RequestID: name, LibraryID: created.ResourceID, Filename: name, Bytes: []byte(name)})
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, r.ResourceID)
		if _, e = database.Exec("UPDATE knowledge_source_versions SET created_at = ? WHERE id = ?", i, r.ResourceID); e != nil {
			t.Fatal(e)
		}
	}
	library, err := engine.GetLibrary(ctx, created.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.AcceptJob(ctx, core.AcceptJobInput{RequestID: "filtered-job", LibraryID: library.ID, ExpectedVersion: library.Version, StructureVersion: library.StructureVersion, SourceVersionIDs: ids[10:], Permission: core.PermissionSnapshot{PolicyVersion: library.PolicyVersion, Operations: []string{"organize"}}, RulesVersion: "rules", ConfigDigest: "config"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		// query 是接口输入。
		query ListSources
		// total 是筛选后总量。
		total int64
		// first 是当前页首个文件名；空串表示无结果。
		first string
	}{
		{ListSources{LibraryID: library.ID, Limit: 10, State: "queued", Sort: "oldest"}, 34, "batch-11.md"},
		{ListSources{LibraryID: library.ID, Limit: 10, Offset: 10, State: "queued", Sort: "oldest"}, 34, "batch-21.md"},
		{ListSources{LibraryID: library.ID, Limit: 10, State: "queued", Sort: "newest"}, 34, "batch-44.md"},
		{ListSources{LibraryID: library.ID, Limit: 10, State: "queued", Sort: "newest", Query: "batch-1"}, 9, "batch-19.md"},
		{ListSources{LibraryID: library.ID, Limit: 10, State: "failed"}, 0, ""},
	} {
		output, e := tc.query.Output(&gin.Context{})
		if e != nil {
			t.Fatal(e)
		}
		page := output.(SourceList)
		if page.Total != tc.total {
			t.Fatalf("query=%+v total=%d want=%d", tc.query, page.Total, tc.total)
		}
		if tc.first == "" {
			if len(page.Items) != 0 {
				t.Fatal("空结果含条目")
			}
			continue
		}
		if len(page.Items) == 0 || page.Items[0].Filename != tc.first {
			t.Fatalf("query=%+v page=%+v", tc.query, page)
		}
		for _, source := range page.Items {
			if source.State != tc.query.State {
				t.Fatalf("状态未投影: %+v", source)
			}
		}
	}
}
