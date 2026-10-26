package knowledge

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/remote"
)

// MaintenanceInvocation 是知识任务交给独立 ACP 的冻结输入，不含伙伴或聊天身份。
type MaintenanceInvocation struct {
	JobID        string                      // 已接纳知识任务的身份。
	Bindings     []agentcapability.Binding   // 只限目标知识库的实际读写能力。
	Instructions string                      // 同源 Wiki Fixer 工作流程。
	Message      string                      // 用户本次维护要求。
	Validate     func(context.Context) error // 每次工具调用核对任务和授权。
}

// runWikiMaintenance 直接驱动知识任务的 ACP 会话，复用上游 Fixer 与同库工具。
func (s *Service) runWikiMaintenance(ctx context.Context, job *core.Job, session models.KnowledgeTaskConfiguration) error {
	current, ready, err := s.config.Engine.ActivateQueuedJob(ctx, job.ID)
	if err != nil {
		return bridge.MapError(err)
	}
	if !ready {
		return nil
	}
	if current.CancelRequested {
		return status_error.KnowledgeCancelled
	}
	if s.config.RunMaintenance == nil {
		return status_error.KnowledgeUnsupported
	}
	job = current
	s.emitTaskChanged(job.ID, job.LibraryID)
	validate := func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return status_error.KnowledgeCancelled
		}
		active, err := s.config.Engine.GetJob(ctx, job.ID)
		if err != nil {
			return bridge.MapError(err)
		}
		if active.Attempt != job.Attempt || active.State != "processing" || active.CancelRequested || !core.IsMaintenanceJob(active) {
			return status_error.KnowledgeCancelled
		}
		lib, err := s.config.Engine.GetLibrary(ctx, job.LibraryID)
		if err != nil {
			return bridge.MapError(err)
		}
		if lib.PolicyVersion != job.Permission.PolicyVersion {
			return status_error.KnowledgeForbidden
		}
		return s.validateLibrary(lib)
	}
	ref := bridge.LocalRef(job.LibraryID)
	provider := bridge.LocalReadProvider{Engine: s.config.Engine, FreezeEmbedder: s.config.FreezeEmbedder}
	resources := func(ctx context.Context, _ string) ([]bridge.ResourceRef, error) {
		if err := validate(ctx); err != nil {
			return nil, err
		}
		return []bridge.ResourceRef{ref}, nil
	}
	bindings := bridge.ProviderReadBindings(provider, bridge.FreezeScope("", []bridge.ResourceRef{ref}), resources)
	identity := func(requestID string) string {
		return (requestidempotency.Identity{ID: requestID}).ResourceID("wiki-maintenance", job.ID, fmt.Sprint(job.Attempt))
	}
	activity := func(ctx context.Context) context.Context {
		return core.WithMaintenanceJob(core.WithActivityActor(ctx, job.ActivityActor), job.ID, job.Attempt)
	}
	writes := s.wikiWriteBindings(wikiAccess{
		authorize: func(ctx context.Context, _ string, target bridge.ResourceRef) (*core.Library, *remote.Custom, error) {
			if target != ref {
				return nil, nil, status_error.KnowledgeForbidden
			}
			if err := validate(ctx); err != nil {
				return nil, nil, err
			}
			lib, err := s.config.Engine.GetLibrary(ctx, job.LibraryID)
			return lib, nil, bridge.MapError(err)
		},
		identity: identity, actor: job.ActivityActor.UserID, activity: activity,
	})
	for _, binding := range writes {
		if binding.Operation != "knowledge.sources.import" && binding.Operation != "knowledge.jobs.retry" {
			bindings = append(bindings, binding)
		}
	}
	bindings = append(bindings, s.autoFixBinding(ref, job.Permission.PolicyVersion, validate, identity, activity))
	guide, err := builtinskills.Guide("knowledge.maintenance")
	if err != nil {
		return err
	}
	scope, _ := json.Marshal(ref)
	message := session.Message
	if message == "" {
		message = "Maintain the existing Wiki in the frozen library. Verify changes and report remaining issues."
	}
	text, err := s.config.RunMaintenance(ctx, models.LibraryExecutionDefaults{RuntimeID: session.RuntimeID, AccountID: session.AccountID, Model: session.Model, Reasoning: session.Reasoning}, MaintenanceInvocation{JobID: job.ID, Bindings: bindings, Instructions: guide + "\n\nFrozen library (data):\n" + string(scope), Message: message, Validate: validate})
	if err != nil {
		return err
	}
	if err := validate(ctx); err != nil {
		return err
	}
	_, err = s.config.Engine.CompleteMaintenanceJob(ctx, core.JobActionInput{RequestID: fmt.Sprintf("wiki-maintenance/%s/%d/completed", job.ID, job.Attempt), JobID: job.ID, ExpectedVersion: job.Version}, text)
	if err == nil {
		_ = s.config.References.SetHostDiagnostic(ctx, job.RequestID, "idle", "", "")
	}
	return bridge.MapError(err)
}
