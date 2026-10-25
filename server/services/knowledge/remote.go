package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/httpapi"
	"strings"
)

// ProfileProvider 只读取服务器固定模型配置，不接收客户端本机账号和路径。
type ProfileProvider interface {
	ResolveKnowledgeProfile(context.Context, string) (models.KnowledgeTaskConfiguration, error)
}

// ProfileMap 是组合根提供的固定服务端配置快照，调用者不得并发修改。
type ProfileMap map[string]models.KnowledgeTaskConfiguration

func (p ProfileMap) ResolveKnowledgeProfile(_ context.Context, id string) (models.KnowledgeTaskConfiguration, error) {
	value, ok := p[id]
	if !ok || value.RuntimeID == "" {
		return value, status_error.KnowledgeUnsupported
	}
	return value, nil
}

// RemoteAdapter 只复用同一知识服务，不开放桌面全域API。
type RemoteAdapter struct {
	// Service 是独立知识核心与模型服务装配。
	Service *Service
	// Profiles 解析服务器固定profile。
	Profiles ProfileProvider
}

// NewRemoteAdapter 在恢复任务前装配同一策略源的可恢复发布闸门。
func NewRemoteAdapter(service *Service, profiles ProfileProvider, security *httpapi.PolicyStore) (*RemoteAdapter, error) {
	if service == nil || profiles == nil || security == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	err := service.SetRemoteGuard(func(ctx context.Context, raw json.RawMessage) (func(), error) {
		var identity httpapi.RecoveryIdentity
		if err := json.Unmarshal(raw, &identity); err != nil {
			return nil, status_error.KnowledgeForbidden
		}
		return security.RestoreGuard(service.config.Engine, identity)(ctx)
	})
	if err != nil {
		return nil, err
	}
	return &RemoteAdapter{Service: service, Profiles: profiles}, nil
}
func (a *RemoteAdapter) session(ctx context.Context, id string, auth httpapi.Authorization) (models.KnowledgeTaskConfiguration, error) {
	session, err := a.Profiles.ResolveKnowledgeProfile(ctx, id)
	if err != nil {
		return session, err
	}
	if auth.Subject == "" || auth.PublishGuard == nil || (len(auth.RecoveryData) == 0 && auth.Recovery.Subject != auth.Subject) {
		return session, status_error.KnowledgeForbidden
	}

	raw, err := json.Marshal(auth.Recovery)
	if len(auth.RecoveryData) > 0 {
		raw = append(json.RawMessage(nil), auth.RecoveryData...)
		err = nil
	}
	if err != nil {
		return session, status_error.KnowledgeForbidden
	}
	session.ID = ""

	session.RemoteSubject = auth.Subject
	session.RemoteProfileID = id
	session.RemoteAuthorization = raw
	return session, nil
}

// Organize 使用服务器固定模型选择和冻结授权接纳独立知识作业。
func (a *RemoteAdapter) Organize(ctx context.Context, in httpapi.OrganizeRequest, auth httpapi.Authorization) (result *core.Receipt, finalErr error) {
	defer func() { finalErr = remoteBoundaryError(finalErr) }()
	session, err := a.session(ctx, in.ProfileID, auth)
	if err != nil {
		return nil, err
	}
	release, err := auth.PublishGuard(ctx)
	if err != nil {
		return nil, err
	}
	release()
	lib, err := a.Service.config.Engine.GetLibrary(ctx, in.LibraryID)
	if err != nil {
		return nil, err
	}
	if lib.Version != in.ExpectedVersion || lib.StructureVersion != in.StructureVersion {
		return nil, &core.Error{Code: core.ErrVersionConflict, Operation: "remote.organize"}
	}
	session.ID = in.RequestID
	message := in.Instruction
	if strings.TrimSpace(message) == "" {
		message = "整理服务器已授权的指定来源"
	}
	receipt, _, err := a.Service.Organize(ctx, OrganizeCommand{RequestID: in.RequestID, LibraryID: in.LibraryID, SourceVersionIDs: in.SourceVersionIDs, Rebuild: in.Rebuild, ExplicitSources: true, Configuration: session, Message: message})
	return receipt, err
}

