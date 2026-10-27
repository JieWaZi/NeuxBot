package runtimes

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// TextMessage 是单次无状态生成的显式消息，不关联聊天或原生会话。
type TextMessage struct {
	// Role 是 system、user 或 assistant 消息角色。
	Role string
	// Content 是完整消息正文。
	Content string
}

// TextRequest 包含本次模型输入、账号启动事实与显式执行权限，不接受工具、工作区或业务身份。
type TextRequest struct {
	// Target 是已经准备账号租约的 Runtime 启动配置。
	Target RuntimeTarget
	// Detection 是有效 Runtime 探测快照。
	Detection DetectionResult
	// PermissionLevel 是本次调用明确授权的原生权限；空值沿用默认权限和只读工作区。
	PermissionLevel PermissionLevel
	// Model 是本次需要实际选择的原生模型标识。
	Model string
	// Messages 是本次完整消息，续写也必须由调用方明确提供。
	Messages []TextMessage
	// Reasoning 是显式推理强度，优先于 Thinking，并须由原生配置确认。
	Reasoning string
	// Thinking 是期望启用推理；关闭请求按实际模型能力选择。
	Thinking bool
	// MaxTokens 是期望的输出上限，ACP 不支持时在结果中明确记录。
	MaxTokens int
	// Temperature 是期望的采样温度，ACP 不支持时在结果中明确记录。
	Temperature float64
}

// TextResult 保存生成正文、原生终态及实际生效的能力诊断。
type TextResult struct {
	// Text 是完整模型正文；任何调用失败时为空。
	Text string
	// StopReason 是 Runtime 实际返回的停止原因，不根据正文猜测。
	StopReason string
	// Usage 是原生协议报告的累计回合用量，未知时为空。
	Usage *TurnUsageData
	// Failure 是原生调用的稳定失败。
	Failure *Failure
	// ActualReasoning 是配置回执确认的实际推理选项。
	ActualReasoning string
	// ThinkingDisabled 只在原生 none、off 或 disabled 选项确认生效时成立。
	ThinkingDisabled bool
	// ReasoningFallback 解释无法完全满足关闭推理时采用的真实降级。
	ReasoningFallback string
	// Limitations 记录协议无法准确表达的生成控制及原生配置边界。
	Limitations []string
}

// textInvocation 是一次模型调用的私有配置与结果收集端口。
type textInvocation struct {
	// thinking 是调用方期望的推理模式。
	thinking bool
	// result 接收会话真实配置诊断，仅由配置阶段写入。
	result *TextResult
	// collector 直接累计 ACP 正文与最终用量，不生成聊天事件。
	collector *textCollector
}

