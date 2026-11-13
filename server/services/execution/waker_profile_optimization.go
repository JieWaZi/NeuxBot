package execution

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// wakerProfileEdit 保存一次基于已读取档案的修订，省略的字段沿用当前值。
type wakerProfileEdit struct {
	// Revision 是读取档案时返回的正文与概览版本。
	Revision string `json:"revision"`
	// ManualInput 只包含本次需要替换的概览字段，空列表表示清除。
	profile.ManualInput
	// Content 是修订后的完整正文，省略时保留当前档案。
	Content *string `json:"content"`
}

// wakerProfileEditSnapshot 是优化命令可以读取的当前伙伴档案。
type wakerProfileEditSnapshot struct {
	// Waker 保存当前伙伴资料及概览。
	Waker models.Waker
	// Content 是执行使用的完整档案正文。
	Content string
	// Revision 标识本次可修改的档案内容和概览。
	Revision string
}

// wakerProfileEditBindings 将当前伙伴的档案操作交给现有聊天能力授权。
func (m *service) wakerProfileEditBindings(chat models.Chat, executionID string, request pendingMessage) []capability.Binding {
	// 本地聊天由 Runtime 接纳后提交；IM 和其他持久调度入口先提交再派发。
	if chat.HiddenFromWakerHistory || request.admission != constants.ExecutionAdmissionCommitOnRuntimeAccept || request.automaticRunID != "" || request.message.Actor != "user" ||
		request.allowedAgentDomains != nil || m.onboardingCommands == nil || m.onboardingTransactions == nil {
		return nil
	}
	return []capability.Binding{
		{Operation: constants.WakerProfileGetOperation, Call: func(ctx context.Context, _ capability.Request) (capability.Result, error) {
			var snapshot wakerProfileEditSnapshot
			if err := m.onboardingTransactions.Transaction(ctx, func(tx context.Context) error {
				var err error
				snapshot, err = m.readWakerProfileForEdit(tx, chat.WakerID)
				return err
			}); err != nil {
				return capability.Result{}, err
			}
			waker := snapshot.Waker
			return capability.Result{Data: map[string]any{
				"revision": snapshot.Revision, "name": waker.Name, "roleName": waker.RoleName, "bio": waker.Bio,
				"expertise": append([]profile.Expertise{}, waker.Expertise...), "workStyle": append([]string{}, waker.WorkStyle...),
				"workMethods": append([]profile.WorkMethod{}, waker.WorkMethods...), "content": snapshot.Content,
			}}, nil
		}},
		{Operation: constants.WakerProfileOptimizeOperation, Call: func(ctx context.Context, request capability.Request) (capability.Result, error) {
			return m.saveWakerProfileEdit(ctx, chat.WakerID, executionID, request)
		}, Receipt: func(ctx context.Context, requestID string) (capability.Result, error) {
			return capability.Lookup(ctx, m.onboardingCommands, executionID, constants.WakerProfileOptimizeOperation, requestID)
		}},
	}
}

func (m *service) readWakerProfileForEdit(ctx context.Context, wakerID string) (wakerProfileEditSnapshot, error) {
	waker, err := m.wakerController.Get(ctx, wakerID)
	if err != nil {
		return wakerProfileEditSnapshot{}, err
	}
	if waker.OnboardingStatus == constants.WakerOnboardingStatusPending {
		return wakerProfileEditSnapshot{}, status_error.WakerOnboardingIncomplete
	}
	documents, err := m.profileController.ListWakerDocuments(ctx, waker.ID)
	if err != nil {
		return wakerProfileEditSnapshot{}, err
	}
	content := builtinrolepreset.ResolveWakerAgents(waker.ID, documents, waker.UpdatedAt).Content
	// 版本只覆盖命令写入的字段，头像和模型变化不影响档案修订。
	revision := profile.ContentRevision(waker.ID + ":" + profile.ContentRevision(content) + waker.ProfileOverview().Revision())
	return wakerProfileEditSnapshot{Waker: waker, Content: content, Revision: revision}, nil
}

func (m *service) saveWakerProfileEdit(ctx context.Context, wakerID, executionID string, request capability.Request) (capability.Result, error) {
	return capability.Execute(ctx, m.onboardingCommands, executionID, request, func(commandContext context.Context) error {
		var input wakerProfileEdit
		if err := capability.DecodeParams(request.Params, &input); err != nil {
			return err
		}
		changesOverview := input.Expertise != nil || input.WorkStyle != nil || input.WorkMethods != nil
		if input.Revision == "" || (!changesOverview && input.Content == nil) {
			return status_error.AgentInputInvalid
		}
		if input.Content != nil {
			if err := profile.ValidateWakerAgents(*input.Content); err != nil {
				return err
			}
		}
		return m.onboardingTransactions.Transaction(commandContext, func(tx context.Context) error {
			current, err := m.readWakerProfileForEdit(tx, wakerID)
			if err != nil {
				return err
			}
			if current.Revision != input.Revision {
				return status_error.WakerProfileChanged
			}
			now := nowMillis()
			if changesOverview {
				projection := profile.ProjectionFromManual(input.ManualInput)
				if input.Expertise == nil {
					projection.Expertise = current.Waker.Expertise
				}
				if input.WorkStyle == nil {
					projection.WorkStyle = current.Waker.WorkStyle
				}
				if input.WorkMethods == nil {
					projection.WorkMethods = current.Waker.WorkMethods
				}
				if err := m.wakerController.UpdateProfileProjection(tx, wakerID, projection, now); err != nil {
					return err
				}
			}
			if input.Content != nil {
				if err := m.profileController.SaveWakerAgents(tx, wakerID, *input.Content, now); err != nil {
					return err
				}
			}
			return capability.Commit(tx, map[string]any{"wakerId": wakerID, "saved": true})
		})
	})
}
