package messageview

import (
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

func (p *MessageViewBuilder) applyAgentCommand(record eventlog.Record) error {
	var command eventlog.AgentCommandData
	if json.Unmarshal(record.Data, &command) != nil || command.CommandID == "" || record.ExecutionID == "" {
		return status_error.ChatLogCorrupted
	}
	operation, exists := contract.Find(command.Operation)
	if !exists || command.Completion != operation.Completion {
		return status_error.ChatLogCorrupted
	}
	if p.agentCommands[record.ExecutionID] == nil {
		p.agentCommands[record.ExecutionID] = map[string]eventlog.AgentCommandData{}
		p.agentMatches[record.ExecutionID] = map[string]string{}
	}
	p.agentCommands[record.ExecutionID][command.CommandID] = command
	return nil
}

// matchAgentCommand 只改写实际工具行；找不到同执行 Core 事实时保持原生展示。
func (p *MessageViewBuilder) matchAgentCommand(executionID string, part *MessageDisplayPart, result map[string]any) {
	if part.ToolStatus == constants.MessageToolStatusFailed {
		return
	}
	response := commandReceipt(result, 0)
	if response == nil {
		return
	}
	id, _ := response["commandId"].(string)
	op, _ := response["operation"].(string)
	fact, exists := p.agentCommands[executionID][id]
	if !exists || fact.Operation != op || response["ok"] != true {
		return
	}
	if matched := p.agentMatches[executionID][id]; matched != "" && matched != part.ToolCallID {
		return
	}
	operation, exists := contract.Find(op)
	if !exists || operation.DisplayLabel() == "" {
		return
	}
	p.agentMatches[executionID][id] = part.ToolCallID
	part.ToolLabel, part.ToolLoadingLabel = completedAndLoadingPhrase(operation.DisplayLabel())
	part.ToolCategory, part.ToolAction, part.ToolSafeSummary = constants.MessageToolCategoryOther, constants.MessageToolActionOther, nil
}

// commandReceipt 只读取单个 JSON stdout 的回执身份，不把工具文本当成完成证据。
func commandReceipt(value any, depth int) map[string]any {
	if depth > 6 {
		return nil
	}
	switch typed := value.(type) {
	case string:
		if len(typed) > constants.AgentCLIRequestMaximumBytes {
			return nil
		}
		var decoded any
		if json.Unmarshal([]byte(typed), &decoded) == nil {
			return commandReceipt(decoded, depth+1)
		}
	case map[string]any:
		if typed["protocolVersion"] == float64(constants.AgentCLIProtocolVersion) && typed["ok"] == true {
			return typed
		}
		for _, key := range []string{"stdout", "output", "text", "content", "result"} {
			if found := commandReceipt(typed[key], depth+1); found != nil {
				return found
			}
		}
	case []any:
		for _, item := range typed {
			if found := commandReceipt(item, depth+1); found != nil {
				return found
			}
		}
	}
	return nil
}