// GenerateText 复用 ACP 的账号、模型、进程和取消能力，独立创建并关闭一次模型会话。
func (sdk *SDK) GenerateText(ctx context.Context, input TextRequest) TextResult {
	var output TextResult
	if ctx == nil || len(input.Messages) == 0 || strings.TrimSpace(input.Target.RuntimeID) == "" {
		output.Failure = newFailure(FailureCodeInvalidRequest, "text_validate", "runtime and messages are required")
		return output
	}
	if err := ctx.Err(); err != nil {
		output.Failure = failedRunError("text_validate", err).Failure
		return output
	}

	for _, message := range input.Messages {
		if message.Role != "system" && message.Role != "user" && message.Role != "assistant" {
			output.Failure = newFailure(FailureCodeInvalidRequest, "text_validate", "unsupported message role")
			return output
		}
	}
	workspace, err := os.MkdirTemp("", "neuxbot-model-")
	if err != nil {
		output.Failure = newFailure(FailureCodeRuntimeUnavailable, "text_workspace", err.Error())
		return output
	}
	defer os.RemoveAll(workspace)
	permission := input.PermissionLevel
	if permission == "" {
		permission = PermissionLevelDefault
	}
	request := RunRequest{
		Target: input.Target, Detection: input.Detection,
		Workspace: Workspace{Path: workspace}, Session: SessionDirective{Mode: SessionModeNew},
		Policy: RunPolicy{Model: input.Model, Reasoning: strings.TrimSpace(input.Reasoning), Execution: ExecutionPolicy{PermissionLevel: permission, WorkspaceReadOnly: permission == PermissionLevelDefault}},
	}
	request.Target.CloseAfterRun = true
	prepared, err := sdk.prepareFromDetection(prepareRequestForRun(sdk, request), input.Detection)
	if err != nil || sdk.acp == nil {
		if err == nil {
			err = status_error.RuntimeUnavailable
		}
		output.Failure = failedRunError("text_prepare", err).Failure
		return output
	}
	prompt := compileTextMessages(input.Messages)
	output.Limitations = append(output.Limitations, "native_message_roles_unavailable: ACP serializes system/user/assistant roles into one user text input")
	if !input.Target.Isolated && prepared.definition.baseRuntimeID != RuntimeIDCodex {
		output.Limitations = append(output.Limitations, "system_account_native_config_retained: global instructions and configured MCP may load; host does not mount external resources")
	}
	if prepared.definition.baseRuntimeID != RuntimeIDClaude && prepared.definition.baseRuntimeID != RuntimeIDWorkBuddy {
		output.Limitations = append(output.Limitations, "native_session_persistence_not_disabled: session is fresh and closed, CLI may retain its own logs")
	}
	if input.MaxTokens != 0 {
		output.Limitations = append(output.Limitations, "max_tokens_not_configurable_via_acp")
	}
	if input.Temperature != 0 {
		output.Limitations = append(output.Limitations, "temperature_not_configurable_via_acp")
	}
	callContext, cancel := context.WithCancel(ctx)
	defer cancel()
	collector := &textCollector{}
	result := sdk.acp.Run(callContext, acpRunRequest{
		InvocationID: util.New(), Prepared: prepared, Request: request,
		Prompt: prompt, EphemeralSession: true,
		Text: &textInvocation{thinking: input.Thinking, result: &output, collector: collector},
	})
	output.StopReason, output.Failure = result.StopReason, result.Failure
	collector.mutex.Lock()
	defer collector.mutex.Unlock()
	output.Usage = collector.usage
	if output.Failure == nil {
		output.Text = collector.text.String()
	}
	return output
}

// compileTextMessages 保留显式消息顺序；ACP 仅支持文本块，不能声称拥有原生消息角色参数。
func compileTextMessages(messages []TextMessage) string {
	var builder strings.Builder
	for _, message := range messages {
		fmt.Fprintf(&builder, "<message role=%q>\n%s\n</message>\n", message.Role, message.Content)
	}
	return builder.String()
}

// textCollector 直接收集 ACP 正文与最终用量，不经过聊天事件映射或交付队列。
type textCollector struct {
	// mutex 保护 ACP 更新和最终用量之间的状态。
	mutex sync.Mutex
	// text 累积模型正文，不收集过程或思考内容。
	text strings.Builder
	// messageID 保留当前最终候选身份，新消息不会拼接旧工具前正文。
	messageID string
	// usage 保存 Prompt 终态的完整累计回合用量，未知时为空。
	usage *TurnUsageData
}

// collectUpdate 仅保留最终答复正文；思考、过程说明和中间用量没有知识生成结果语义。
func (collector *textCollector) collectUpdate(update acp.SessionUpdate, binding acpRuntimeBinding) {
	chunk := update.AgentMessageChunk
	if chunk == nil || binding.messagePhase(chunk.Meta) == constants.RuntimeACPMessagePhaseCommentary {
		return
	}
	text := acpText(chunk.Content)
	collector.mutex.Lock()
	defer collector.mutex.Unlock()
	if chunk.MessageId != nil && *chunk.MessageId != "" && *chunk.MessageId != collector.messageID {
		collector.messageID = *chunk.MessageId
		collector.text.Reset()
	}
	collector.text.WriteString(text)
}

// collectUsage 只接收 Prompt 终态的完整累计用量，不把上下文快照误作回合用量。
func (collector *textCollector) collectUsage(usage acp.Usage) {
	collector.mutex.Lock()
	defer collector.mutex.Unlock()
	collector.usage = acpPromptTurnUsage(usage)
}

