package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/remote"
)

// wikiCommand 只转换已经通过唯一命令 Schema 校验的维护参数，授权仍由服务端冻结。
type wikiCommand struct {
	LibraryID         string   `json:"libraryId"`         // 目标知识库。
	Backend           string   `json:"backend"`           // 服务协议。
	ConnectionID      string   `json:"connectionId"`      // 冻结连接。
	PageID            string   `json:"pageId"`            // 稳定页面身份。
	ExpectedVersion   int      `json:"expectedVersion"`   // 已读取的页面版本。
	ExpectedUpdatedAt string   `json:"expectedUpdatedAt"` // 整页改写时读取到的 RFC3339 更新时间。
	StructureVersion  int64    `json:"structureVersion"`  // 已读取的目录版本。
	Slug              string   `json:"slug"`              // 页面可读地址。
	Title             string   `json:"title"`             // 页面标题。
	Summary           string   `json:"summary"`           // 页面简介。
	Content           string   `json:"content"`           // 完整正文。
	PageType          string   `json:"pageType"`          // 页面类型。
	Aliases           []string `json:"aliases"`           // 页面别名。
	SourceVersionIDs  []string `json:"sourceVersionIds"`  // 真实来源版本集合。
	OldText           string   `json:"oldText"`           // 精确匹配的原文。
	NewText           string   `json:"newText"`           // 替换后的原文。
	NewSlug           string   `json:"newSlug"`           // 新页面地址。
	RevisionVersion   int      `json:"revisionVersion"`   // 要恢复的历史版本。
	ThemeID           string   `json:"themeId"`           // 目录身份。
	Name              string   `json:"name"`              // 目录名称。
	ParentID          string   `json:"parentId"`          // 上级目录。
	Delete            bool     `json:"delete"`            // 明确删除空目录。
	Filename          string   `json:"filename"`          // 导入文件名。
	MIME              string   `json:"mime"`              // 原件媒体类型。
	Bytes             []byte   `json:"bytes"`             // JSON 中明确提供的原件字节。
	SourceID          string   `json:"sourceId"`          // 替换原件时的稳定来源身份。
	JobID             string   `json:"jobId"`             // 原整理任务身份。
}

func (in wikiCommand) ref() bridge.ResourceRef {
	if in.Backend == "" {
		in.Backend = "local"
	}
	if in.ConnectionID == "" && in.Backend == "local" {
		in.ConnectionID = "local"
	}
	return bridge.ResourceRef{Backend: in.Backend, ConnectionID: in.ConnectionID, KnowledgeBaseID: in.LibraryID}
}

