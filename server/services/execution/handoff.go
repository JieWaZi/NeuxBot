package execution

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const handoffPrompt = "Summarize the current conversation for a new agent taking over this task. " +
	"Include the user's goal, confirmed requirements, important decisions, completed work, " +
	"relevant files or facts, and the next action. Keep the original conversation language. " +
	"Use no tools, do not change files, and output only the handoff summary. Keep it concise."

// handoffSink 仅在内存中接收交接正文，拒绝工具事件且不接入聊天日志或实时流。
type handoffSink struct {
	// content 聚合智能体返回的最终正文增量。
	content strings.Builder
	// rejected 表示输出过长或尝试了工具调用。
	rejected bool
}

// Accept 只接收最终回复的文本增量，其他过程事件不进入交接内容。
func (sink *handoffSink) Accept(_ context.Context, event runtimes.Event) error {
	switch event.Type {
	case runtimes.EventTypeToolUse:
		sink.rejected = true
		return status_error.RuntimeHandoffInvalid
	case runtimes.EventTypeTextDelta:
		var payload struct {
			// Text 是本次交付的最终正文增量。
			Text string `json:"text"`
		}
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		if sink.content.Len()+len(payload.Text) > constants.RuntimeHandoffMaximumBytes {
			sink.rejected = true
			return status_error.RuntimeHandoffInvalid
		}
		_, err := sink.content.WriteString(payload.Text)
		return err
	}
	return nil
}

// summarizeCurrentSession 使用切换前的智能体、账号和原生会话生成一次隐藏交接。
// 原会话不可用或总结失败时返回 false，由调用方改用最近完整对话。
func (m *service) summarizeCurrentSession(
	ctx context.Context,
	state messageChat,
	request pendingMessage,
	files *preparedMessageFiles,
) (string, bool) {
	runtimeID := state.chat.LastRuntimeID
	session := state.runtimeSessions[runtimeID]
	if runtimeID == "" || session.SessionID == "" ||
		session.LastExecutionID != lastAssistantExecutionID(state.previousMessages) {
		return "", false
	}
	environment, err := m.runtimeEnvironments.Get(ctx, runtimeID)
	if err != nil || !environment.Enabled || environment.Detection.State != constants.RuntimeDetectionStateReady {
		return "", false
	}
	model := request.runtime.SummaryModel
	if model == "" {
		model = state.chat.LastModel
	}
	if model == "" {
		model = environment.DefaultModel
	}
	reasoning := request.runtime.SummaryReasoning
	if reasoning == "" {
		reasoning = state.chat.LastReasoning
	}
	if !m.summarySelectionAllowed(ctx, state.chat.LastAccountID, environment, model, reasoning) {
		return "", false
	}
	run := runtimes.RunRequest{
		Target: runtimes.RuntimeTarget{
			RuntimeID:  runtimeID,
			Binary:     environment.Binary,
			PrefixArgs: append([]string(nil), environment.PrefixArgs...),
			ConfigHome: environment.ConfigHome,
		},
		Detection: environment.Detection,
		Workspace: runtimes.Workspace{
			Path:               files.executionWorkspaceDirectory,
			Managed:            true,
			ProjectDirectories: runtimeProjectDirectories(state.chat, state.projectSources),
		},
		Message: runtimes.Message{Text: handoffPrompt},
		Session: runtimes.SessionDirective{Mode: runtimes.SessionModeResume, ID: session.SessionID},
		Policy: runtimes.RunPolicy{
			Model:     model,
			Reasoning: reasoning,
			Execution: runtimes.ExecutionPolicy{
				PermissionLevel:   runtimes.PermissionLevelDefault,
				WorkspaceReadOnly: true,
			},
		},
	}
	if accountID := state.chat.LastAccountID; accountID != "" {
		if m.accounts == nil {
			return "", false
		}
		target, release, err := m.accounts.Prepare(
			ctx,
			accountID,
			environment.BaseRuntimeID,
			files.executionWorkspaceDirectory,
			model,
		)
		if err != nil {
			return "", false
		}
		defer release()
		run.Target.AccountID = target.AccountID
		run.Target.RefreshCredentials = target.RefreshCredentials
		run.Target.Environment = target.Environment
		run.Target.Isolated = target.Isolated
		run.Target.CloseAfterRun = target.CloseAfterRun
		run.Target.ConfigHome = ""
	}
	sink := &handoffSink{}
	result := m.runtime.Run(ctx, run, sink)
	if result.Failure != nil || sink.rejected {
		return "", false
	}
	summary := strings.TrimSpace(sink.content.String())
	return summary, summary != ""
}

// summarySelectionAllowed 校验总结模型属于切换前账号或智能体的可选范围。
func (m *service) summarySelectionAllowed(
	ctx context.Context,
	accountID string,
	environment models.RuntimeEnvironment,
	model string,
	reasoning string,
) bool {
	if model == "" {
		return false
	}
	if accountID != "" {
		if m.accounts == nil {
			return false
		}
		account, err := m.accounts.Get(ctx, accountID)
		if err != nil {
			return false
		}
		if account.AuthMode == constants.AccountAuthModeAPIKey {
			allowed := false
			for _, selected := range account.SelectedModels {
				if selected == model {
					allowed = true
					break
				}
			}
			if !allowed {
				return false
			}
			return summaryReasoningAllowed(environment, model, reasoning)
		}
	}
	return validateExecutionConfiguration(environment, ExecutionConfiguration{
		Model:           model,
		Reasoning:       reasoning,
		PermissionLevel: runtimes.PermissionLevelDefault,
	}) == nil
}

// summaryReasoningAllowed 对账号自选模型使用智能体通用推理选项校验。
func summaryReasoningAllowed(environment models.RuntimeEnvironment, model, reasoning string) bool {
	if reasoning == "" {
		return true
	}
	options := environment.Detection.ReasoningOptions
	for _, item := range runtimes.AvailableModels(environment.Detection, environment.CustomModels) {
		if item.ID == model && len(item.ReasoningOptions) > 0 {
			options = item.ReasoningOptions
			break
		}
	}
	for _, option := range options {
		if option.ID == reasoning {
			return true
		}
	}
	return false
}

// handoffRunPrompt 将隐藏交接正文作为新会话背景，与用户原始输入分开。
func handoffRunPrompt(summary, message string) string {
	return "Task handoff from the previous session (background, not a new user request):\n" +
		summary + "\n\nCurrent user request:\n" + message
}
