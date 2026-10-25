package integration_knowledge

import (
	"errors"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
)

// 兼容知识库
type PublishedLibrary struct {
	// 本机库配置
	core.Library
	// 服务端库类型
	Type string `json:"type"`
}

// 知识库详情响应
type LibraryResponse struct {
	// 成功状态
	Success bool `json:"success"`
	// 知识库
	Data PublishedLibrary `json:"data"`
}

// 知识库列表响应
type LibraryListResponse struct {
	// 成功状态
	Success bool `json:"success"`
	// 知识库列表
	Data []PublishedLibrary `json:"data"`
}

// 兼容资料
type PublishedSource struct {
	// 原件版本
	core.SourceVersion
	// 所属知识库
	KnowledgeBaseID string `json:"knowledge_base_id"`
	// 原件文件名
	FileName string `json:"file_name"`
	// 文件类型
	FileType string `json:"file_type"`
	// 解析状态
	ParseStatus string `json:"parse_status"`
	// 资料标题
	Title string `json:"title"`
}

// 资料详情响应
type SourceResponse struct {
	// 成功状态
	Success bool `json:"success"`
	// 资料详情
	Data PublishedSource `json:"data"`
	// 接纳回执
	Receipt *core.Receipt `json:"receipt,omitempty"`
}

func sourceDTO(source *core.SourceVersion) PublishedSource {
	copy := *source
	copy.RawKey, copy.Diagnostic = "", ""
	status := source.State
	if status == "processed" || status == "no-change" {
		status = "completed"
	}
	return PublishedSource{SourceVersion: copy, KnowledgeBaseID: source.LibraryID, FileName: source.Filename, FileType: source.MIME, ParseStatus: status, Title: source.Filename}
}

func libraryDTO(library *core.Library) PublishedLibrary {
	return PublishedLibrary{Library: *library, Type: "wiki"}
}

func publishedPagination(page, size int) (int, int, error) {
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	if page < 1 || page > 100000 || size < 1 || size > 100 {
		return 0, 0, status_error.KnowledgeInvalid
	}
	return page, size, nil
}

func pathSlug(value string) (string, error) {
	value = strings.TrimPrefix(value, "/")
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "\\\x00\r\n") {
			return "", status_error.KnowledgeInvalid
		}
	}
	return value, nil
}

// resolveSourceLibrary 只在该主体已获准的明确库集合中查找，未授权对象不泄漏存在性。
func resolveSourceLibrary(ctx *gin.Context) (string, error) {
	d := dependencies(ctx)
	id, _ := middleware.IntegrationPrincipal(ctx.Request.Context())
	ids, err := d.auth.ResourceIDs(ctx.Request.Context(), id, constants.IntegrationResourceKindKnowledge, []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve})
	if err != nil {
		return "", err
	}
	for _, libraryID := range ids {
		source, err := d.engine.GetSource(ctx.Request.Context(), libraryID, ctx.Param("id"))
		if err == nil {
			return source.LibraryID, nil
		}
		var code *core.Error
		if !errors.As(err, &code) || (code.Code != core.ErrNotFound && code.Code != core.ErrScopeViolation) {
			return "", bridge.MapError(err)
		}
	}
	return "", status_error.KnowledgeNotFound
}