// wikiBindings 将维护工具绑定到冻结的库和权限代次，复用已有领域事务与持久回执。
func (s *Service) wikiBindings(ctx context.Context, chat models.Chat, refs []bridge.ResourceRef, provider *frozenKnowledgeReads, pageOnly string) []agentcapability.Binding {
	policies := map[bridge.ResourceRef]int64{}
	for _, ref := range refs {
		if ref.Backend != "local" && ref.Backend != "custom" {
			continue
		}
		lib, err := provider.Library(ctx, ref)
		if err == nil && s.validateTarget(ctx, chat.WakerID, chat.ProjectID, lib) == nil {
			policies[ref] = lib.PolicyVersion
		}
	}
	authorize := func(ctx context.Context, ref bridge.ResourceRef) (*core.Library, *remote.Custom, error) {
		version, ok := policies[ref]
		if !ok {
			return nil, nil, status_error.KnowledgeForbidden
		}
		current, err := s.agentReadResources(ctx, provider)
		if err != nil {
			return nil, nil, err
		}
		if !slices.Contains(current, ref) {
			return nil, nil, status_error.KnowledgeForbidden
		}
		if chat.ID != "" && chat.ActiveExecutionID != "" {
			summary, err := s.config.Chats.GetChatSummary(ctx, chat.ID)
			if err != nil {
				return nil, nil, err
			}
			if summary.Chat.ActiveExecutionID != chat.ActiveExecutionID || summary.Chat.WakerID != chat.WakerID {
				return nil, nil, status_error.KnowledgeForbidden
			}
		}
		lib, err := provider.Library(ctx, ref)
		if err != nil {
			return nil, nil, err
		}
		if lib.PolicyVersion != version {
			return nil, nil, status_error.KnowledgeForbidden
		}
		if err = s.validateTarget(ctx, chat.WakerID, chat.ProjectID, lib); err != nil {
			return nil, nil, err
		}
		if ref.Backend == "local" {
			return lib, nil, nil
		}
		p, err := provider.remote(ctx, ref)
		if err != nil {
			return nil, nil, err
		}
		return lib, p.Custom, nil
	}
	key := func(id string) string {
		return (requestidempotency.Identity{ID: id}).ResourceID("knowledge-agent", chat.ID, chat.ActiveExecutionID)
	}
	result := []agentcapability.Binding{}
	for _, operation := range []string{"knowledge.pages.write", "knowledge.pages.replace", "knowledge.pages.rename", "knowledge.pages.delete", "knowledge.revisions.restore", "knowledge.themes.write", "knowledge.sources.import", "knowledge.jobs.retry"} {
		op := operation
		if pageOnly != "" && slices.Contains([]string{"knowledge.themes.write", "knowledge.sources.import", "knowledge.jobs.retry"}, op) {
			continue
		}
		binding := agentcapability.Binding{Operation: op}
		binding.Call = func(ctx context.Context, r agentcapability.Request) (agentcapability.Result, error) {
			var in wikiCommand
			if err := agentcapability.DecodeParams(r.Params, &in); err != nil {
				return agentcapability.Result{}, err
			}
			ref := in.ref()
			if op != "knowledge.jobs.retry" {
				s.policy.RLock()
				defer s.policy.RUnlock()
			}
			lib, client, err := authorize(ctx, ref)
			if err != nil {
				return agentcapability.Result{}, err
			}
			if client != nil {
				caps, e := client.Capabilities(ctx)
				if e != nil {
					return agentcapability.Result{}, bridge.MapRemoteError(e)
				}
				if !caps.Operations["write"] {
					return agentcapability.Result{}, status_error.KnowledgeForbidden
				}
			}
			if pageOnly != "" && in.PageID != "" && in.PageID != pageOnly {
				return agentcapability.Result{}, status_error.KnowledgeForbidden
			}
			if pageOnly != "" && slices.Contains([]string{"knowledge.pages.write", "knowledge.pages.replace", "knowledge.pages.rename", "knowledge.pages.delete", "knowledge.revisions.restore"}, op) && in.PageID != pageOnly {
				return agentcapability.Result{}, status_error.KnowledgeForbidden
			}
			if r.RequestID == "" {
				return agentcapability.Result{}, status_error.KnowledgeInvalid
			}
			raw, _ := json.Marshal(struct {
				Operation string             // 实际命令身份。
				Ref       bridge.ResourceRef // 冻结的完整知识资源身份。
				Params    map[string]any     // 经命令Schema验证的原始业务参数。
			}{op, ref, r.Params})
			digest := sha256.Sum256(raw)
			id := key(r.RequestID)
			link := models.KnowledgeOperationLink{RequestID: id, ReceiptRequestID: id, Operation: op, Digest: hex.EncodeToString(digest[:]), Backend: ref.Backend, ConnectionID: ref.ConnectionID, KnowledgeBaseID: lib.ID, ChatID: chat.ID, ExecutionID: chat.ActiveExecutionID}
			link.ExpectedResourceID = wikiTargetResource(op, in)
			if saved, ok := provider.Snapshots[ref.ConnectionID]; ok {
				link.ConnectionRevision = saved.Revision
			}
			var fresh bool
			if client == nil {
				_, priorErr := s.config.References.GetOperation(ctx, id)
				fresh = errors.Is(priorErr, status_error.KnowledgeNotFound)
				if priorErr != nil && !fresh {
					return agentcapability.Result{}, priorErr
				}
				err = s.config.References.BeginOperation(ctx, link)
			} else {
				fresh, err = s.config.References.ClaimRemoteOperation(ctx, link)
			}
			if err != nil {
				return agentcapability.Result{}, err
			}
			var receipt *core.Receipt
			if !fresh {
				saved, e := s.config.References.GetOperation(ctx, id)
				if e != nil {
					return agentcapability.Result{}, e
				}
				if saved.RemoteErrorCode != 0 {
					return agentcapability.Result{}, status_error.StatusError(saved.RemoteErrorCode)
				}
				if client == nil {
					receipt, err = s.config.Engine.GetReceipt(ctx, id)
				} else {
					receipt, err = client.GetReceipt(ctx, id)
				}
				if receipt != nil && err == nil {
					if err = validateWikiReceipt(receipt, op, id, lib.ID, in, wikiReceiptTarget(saved)); err != nil {
						return agentcapability.Result{}, err
					}
					if err = s.config.References.CompleteOperation(ctx, id, receipt.LibraryID, receipt.ResourceID, receipt.JobID); err != nil {
						return agentcapability.Result{}, err
					}
					return agentcapability.Result{CommandID: r.RequestID, Replayed: true, Data: map[string]any{"ref": ref, "receipt": receipt}}, nil
				}
				if client != nil {
					return agentcapability.Result{}, status_error.KnowledgeRemoteOutcomeUnknown
				}
				if !errors.Is(bridge.MapError(err), status_error.KnowledgeNotFound) {
					return agentcapability.Result{}, bridge.MapError(err)
				}
			}
			writeContext := ctx
			if client == nil {
				writeContext = bridge.LocalUserActivity(ctx)
			}
			receipt, err = s.applyWikiCommand(writeContext, op, id, in, lib, client, chat.WakerID, pageOnly)
			if err != nil {
				mapped := wikiBoundaryError(err, client)
				if client != nil && !errors.Is(mapped, status_error.KnowledgeRemoteOutcomeUnknown) {
					var stable interface{ Code() int64 }
					if errors.As(mapped, &stable) {
						if persist := s.config.References.RejectRemoteOperation(ctx, id, stable.Code()); persist != nil {
							return agentcapability.Result{}, persist
						}
					}
				}
				return agentcapability.Result{}, mapped
			}
			if err = validateWikiReceipt(receipt, op, id, lib.ID, in, ""); err != nil {
				return agentcapability.Result{}, err
			}
			if err = s.config.References.CompleteOperation(ctx, id, receipt.LibraryID, receipt.ResourceID, receipt.JobID); err != nil {
				return agentcapability.Result{}, err
			}
			if s.config.Events != nil {
				_ = events.Emit(s.config.Events, "knowledge", "knowledge.changed", events.Resource{Kind: "knowledge_library", ID: lib.ID, ParentID: ref.ConnectionID}, map[string]any{"ref": ref, "requestId": id})
			}
			return agentcapability.Result{CommandID: r.RequestID, Data: map[string]any{"ref": ref, "receipt": receipt}}, nil
		}
		binding.Receipt = func(ctx context.Context, id string) (agentcapability.Result, error) {
			link, err := s.config.References.GetOperation(ctx, key(id))
			if err != nil {
				return agentcapability.Result{}, err
			}
			if link.Operation != op || link.ChatID != chat.ID || link.ExecutionID != chat.ActiveExecutionID {
				return agentcapability.Result{}, status_error.KnowledgeForbidden
			}
			if link.RemoteErrorCode != 0 {
				return agentcapability.Result{}, status_error.StatusError(link.RemoteErrorCode)
			}
			ref := bridge.ResourceRef{Backend: link.Backend, ConnectionID: link.ConnectionID, KnowledgeBaseID: link.KnowledgeBaseID}
			_, client, err := authorize(ctx, ref)
			if err != nil {
				return agentcapability.Result{}, err
			}
			var receipt *core.Receipt
			if client == nil {
				receipt, err = s.config.Engine.GetReceipt(ctx, key(id))
			} else {
				receipt, err = client.GetReceipt(ctx, key(id))
			}
			if err != nil {
				if client != nil && errors.Is(bridge.MapRemoteError(err), status_error.KnowledgeNotFound) {
					return agentcapability.Result{}, status_error.KnowledgeRemoteOutcomeUnknown
				}
				return agentcapability.Result{}, wikiBoundaryError(err, client)
			}
			if err = validateWikiReceipt(receipt, op, key(id), ref.KnowledgeBaseID, wikiCommand{}, wikiReceiptTarget(link)); err != nil {
				return agentcapability.Result{}, err
			}
			if err = s.config.References.CompleteOperation(ctx, key(id), receipt.LibraryID, receipt.ResourceID, receipt.JobID); err != nil {
				return agentcapability.Result{}, err
			}
			return agentcapability.Result{CommandID: id, Replayed: true, Data: map[string]any{"ref": ref, "receipt": receipt}}, nil
		}
		result = append(result, binding)
	}
	return result
}

