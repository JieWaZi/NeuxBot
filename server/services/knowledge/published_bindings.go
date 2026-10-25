package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/remote"
)

// PublishedBindings 将 API Key 的明确本地库绑定到与 allycli 相同的知识命令实现。
func (s *Service) PublishedBindings(ctx context.Context, authority *integrationservice.Service, identity models.IntegrationIdentity) ([]agentcapability.Binding, error) {
	readCaps := []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve}
	resources := func(ctx context.Context, _ string) ([]bridge.ResourceRef, error) {
		ids, err := authority.ResourceIDs(ctx, identity, constants.IntegrationResourceKindKnowledge, readCaps)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, status_error.IntegrationForbidden
		}
		refs := []bridge.ResourceRef{}
		for _, id := range ids {
			lib, err := s.config.Engine.GetLibrary(ctx, id)
			if err != nil {
				if errors.Is(bridge.MapError(err), status_error.KnowledgeNotFound) {
					continue
				}
				return nil, bridge.MapError(err)
			}
			if s.validateLibrary(lib) == nil {
				refs = append(refs, bridge.LocalRef(id))
			}
		}
		return refs, nil
	}
	refs, err := resources(ctx, "")
	if err != nil {
		return nil, err
	}
	bindings := bridge.ProviderReadBindings(bridge.LocalReadProvider{Engine: s.config.Engine, FreezeEmbedder: s.config.FreezeEmbedder}, bridge.FreezeScope("", refs), resources)
	authorize := func(ctx context.Context, op string, ref bridge.ResourceRef) (models.IntegrationAuthorization, error) {
		if err := bridge.RequireLocal(ref); err != nil {
			return models.IntegrationAuthorization{}, err
		}
		required := []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityIngest}
		if op == "knowledge.jobs.start" || op == "knowledge.jobs.retry" {
			required[1] = constants.IntegrationCapabilityOrganize
		}
		return authority.Authorize(ctx, identity, constants.IntegrationResourceKindKnowledge, ref.KnowledgeBaseID, required)
	}
	key := func(id string) string {
		return (requestidempotency.Identity{ID: id}).ResourceID("knowledge-mcp", identity.KeyID)
	}
	guard := func(ctx context.Context, op string, ref bridge.ResourceRef) (func(), error) {
		auth, err := authorize(ctx, op, ref)
		if err != nil {
			return nil, err
		}
		return authority.AcquireGuard(ctx, auth)
	}
	loadLibrary := func(ctx context.Context, _ string, ref bridge.ResourceRef) (*core.Library, *remote.Custom, error) {
		if err := bridge.RequireLocal(ref); err != nil {
			return nil, nil, err
		}
		lib, err := s.config.Engine.GetLibrary(ctx, ref.KnowledgeBaseID)
		if err != nil {
			return nil, nil, bridge.MapError(err)
		}
		return lib, nil, s.validateLibrary(lib)
	}
	recovery := func(ctx context.Context, op string, ref bridge.ResourceRef) (json.RawMessage, error) {
		auth, err := authorize(ctx, op, ref)
		if err != nil {
			return nil, err
		}
		lib, _, err := loadLibrary(ctx, op, ref)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(integrationservice.KnowledgeRecovery{Origin: "direct_api", Subject: identity.KeyID, Authorization: auth, Permission: core.PermissionSnapshot{PolicyVersion: lib.PolicyVersion, Scope: auth.Scope, Operations: []string{"organize"}}})
		if err != nil {
			return nil, status_error.KnowledgeInvalid
		}
		return raw, nil
	}
	writes := s.wikiWriteBindings(wikiAccess{authorize: loadLibrary, guard: guard, identity: key, actor: identity.KeyID,
		activity: func(ctx context.Context) context.Context {
			return core.WithActivityActor(ctx, core.ActivityActor{UserID: identity.KeyID, Trigger: "api"})
		},
		retry: func(ctx context.Context, request, job string, version int64) (*core.Receipt, error) {
			j, err := s.config.Engine.GetJob(ctx, job)
			if err != nil {
				return nil, bridge.MapError(err)
			}
			raw, err := recovery(ctx, "knowledge.jobs.retry", bridge.LocalRef(j.LibraryID))
			if err != nil {
				return nil, err
			}
			return s.retry(ctx, request, job, version, raw)
		},
	})
	// 重试不持外层同步锁，原与本次授权在共用 Retry 提交边界一次核对。
	for i := range writes {
		if writes[i].Operation == "knowledge.jobs.retry" {
			call := writes[i].Call
			writes[i].Call = func(ctx context.Context, r agentcapability.Request) (agentcapability.Result, error) {
				var in wikiCommand
				if err := agentcapability.DecodeParams(r.Params, &in); err != nil {
					return agentcapability.Result{}, err
				}
				if _, err := authorize(ctx, "knowledge.jobs.retry", in.ref()); err != nil {
					return agentcapability.Result{}, err
				}
				j, err := s.config.Engine.GetJob(ctx, in.JobID)
				if err != nil {
					return agentcapability.Result{}, bridge.MapError(err)
				}
				if j.LibraryID != in.LibraryID {
					return agentcapability.Result{}, status_error.KnowledgeForbidden
				}
				return call(ctx, r)
			}
		}
	}
	bindings = append(bindings, writes...)
	bindings = append(bindings, agentcapability.Binding{Operation: "knowledge.jobs.start", Call: func(ctx context.Context, r agentcapability.Request) (agentcapability.Result, error) {
		var in struct {
			LibraryID         string   `json:"libraryId"`         // 明确本地库。
			Backend           string   `json:"backend"`           // 目标协议。
			ConnectionID      string   `json:"connectionId"`      // 目标连接。
			AttachmentIndexes []int    `json:"attachmentIndexes"` // 外部调用不能引用内部附件。
			SourceVersionIDs  []string `json:"sourceVersionIds"`  // 已导入的真实来源版本。
			Message           string   `json:"message"`           // 本次整理要求。
		}
		if err := agentcapability.DecodeParams(r.Params, &in); err != nil {
			return agentcapability.Result{}, err
		}
		if in.LibraryID == "" || r.RequestID == "" {
			return agentcapability.Result{}, status_error.KnowledgeInvalid
		}
		if len(in.AttachmentIndexes) > 0 {
			return agentcapability.Result{}, agentcapability.Reject(status_error.KnowledgeInvalid, "Import source bytes first, then select sourceVersionIds; attachmentIndexes belongs to internal executions.")
		}
		ref := (wikiCommand{LibraryID: in.LibraryID, Backend: in.Backend, ConnectionID: in.ConnectionID}).ref()
		raw, err := recovery(ctx, "knowledge.jobs.start", ref)
		if err != nil {
			return agentcapability.Result{}, err
		}
		id := key(r.RequestID)
		session := models.KnowledgeTaskConfiguration{ID: id, RemoteSubject: identity.KeyID, RemoteAuthorization: raw}
		if old, e := s.config.References.GetTaskConfiguration(ctx, id); e == nil {
			session.RemoteAuthorization = old.RemoteAuthorization
		} else if !errors.Is(e, status_error.KnowledgeNotFound) {
			return agentcapability.Result{}, e
		}
		receipt, _, err := s.Organize(ctx, OrganizeCommand{RequestID: id, LibraryID: in.LibraryID, SourceVersionIDs: in.SourceVersionIDs, ExplicitSources: in.SourceVersionIDs != nil, Message: in.Message, Configuration: session})
		if err != nil {
			return agentcapability.Result{}, err
		}
		return agentcapability.Result{CommandID: r.RequestID, Data: map[string]any{"receipt": receipt, "noop": receipt == nil || receipt.State == "no-op"}}, nil
	}})
	return bindings, nil
}