// textReasoningSelection 只把模型当前声明的关闭选项当作关闭，否则记录真实低强度或默认降级。
func textReasoningSelection(options []acp.SessionConfigOption, id acp.SessionConfigId, thinking bool) (string, bool, string) {
	selected, exists := acpSelectConfig(options, id)
	if !exists {
		return "", false, "reasoning_not_configurable"
	}
	if thinking {
		return string(selected.CurrentValue), false, ""
	}
	values := acpSelectValues(selected.Options)
	for _, wanted := range []string{"none", "off", "disabled"} {
		for _, value := range values {
			if strings.EqualFold(string(value.Value), wanted) {
				return string(value.Value), true, ""
			}
		}
	}
	for _, wanted := range []string{"minimal", "min", "low", "medium", "high", "max", "xhigh"} {
		for _, value := range values {
			if strings.EqualFold(string(value.Value), wanted) {
				return string(value.Value), false, "thinking_off_unsupported: selected " + string(value.Value)
			}
		}
	}
	if len(values) > 0 {
		return string(values[0].Value), false, "thinking_off_unsupported: selected " + string(values[0].Value)
	}
	return string(selected.CurrentValue), false, "thinking_off_unsupported: retained native default " + string(selected.CurrentValue)
}

// configureTextSession 读取所选模型的真实配置并校验每个写入回执，不继承其他生成调用。
func (agent *embeddedACPAgent) configureTextSession(ctx context.Context, sessionID acp.SessionId, request *acpRunRequest, options []acp.SessionConfigOption) error {
	explicitReasoning := request.Request.Policy.Reasoning
	request.Request = agent.binding.resolveSelections(agent, sessionID, request.Request)
	model := request.Request.Policy.Model
	if model != "" {
		response, err := agent.connection.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{ValueId: &acp.SetSessionConfigOptionValueId{SessionId: sessionID, ConfigId: agent.binding.modelConfigID, Value: acp.SessionConfigValueId(model)}})
		if err != nil {
			return err
		}
		options = response.ConfigOptions
		if acpConfigCurrentValue(options, agent.binding.modelConfigID) != acp.SessionConfigValueId(model) {
			return status_error.RuntimeProtocolIncompatible
		}
	}
	reasoning, disabled, fallback := textReasoningSelection(options, agent.binding.reasoningConfigID, request.Text.thinking)
	if explicit := explicitReasoning; explicit != "" {
		option, exists := acpSelectConfig(options, agent.binding.reasoningConfigID)
		found := false
		if exists {
			for _, value := range acpSelectValues(option.Options) {
				found = found || string(value.Value) == explicit
			}
		}
		if !found {
			return status_error.RuntimePolicyUnsupported
		}
		reasoning, fallback = explicit, ""
		disabled = strings.EqualFold(explicit, "none") || strings.EqualFold(explicit, "off") || strings.EqualFold(explicit, "disabled")
	}
	request.Text.result.ActualReasoning = string(acpConfigCurrentValue(options, agent.binding.reasoningConfigID))
	request.Text.result.ReasoningFallback = "reasoning_selection_not_confirmed"
	if reasoning != "" {
		response, err := agent.connection.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{ValueId: &acp.SetSessionConfigOptionValueId{SessionId: sessionID, ConfigId: agent.binding.reasoningConfigID, Value: acp.SessionConfigValueId(reasoning)}})
		if err != nil {
			return err
		}
		if acpConfigCurrentValue(response.ConfigOptions, agent.binding.reasoningConfigID) != acp.SessionConfigValueId(reasoning) {
			return status_error.RuntimeProtocolIncompatible
		}
	}
	request.Text.result.ActualReasoning, request.Text.result.ThinkingDisabled, request.Text.result.ReasoningFallback = reasoning, disabled, fallback
	request.Request.Policy.Reasoning = reasoning
	agent.sessionMutex.Lock()
	modes := agent.sessionModes[sessionID]
	agent.sessionMutex.Unlock()
	mode, err := agent.binding.modeID(request.Request.Policy.Execution, modes)
	if err != nil {
		return err
	}
	if mode != "" {
		_, err = agent.connection.SetSessionMode(ctx, acp.SetSessionModeRequest{SessionId: sessionID, ModeId: mode})
	}
	return err
}