// wikiBoundaryError 保留本地核心错误与远端传输错误的真实边界。
func wikiBoundaryError(err error, client *remote.Custom) error {
	var stable interface{ Code() int64 }
	if errors.As(err, &stable) {
		return err
	}
	if client == nil {
		return bridge.MapError(err)
	}
	return bridge.MapRemoteError(err)
}

// validateWikiReceipt 核对恢复结果仍属于原操作和目标，不以错误资源确认未知写入。
func validateWikiReceipt(receipt *core.Receipt, op, requestID, libraryID string, in wikiCommand, savedResourceID string) error {
	operations := map[string]string{
		"knowledge.pages.write": "page.agent-write", "knowledge.pages.replace": "page.replace-text",
		"knowledge.pages.rename": "page.rename", "knowledge.pages.delete": "page.delete",
		"knowledge.revisions.restore": "page.restore", "knowledge.themes.write": "theme.write",
		"knowledge.sources.import": "source.import", "knowledge.jobs.retry": "job.retry",
	}
	target := wikiTargetResource(op, in)
	if target == "" {
		target = savedResourceID
	}
	if receipt == nil || receipt.RequestID != requestID || receipt.LibraryID != libraryID || receipt.Operation != operations[op] || target != "" && receipt.ResourceID != target {
		return status_error.KnowledgeRemoteOutcomeUnknown
	}
	state := "saved"
	if op == "knowledge.jobs.retry" {
		state = "accepted"
	}
	if receipt.State != state || receipt.Version < 1 {
		return status_error.KnowledgeRemoteOutcomeUnknown
	}
	return nil
}

