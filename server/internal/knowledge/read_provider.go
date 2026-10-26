package knowledge

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/remote"
	"math"
	"strings"
	"unicode/utf8"
)

// ReadProvider 只适配同一知识读取合同，不暴露本机生成、写入或索引端口。
type ReadProvider interface {
	Library(context.Context, ResourceRef) (*core.Library, error)
	Pages(context.Context, ResourceRef, core.PageQuery) ([]*core.Page, int64, error)
	Page(context.Context, ResourceRef, string, string) (*core.Page, error)
	Source(context.Context, ResourceRef, string) (map[string]any, error)
}

// ExtendedReadProvider 复用已授权的提供者读取扩展对象，不增加连接或业务状态。
type ExtendedReadProvider interface {
	ExtendedRead(context.Context, ResourceRef, string, map[string]any) (map[string]any, error)
}

// LocalReadProvider 仅转调唯一独立SDK。
type LocalReadProvider struct {
	// Engine 是独立核心权威，不持有业务副本。
	Engine *core.Engine
	// Limit 是调用入口的返回上限；零保持既有工具默认五条。
	Limit int
	// FreezeEmbedder 冻结同一库独立配置，查询失败不回落到关键词。
	FreezeEmbedder func(context.Context, string, string) (core.DocumentEmbedder, error)
}

func (p LocalReadProvider) Library(ctx context.Context, ref ResourceRef) (*core.Library, error) {
	if err := RequireLocal(ref); err != nil {
		return nil, err
	}
	v, err := p.Engine.GetLibrary(ctx, ref.KnowledgeBaseID)
	return v, MapError(err)
}
func (p LocalReadProvider) Pages(ctx context.Context, ref ResourceRef, q core.PageQuery) ([]*core.Page, int64, error) {
	if err := RequireLocal(ref); err != nil {
		return nil, 0, err
	}
	q.LibraryID = ref.KnowledgeBaseID
	v, total, err := p.Engine.ListPages(ctx, q)
	return v, total, MapError(err)
}

// DocumentReadHit 保留命中子块的定位，并返回父块上下文。
type DocumentReadHit struct {
	Chunk     core.IndexedDocumentChunk  `json:"chunk"`            // 命中子块及真实来源版本。
	Score     float64                    `json:"score"`            // 实际向量、关键词或融合相关性分数。
	MatchType core.MatchType             `json:"matchType"`        // 实际召回路径，向量为 0，关键词为 1。
	Parent    *core.IndexedDocumentChunk `json:"parent,omitempty"` // 父子模式的完整父块上下文。
}

// DocumentReadProvider 为既有 Wiki 搜索补充真实原件检索，不改变远端读取端口。
type DocumentReadProvider interface {
	SearchChunks(context.Context, ResourceRef, string) ([]DocumentReadHit, string, error)
}

