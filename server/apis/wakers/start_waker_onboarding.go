package wakers

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 伙伴建档接纳结果
type WakerOnboardingResponse struct {
	// 伙伴标识
	WakerID string `json:"wakerId"`
	// 已接纳对话
	ChatID string `json:"chatId,omitempty"`
	// 启动错误
	StartError string `json:"startError,omitempty"`
}

// 开始伙伴建档
type StartWakerOnboarding struct {
	// MethodPost 声明建档接纳方法。
	ginx.MethodPost
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required,max=32"`
}

// Path 返回开始伙伴建档的接口路径。
func (o *StartWakerOnboarding) Path() string { return "/:wakerId/onboarding" }

// Output 恢复既有建档对话，或重试同一个首发入口。
func (o *StartWakerOnboarding) Output(ctx *gin.Context) (interface{}, error) {
	waker, err := global.WakerControllerManager.Get(ctx, o.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if waker.OnboardingChatID == "" {
		return nil, ginx.WithStack(status_error.WakerOnboardingUnavailable)
	}
	return startWakerOnboarding(ctx, waker, true), nil
}

func startWakerOnboarding(ctx *gin.Context, waker models.Waker, retry bool) WakerOnboardingResponse {
	response := WakerOnboardingResponse{WakerID: waker.ID}
	failure := func(err error) WakerOnboardingResponse {
		response.StartError = middleware.DescribeApplicationError(ctx, err).Message
		return response
	}
	var retryAfter string
	if global.ChatService != nil {
		chat, err := global.ChatService.LoadExecutionChat(ctx, waker.OnboardingChatID)
		switch {
		case err == nil && chat.WakerID == waker.ID:
			response.ChatID = chat.ID
			if !retry || waker.OnboardingStatus != constants.WakerOnboardingStatusPending {
				return response
			}
			retryAfter, err = failedWakerOnboardingKickoff(ctx, chat)
			if err != nil {
				return failure(err)
			}
			if retryAfter == "" {
				return response
			}
		case err == nil:
			return failure(status_error.AgentContextUnauthorized)
		case !util.HasStatusCode(err, status_error.ChatNotFound):
			return failure(err)
		}
	}
	if waker.OnboardingStatus != constants.WakerOnboardingStatusPending {
		return failure(status_error.WakerOnboardingUnavailable)
	}
	if global.ExecutionService == nil || global.RuntimeEnvironmentControllerManager == nil {
		return failure(status_error.RuntimeUnavailable)
	}
	environment, err := onboardingEnvironment(ctx, waker.DefaultModel)
	if err != nil {
		return failure(err)
	}
	permission := environment.DefaultPermissionLevel
	if !constants.SelectablePermissionLevel(permission) {
		permission = constants.PermissionLevelDefault
	}
	options := execution.WorkOptions{RuntimeID: environment.RuntimeID, PermissionLevel: permission}
	if binding := waker.DefaultModel; binding != nil {
		options.AccountID, options.Model, options.Reasoning = binding.AccountID, binding.Model, binding.Reasoning
		options.ConfigurationResolved = true
	}
	language := "Chinese (Simplified)"
	if strings.HasPrefix(strings.ToLower(ginx.GetLang(ctx)), "en") {
		language = "English"
	}
	intent := execution.WorkIntent{
		ClientRequestID: waker.ID,
		Chat:            execution.ChatTarget{Create: &execution.CreateChatSpec{WakerID: waker.ID}},
		Input:           execution.WorkInput{Actor: constants.WakerOnboardingActor, Content: "Begin my partner setup with an interactive choice card. Start in " + language + ", unless my overview clearly indicates another language."},
		Options:         options,
		Admission:       constants.ExecutionAdmissionCommitBeforeDispatch,
	}
	if retryAfter != "" {
		intent.ClientRequestID = ""
		intent.Chat = execution.ChatTarget{ChatID: response.ChatID}
		intent.Input.SubmissionID = "waker-onboarding:" + retryAfter
		intent.Options.ParentExecutionID = retryAfter
		intent.Options.SessionPolicy = constants.RuntimeSessionPolicyNew
	}
	accepted, err := global.ExecutionService.Start(ctx, intent)
	if err != nil {
		// 原对话已被并发重试或用户续聊接管。
		if retryAfter != "" && (util.HasStatusCode(err, status_error.ChatHasActiveExecution) || util.HasStatusCode(err, status_error.WakerProfileDraftStale)) {
			return response
		}
		return failure(err)
	}
	response.ChatID = accepted.Chat.ID
	return response
}

func failedWakerOnboardingKickoff(ctx *gin.Context, chat models.Chat) (string, error) {
	if chat.ActiveExecutionID != "" || chat.LastEndedExecutionID == "" ||
		(chat.LastEndedExecutionStatus != constants.ExecutionStatusFailed && chat.LastEndedExecutionStatus != constants.ExecutionStatusCanceled) {
		return "", nil
	}
	messages, err := global.ChatService.ReadExecutionMessages(ctx, chat.ID, chat.LastEndedExecutionID)
	if err != nil {
		return "", err
	}
	var internal bool
	for _, message := range messages {
		internal = internal || message.Actor == constants.WakerOnboardingActor
		if message.Role != constants.MessageRoleAssistant {
			continue
		}
		if message.ErrorCode == status_error.WakerOnboardingInterviewRequired.Key() {
			continue
		}
		if strings.TrimSpace(message.Content) != "" {
			return "", nil
		}
		for _, part := range message.DisplayParts {
			if part.Type == constants.MessageDisplayPartText && strings.TrimSpace(part.Text) != "" {
				return "", nil
			}
		}
	}
	if internal {
		return chat.LastEndedExecutionID, nil
	}
	return "", nil
}

func onboardingEnvironment(ctx *gin.Context, binding *models.WakerDefaultModel) (models.RuntimeEnvironment, error) {
	if binding != nil {
		return global.RuntimeEnvironmentControllerManager.Get(ctx, binding.RuntimeID)
	}
	environments, err := global.RuntimeEnvironmentControllerManager.List(ctx)
	if err != nil {
		return models.RuntimeEnvironment{}, err
	}
	for _, environment := range environments {
		if environment.IsDefault {
			return environment, nil
		}
	}
	return models.RuntimeEnvironment{}, status_error.RuntimeUnavailable
}