// jobConfiguration 仅采用真实核心重试回执确认的配置，未接纳候选不会污染后续尝试。
func (s *Service) jobConfiguration(ctx context.Context, job *core.Job, original string) (models.KnowledgeTaskConfiguration, error) {
	session, err := s.config.References.GetTaskConfiguration(ctx, original)
	if err != nil {
		return session, err
	}
	if job.Attempt < 2 {
		return session, nil
	}
	rows, err := s.config.References.ListJobRetryOperations(ctx, job.ID)
	if err != nil {
		return session, err
	}
	chosen := 0
	for _, row := range rows {
		if row.Operation != constants.KnowledgeRetryAuthorityOperation || row.Backend != "local" || row.ConnectionID != "local" || row.KnowledgeBaseID != job.LibraryID || row.ExpectedResourceID != job.ID || row.ReceiptRequestID == "" || row.TargetAttempt < 2 || row.TargetVersion < 1 {
			return session, status_error.KnowledgeForbidden
		}
		if row.TargetAttempt > job.Attempt || row.TargetAttempt < chosen {
			continue
		}
		receipt, e := s.config.Engine.GetReceipt(ctx, row.ReceiptRequestID)
		if errors.Is(bridge.MapError(e), status_error.KnowledgeNotFound) {
			continue
		}
		if e != nil {
			return session, bridge.MapError(e)
		}
		if receipt.RequestID != row.ReceiptRequestID || receipt.Operation != "job.retry" || receipt.JobID != job.ID || receipt.LibraryID != job.LibraryID || receipt.Version != row.TargetVersion+1 || receipt.State != "accepted" {
			return session, status_error.KnowledgeForbidden
		}
		if row.TargetAttempt == chosen {
			return session, status_error.KnowledgeConflict
		}
		next, e := s.config.References.GetTaskConfiguration(ctx, row.ConfigurationID)
		if e != nil {
			return session, e
		}
		if len(next.PublicationAuthorizations) < len(session.PublicationAuthorizations) || len(session.PublicationAuthorizations) > 0 && !reflect.DeepEqual(next.PublicationAuthorizations[:len(session.PublicationAuthorizations)], session.PublicationAuthorizations) {
			return session, status_error.KnowledgeForbidden
		}
		if next.Backend != session.Backend || next.ConnectionID != session.ConnectionID || next.KnowledgeBaseID != session.KnowledgeBaseID || next.RuntimeID != session.RuntimeID || next.AccountID != session.AccountID || next.Model != session.Model || next.Message != session.Message || next.RemoteSubject != session.RemoteSubject || string(next.RemoteAuthorization) != string(session.RemoteAuthorization) {
			return session, status_error.KnowledgeForbidden
		}
		session = next
		chosen = row.TargetAttempt
	}
	return session, nil
}

