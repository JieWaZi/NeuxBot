package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// wakerProfileProposal 是 CLI 冻结为待确认版本的完整档案。
type wakerProfileProposal struct {
	// ManualInput 保存三个概览展示字段。
	profile.ManualInput
	// Content 是用户需要看到的完整 Markdown。
	Content string `json:"content"`
	// Language 选择问答卡片的说明语言。
	Language string `json:"language"`
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
	if draft := waker.OnboardingDraft; draft != nil && draft.ExecutionID != executionID {
		answer, readErr := m.elicitations.questionSnapshot(chat.ID, draft.ExecutionID, draft.ID)
		switch {
		case readErr == nil && wakerProfileConfirmed(answer):
			*contextInstructions = append(*contextInstructions, agent.Instruction{
				Name:    "Recovered partner confirmation",
				Content: "The user confirmed this exact draft in this chat. If the user has not requested further changes, save draft ID " + draft.ID + " using waker profile save.",
			})
		case readErr != nil && !util.HasStatusCode(readErr, status_error.AgentReceiptNotFound) && !util.HasStatusCode(readErr, status_error.ExecutionNotFound) && !util.HasStatusCode(readErr, status_error.ChatNotFound):
			return nil, readErr
		}
	}
	return m.wakerProfileBindings(chat, executionID), nil
}

func (m *service) wakerProfileBindings(chat models.Chat, executionID string) []capability.Binding {
	proposalID := func(requestID string) string {
		return (requestidempotency.Identity{ID: requestID}).ResourceID("waker-profile-draft", chat.ID, executionID)
	}
	return []capability.Binding{
		{Operation: constants.WakerProfileProposeOperation, Call: func(ctx context.Context, request capability.Request) (capability.Result, error) {
			return m.proposeWakerProfile(ctx, chat, executionID, proposalID(request.RequestID), request)
		}, Receipt: func(_ context.Context, requestID string) (capability.Result, error) {
			draftID := proposalID(requestID)
			answer, err := m.elicitations.questionSnapshot(chat.ID, executionID, draftID)
			return wakerProfileProposalResult(draftID, answer, true), err
		}},
		{Operation: constants.WakerProfileSaveOperation, Call: func(ctx context.Context, request capability.Request) (capability.Result, error) {
			return m.saveWakerProfile(ctx, chat, request)
		}, Receipt: func(ctx context.Context, requestID string) (capability.Result, error) {
			return capability.Lookup(ctx, m.onboardingCommands, chat.ID, constants.WakerProfileSaveOperation, requestID)
		}},
		m.wakerInterviewBinding(chat, executionID),
	}
}

func (m *service) proposeWakerProfile(ctx context.Context, chat models.Chat, executionID, draftID string, request capability.Request) (capability.Result, error) {
	var input wakerProfileProposal
	if err := capability.DecodeParams(request.Params, &input); err != nil {
		return capability.Result{}, err
	}
	if err := profile.ValidateContent(input.Content); err != nil {
		return capability.Result{}, err
	}
	if strings.TrimSpace(input.Content) == "" {
		return capability.Result{}, status_error.AgentInputInvalid
	}
	projection := profile.ProjectionFromManual(input.ManualInput)
	question := wakerProfileQuestion(request.RequestID, input.Language, projection, input.Content)
	encodedPreview, _ := json.Marshal(question.Params["message"])
	if len(encodedPreview) > constants.WakerProfilePreviewMaximumBytes {
		return capability.Result{}, status_error.ProfileDocumentTooLarge
	}
	identity, err := requestidempotency.NewSubmission(request.RequestID, []any{projection, input.Content, input.Language})
	if err != nil {
		return capability.Result{}, err
	}
	question.InputDigests = map[string]string{"profile": identity.Hash}
	m.onboardingMu.Lock()
	defer m.onboardingMu.Unlock()
	waker, err := m.pendingOnboardingWaker(ctx, chat)
	if err != nil {
		return capability.Result{}, err
	}
	interview, err := m.onboardingInterviewAnswer(waker)
	if err != nil {
		return capability.Result{}, err
	}
	if !acceptedWakerInterview(interview) {
		return capability.Result{}, status_error.WakerOnboardingInterviewRequired
	}
	// 重放原问题不改变伙伴当前待确认的草稿版本。
	existing, err := m.elicitations.questionSnapshot(chat.ID, executionID, draftID)
	if err != nil && !util.HasStatusCode(err, status_error.AgentReceiptNotFound) {
		return capability.Result{}, err
	}
	if existing == nil {
		current := waker.OnboardingDraft
		if current != nil && current.ID == draftID && current.RequestHash != identity.Hash {
			return capability.Result{}, status_error.AgentCommandRequestConflict
		}
		if current != nil && current.ID != draftID && current.ExecutionID == executionID {
			answer, readErr := m.elicitations.questionSnapshot(chat.ID, current.ExecutionID, current.ID)
			if readErr != nil && !util.HasStatusCode(readErr, status_error.AgentReceiptNotFound) {
				return capability.Result{}, readErr
			}
			if answer["status"] == "pending" {
				return capability.Result{}, status_error.WakerProfileConfirmationPending
			}
		}
		draft := models.WakerOnboardingDraft{
			ID: draftID, ExecutionID: executionID, RequestHash: identity.Hash,
			Projection: projection, Content: input.Content,
		}
		if err := m.wakerController.SetOnboardingDraft(ctx, waker.ID, chat.ID, draft); err != nil {
			return capability.Result{}, err
		}
	}
	answer, replayed, err := m.elicitations.createQuestionWithLimit(ctx, chat.ID, executionID, draftID, question, constants.WakerProfilePreviewMaximumBytes, validateQuestionForm)
	return wakerProfileProposalResult(draftID, answer, replayed), err
}