// SearchChunks 按实际库策略检索原件，父块来自同一来源版本的持久结果。
func (p LocalReadProvider) SearchChunks(ctx context.Context, ref ResourceRef, query string) ([]DocumentReadHit, string, error) {
	lib, err := p.Library(ctx, ref)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(query) == "" || (!lib.IndexingStrategy.VectorEnabled && !lib.IndexingStrategy.KeywordEnabled) {
		return nil, "", nil
	}
	mode := "keyword"
	keywordQuery := query
	var vector []float32
	fingerprint := ""
	if lib.IndexingStrategy.VectorEnabled {
		mode = "vector"
		keywordQuery = ""
		if lib.IndexingStrategy.KeywordEnabled {
			mode = "hybrid"
			keywordQuery = query
		}
		if p.FreezeEmbedder == nil {
			return nil, "", status_error.KnowledgeEmbeddingNotConfigured
		}
		frozen, e := p.FreezeEmbedder(ctx, lib.ID, lib.EmbeddingModelID)
		if e != nil {
			return nil, "", e
		}
		if frozen.Embed == nil || frozen.Fingerprint == "" {
			return nil, "", status_error.KnowledgeEmbeddingNotConfigured
		}
		values, e := frozen.Embed(ctx, []string{query})
		if e != nil {
			return nil, "", e
		}
		if len(values) != 1 || len(values[0]) == 0 {
			return nil, "", status_error.KnowledgeEmbeddingResponseInvalid
		}
		norm := 0.0
		for _, x := range values[0] {
			if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
				return nil, "", status_error.KnowledgeEmbeddingResponseInvalid
			}
			norm += float64(x) * float64(x)
		}
		if norm == 0 {
			return nil, "", status_error.KnowledgeEmbeddingResponseInvalid
		}
		vector = values[0]
		fingerprint = frozen.Fingerprint
	}
	limit := p.Limit
	if limit == 0 {
		limit = 5
	}
	if limit < 1 || limit > 30 {
		return nil, "", status_error.KnowledgeInvalid
	}
	hits, err := p.Engine.SearchDocumentChunks(ctx, ref.KnowledgeBaseID, keywordQuery, vector, fingerprint, limit)
	if err != nil {
		return nil, "", MapError(err)
	}
	out := make([]DocumentReadHit, 0, len(hits))
	parentIDs := []string{}
	seenParents := map[string]bool{}
	for _, hit := range hits {
		if hit.Chunk.ParentID != "" && !seenParents[hit.Chunk.ParentID] {
			seenParents[hit.Chunk.ParentID] = true
			parentIDs = append(parentIDs, hit.Chunk.ParentID)
		}
	}
	parents := map[string]core.IndexedDocumentChunk{}
	for start := 0; start < len(parentIDs); start += 5 {
		end := min(start+5, len(parentIDs))
		rows, e := p.Engine.GetDocumentChunkParents(ctx, ref.KnowledgeBaseID, parentIDs[start:end])
		if e != nil {
			return nil, "", MapError(e)
		}
		for _, row := range rows {
			parents[row.ChunkID] = row
		}
	}
	for _, hit := range hits {
		item := DocumentReadHit{Chunk: hit.Chunk, Score: hit.Score, MatchType: hit.MatchType}
		if hit.Chunk.ParentID != "" {
			parent, ok := parents[hit.Chunk.ParentID]
			if !ok || parent.SourceVersionID != hit.Chunk.SourceVersionID {
				return nil, "", status_error.KnowledgeStorageError
			}
			item.Parent = &parent
		}
		out = append(out, item)
	}
	return out, mode, nil
}

func (p LocalReadProvider) Page(ctx context.Context, ref ResourceRef, id, slug string) (*core.Page, error) {
	if err := RequireLocal(ref); err != nil {
		return nil, err
	}
	if (id == "") == (slug == "") {
		return nil, status_error.KnowledgeInvalid
	}
	var v *core.Page
	var err error
	if slug != "" {
		v, err = p.Engine.GetPageBySlug(ctx, ref.KnowledgeBaseID, slug)
	} else {
		v, err = p.Engine.GetPage(ctx, ref.KnowledgeBaseID, id)
	}
	return v, MapError(err)
}
func (p LocalReadProvider) Source(ctx context.Context, ref ResourceRef, id string) (map[string]any, error) {
	if err := RequireLocal(ref); err != nil {
		return nil, err
	}
	v, err := p.Engine.GetSource(ctx, ref.KnowledgeBaseID, id)
	if err != nil {
		return nil, MapError(err)
	}
	return map[string]any{"sourceId": v.SourceID, "sourceVersionId": v.ID, "sha256": v.SHA256, "filename": v.Filename, "mime": v.MIME, "state": v.State, "coverage": v.Coverage, "diagnostic": v.Diagnostic}, nil
}

// RemoteReadProvider 只有HTTP客户端，不能触达本机Reader、Generator或索引。
type RemoteReadProvider struct {
	// Custom 是绑定完整远端引用的自有协议客户端。
	Custom *remote.Custom
	// Official 是官方固定合同客户端。
	Official *remote.WeKnora
}

