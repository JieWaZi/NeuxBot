package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
)

// 知识资源引用
type ResourceRef struct {
	// 服务类型
	Backend string `json:"backend"`
	// 连接标识
	ConnectionID string `json:"connectionId"`
	// 知识库标识
	KnowledgeBaseID string `json:"knowledgeBaseId"`
}

// 知识库详情
type Library struct {
	// 远端离线
	Offline bool `json:"offline,omitempty"`
	// 远端入口标识
	MountID string `json:"mountId,omitempty"`
	// 远端入口版本
	MountRevision int64 `json:"mountRevision,omitempty"`
	// Wiki数量
	PageTotal *int64 `json:"pageTotal,omitempty"`
	// 统计时间
	PageTotalObservedAt int64 `json:"pageTotalObservedAt,omitempty"`
	// 资源引用
	Ref ResourceRef `json:"ref"`
	// 知识库标识
	ID string `json:"id"`
	// 名称
	Name string `json:"name"`
	// 库类型
	Kind string `json:"kind"`
	// 正文语言
	WikiLanguage string `json:"wikiLanguage"`
	// 用途
	Purpose string `json:"purpose"`
	// 分块设置
	ChunkingConfig *core.ChunkingConfig `json:"chunkingConfig,omitempty"`
	// Wiki 整理配置
	WikiConfig *core.WikiConfig `json:"wikiConfig,omitempty"`
	// 索引策略
	IndexingStrategy core.IndexingStrategy `json:"indexingStrategy"`
	// 嵌入模型标识
	EmbeddingModelID string `json:"embeddingModelId,omitempty"`
	// 当前版本
	Version int64 `json:"version"`
	// 目录版本
	StructureVersion int64 `json:"structureVersion"`
	// 权限版本
	PolicyVersion int64 `json:"policyVersion"`
	// 已归档
	Archived bool `json:"archived"`
	// 创建时间
	CreatedAt int64 `json:"createdAt"`
	// 更新时间
	UpdatedAt int64 `json:"updatedAt"`
}

// 原件版本
type Source struct {
	// 整理占用
	OrganizeBlocked bool `json:"organizeBlocked"`
	// 版本标识
	ID string `json:"id"`
	// 来源标识
	SourceID string `json:"sourceId"`
	// 原件版本
	Version int64 `json:"version"`
	// 状态版本
	RowVersion int64 `json:"rowVersion"`
	// 文件名
	Filename string `json:"filename"`
	// 媒体类型
	MIME string `json:"mime"`
	// 字节摘要
	SHA256 string `json:"sha256"`
	// 字节大小
	Size int64 `json:"size"`
	// 处理状态
	State string `json:"state"`
	// 文档摘要
	Description string `json:"description"`
	// 文档画像
	Profile *core.KnowledgeProfile `json:"profile,omitempty"`
	// 摘要状态
	SummaryStatus string `json:"summaryStatus"`
	// 已归档
	Archived bool `json:"archived"`
	// 阅读覆盖
	Coverage json.RawMessage `json:"coverage"`
	// 处理说明
	Diagnostic string `json:"diagnostic"`
	// 创建时间
	CreatedAt int64 `json:"createdAt"`
}

// 证据位置
type SourceLocator struct {
	// 位置类型
	Kind string `json:"kind"`
	// 页码
	Page int `json:"page"`
	// 起始行
	StartLine int `json:"startLine"`
	// 结束行
	EndLine int `json:"endLine"`
	// 章节
	Section string `json:"section"`
}

// 原件证据
type SourceEvidence struct {
	// 来源标识
	SourceID string `json:"sourceId"`
	// 来源版本
	SourceVersionID string `json:"sourceVersionId"`
	// 原件摘要
	SHA256 string `json:"sha256"`
	// 证据位置
	Locator SourceLocator `json:"locator"`
	// 原文摘录
	Excerpt string `json:"excerpt"`
	// 摘录摘要
	ExcerptHash string `json:"excerptHash"`
	// 论断类型
	ClaimKind string `json:"claimKind"`
}

// 实现证据
type CodeEvidence struct {
	// 仓库身份
	Repository string `json:"repository"`
	// 提交身份
	Commit string `json:"commit"`
	// 相对路径
	Path string `json:"path"`
	// 符号
	Symbol string `json:"symbol"`
	// 起始行
	StartLine int `json:"startLine"`
	// 结束行
	EndLine int `json:"endLine"`
	// 核验时间
	VerifiedAt int64 `json:"verifiedAt"`
}