// retryPublicationGuard 保留原授权并批量核对每次外部重试附加的 Key 约束。
func (s *Service) retryPublicationGuard(ctx context.Context, session models.KnowledgeTaskConfiguration) (func(), error) {
	if s.config.IntegrationPublicationGuard == nil {
		return nil, status_error.KnowledgeForbidden
	}
	raws := append([]json.RawMessage{}, session.PublicationAuthorizations...)
	runID := ""
	release := func() {}
	if session.RemoteSubject != "" {
		var origin struct {
			Origin         string `json:"origin"`         // 原授权恢复机制。
			AutomaticRunID string `json:"automaticRunId"` // 原自动运行身份。
		}
		if json.Unmarshal(session.RemoteAuthorization, &origin) != nil {
			return nil, status_error.KnowledgeForbidden
		}
		switch origin.Origin {
		case "direct_api":
			raws = append(raws, session.RemoteAuthorization)
		case "automatic_task":
			runID = origin.AutomaticRunID
			if runID == "" {
				return nil, status_error.KnowledgeForbidden
			}
		default:
			guard, _, err := s.remotePublicationGuard(session)
			if err != nil {
				return nil, err
			}
			if guard == nil {
				return nil, status_error.KnowledgeForbidden
			}
			var err2 error
			release, err2 = guard(ctx, session.RemoteAuthorization)
			if err2 != nil {
				return nil, err2
			}
		}
	}
	combined, err := s.config.IntegrationPublicationGuard(ctx, raws, runID)
	if err != nil {
		release()
		return nil, err
	}
	return func() { combined(); release() }, nil
}