func (p RemoteReadProvider) Library(ctx context.Context, ref ResourceRef) (*core.Library, error) {
	if p.Custom != nil {
		v, err := p.Custom.GetLibrary(ctx)
		return v, MapRemoteError(err)
	}
	if p.Official == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	v, err := p.Official.GetLibrary(ctx, ref.KnowledgeBaseID)
	if err != nil {
		return nil, MapRemoteError(err)
	}
	return &core.Library{ID: v.ID, Name: v.Name, Kind: "official"}, nil
}
func (p RemoteReadProvider) Pages(ctx context.Context, ref ResourceRef, q core.PageQuery) ([]*core.Page, int64, error) {
	q.LibraryID = ref.KnowledgeBaseID
	if p.Custom != nil {
		v, err := p.Custom.ListPages(ctx, q)
		if err != nil {
			return nil, 0, MapRemoteError(err)
		}
		return v.Items, v.Total, nil
	}
	if p.Official == nil {
		return nil, 0, status_error.KnowledgeUnsupported
	}
	if q.Limit <= 0 {
		q.Limit = 50
	}
	if q.Offset%q.Limit != 0 {
		return nil, 0, status_error.KnowledgeInvalid
	}
	v, err := p.Official.ListPages(ctx, ref.KnowledgeBaseID, remote.PageOptions{Page: q.Offset/q.Limit + 1, PageSize: q.Limit, Query: q.Query, PageType: q.PageType, FolderID: q.ThemeID})
	if err != nil {
		return nil, 0, MapRemoteError(err)
	}
	items := []*core.Page{}
	for _, page := range v.Pages {
		items = append(items, officialReadPage(page))
	}
	return items, v.Total, nil
}
func (p RemoteReadProvider) Page(ctx context.Context, ref ResourceRef, id, slug string) (*core.Page, error) {
	if (id == "") == (slug == "") {
		return nil, status_error.KnowledgeInvalid
	}
	if p.Custom != nil {
		var v *core.Page
		var err error
		if slug != "" {
			v, err = p.Custom.GetPageBySlug(ctx, slug)
		} else {
			v, err = p.Custom.GetPage(ctx, id)
		}
		return v, MapRemoteError(err)
	}
	if p.Official == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	if slug == "" {
		return nil, status_error.KnowledgeUnsupported
	}
	v, err := p.Official.GetPage(ctx, ref.KnowledgeBaseID, slug)
	if err != nil {
		return nil, MapRemoteError(err)
	}
	if id != "" && v.ID != id {
		return nil, status_error.KnowledgeNotFound
	}
	return officialReadPage(*v), nil
}

// ExtendedRead 将读取交给唯一核心；任务身份额外核对库边界，接纳状态不推断为完成。
func (p LocalReadProvider) ExtendedRead(ctx context.Context, ref ResourceRef, operation string, params map[string]any) (map[string]any, error) {
	if err := RequireLocal(ref); err != nil {
		return nil, err
	}
	var data map[string]any
	var err error
	offset, limit := number(params, "offset"), number(params, "limit")
	switch operation {
	case "knowledge.libraries.get":
		var v *core.Library
		v, err = p.Engine.GetLibrary(ctx, ref.KnowledgeBaseID)
		data = map[string]any{"library": v}
	case "knowledge.sources.list":
		var rows []*core.SourceVersion
		var total int64
		rows, total, err = p.Engine.ListSourcesQuery(ctx, ref.KnowledgeBaseID, core.SourceQuery{Offset: offset, Limit: limit, Query: stringParam(params, "query"), State: stringParam(params, "state"), Sort: stringParam(params, "sort")})
		data = map[string]any{"items": rows, "total": total}
	case "knowledge.sources.read":
		var source *core.SourceVersion
		source, err = p.Engine.GetSource(ctx, ref.KnowledgeBaseID, stringParam(params, "sourceVersionId"))
		if err != nil {
			return nil, MapError(err)
		}
		var rows []core.IndexedDocumentChunk
		rows, err = p.Engine.ListDocumentChunks(ctx, ref.KnowledgeBaseID, source.ID)
		if err != nil {
			return nil, MapError(err)
		}
		return sourceChunks(ref, source, rows, params)
	case "knowledge.pages.list":
		var rows []*core.Page
		var total int64
		rows, total, err = p.Pages(ctx, ref, pageReadQuery(params))
		return pageListMetadata(rows, total, offset), err
	case "knowledge.themes.list":
		var rows []*core.Theme
		rows, err = p.Engine.ListThemes(ctx, ref.KnowledgeBaseID)
		data = map[string]any{"items": rows}
	case "knowledge.index.get":
		types, _ := params["pageTypes"].([]string)
		var v *core.WikiIndexResponse
		v, err = p.Engine.GetIndexView(ctx, ref.KnowledgeBaseID, types, limit, stringParam(params, "cursor"))
		data = map[string]any{"index": v}
	case "knowledge.revisions.list":
		var rows []*core.Revision
		var total int64
		rows, total, err = p.Engine.ListRevisions(ctx, ref.KnowledgeBaseID, stringParam(params, "pageId"), offset, limit)
		data = map[string]any{"items": rows, "total": total}
	case "knowledge.revisions.get":
		var v *core.Revision
		v, err = p.Engine.GetRevision(ctx, ref.KnowledgeBaseID, stringParam(params, "pageId"), number(params, "version"))
		data = map[string]any{"revision": v}
	case "knowledge.jobs.list":
		var rows []*core.Job
		var total int64
		rows, total, err = p.Engine.ListJobs(ctx, ref.KnowledgeBaseID, offset, limit)
		data = map[string]any{"items": rows, "total": total}
	case "knowledge.jobs.get":
		var v *core.Job
		v, err = p.Engine.GetJob(ctx, stringParam(params, "jobId"))
		if err == nil && v.LibraryID != ref.KnowledgeBaseID {
			return nil, status_error.KnowledgeNotFound
		}
		data = map[string]any{"job": v}
	case "knowledge.audit.get":
		var v *core.LintReport
		v, err = p.Engine.LintLibrary(ctx, ref.KnowledgeBaseID)
		data = map[string]any{"audit": v}
	case "knowledge.activity.list":
		var rows []*core.AuditLog
		var cursor uint64
		rows, cursor, err = p.Engine.ListLibraryActivity(ctx, ref.KnowledgeBaseID, activityParams(params))
		data = map[string]any{"items": rows, "nextCursor": cursor}
	case "knowledge.export.get":
		ids, _ := params["pageIds"].([]string)
		var v *core.MarkdownExport
		v, err = p.Engine.ExportMarkdown(ctx, core.ExportInput{LibraryID: ref.KnowledgeBaseID, PageIDs: ids})
		data = map[string]any{"export": v}
	default:
		return nil, status_error.KnowledgeUnsupported
	}
	return data, MapError(err)
}

