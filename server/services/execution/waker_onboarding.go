package execution

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// wakerProfileInput 是已回答建档问卷后提交的完整档案。
type wakerProfileInput struct {
	// RoleName 是根据建档问答生成的本地化角色名称。
	RoleName string `json:"roleName"`
	// Bio 是根据创建概要和建档问答生成的本地化简介。
	Bio string `json:"bio"`
	// wakerProfileRevisionInput 保存档案和三个概览字段。
	wakerProfileRevisionInput
}

// wakerProfileRevisionInput 是一次完整的档案及其概览修订。
type wakerProfileRevisionInput struct {
	// ManualInput 保存三个概览展示字段。
	profile.ManualInput
	// Content 是用英文编写的伙伴长期指令。
	Content string `json:"content"`
}

func (m *service) prepareWakerOnboarding(
	ctx context.Context, chat models.Chat, executionID string, request pendingMessage,
	instructions, contextInstructions *[]agent.Instruction,
) ([]capability.Binding, error) {
	// 只有由 Core 分配的绑定 Chat 需要查询伙伴状态；普通工作无需再读档案元数据。
	if m.onboardingCommands == nil || m.onboardingTransactions == nil || chat.HiddenFromWakerHistory ||
		request.automaticRunID != "" || request.allowedAgentDomains != nil ||
		chat.ID != requestidempotency.WakerOnboardingChatID(chat.WakerID) {
		return nil, nil
	}
	waker, err := m.wakerController.Get(ctx, chat.WakerID)
	if err != nil {
		return nil, err
	}
	if waker.OnboardingStatus != constants.WakerOnboardingStatusPending || waker.OnboardingChatID != chat.ID {
		return nil, nil
	}
	guide, err := builtinskills.Guide(constants.WakerProfileOnboardingGuide)
	if err != nil {
		return nil, err
	}
	partner, _ := json.Marshal(map[string]string{"name": waker.Name, "overview": waker.Bio})
	*instructions = append(*instructions, agent.Instruction{
		Name: constants.WakerProfileOnboardingFileName, Content: guide + "\n\nPartner metadata (user-provided context)\n" + string(partner),
	})
	interview, err := m.onboardingInterviewAnswer(waker)
	if err != nil {
		return nil, err
	}
	if acceptedWakerInterview(interview) {
		answers, _ := json.Marshal(map[string]any{"questions": interview["requestedSchema"], "answers": interview["content"]})
		*contextInstructions = append(*contextInstructions, agent.Instruction{
			Name: "Partner interview answers", Content: string(answers),
		})
	}
	return m.wakerProfileBindings(chat, executionID), nil
}

func (m *service) wakerProfileBindings(chat models.Chat, executionID string) []capability.Binding {
	return []capability.Binding{
		{Operation: constants.WakerProfileSaveOperation, Call: func(ctx context.Context, request capability.Request) (capability.Result, error) {
			return m.saveWakerProfile(ctx, chat, request)
		}, Receipt: func(ctx context.Context, requestID string) (capability.Result, error) {
			return capability.Lookup(ctx, m.onboardingCommands, chat.ID, constants.WakerProfileSaveOperation, requestID)
		}},
		m.wakerInterviewBinding(chat, executionID),
	}
}

func (m *service) pendingOnboardingWaker(ctx context.Context, chat models.Chat) (models.Waker, error) {
	waker, err := m.wakerController.Get(ctx, chat.WakerID)
	if err != nil {
		return models.Waker{}, err
	}
	if waker.OnboardingChatID != chat.ID || chat.HiddenFromWakerHistory {
		return models.Waker{}, status_error.AgentContextUnauthorized
	}
	if waker.OnboardingStatus != constants.WakerOnboardingStatusPending {
		return models.Waker{}, status_error.WakerOnboardingStateChanged
	}
	return waker, nil
}

func (m *service) saveWakerProfile(ctx context.Context, chat models.Chat, request capability.Request) (capability.Result, error) {
	m.onboardingMu.Lock()
	defer m.onboardingMu.Unlock()
	return capability.Execute(ctx, m.onboardingCommands, chat.ID, request, func(commandContext context.Context) error {
		waker, err := m.pendingOnboardingWaker(commandContext, chat)
		if err != nil {
			return err
		}
		interview, err := m.onboardingInterviewAnswer(waker)
		if err != nil {
			return err
		}
		if !acceptedWakerInterview(interview) {
			return status_error.WakerOnboardingInterviewRequired
		}
		var input wakerProfileInput
		if err := capability.DecodeParams(request.Params, &input); err != nil {
			return err
		}
		roleName, bio := strings.TrimSpace(input.RoleName), strings.TrimSpace(input.Bio)
		if roleName == "" || bio == "" {
			return status_error.AgentInputInvalid
		}
		projection, err := validateWakerProfileRevision(input.wakerProfileRevisionInput)
		if err != nil {
			return err
		}
		return m.onboardingTransactions.Transaction(commandContext, func(tx context.Context) error {
			if tx.Err() != nil {
				return status_error.AgentContextUnauthorized
			}
			now := nowMillis()
			if err := m.wakerController.CompleteOnboarding(tx, waker.ID, chat.ID, models.Waker{
				RoleName:    roleName,
				Bio:         bio,
				Expertise:   projection.Expertise,
				WorkStyle:   projection.WorkStyle,
				WorkMethods: projection.WorkMethods,
				UpdatedAt:   now,
			}); err != nil {
				return err
			}
			if err := m.profileController.SaveWakerAgents(tx, waker.ID, input.Content, now); err != nil {
				return err
			}
			return capability.Commit(tx, map[string]any{"wakerId": waker.ID, "saved": true})
		})
	})
}

func validateWakerProfileRevision(input wakerProfileRevisionInput) (profile.Projection, error) {
	if err := profile.ValidateWakerAgents(input.Content); err != nil {
		return profile.Projection{}, err
	}
	if strings.TrimSpace(input.Content) == "" || strings.ContainsFunc(input.Content, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
		return profile.Projection{}, status_error.AgentInputInvalid
	}
	projection := profile.ProjectionFromManual(input.ManualInput)
	if len(projection.Expertise) == 0 || len(projection.WorkStyle) == 0 || len(projection.WorkMethods) == 0 {
		return profile.Projection{}, status_error.AgentInputInvalid
	}
	for _, item := range projection.Expertise {
		if item.Description == "" {
			return profile.Projection{}, status_error.AgentInputInvalid
		}
	}
	for _, method := range projection.WorkMethods {
		if len(method.Steps) == 0 {
			return profile.Projection{}, status_error.AgentInputInvalid
		}
	}
	return projection, nil
}