// Maintain 使用部署者固定的模型服务完成明确页面修订，不创建后台聊天。
func (a *RemoteAdapter) Maintain(ctx context.Context, in httpapi.MaintenanceRequest, auth httpapi.Authorization) (result *core.Receipt, finalErr error) {
	defer func() { finalErr = remoteBoundaryError(finalErr) }()
	session, err := a.session(ctx, in.ProfileID, auth)
	if err != nil {
		return nil, err
	}
	operationID := (requestidempotency.Identity{ID: in.RequestID}).ResourceID("knowledge-remote-maintenance", auth.Subject)
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	link := models.KnowledgeOperationLink{RequestID: operationID, Operation: "maintenance.generate", Digest: hex.EncodeToString(sum[:]), Backend: "local", ConnectionID: "local", KnowledgeBaseID: in.LibraryID}
	if err := a.Service.config.References.BeginOperation(ctx, link); err != nil {
		return nil, err
	}
	if receipt, err := a.Service.config.Engine.GetReceipt(ctx, in.RequestID); err == nil {
		return receipt, nil
	}
	release, err := auth.PublishGuard(ctx)
	if err != nil {
		return nil, err
	}
	library, err := a.Service.config.Engine.GetLibrary(ctx, in.LibraryID)
	release()
	if err != nil {
		return nil, err
	}
	if library.Version != in.ExpectedVersion || library.StructureVersion != in.StructureVersion {
		return nil, status_error.KnowledgeConflict
	}
	page, err := a.Service.config.Engine.GetPage(ctx, in.LibraryID, in.PageID)
	if err != nil {
		return nil, err
	}
	if page.Version != in.ExpectedPageVersion {
		return nil, status_error.KnowledgeConflict
	}
	selected, err := a.Service.ResolveModel(ctx, models.LibraryExecutionDefaults{RuntimeID: session.RuntimeID, AccountID: session.AccountID, Model: session.Model, Reasoning: session.Reasoning})
	if err != nil {
		return nil, err
	}
	if a.Service.config.Generator == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	generator, err := a.Service.config.Generator(ctx, selected)
	if err != nil {
		return nil, err
	}
	generated, err := generator.Generate(ctx, core.GenerateRequest{JobID: operationID, StageID: operationID, Stage: "page-maintenance", MaxTokens: 32768, Temperature: .3, Thinking: false, Messages: []core.ModelMessage{{Role: "system", Content: "Revise only the supplied wiki page according to the user's instruction. Preserve existing factual details and citations. Return only the complete Markdown page body, without code fences or commentary."}, {Role: "user", Content: in.Instruction + "\n\n" + page.Content}}})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(generated.Text) == "" || (generated.FinishReason != "stop" && generated.FinishReason != "end_turn" && generated.FinishReason != "completed") {
		return nil, &core.Error{Code: core.ErrExecutionNotSuccessful, Operation: "remote.maintenance"}
	}
	release, err = auth.PublishGuard(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	next := *page
	next.Content = generated.Text
	receipt, err := a.Service.config.Engine.WritePage(ctx, core.WritePageInput{RequestID: in.RequestID, LibraryID: in.LibraryID, ExpectedPolicyVersion: library.PolicyVersion, ExpectedVersion: page.Version, StructureVersion: library.StructureVersion, Page: next})
	if err != nil {
		return nil, err
	}
	if err := a.Service.config.References.CompleteOperation(ctx, operationID, receipt.LibraryID, receipt.ResourceID, receipt.JobID); err != nil {
		return nil, err
	}
	a.Service.emitTaskChanged(receipt.JobID, receipt.LibraryID)
	return receipt, nil
}

var _ httpapi.ExecutionPort = (*RemoteAdapter)(nil)

// remoteBoundaryError 在专用服务器外部边界翻译Ally稳定错误，不泄露本机诊断。
func remoteBoundaryError(err error) error {
	if err == nil {
		return nil
	}
	var domain *core.Error
	var remote *httpapi.APIError
	if errors.As(err, &domain) || errors.As(err, &remote) {
		return err
	}
	code, status := "execution_failed", 502
	switch {
	case errors.Is(err, status_error.KnowledgeUnsupported):
		code, status = "unsupported", 501
	case errors.Is(err, status_error.KnowledgeInvalid):
		code, status = "invalid", 400
	case errors.Is(err, status_error.KnowledgeNotFound):
		code, status = "not_found", 404
	case errors.Is(err, status_error.KnowledgeForbidden):
		code, status = "forbidden", 403
	case errors.Is(err, status_error.KnowledgeConflict):
		code, status = "version_conflict", 409
	case errors.Is(err, status_error.KnowledgeCancelled):
		code, status = "cancelled", 409
	case errors.Is(err, status_error.KnowledgeCandidateInvalid):
		code, status = "candidate_invalid", 422
	case errors.Is(err, status_error.DatabaseBusy):
		code, status = "database_busy", 503
	}
	return &httpapi.APIError{Code: code, Status: status}
}