// ExtendedRead 只调用自有远端已存在的读取合同；官方未适配操作准确拒绝。
func (p RemoteReadProvider) ExtendedRead(ctx context.Context, ref ResourceRef, operation string, params map[string]any) (map[string]any, error) {
	if operation == "knowledge.libraries.get" {
		v, err := p.Library(ctx, ref)
		return map[string]any{"library": v}, err
	}
	if operation == "knowledge.pages.list" {
		rows, total, err := p.Pages(ctx, ref, pageReadQuery(params))
		return pageListMetadata(rows, total, number(params, "offset")), err
	}
	if operation == "knowledge.sources.list" && p.Custom == nil && p.Official != nil {
		offset, limit := number(params, "offset"), number(params, "limit")
		if limit < 1 || offset%limit != 0 {
			return nil, status_error.KnowledgeInvalid
		}
		v, err := p.Official.ListSources(ctx, ref.KnowledgeBaseID, offset/limit+1, limit, core.SourceQuery{Query: stringParam(params, "query"), State: stringParam(params, "state"), Sort: stringParam(params, "sort")})
		if err != nil {
			return nil, MapRemoteError(err)
		}
		return map[string]any{"items": v.Data, "total": v.Total, "versioning": "unavailable"}, nil
	}
	if p.Custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	var data map[string]any
	var err error
	offset, limit := number(params, "offset"), number(params, "limit")
	switch operation {
	case "knowledge.sources.list":
		var v *remote.CustomList[*core.SourceVersion]
		v, err = p.Custom.ListSources(ctx, offset, limit, core.SourceQuery{Query: stringParam(params, "query"), State: stringParam(params, "state"), Sort: stringParam(params, "sort")})
		if err == nil {
			data = map[string]any{"items": v.Items, "total": v.Total}
		}
	case "knowledge.sources.read":
		var source *core.SourceVersion
		source, err = p.Custom.GetSource(ctx, stringParam(params, "sourceVersionId"))
		if err != nil {
			return nil, MapRemoteError(err)
		}
		var rows []core.IndexedDocumentChunk
		rows, err = p.Custom.ListSourceChunks(ctx, source.ID)
		if err != nil {
			return nil, MapRemoteError(err)
		}
		return sourceChunks(ref, source, rows, params)
	case "knowledge.themes.list":
		var rows []*core.Theme
		rows, err = p.Custom.ListThemes(ctx)
		data = map[string]any{"items": rows}
	case "knowledge.revisions.list":
		var v *remote.CustomList[*core.Revision]
		v, err = p.Custom.ListRevisions(ctx, stringParam(params, "pageId"), offset, limit)
		if err == nil {
			data = map[string]any{"items": v.Items, "total": v.Total}
		}
	case "knowledge.revisions.get":
		var v *core.Revision
		v, err = p.Custom.GetRevision(ctx, stringParam(params, "pageId"), number(params, "version"))
		data = map[string]any{"revision": v}
	case "knowledge.jobs.list":
		var v *remote.CustomList[*core.Job]
		v, err = p.Custom.ListJobs(ctx, offset, limit)
		if err == nil {
			data = map[string]any{"items": v.Items, "total": v.Total}
		}
	case "knowledge.jobs.get":
		var v *core.Job
		v, err = p.Custom.GetJob(ctx, stringParam(params, "jobId"))
		data = map[string]any{"job": v}
	case "knowledge.audit.get":
		var v *core.LintReport
		v, err = p.Custom.LintLibrary(ctx)
		if err == nil && v.LibraryID != ref.KnowledgeBaseID {
			return nil, status_error.KnowledgeRemoteIncompatible
		}
		data = map[string]any{"audit": v}
	case "knowledge.activity.list":
		var rows []*core.AuditLog
		var cursor uint64
		rows, cursor, err = p.Custom.ListLibraryActivity(ctx, activityParams(params))
		data = map[string]any{"items": rows, "nextCursor": cursor}
	case "knowledge.export.get":
		ids, _ := params["pageIds"].([]string)
		var v *core.MarkdownExport
		v, err = p.Custom.ExportMarkdown(ctx, ids)
		if err == nil && v.LibraryID != ref.KnowledgeBaseID {
			return nil, status_error.KnowledgeRemoteIncompatible
		}
		data = map[string]any{"export": v}
	default:
		return nil, status_error.KnowledgeUnsupported
	}
	return data, MapRemoteError(err)
}

