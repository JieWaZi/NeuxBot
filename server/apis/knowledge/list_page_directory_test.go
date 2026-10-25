package knowledge

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
)

func TestPageDirectoryHonorsCancelledHTTPRequest(t *testing.T) {
	root := t.TempDir()
	engine, err := core.Open(t.Context(), core.Options{DBPath: filepath.Join(root, "knowledge.db"), RawDir: filepath.Join(root, "raw")})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	receipt, err := engine.CreateLibrary(t.Context(), core.CreateLibraryInput{RequestID: "cancel-directory", Name: "目录", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	previous := global.KnowledgeEngine
	global.KnowledgeEngine = engine
	t.Cleanup(func() { global.KnowledgeEngine = previous })
	requestCtx, cancel := context.WithCancel(t.Context())
	cancel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/page-directory", nil).WithContext(requestCtx)
	_, err = (&ListPageDirectory{LibraryID: receipt.ResourceID, Limit: 1000}).Output(ctx)
	if err != status_error.KnowledgeCancelled {
		t.Fatalf("cancelled request returned %v", err)
	}
}
