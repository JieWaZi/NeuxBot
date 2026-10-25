package integration_knowledge

import (
	"io"
	"net/http"
	"strconv"

	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 上传集成知识资料
type PublishedImportSource struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"id" validate:"required"`
}

// Path 保留 WeKnora multipart 上传路径。
func (*PublishedImportSource) Path() string { return "/knowledge-bases/:id/knowledge/file" }

// Output 真正保存原件，不调用 Generator 或启动整理。
func (o *PublishedImportSource) Output(ctx *gin.Context) (interface{}, error) {
	originalID, requestID, err := scopedRequest(ctx, o.LibraryID, "")
	if err != nil {
		return nil, err
	}
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, (32<<20)+(1<<20))
	if err := ctx.Request.ParseMultipartForm(1 << 20); err != nil {
		return nil, status_error.KnowledgeInvalid
	}
	defer ctx.Request.MultipartForm.RemoveAll()
	form := ctx.Request.MultipartForm
	if len(form.File) != 1 || len(form.File["file"]) != 1 {
		return nil, status_error.KnowledgeInvalid
	}
	for key := range form.Value {
		if key != "source_id" && key != "expected_version" {
			return nil, status_error.KnowledgeUnsupported
		}
	}
	file, header, err := ctx.Request.FormFile("file")
	if err != nil {
		return nil, status_error.KnowledgeInvalid
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (32<<20)+1))
	if err != nil || len(data) > 32<<20 {
		return nil, status_error.KnowledgeInvalid
	}
	version := int64(0)
	if text := ctx.Request.FormValue("expected_version"); text != "" {
		version, err = strconv.ParseInt(text, 10, 64)
		if err != nil || version < 0 {
			return nil, status_error.KnowledgeInvalid
		}
	}
	d := dependencies(ctx)
	auth, _ := middleware.IntegrationAuthorization(ctx.Request.Context())
	library, err := d.engine.GetLibrary(ctx.Request.Context(), o.LibraryID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	release, err := d.auth.AcquireGuard(ctx.Request.Context(), auth)
	if err != nil {
		return nil, err
	}
	defer release()
	activity := core.WithActivityActor(ctx.Request.Context(), core.ActivityActor{UserID: auth.Identity.KeyID, Trigger: "api"})
	input := core.ImportSourceInput{RequestID: requestID, LibraryID: o.LibraryID, SourceID: ctx.Request.FormValue("source_id"), ExpectedVersion: version, Filename: header.Filename, MIME: header.Header.Get("Content-Type"), Bytes: data}
	logical := input
	input.ExpectedPolicyVersion = library.PolicyVersion
	receipt, err := performWrite(ctx, activity, requestID, "source.import", o.LibraryID, logical, func() (*core.Receipt, error) { return d.engine.ImportSource(activity, input) })
	if err != nil {
		return nil, err
	}
	source, err := d.engine.GetSource(ctx.Request.Context(), o.LibraryID, receipt.ResourceID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return SourceResponse{Success: true, Data: sourceDTO(source), Receipt: publicReceipt(receipt, originalID)}, nil
}