func activityParams(params map[string]any) core.ActivityQuery {
	cursor, _ := params["afterId"].(uint64)
	return core.ActivityQuery{AfterID: cursor, Limit: number(params, "limit"), Action: core.AuditAction(stringParam(params, "action")), Outcome: core.AuditOutcome(stringParam(params, "outcome")), ActorUserID: stringParam(params, "actorUserId")}
}

// pageListMetadata 复用目录条目合同；正文与证据只通过单页读取交付，避免 CLI 列表淹没上下文。
func pageListMetadata(rows []*core.Page, total int64, offset int) map[string]any {
	items := make([]core.PageDirectoryEntry, 0, len(rows))
	for _, page := range rows {
		items = append(items, core.PageDirectoryEntry{ID: page.ID, KnowledgeBaseID: page.KnowledgeBaseID, Slug: page.Slug, Title: page.Title, PageType: page.PageType, Summary: page.Summary, FolderID: page.FolderID, CategoryPath: page.CategoryPath, SortOrder: page.SortOrder, Version: page.Version})
	}
	next := offset + len(items)
	return map[string]any{"items": items, "total": total, "offset": offset, "nextOffset": next, "hasMore": int64(next) < total}
}

func pageReadQuery(params map[string]any) core.PageQuery {
	q := core.PageQuery{Query: stringParam(params, "query"), PageType: stringParam(params, "pageType"), Status: "published", Offset: number(params, "offset"), Limit: number(params, "limit")}
	if value, exists := params["themeId"]; exists {
		theme := value.(string)
		q.ThemeID = &theme
	}
	return q
}