// 页面证据
type PageEvidence struct {
	// 原件证据
	Sources []SourceEvidence `json:"sources"`
	// 实现证据
	Code []CodeEvidence `json:"code"`
}

// 知识页面
type Page struct {
	// 来源清理核查
	SourcePurgeReview *SourcePurgeReview `json:"sourcePurgeReview,omitempty"`
	// 页面证据
	Evidence PageEvidence `json:"evidence"`
	// 页面标识
	ID string `json:"id"`
	// 语义地址
	Slug string `json:"slug"`
	// 标题
	Title string `json:"title"`
	// 页面类型
	PageType string `json:"pageType"`
	// 状态
	Status string `json:"status"`
	// 正文
	Content string `json:"content"`
	// 页面说明
	Summary string `json:"summary"`
	// 别名
	Aliases []string `json:"aliases"`
	// 主题标识
	ThemeID string `json:"themeId"`
	// 排序
	SortOrder int `json:"sortOrder"`
	// 当前版本
	Version int `json:"version"`
	// 原件引用
	SourceRefs []string `json:"sourceRefs"`
	// 阅读块引用
	ChunkRefs []string `json:"chunkRefs"`
	// 入链
	InLinks []string `json:"inLinks"`
	// 出链
	OutLinks []string `json:"outLinks"`
	// 目录路径
	CategoryPath []string `json:"categoryPath"`
	// 人工位置
	ManualPlacement bool `json:"manualPlacement"`
	// 正文语言
	WikiLanguage string `json:"wikiLanguage"`
	// 更新时间
	UpdatedAt int64 `json:"updatedAt"`
}

// 知识主题
type Theme struct {
	// 主题标识
	ID string `json:"id"`
	// 上级标识
	ParentID string `json:"parentId"`
	// 名称
	Name string `json:"name"`
	// 主题路径
	Path string `json:"path"`
	// 排序
	SortOrder int `json:"sortOrder"`
	// 人工主题
	Manual bool `json:"manual"`
	// 系统标记
	SystemKey string `json:"systemKey"`
}

// 页面修订
type Revision struct {
	// 页面说明
	Summary string `json:"summary"`
	// 页面证据
	Evidence PageEvidence `json:"evidence"`
	// 修订标识
	ID string `json:"id"`
	// 页面标识
	PageID string `json:"pageId"`
	// 版本
	Version int `json:"version"`
	// 标题
	Title string `json:"title"`
	// 正文
	Content string `json:"content"`
	// 编辑来源
	EditSource string `json:"editSource"`
	// 编辑时间
	EditedAt int64 `json:"editedAt"`
	// 原件引用
	SourceRefs []string `json:"sourceRefs"`
}

// 知识操作回执
type Receipt struct {
	// 请求标识
	RequestID string `json:"requestId"`
	// 资源标识
	ResourceID string `json:"resourceId"`
	// 知识库标识
	LibraryID string `json:"libraryId"`
	// 任务标识
	JobID string `json:"jobId"`
	// 提交版本
	Version int64 `json:"version"`
	// 提交事实
	State string `json:"state"`
	// 页面标识
	PageIDs []string `json:"pageIds"`
}

