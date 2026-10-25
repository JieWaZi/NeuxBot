package knowledge

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestSourcePurgeRequiresConsentAndReportsActualCleanup(t *testing.T) {
	ctx := context.Background()
	engine, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "ally.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	defer sqlDB.Close()
	oldEngine, oldRefs := global.KnowledgeEngine, global.KnowledgeControllerManager
	global.KnowledgeEngine = engine
	global.KnowledgeControllerManager = controllers.NewKnowledgeController(database.DB)
	defer func() { global.KnowledgeEngine = oldEngine; global.KnowledgeControllerManager = oldRefs }()
	g := &gin.Context{}
	library, err := (&CreateLibrary{Body: CreateLibraryInput{RequestID: "purge-lib", Name: "清理", WikiLanguage: "zh-CN"}}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	libraryID := library.(Receipt).ResourceID
	imported, err := (&ImportSource{LibraryID: libraryID, Body: ImportSourceInput{RequestID: "purge-import", Filename: "private.md", Bytes: base64.StdEncoding.EncodeToString([]byte("private raw bytes"))}}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	source, err := engine.GetSource(ctx, libraryID, imported.(Receipt).ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	previewValue, err := (&PreviewSourcePurge{LibraryID: libraryID, SourceID: source.SourceID}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	preview := previewValue.(*core.SourcePurgePreview)
	input := ConfirmSourcePurgeInput{RequestID: "purge-confirm-no", SourceID: source.SourceID, PreviewDigest: preview.Digest}
	if _, err = (&ConfirmSourcePurge{LibraryID: libraryID, Body: input}).Output(g); err == nil {
		t.Fatal("missing explicit consent accepted")
	}
	if _, err = engine.ReadRaw(ctx, libraryID, source.ID); err != nil {
		t.Fatal("invalid confirmation removed raw", err)
	}
	input.RequestID = "purge-confirm"
	input.Confirmed = true
	value, err := (&ConfirmSourcePurge{LibraryID: libraryID, Body: input}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	receipt := value.(Receipt)
	if receipt.State != "accepted" {
		t.Fatal("acceptance falsely claimed raw cleanup", receipt)
	}
	value, err = (&GetSourcePurge{LibraryID: libraryID, PurgeID: receipt.ResourceID}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	status := value.(*core.SourcePurgeStatus)
	if status.State != "pending" || status.PendingBlobCount != 1 {
		t.Fatal(status)
	}
	listed, err := (&ListSourcePurges{LibraryID: libraryID, Limit: 1}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	list := listed.(*core.SourcePurgeList)
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != receipt.ResourceID {
		t.Fatalf("lost pending cleanup: %+v", list)
	}
	lib, _ := engine.GetLibrary(ctx, libraryID)
	_, err = (&RecoverSourcePurge{LibraryID: libraryID, PurgeID: receipt.ResourceID, Body: RecoverSourcePurgeInput{RequestID: "purge-recover", ExpectedPolicyVersion: lib.PolicyVersion}}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	value, err = (&GetSourcePurge{LibraryID: libraryID, PurgeID: receipt.ResourceID}).Output(g)
	if err != nil {
		t.Fatal(err)
	}
	status = value.(*core.SourcePurgeStatus)
	if status.State != "completed" || status.DeletedBlobCount != 1 || status.PendingBlobCount != 0 {
		t.Fatal(status)
	}
}