// sourceChunks 对真实持久块作字面查询与分页；截断保持 UTF-8，定位仍指向原始完整块。
func sourceChunks(ref ResourceRef, source *core.SourceVersion, rows []core.IndexedDocumentChunk, params map[string]any) (map[string]any, error) {
	if source == nil || source.LibraryID != ref.KnowledgeBaseID || source.ID != stringParam(params, "sourceVersionId") {
		return nil, status_error.KnowledgeNotFound
	}
	query, chunkID := strings.ToLower(stringParam(params, "query")), stringParam(params, "chunkId")
	filtered := make([]core.IndexedDocumentChunk, 0, len(rows))
	for _, row := range rows {
		if row.LibraryID != ref.KnowledgeBaseID || row.SourceVersionID != source.ID {
			return nil, status_error.KnowledgeNotFound
		}
		if chunkID != "" && row.ChunkID != chunkID {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(row.Content), query) {
			continue
		}
		filtered = append(filtered, row)
	}
	if chunkID != "" && len(filtered) == 0 {
		return nil, status_error.KnowledgeNotFound
	}
	offset, limit := number(params, "offset"), number(params, "limit")
	start := min(offset, len(filtered))
	end := min(start+limit, len(filtered))
	items := make([]core.IndexedDocumentChunk, 0, end-start)
	budget, truncated := 64<<10, false
	for _, row := range filtered[start:end] {
		if budget == 0 {
			break
		}
		cut := len(row.Content) > budget
		if cut {
			size := budget
			for size > 0 && !utf8.ValidString(row.Content[:size]) {
				size--
			}
			row.Content = row.Content[:size]
			truncated = true
		}
		budget -= len(row.Content)
		// 向量不属于读取输出，也不保留到返回对象的内存引用。
		row.Vector = nil
		items = append(items, row)
		if cut {
			break
		}
	}
	next := start + len(items)
	return map[string]any{"source": source, "items": items, "total": len(filtered), "offset": offset, "nextOffset": next, "hasMore": next < len(filtered), "truncated": truncated}, nil
}
func (p RemoteReadProvider) Source(ctx context.Context, ref ResourceRef, id string) (map[string]any, error) {
	if p.Custom != nil {
		v, err := p.Custom.GetSource(ctx, id)
		if err != nil {
			return nil, MapRemoteError(err)
		}
		return map[string]any{"sourceId": v.SourceID, "sourceVersionId": v.ID, "sha256": v.SHA256, "filename": v.Filename, "mime": v.MIME, "state": v.State, "coverage": v.Coverage, "diagnostic": v.Diagnostic}, nil
	}
	if p.Official == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	v, err := p.Official.GetSource(ctx, ref.KnowledgeBaseID, id)
	if err != nil {
		return nil, MapRemoteError(err)
	}
	return map[string]any{"sourceId": v.ID, "filename": v.Filename, "state": v.ParseStatus, "versioning": "unavailable"}, nil
}
func officialReadPage(v remote.Page) *core.Page {
	return &core.Page{KnowledgeBaseID: v.KnowledgeBaseID, ID: v.ID, Slug: v.Slug, Title: v.Title, PageType: v.PageType, Content: v.Content, Version: v.Version, FolderID: v.FolderID, Aliases: v.Aliases, SourceRefs: v.SourceRefs, InLinks: v.InLinks, OutLinks: v.OutLinks}
}

// MapRemoteError 在SDK首个外部边界保留稳定分类，不泄露远端响应或凭据。
func MapRemoteError(err error) error {
	if err == nil {
		return nil
	}
	var e *remote.Error
	if errors.As(err, &e) {
		switch e.Code {
		case "unauthenticated":
			return status_error.KnowledgeRemoteUnauthorized
		case "forbidden", "scope_violation", "policy_changed", "write_ip_forbidden":
			return status_error.KnowledgeForbidden
		case "not_found":
			return status_error.KnowledgeNotFound
		case "version_conflict", "idempotency_conflict", "manual_protected":
			return status_error.KnowledgeConflict
		case "unsupported", "unsupported_profile", "unsupported_source_state":
			return status_error.KnowledgeUnsupported
		case "invalid", "invalid_connection", "invalid_resource", "invalid_request", "invalid_query", "invalid_pagination":
			return status_error.KnowledgeInvalid
		case "outcome_unknown":
			return status_error.KnowledgeRemoteOutcomeUnknown
		case "protocol_incompatible", "redirect_rejected", "response_too_large", "response_incomplete":
			return status_error.KnowledgeRemoteIncompatible
		}
	}
	return status_error.KnowledgeRemoteUnreachable
}