func wakerProfileQuestion(requestID, language string, projection profile.Projection, content string) capability.Request {
	title, expertise, style, methods, save, revise := "Save this partner profile?", "Responsibilities", "Work style", "Working methods", "Save profile", "Revise first"
	if language == "zh" {
		title, expertise, style, methods, save, revise = "保存这份伙伴档案？", "核心职能", "工作风格", "工作方式", "保存档案", "先修改"
	}
	var preview strings.Builder
	fmt.Fprintf(&preview, "%s\n\n%s\n", title, expertise)
	for _, item := range projection.Expertise {
		fmt.Fprintf(&preview, "- %s", item.Title)
		if item.Description != "" {
			fmt.Fprintf(&preview, " — %s", item.Description)
		}
		preview.WriteByte('\n')
	}
	fmt.Fprintf(&preview, "\n%s\n", style)
	for _, item := range projection.WorkStyle {
		fmt.Fprintf(&preview, "- %s\n", item)
	}
	fmt.Fprintf(&preview, "\n%s\n", methods)
	for _, method := range projection.WorkMethods {
		fmt.Fprintf(&preview, "- %s\n", method.Name)
		for index, step := range method.Steps {
			fmt.Fprintf(&preview, "  %d. %s\n", index+1, step)
		}
	}
	fmt.Fprintf(&preview, "\n%s\n\n%s\n\n---", constants.ProfileDocumentAgentsFileName, content)
	return capability.Request{
		RequestID: requestID,
		Params: map[string]any{
			"message": preview.String(), "allowSkip": true,
			"requestedSchema": map[string]any{
				"type": "object", "required": []string{"decision"}, "additionalProperties": false,
				"properties": map[string]any{"decision": map[string]any{
					"type": "string", "title": title,
					"oneOf": []map[string]any{{"const": constants.WakerProfileSaveDecision, "title": save}, {"const": "revise", "title": revise}},
				}},
			},
		},
	}
}

func wakerProfileProposalResult(id string, answer map[string]any, replayed bool) capability.Result {
	return capability.Result{CommandID: id, Replayed: replayed, Data: map[string]any{"draftId": id, "questionId": id, "status": answer["status"]}}
}

func wakerProfileConfirmed(answer map[string]any) bool {
	decision, _ := answer["content"].(map[string]any)
	return answer["status"] == string(constants.ElicitationResultAccepted) && decision["decision"] == constants.WakerProfileSaveDecision
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
		return models.Waker{}, status_error.WakerProfileDraftStale
	}
	return waker, nil
}

func (m *service) saveWakerProfile(ctx context.Context, chat models.Chat, request capability.Request) (capability.Result, error) {
	draftID, _ := request.Params["draftId"].(string)
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
		draft := waker.OnboardingDraft
		if draft == nil || draft.ID != draftID {
			return status_error.WakerProfileDraftStale
		}
		answer, err := m.elicitations.questionSnapshot(chat.ID, draft.ExecutionID, draft.ID)
		if err != nil {
			if util.HasStatusCode(err, status_error.AgentReceiptNotFound) {
				return status_error.WakerProfileConfirmationRequired
			}
			return err
		}
		if !wakerProfileConfirmed(answer) {
			return status_error.WakerProfileConfirmationRequired
		}
		return m.onboardingTransactions.Transaction(commandContext, func(tx context.Context) error {
			if tx.Err() != nil {
				return status_error.AgentContextUnauthorized
			}
			now := nowMillis()
			if err := m.wakerController.CompleteOnboarding(tx, waker.ID, chat.ID, draft.Projection, now); err != nil {
				return err
			}
			if err := m.profileController.SaveWakerAgents(tx, waker.ID, draft.Content, now); err != nil {
				return err
			}
			return capability.Commit(tx, map[string]any{"wakerId": waker.ID, "draftId": draft.ID, "saved": true})
		})
	})
}
