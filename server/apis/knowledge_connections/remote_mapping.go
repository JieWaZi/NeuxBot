package knowledge_connections

import (
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/gin-gonic/gin"
)

// remoteRef 始终保留连接与远端权威库身份，不能落入本机默认库。
func remoteRef(c *remoteClients, libraryID string) knowledge.ResourceRef {
	return knowledge.ResourceRef{Backend: c.connection.Backend, ConnectionID: c.connection.ID, KnowledgeBaseID: libraryID}
}
func customLibrary(c *remoteClients, v *core.Library) knowledge.Library {
	return knowledge.Library{Ref: remoteRef(c, v.ID), ID: v.ID, Name: v.Name, Kind: v.Kind, WikiLanguage: v.WikiLanguage, Purpose: v.Purpose, ChunkingConfig: v.ChunkingConfig, WikiConfig: v.WikiConfig, IndexingStrategy: v.IndexingStrategy, EmbeddingModelID: v.EmbeddingModelID, Version: v.Version, StructureVersion: v.StructureVersion, PolicyVersion: v.PolicyVersion, Archived: v.Archived, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
func officialLibrary(c *remoteClients, v remote.Library) knowledge.Library {
	// 官方类别不承诺定制软件规则，零值代次不能作为可写版本使用。
	return knowledge.Library{Ref: remoteRef(c, v.ID), ID: v.ID, Name: v.Name, Kind: "official", Purpose: v.Type}
}
func customSource(v *core.SourceVersion) knowledge.Source {
	return knowledge.Source{OrganizeBlocked: v.OrganizeBlocked, ID: v.ID, SourceID: v.SourceID, Version: v.Version, RowVersion: v.RowVersion, Filename: v.Filename, MIME: v.MIME, SHA256: v.SHA256, Size: v.Size, State: v.State, Description: v.Description, Profile: v.Profile, SummaryStatus: v.SummaryStatus, Archived: v.Archived, Coverage: v.Coverage, Diagnostic: v.Diagnostic, CreatedAt: v.CreatedAt}
}

func customActivity(v *core.AuditLog) knowledge.LibraryActivity {
	return knowledge.LibraryActivity{ID: v.ID, RequestID: v.RequestID, Operation: v.Operation, ActorUserID: v.ActorUserID, ActorRole: v.ActorRole, Action: string(v.Action), ScopeType: v.ScopeType, ScopeID: v.ScopeID, TargetType: v.TargetType, TargetID: v.TargetID, Outcome: string(v.Outcome), Details: json.RawMessage(v.Details), CreatedAt: v.CreatedAt}
}
func officialSource(v remote.Source) knowledge.Source {
	// 官方原件身份与解析状态原样保留，不补造不可变版本或定制发布结论。
	return knowledge.Source{ID: v.ID, SourceID: v.ID, Filename: v.Filename, State: v.ParseStatus, Diagnostic: v.ParseStatus}
}
func customPage(v *core.Page) knowledge.Page {
	return knowledge.Page{SourcePurgeReview: knowledge.SourcePurgeReviewFromMetadata(v.PageMetadata), Evidence: customEvidence(v.Evidence), ID: v.ID, Slug: v.Slug, Title: v.Title, PageType: v.PageType, Status: v.Status, Content: v.Content, Summary: v.Summary, Aliases: v.Aliases, ThemeID: v.FolderID, SortOrder: v.SortOrder, Version: v.Version, SourceRefs: v.SourceRefs, ChunkRefs: v.ChunkRefs, InLinks: v.InLinks, OutLinks: v.OutLinks, CategoryPath: v.CategoryPath, ManualPlacement: v.ManualPlacement, WikiLanguage: v.WikiLanguage, UpdatedAt: v.UpdatedAt.UnixMilli()}
}
func officialPage(v remote.Page) knowledge.Page {
	return knowledge.Page{Summary: v.Summary, ID: v.ID, Slug: v.Slug, Title: v.Title, PageType: v.PageType, Content: v.Content, Version: v.Version, ThemeID: v.FolderID, Aliases: v.Aliases, SourceRefs: v.SourceRefs, InLinks: v.InLinks, OutLinks: v.OutLinks}
}
func customEvidence(v core.PageEvidence) knowledge.PageEvidence {
	out := knowledge.PageEvidence{Sources: []knowledge.SourceEvidence{}, Code: []knowledge.CodeEvidence{}}
	for _, s := range v.Sources {
		out.Sources = append(out.Sources, knowledge.SourceEvidence{SourceID: s.SourceID, SourceVersionID: s.SourceVersionID, SHA256: s.SHA256, Locator: knowledge.SourceLocator{Kind: s.Locator.Kind, Page: s.Locator.Page, StartLine: s.Locator.StartLine, EndLine: s.Locator.EndLine, Section: s.Locator.Section}, Excerpt: s.Excerpt, ExcerptHash: s.ExcerptHash, ClaimKind: s.ClaimKind})
	}
	for _, s := range v.Code {
		out.Code = append(out.Code, knowledge.CodeEvidence{Repository: s.Repository, Commit: s.Commit, Path: s.Path, Symbol: s.Symbol, StartLine: s.StartLine, EndLine: s.EndLine, VerifiedAt: s.VerifiedAt})
	}
	return out
}
func customRevision(v *core.Revision) knowledge.Revision {
	return knowledge.Revision{Summary: v.Summary, Evidence: customEvidence(v.Evidence), ID: v.ID, PageID: v.PageID, Version: v.Version, Title: v.Title, Content: v.Content, EditSource: v.EditSource, EditedAt: v.EditedAt.UnixMilli(), SourceRefs: v.SourceRefs}
}
func customReceipt(v *core.Receipt) knowledge.Receipt {
	return knowledge.Receipt{RequestID: v.RequestID, ResourceID: v.ResourceID, LibraryID: v.LibraryID, JobID: v.JobID, Version: v.Version, State: v.State, PageIDs: v.PageIDs}
}
func customJob(v *core.Job) knowledge.Job {
	return knowledge.Job{Maintenance: core.IsMaintenanceJob(v), AttemptStartedAt: v.AttemptStartedAt, Diagnostic: v.Diagnostic, ID: v.ID, LibraryID: v.LibraryID, RequestID: v.RequestID, State: v.State, Attempt: v.Attempt, Version: v.Version, SourceVersionIDs: v.SourceVersionIDs, CancelRequested: v.CancelRequested, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
func remotePagination(p knowledge.Pagination) (int, int) {
	limit := p.Limit
	if limit == 0 {
		limit = 50
	}
	return p.Offset, limit
}

// remoteWriteResult 只广播已由远端回执确认的变化，不把任务接纳视作完成。
func remoteWriteResult(ctx *gin.Context, c *remoteClients, v *core.Receipt, err error) (knowledge.Receipt, error) {
	if err != nil {
		return knowledge.Receipt{}, remoteError(err)
	}
	if global.KnowledgeControllerManager == nil {
		return knowledge.Receipt{}, status_error.KnowledgeUnsupported
	}
	if err := global.KnowledgeControllerManager.CompleteOperation(ctx, v.RequestID, v.LibraryID, v.ResourceID, v.JobID); err != nil {
		return knowledge.Receipt{}, err
	}
	if global.EventHub != nil {
		_ = events.Emit(global.EventHub, "knowledge", "knowledge.changed", events.Resource{Kind: "knowledge_library", ID: v.LibraryID, ParentID: c.connection.ID}, map[string]any{"ref": remoteRef(c, v.LibraryID), "requestId": v.RequestID, "resourceId": v.ResourceID})
	}
	return customReceipt(v), nil
}
