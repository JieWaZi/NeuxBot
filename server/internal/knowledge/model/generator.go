// Package model 将知识核心的单次消息请求适配为CLI 最终结果生成，不创建聊天或执行。
package model

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/host"
	"github.com/JieWaZi/neux-knowledge/core"
)

// TextRuntime 是模型适配唯一需要的 Runtime 能力。
type TextRuntime interface {
	// GenerateText 执行一次独立上下文并返回真实文本、终态及诊断。
	GenerateText(context.Context, runtimehost.TextRequest) (runtimehost.TextResult, error)
}

// Config 显式提供模型选择与每调用账号租约，不依赖伙伴或 ExecutionService。
type Config struct {
	// Runtime 提供可取消的独立文本调用。
	Runtime TextRuntime
	// RuntimeID 是已接纳的智能体选择。
	RuntimeID string
	// AccountID 是已接纳的账号选择。
	AccountID string
	// Model 是已接纳知识任务冻结的模型选择。
	Model string
	// Reasoning 是冻结的推理强度，空值沿模型最低档策略。
	Reasoning string
	// Observe 接收原始结果诊断，不能改变模型正文或知识发布事实。
	Observe func(context.Context, core.GenerateRequest, runtimehost.TextResult)
}

// Generator 是无跨调用会话状态的知识模型适配器。
type Generator struct {
	// config 是构造时固定的模型与外部端口。
	config Config
}

// New 构造不启动进程、不接纳聊天且不保存模型上下文的适配器。
func New(config Config) *Generator { return &Generator{config: config} }

// Generate 把本次完整消息交给 CLI；账号租约覆盖进程退出且每次独立释放。
func (generator *Generator) Generate(ctx context.Context, input core.GenerateRequest) (core.GenerateResult, error) {
	if generator == nil || generator.config.Runtime == nil {
		return core.GenerateResult{}, status_error.RuntimeUnavailable
	}
	messages := make([]runtimehost.Message, len(input.Messages))
	for index, message := range input.Messages {
		messages[index] = runtimehost.Message{Role: message.Role, Content: message.Content}
	}
	result, err := generator.config.Runtime.GenerateText(ctx, runtimehost.TextRequest{
		RuntimeID: generator.config.RuntimeID, AccountID: generator.config.AccountID, Model: generator.config.Model,
		Messages: messages, Reasoning: generator.config.Reasoning, Thinking: input.Thinking, MaxTokens: input.MaxTokens, Temperature: input.Temperature,
	})
	if generator.config.Observe != nil {
		generator.config.Observe(ctx, input, result)
	}
	output := core.GenerateResult{Text: result.Text, FinishReason: textFinishReason(result.StopReason), Diagnostics: &core.ModelDiagnostics{
		ActualReasoning: result.ActualReasoning, ThinkingDisabled: result.ThinkingDisabled,
		ReasoningFallback: result.ReasoningFallback, ToolsDisabled: false, Limitations: append([]string(nil), result.Limitations...),
	}}
	if result.Usage != nil {
		inputTokens := result.Usage.InputTokens + result.Usage.CacheReadTokens + result.Usage.CacheCreationTokens
		output.Usage = &core.ModelUsage{InputTokens: inputTokens, OutputTokens: result.Usage.OutputTokens, TotalTokens: inputTokens + result.Usage.OutputTokens}
	}
	if err != nil {
		output.Text = ""
		return output, err
	}
	return output, nil
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
