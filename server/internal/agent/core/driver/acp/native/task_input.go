package native

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
)

// Message 是独立调用交给原生适配器的角色化输入。
type Message struct {
	Role    string // Role 仅接受 system、user 或 assistant。
	Content string // Content 是本条角色输入的完整正文。
}

// TaskInput 保存原生角色转换结果，不拥有产品请求或资源。
type TaskInput struct {
	Text         string   // Text 是交给 ACP Prompt 的本轮正文。
	Instructions []string // Instructions 是需要由原生配置安装的系统指令。
	Limitations  []string // Limitations 是原生角色语义的明确限制。
}

// CompileTaskMessages 根据技术声明转换角色，不在 Host 散落 CLI 分支。
func CompileTaskMessages(runtimeID string, messages []Message) (TaskInput, error) {
	descriptor, ok := Lookup(runtimeID)
	if !ok {
		return TaskInput{}, &c.Fault{Code: c.FaultUnsupported, Stage: "input", Message: "native runtime is not registered"}
	}
	if len(messages) == 0 {
		return TaskInput{}, &c.Fault{Code: c.FaultInvalidSpec, Stage: "input", Message: "task messages are required"}
	}
	result := TaskInput{}
	remaining := make([]Message, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case "system":
			result.Instructions = append(result.Instructions, message.Content)
		case "user", "assistant":
			remaining = append(remaining, message)
			if message.Role == "assistant" {
				result.Limitations = append(result.Limitations, "assistant_history_serialized_as_text: ACP cannot install native assistant history")
			}
		default:
			return TaskInput{}, &c.Fault{Code: c.FaultInvalidSpec, Stage: "input", Message: "unsupported task message role"}
		}
	}
	if !descriptor.NativeSystemInstructions && len(result.Instructions) > 0 {
		remaining = messages
		result.Instructions = nil
		result.Limitations = append(result.Limitations, "native_system_role_unavailable: task messages serialized into one user text input")
	} else if len(remaining) == 1 && remaining[0].Role == "user" {
		result.Text = remaining[0].Content
		return result, nil
	} else {
		result.Limitations = append(result.Limitations, "native_message_roles_unavailable: ACP serializes user/assistant roles into one user text input")
	}
	var text strings.Builder
	for _, message := range remaining {
		fmt.Fprintf(&text, "<message role=%q>\n%s\n</message>\n", message.Role, message.Content)
	}
	result.Text = text.String()
	return result, nil
}

// ConfigureTaskInstructions 在明确支持原生系统指令的工厂安装文件配置。
// 文件属于调用方已持有的工作区，随同一会话资源释放，不另建清理所有者。
func ConfigureTaskInstructions(config Config, instructions []string) (Config, error) {
	descriptor, ok := Lookup(config.RuntimeID)
	if !ok {
		return config, &c.Fault{Code: c.FaultUnsupported, Stage: "configure", Message: "native runtime is not registered"}
	}
	if !descriptor.NativeSystemInstructions || len(instructions) == 0 {
		return config, nil
	}
	path := filepath.Join(config.WorkingDirectory, "model-instructions.md")
	if !filepath.IsAbs(config.WorkingDirectory) {
		return config, &c.Fault{Code: c.FaultInvalidSpec, Stage: "configure", Message: "native instruction workspace must be absolute"}
	}
	if err := os.WriteFile(path, []byte(strings.Join(instructions, "\n\n")), 0600); err != nil {
		return config, &c.Fault{Code: c.FaultPreparation, Stage: "configure", Message: "native model instructions cannot be written", Cause: err}
	}
	config.CodexConfiguration = maps.Clone(config.CodexConfiguration)
	if config.CodexConfiguration == nil {
		config.CodexConfiguration = make(map[string]string)
	}
	config.CodexConfiguration["model_instructions_file"] = strconv.Quote(path)
	return config, nil
}

// ModelID 根据原生技术声明投射受管账号模型，系统账号保持原生标识。
func ModelID(runtimeID string, managed bool, model string) string {
	descriptor, ok := Lookup(runtimeID)
	if ok && managed && model != "" {
		return descriptor.ManagedModelPrefix + model
	}
	return model
}
