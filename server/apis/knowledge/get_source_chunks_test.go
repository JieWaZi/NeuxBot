package knowledge

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"path/filepath"
	"testing"
)

func TestSourceChunksUsesActualDocumentAndRejectsCrossLibrary(t *testing.T) {
	ctx := context.Background()
	engine, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	old := global.KnowledgeEngine
	global.KnowledgeEngine = engine
	defer func() { global.KnowledgeEngine = old }()
	lib, _ := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "lib", Name: "documents", WikiLanguage: "zh-CN", Kind: "general"})
	source, _ := engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "source", LibraryID: lib.ResourceID, Filename: "original.md", Bytes: []byte("original text")})
	got, err := (&GetSourceChunks{LibraryID: lib.ResourceID, SourceID: source.ResourceID}).Output(&gin.Context{})
	if err != nil || len(got.([]core.IndexedDocumentChunk)) != 0 {
		t.Fatal("unprocessed source fabricated content", got, err)
	}

	library, _ := engine.GetLibrary(ctx, lib.ResourceID)
	original, _ := engine.GetSource(ctx, lib.ResourceID, source.ResourceID)
	job, err := engine.AcceptJob(ctx, core.AcceptJobInput{RequestID: "parse", LibraryID: library.ID, ExpectedVersion: library.Version, StructureVersion: library.StructureVersion, SourceVersionIDs: []string{original.ID}, Permission: core.PermissionSnapshot{PolicyVersion: library.PolicyVersion, Operations: []string{"organize"}}, RulesVersion: "rules", ConfigDigest: "config"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := (core.MarkdownReader{}).Read(ctx, core.ReadRequest{SourceID: original.SourceID, SourceVersionID: original.ID, SHA256: original.SHA256, Filename: original.Filename, Raw: []byte("original text"), Scope: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.IndexDocument(ctx, job.JobID, original.ID, artifact, core.DocumentEmbedder{}); err != nil {
		t.Fatal(err)
	}
	got, err = (&GetSourceChunks{LibraryID: library.ID, SourceID: original.ID}).Output(&gin.Context{})
	if err != nil || len(got.([]core.IndexedDocumentChunk)) == 0 || got.([]core.IndexedDocumentChunk)[0].Content != "original text" {
		t.Fatal("preview did not expose real parsed content", got, err)
	}
	another, _ := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "another", Name: "other", WikiLanguage: "zh-CN", Kind: "general"})
	if _, err = (&GetSourceChunks{LibraryID: another.ResourceID, SourceID: source.ResourceID}).Output(&gin.Context{}); err == nil {
		t.Fatal("source preview crossed library boundary")
	}
}
