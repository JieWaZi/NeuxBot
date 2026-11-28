package global

import (
	"context"
	"path/filepath"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"go.opentelemetry.io/otel/trace"
)

func mainConversationRouterScratchRoot(configuration *Configuration) string {
	return filepath.Join(configuration.DataDirectory, "main-conversation-router-scratch")
}

// GenerateMainConversationRoute 沿该主会话和执行伙伴共同拥有的私有文本会话生成路由结果。
func GenerateMainConversationRoute(ctx context.Context, conversationID, wakerID string, options execution.WorkOptions, system, input string, forceNew bool, beforeSubmit func(context.Context) error) (string, error) {
	return generateMainConversationRoute(ctx, conversationID, wakerID, options, system, input, true, forceNew, beforeSubmit)
}

// GenerateDetachedMainConversationRoute 为尚待同步核验的资源输入生成独立路由结果。
func GenerateDetachedMainConversationRoute(ctx context.Context, conversationID, wakerID string, options execution.WorkOptions, system, input string) (string, error) {
	return generateMainConversationRoute(ctx, conversationID, wakerID, options, system, input, false, false, nil)
}

func generateMainConversationRoute(ctx context.Context, conversationID, wakerID string, options execution.WorkOptions, system, input string, retain, forceNew bool, beforeSubmit func(context.Context) error) (_ string, resultErr error) {
	started := time.Now()
	var timing agent.TextTimings
	var configured time.Duration
	defer func() {
		if configured == 0 {
			configured = time.Since(started)
		}
		logx.Infof("main conversation router timing: trace_id=%s conversation_id=%s configuration=%s local_preparation=%s native_session_preparation=%s pre_submit=%s native_turn=%s settlement=%s submitted=%t total=%s error_code=%s",
			trace.SpanContextFromContext(ctx).TraceID(), conversationID, configured, timing.LocalPreparation, timing.SessionPreparation,
			timing.BeforeSubmit, timing.NativeTurn, timing.Settlement, timing.Submitted, time.Since(started), util.ErrorCode(resultErr, "-"))
	}()
	if Config == nil || ExecutionService == nil || RuntimeEnvironmentControllerManager == nil || AgentThreads == nil || AIAccountService == nil {
		return "", status_error.RuntimeUnavailable
	}
	if conversationID == "" || wakerID == "" {
		return "", status_error.BadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	selected, err := ExecutionService.ResolveConfiguration(ctx, execution.ExecutionConfiguration{
		Resolved: options.ConfigurationResolved, RuntimeID: options.RuntimeID,
		AccountID: options.AccountID, Model: options.Model, Reasoning: options.Reasoning, Fast: options.Fast,
		PermissionLevel: constants.PermissionLevelDefault,
	})
	if err != nil {
		return "", err
	}
	environment, err := RuntimeEnvironmentControllerManager.Get(ctx, selected.RuntimeID)
	if err != nil {
		return "", err
	}
	environment.BaseRuntimeID = reviewProvider(environment)
	model := selected.Model
	if selected.AccountID != constants.AiAccountSystemID {
		model = accountconfig.NativeModelID(environment.BaseRuntimeID, model)
	}
	connection := detachedConnection(models.LibraryExecutionDefaults{RuntimeID: selected.RuntimeID, AccountID: selected.AccountID, Model: model}, environment)
	configured = time.Since(started)
	request := agent.TextRequest{
		Connection: connection, ResolveAccount: AIAccountService.ResolveAccount,
		BeforeSubmit: beforeSubmit,
		Selection:    &agent.TurnSelection{Reasoning: selected.Reasoning, Fast: selected.Fast, ContextWindow: options.ContextWindow},
		Messages:     []agent.TextMessage{{Role: "system", Content: system}, {Role: "user", Content: input}},
		UsageSource:  "main_conversation_router", UsageWakerID: wakerID,
		ScratchRoot:    mainConversationRouterScratchRoot(Config),
		ObserveTimings: func(value agent.TextTimings) { timing = value },
	}
	if retain && agent.SupportsContinuedText(environment.BaseRuntimeID) {
		request.ConversationKey = conversationID + ":" + wakerID
		request.Messages[1].Content = mainconversation.RouterTurnInput(input)
		request.FreshInput = input
		request.ForceNew = forceNew || options.SessionPolicy == constants.RuntimeSessionPolicyNew
	} else {
		request.Ephemeral = true
	}
	result, err := AgentThreads.GenerateText(ctx, request)
	if err != nil {
		return "", err
	}
	return result.Text, nil
}
