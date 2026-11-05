// Package model 将知识核心的消息请求交给正式运行时，不创建公开聊天或执行。
package model

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/neux-knowledge/core"
)

// TextRuntime 提供无工具的独立文本调用。
type TextRuntime interface {
	GenerateText(context.Context, agent.TextRequest) (agent.TextResult, error)
}

// Config 固定知识任务选择的运行时与账号。
type Config struct {
	// Runtime 提供可取消的独立文本调用。
	Runtime TextRuntime
	// Connection 指定已接纳的程序、账号和模型。
	Connection agent.ProviderConnectionRequest
	// ResolveAccount 在每次调用中解析实际的账号身份。
	ResolveAccount agent.DetachedAccountResolver
	// Reasoning 是冻结的推理强度，空值允许按请求选择最低档。
	Reasoning string
}

// Generator 是无跨调用会话状态的知识模型适配器。
type Generator struct {
	// config 是构造时固定的模型与外部端口。
	config Config
}

// New 构造独立生成适配器。
func New(config Config) *Generator { return &Generator{config: config} }

// Generate 交付本次完整消息并按原生证据映射最终结果。
func (generator *Generator) Generate(ctx context.Context, input core.GenerateRequest) (core.GenerateResult, error) {
	if generator == nil || generator.config.Runtime == nil {
		return core.GenerateResult{}, status_error.RuntimeUnavailable
	}
	messages := make([]agent.TextMessage, len(input.Messages))
	for index, message := range input.Messages {
		messages[index] = agent.TextMessage{Role: message.Role, Content: message.Content}
	}
	result, err := generator.config.Runtime.GenerateText(ctx, agent.TextRequest{
		Connection: generator.config.Connection, ResolveAccount: generator.config.ResolveAccount,
		Messages: messages, Selection: &agent.TurnSelection{Reasoning: generator.config.Reasoning, PreferNoThinking: !input.Thinking},
		UsageSource: "knowledge_generation", MaxTokens: input.MaxTokens, Temperature: input.Temperature,
	})
	diagnostics := &core.ModelDiagnostics{ToolsDisabled: result.ToolsDisabled, Limitations: result.Limitations}
	for _, option := range result.Turn.Effective.Options {
		if option.Category == "thought_level" {
			diagnostics.ActualReasoning = option.Value.ID
			break
		}
	}
	switch strings.ToLower(diagnostics.ActualReasoning) {
	case "none", "off", "disabled":
		diagnostics.ThinkingDisabled = true
	case "":
		if result.ToolsDisabled && !input.Thinking && generator.config.Reasoning == "" {
			diagnostics.ReasoningFallback = "reasoning_not_configurable"
		}
	default:
		if !input.Thinking && generator.config.Reasoning == "" {
			diagnostics.ReasoningFallback = "thinking_off_unsupported: selected " + diagnostics.ActualReasoning
		}
	}
	output := core.GenerateResult{Text: result.Text, Diagnostics: diagnostics}
	if outcome := result.Turn.Outcome; outcome != nil {
		output.FinishReason = textFinishReason(outcome.StopReason)
		usage := outcome.Turn.Usage
		// 知识核心的汇总没有未知分量，只有完整观测时才能给出总数。
		if usage != nil && usage.Complete && usage.InputTokens != nil && usage.CacheReadTokens != nil &&
			usage.CacheWriteTokens != nil && usage.OutputTokens != nil {
			inputTokens := *usage.InputTokens + *usage.CacheReadTokens + *usage.CacheWriteTokens
			output.Usage = &core.ModelUsage{InputTokens: inputTokens, OutputTokens: *usage.OutputTokens, TotalTokens: inputTokens + *usage.OutputTokens}
		}
	}
	if err != nil {
		output.Text = ""
	}
	return output, err
}

// textFinishReason 只归一原生可判定的完成与长度终态，其他开放值原样保留。
func textFinishReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	default:
		return reason
	}
}

var _ core.Generator = (*Generator)(nil)
