package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
)

func TestPreviewChunkingUsesDraftConfigWithoutLibraryOrModel(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/chunking/preview", nil)
	operation := PreviewChunking{Body: core.PreviewChunkingRequest{Text: strings.Repeat("# 标题\n真实样例文本用于核对分块和偏移。\n\n", 20), ChunkingConfig: core.PreviewChunkingPayload{ChunkSize: 100, ChunkOverlap: 20, Strategy: "heading"}}}
	value, err := operation.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := value.(PreviewChunkingResult)
	if !ok || len(result.Chunks) == 0 || result.Profile == nil || result.Stats.Count == 0 {
		t.Fatalf("preview missing actual chunks: %+v", value)
	}
	operation.Body.Text = " \n"
	if _, err = operation.Output(ctx); !errors.Is(err, status_error.KnowledgeInvalid) {
		t.Fatalf("empty sample accepted: %v", err)
	}
	operation.Body.Text = strings.Repeat("中", 64*1024+1)
	if _, err = operation.Output(ctx); !errors.Is(err, status_error.KnowledgeInvalid) {
		t.Fatalf("oversized sample accepted: %v", err)
	}
}

func TestPreviewChunkingPreservesUpstreamJSONAndCancellationKinds(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/chunking/preview", nil)
	body := core.PreviewChunkingRequest{Text: "# 标题\n原文保留父子上下文。", ChunkingConfig: core.PreviewChunkingPayload{Strategy: "auto"}}
	original, err := core.PreviewDocumentChunking(ctx.Request.Context(), body)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := (&PreviewChunking{Body: body}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(original)
	got, _ := json.Marshal(projected)
	if string(want) != string(got) {
		t.Fatalf("host projection changed upstream JSON:\n%s\n%s", want, got)
	}
	deadline, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	ctx.Request = ctx.Request.WithContext(deadline)
	if _, err = (&PreviewChunking{Body: body}).Output(ctx); !errors.Is(err, status_error.OperationTimeout) {
		t.Fatalf("deadline misclassified: %v", err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	ctx.Request = ctx.Request.WithContext(cancelled)
	if _, err = (&PreviewChunking{Body: body}).Output(ctx); !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("cancellation misclassified: %v", err)
	}
}