func wikiReceiptTarget(link models.KnowledgeOperationLink) string {
	if link.ExpectedResourceID != "" {
		return link.ExpectedResourceID
	}
	if link.ReceiptConfirmed {
		return link.ResourceID
	}
	return ""
}

func wikiTargetResource(op string, in wikiCommand) string {
	switch op {
	case "knowledge.pages.write", "knowledge.pages.replace", "knowledge.pages.rename", "knowledge.pages.delete", "knowledge.revisions.restore":
		return in.PageID
	case "knowledge.themes.write":
		return in.ThemeID
	}
	return ""
}

func (s *Service) applyWikiCommand(ctx context.Context, op, id string, in wikiCommand, lib *core.Library, c *remote.Custom, actor, pageOnly string) (*core.Receipt, error) {
	getPage := func() (core.Page, error) {
		if in.PageID == "" {
			return core.Page{KnowledgeBaseID: lib.ID}, nil
		}
		var p *core.Page
		var err error
		if c == nil {
			p, err = s.config.Engine.GetPage(ctx, lib.ID, in.PageID)
		} else {
			p, err = c.GetPage(ctx, in.PageID)
		}
		if err != nil {
			return core.Page{}, err
		}
		return *p, nil
	}
	switch op {
	case "knowledge.pages.write":
		p, err := getPage()
		if err != nil {
			return nil, err
		}
		if in.PageID != "" {
			readTime, e := time.Parse(time.RFC3339Nano, in.ExpectedUpdatedAt)
			if e != nil {
				return nil, status_error.KnowledgeInvalid
			}
			p.UpdatedAt = readTime
		}
		p.Slug = in.Slug
		p.Title = in.Title
		p.Summary = in.Summary
		p.Content = in.Content
		p.PageType = in.PageType
		p.Aliases = in.Aliases
		p.LastEditorID = actor
		if in.SourceVersionIDs != nil {
			prior := p.Evidence.Sources
			p.Evidence.Sources = nil
			p.SourceRefs = nil
			for _, evidence := range prior {
				if !slices.Contains(in.SourceVersionIDs, evidence.SourceVersionID) {
					p.ChunkRefs = nil
					break
				}
			}
			for _, sid := range in.SourceVersionIDs {
				var source *core.SourceVersion
				if c == nil {
					source, err = s.config.Engine.GetSource(ctx, lib.ID, sid)
				} else {
					source, err = c.GetSource(ctx, sid)
				}
				if err != nil {
					return nil, err
				}
				kept := false
				for _, evidence := range prior {
					if evidence.SourceVersionID == sid {
						p.Evidence.Sources = append(p.Evidence.Sources, evidence)
						kept = true
					}
				}
				if !kept {
					p.Evidence.Sources = append(p.Evidence.Sources, core.SourceRef{SourceID: source.SourceID, SourceVersionID: source.ID, SHA256: source.SHA256, ClaimKind: "document_context"})
				}
			}
		}
		input := core.WritePageInput{RequestID: id, LibraryID: lib.ID, ExpectedVersion: in.ExpectedVersion, StructureVersion: in.StructureVersion, ExpectedPolicyVersion: lib.PolicyVersion, Page: p}
		if c != nil {
			return c.WriteAgentPage(ctx, input)
		}
		return s.config.Engine.WriteAgentPage(ctx, input)
	case "knowledge.pages.replace":
		input := core.ReplacePageTextInput{RequestID: id, LibraryID: lib.ID, PageID: in.PageID, ExpectedVersion: in.ExpectedVersion, StructureVersion: in.StructureVersion, ExpectedPolicyVersion: lib.PolicyVersion, OldText: in.OldText, NewText: in.NewText}
		if c != nil {
			return c.ReplacePageText(ctx, input)
		}
		return s.config.Engine.ReplacePageText(ctx, input)
	case "knowledge.pages.rename":
		input := core.RenamePageInput{RequestID: id, LibraryID: lib.ID, PageID: in.PageID, ExpectedVersion: in.ExpectedVersion, StructureVersion: in.StructureVersion, ExpectedPolicyVersion: lib.PolicyVersion, NewSlug: in.NewSlug}
		if c != nil {
			return c.RenamePage(ctx, input)
		}
		return s.config.Engine.RenamePage(ctx, input)
	case "knowledge.pages.delete":
		input := core.DeletePageInput{RequestID: id, LibraryID: lib.ID, PageID: in.PageID, ExpectedVersion: in.ExpectedVersion, StructureVersion: in.StructureVersion, ExpectedPolicyVersion: lib.PolicyVersion}
		if c != nil {
			return c.DeletePage(ctx, input)
		}
		return s.config.Engine.DeletePage(ctx, input)
	case "knowledge.revisions.restore":
		input := core.RestorePageInput{RequestID: id, LibraryID: lib.ID, PageID: in.PageID, ExpectedVersion: in.ExpectedVersion, RevisionVersion: in.RevisionVersion, StructureVersion: in.StructureVersion}
		if c != nil {
			return c.RestorePage(ctx, input)
		}
		return s.config.Engine.RestorePage(ctx, input)
	case "knowledge.themes.write":
		if pageOnly != "" {
			return nil, status_error.KnowledgeForbidden
		}
		input := core.WriteThemeInput{RequestID: id, LibraryID: lib.ID, StructureVersion: in.StructureVersion, Theme: core.Theme{ID: in.ThemeID, Name: in.Name, ParentID: in.ParentID}, Delete: in.Delete}
		if c != nil {
			return c.WriteTheme(ctx, input)
		}
		return s.config.Engine.WriteTheme(ctx, input)
	case "knowledge.sources.import":
		if pageOnly != "" {
			return nil, status_error.KnowledgeForbidden
		}
		input := core.ImportSourceInput{RequestID: id, LibraryID: lib.ID, SourceID: in.SourceID, ExpectedVersion: int64(in.ExpectedVersion), Filename: in.Filename, MIME: in.MIME, Bytes: in.Bytes}
		if c != nil {
			return c.ImportSource(ctx, input)
		}
		return s.config.Engine.ImportSource(ctx, input)
	case "knowledge.jobs.retry":
		if c != nil || pageOnly != "" {
			return nil, status_error.KnowledgeUnsupported
		}
		job, err := s.config.Engine.GetJob(ctx, in.JobID)
		if err != nil {
			return nil, err
		}
		if job.LibraryID != lib.ID {
			return nil, status_error.KnowledgeForbidden
		}
		return s.Retry(ctx, id, in.JobID, int64(in.ExpectedVersion))
	}
	return nil, status_error.KnowledgeUnsupported
}
