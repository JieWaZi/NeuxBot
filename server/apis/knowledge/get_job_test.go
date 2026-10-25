package knowledge

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
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
}