// 分页参数
type Pagination struct {
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0"`
	// 每页条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

func pagination(p Pagination) (int, int) {
	if p.Limit == 0 {
		p.Limit = 50
	}
	return p.Offset, p.Limit
}
func libraryDTO(v *core.Library) Library {
	return Library{Ref: ResourceRef{Backend: "local", ConnectionID: "local", KnowledgeBaseID: v.ID}, ID: v.ID, Name: v.Name, Kind: v.Kind, WikiLanguage: v.WikiLanguage, Purpose: v.Purpose, WikiConfig: v.WikiConfig, ChunkingConfig: v.ChunkingConfig, IndexingStrategy: v.IndexingStrategy, EmbeddingModelID: v.EmbeddingModelID, Version: v.Version, StructureVersion: v.StructureVersion, PolicyVersion: v.PolicyVersion, Archived: v.Archived, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
func sourceDTO(v *core.SourceVersion) Source {
	return Source{OrganizeBlocked: v.OrganizeBlocked, ID: v.ID, SourceID: v.SourceID, Version: v.Version, RowVersion: v.RowVersion, Filename: v.Filename, MIME: v.MIME, SHA256: v.SHA256, Size: v.Size, State: v.State, Description: v.Description, Profile: v.Profile, SummaryStatus: v.SummaryStatus, Archived: v.Archived, Coverage: v.Coverage, Diagnostic: v.Diagnostic, CreatedAt: v.CreatedAt}
}
func pageDTO(v *core.Page) Page {
	return Page{SourcePurgeReview: SourcePurgeReviewFromMetadata(v.PageMetadata), Evidence: evidenceDTO(v.Evidence), ID: v.ID, Slug: v.Slug, Title: v.Title, PageType: v.PageType, Status: v.Status, Content: v.Content, Summary: v.Summary, Aliases: v.Aliases, ThemeID: v.FolderID, SortOrder: v.SortOrder, Version: v.Version, SourceRefs: v.SourceRefs, ChunkRefs: v.ChunkRefs, InLinks: v.InLinks, OutLinks: v.OutLinks, CategoryPath: v.CategoryPath, ManualPlacement: v.ManualPlacement, WikiLanguage: v.WikiLanguage, UpdatedAt: v.UpdatedAt.UnixMilli()}
}
func themeDTO(v *core.Theme) Theme {
	return Theme{ID: v.ID, ParentID: v.ParentID, Name: v.Name, Path: v.Path, SortOrder: v.SortOrder, Manual: v.Manual, SystemKey: v.SystemKey}
}
func revisionDTO(v *core.Revision) Revision {
	return Revision{Summary: v.Summary, Evidence: evidenceDTO(v.Evidence), ID: v.ID, PageID: v.PageID, Version: v.Version, Title: v.Title, Content: v.Content, EditSource: v.EditSource, EditedAt: v.EditedAt.UnixMilli(), SourceRefs: v.SourceRefs}
}
func receiptDTO(v *core.Receipt) Receipt {
	return Receipt{RequestID: v.RequestID, ResourceID: v.ResourceID, LibraryID: v.LibraryID, JobID: v.JobID, Version: v.Version, State: v.State, PageIDs: v.PageIDs}
}

// executeWrite 在外部调用前持久化锚点；重放仍由核心比较原请求摘要。
func executeWrite(ctx context.Context, id, op, library string, input any, call func() (*core.Receipt, error)) (*core.Receipt, error) {
	if global.KnowledgeEngine == nil || global.KnowledgeControllerManager == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	raw, err := json.Marshal(struct {
		// Operation 固定实际操作。
		Operation string
		// Library 固定完整本地目标。
		Library string
		// Input 保存规范化请求。
		Input any
	}{op, library, input})
	if err != nil {
		return nil, status_error.KnowledgeInvalid
	}
	digest := sha256.Sum256(raw)
	link := models.KnowledgeOperationLink{RequestID: id, Operation: op, Digest: hex.EncodeToString(digest[:]), Backend: "local", ConnectionID: "local", KnowledgeBaseID: library}
	if err := global.KnowledgeControllerManager.BeginOperation(ctx, link); err != nil {
		return nil, err
	}
	receipt, err := call()
	if err != nil {
		return nil, bridge.MapError(err)
	}
	if err := global.KnowledgeControllerManager.CompleteOperation(ctx, id, receipt.LibraryID, receipt.ResourceID, receipt.JobID); err != nil {
		return nil, err
	}
	if global.EventHub != nil {
		_ = events.Emit(global.EventHub, "knowledge", "knowledge.changed", events.Resource{Kind: "knowledge_library", ID: receipt.LibraryID, ParentID: "local"}, map[string]any{"ref": bridge.LocalRef(receipt.LibraryID), "requestId": receipt.RequestID, "resourceId": receipt.ResourceID})
	}
	return receipt, nil
}
func engineAvailable(*gin.Context) error {
	if global.KnowledgeEngine == nil {
		return status_error.KnowledgeUnsupported
	}
	return nil
}

func evidenceDTO(v core.PageEvidence) PageEvidence {
	out := PageEvidence{Sources: []SourceEvidence{}, Code: []CodeEvidence{}}
	for _, ref := range v.Sources {
		out.Sources = append(out.Sources, SourceEvidence{SourceID: ref.SourceID, SourceVersionID: ref.SourceVersionID, SHA256: ref.SHA256, Locator: SourceLocator{Kind: ref.Locator.Kind, Page: ref.Locator.Page, StartLine: ref.Locator.StartLine, EndLine: ref.Locator.EndLine, Section: ref.Locator.Section}, Excerpt: ref.Excerpt, ExcerptHash: ref.ExcerptHash, ClaimKind: ref.ClaimKind})
	}
	for _, ref := range v.Code {
		out.Code = append(out.Code, CodeEvidence{Repository: ref.Repository, Commit: ref.Commit, Path: ref.Path, Symbol: ref.Symbol, StartLine: ref.StartLine, EndLine: ref.EndLine, VerifiedAt: ref.VerifiedAt})
	}
	return out
}
