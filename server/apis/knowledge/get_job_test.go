package knowledge

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// TestGetJobReturnsOnlyIndependentTaskStatus 验证任务详情只公开独立状态，不暴露已退役的阶段与会话协议。
func TestGetJobReturnsOnlyIndependentTaskStatus(t *testing.T) {
	ctx := context.Background()
	engine, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
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
	previousEngine, previousReferences := global.KnowledgeEngine, global.KnowledgeControllerManager
	global.KnowledgeEngine, global.KnowledgeControllerManager = engine, controllers.NewKnowledgeController(host.DB)
	t.Cleanup(func() { global.KnowledgeEngine, global.KnowledgeControllerManager = previousEngine, previousReferences })
	created, err := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "library", Name: "Documents", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	source, err := engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "source", LibraryID: created.ResourceID, Filename: "source.md", Bytes: []byte("evidence")})
	if err != nil {
		t.Fatal(err)
	}
	library, _ := engine.GetLibrary(ctx, created.ResourceID)
	receipt, err := engine.AcceptJob(ctx, core.AcceptJobInput{RequestID: "job", LibraryID: library.ID, ExpectedVersion: library.Version, StructureVersion: library.StructureVersion, SourceVersionIDs: []string{source.ResourceID}, Permission: core.PermissionSnapshot{PolicyVersion: library.PolicyVersion, Operations: []string{"organize"}}, RulesVersion: "rules", ConfigDigest: "config"})
	if err != nil {
		t.Fatal(err)
	}
	output, err := (&GetJob{LibraryID: library.ID, JobID: receipt.JobID}).Output(&gin.Context{})
	if err != nil {
		t.Fatal(err)
	}
	detail := output.(JobDetail)
	jobFields := map[string]any{}
	jobRaw, _ := json.Marshal(detail.Job)
	if err := json.Unmarshal(jobRaw, &jobFields); err != nil || jobFields["maintenance"] != false {
		t.Fatalf("原件整理任务类型不明确: %s %v", jobRaw, err)
	}
	if detail.Job.ID != receipt.JobID || detail.Job.State != "queued" {
		t.Fatalf("状态响应=%+v", detail)
	}
	raw, _ := json.Marshal(detail)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"stages", "session"} {
		if _, ok := fields[forbidden]; ok {
			t.Fatalf("任务详情包含已退役过程协议: %s", raw)
		}
	}
	if _, ok := fields["report"]; ok {
		t.Fatalf("原件整理不能公开阶段正文: %s", raw)
	}

	maintenanceLibrary, err := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "maintenance-library", Name: "Maintenance", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	ml, err := engine.GetLibrary(ctx, maintenanceLibrary.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := engine.AcceptJob(ctx, core.AcceptJobInput{Maintenance: true, RequestID: "maintenance-report", LibraryID: ml.ID, ExpectedVersion: ml.Version, StructureVersion: ml.StructureVersion, Permission: core.PermissionSnapshot{PolicyVersion: ml.PolicyVersion, Operations: []string{"maintain"}}, RulesVersion: "wiki-fixer", ConfigDigest: "config"})
	if err != nil {
		t.Fatal(err)
	}
	active, ready, err := engine.ActivateQueuedJob(ctx, accepted.JobID)
	if err != nil || !ready {
		t.Fatalf("启动维护: %v %v", ready, err)
	}
	if _, err = engine.CompleteMaintenanceJob(ctx, core.JobActionInput{RequestID: "maintenance-complete", JobID: active.ID, ExpectedVersion: active.Version}, "已核查当前 Wiki，归并重复知识并保留来源。"); err != nil {
		t.Fatal(err)
	}
	output, err = (&GetJob{LibraryID: ml.ID, JobID: active.ID}).Output(&gin.Context{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(output)
	fields = map[string]json.RawMessage{}
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	jobFields = map[string]any{}
	if err = json.Unmarshal(fields["job"], &jobFields); err != nil || jobFields["maintenance"] != true {
		t.Fatalf("已有Wiki维护任务类型不明确: %s %v", fields["job"], err)
	}
	var report string
	if err = json.Unmarshal(fields["report"], &report); err != nil || report != "已核查当前 Wiki，归并重复知识并保留来源。" {
		t.Fatalf("维护任务未返回真实报告: %s %v", raw, err)
	}
	for _, forbidden := range []string{"stages", "session"} {
		if _, ok := fields[forbidden]; ok {
			t.Fatalf("维护结果泄露阶段或聊天: %s", raw)
		}
	}
}

func TestMaintenanceReportExcludesOtherAttemptsAndStages(t *testing.T) {
	job := &core.Job{ID: "maintenance", Attempt: 2, State: "completed", Permission: core.PermissionSnapshot{Operations: []string{"maintain"}}}
	stages := []*core.Stage{
		{JobID: "maintenance", JobAttempt: 1, Kind: "wiki_maintenance", State: "finished", Outcome: "succeeded", ResultText: "旧尝试报告"},
		{JobID: "maintenance", JobAttempt: 2, Kind: "extract", State: "finished", Outcome: "succeeded", ResultText: "内部阶段正文"},
		{JobID: "other", JobAttempt: 2, Kind: "wiki_maintenance", State: "finished", Outcome: "succeeded", ResultText: "其他任务报告"},
		{JobID: "maintenance", JobAttempt: 2, Kind: "wiki_maintenance", State: "finished", Outcome: "failed", ResultText: "失败尝试残留"},
	}
	if got := bridge.MaintenanceReportForAttempt(job, stages); got != "" {
		t.Fatalf("泄露非当前成功报告: %q", got)
	}
	stages = append(stages, &core.Stage{JobID: "maintenance", JobAttempt: 2, Kind: "wiki_maintenance", State: "finished", Outcome: "succeeded", ResultText: "本次实际报告"})
	if got := bridge.MaintenanceReportForAttempt(job, stages); got != "本次实际报告" {
		t.Fatalf("当前报告=%q", got)
	}
	job.State = "processing"
	if got := bridge.MaintenanceReportForAttempt(job, stages); got != "" {
		t.Fatalf("未完成维护提前公开报告: %q", got)
	}
}
