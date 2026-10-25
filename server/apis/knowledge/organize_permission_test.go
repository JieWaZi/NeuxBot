package knowledge

import (
	"context"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	service "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"path/filepath"
	"strings"
	"testing"
)

// TestLocalOrganizeHasOnlyIndependentModelConfiguration 验证HTTP整理接纳没有伙伴、聊天或执行权限选择。
func TestLocalOrganizeHasOnlyIndependentModelConfiguration(t *testing.T) {
	ctx := context.Background()
	e, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "core.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "host.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB.DB()
	defer sqlDB.Close()
	created, err := e.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "library", Name: "独立知识", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	previous := global.KnowledgeService
	global.KnowledgeService = service.New(service.Config{Engine: e, References: controllers.NewKnowledgeController(db.DB), ResolveModel: func(_ context.Context, value models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
		return value, nil
	}})
	defer func() { global.KnowledgeService = previous }()
	value, err := (&Organize{LibraryID: created.LibraryID, Body: OrganizeInput{RequestID: "organize", RuntimeID: constants.RuntimeIDCodex, Reasoning: "high"}}).Output(&gin.Context{})
	if err != nil {
		t.Fatal(err)
	}
	response := value.(OrganizeResponse)
	if !response.Noop || response.Configuration.RuntimeID != constants.RuntimeIDCodex || response.Configuration.Reasoning != "high" {
		t.Fatalf("响应=%+v", response)
	}
	raw, _ := json.Marshal(response)
	for _, forbidden := range []string{"wakerId", "chatId", "permissionLevel", "acceptedExecution"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("独立任务包含聊天配置: %s", raw)
		}
	}
}
