package messageview

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

// MessagePartIncrement 是对稳定展示部件的安全增量操作。
type MessagePartIncrement struct {
	// Kind 表示追加文本、修改阶段或替换安全工具事实。
	Kind string `json:"kind"`
	// PartID 是消息内已经冻结的部件身份。
	PartID string `json:"partId"`
	// PartType 是文本部件的当前语义阶段。
	PartType constants.MessageDisplayPartType `json:"partType,omitempty"`
	// Text 只包含本次新增文本，空值不代表清空。
	Text string `json:"text,omitempty"`
	// Part 是完整安全工具状态，替换时省略的字段表示清空。
	Part *MessageDisplayPart `json:"part,omitempty"`
}

// MessageIncrement 是一次原始有序输入造成的安全消息变化，不包含累计正文。
type MessageIncrement struct {
	// BaseRevision 是应用本次变化前必须已经覆盖的消息版本。
	BaseRevision int64 `json:"baseRevision"`
	// Revision 是成功应用本次变化后覆盖的消息版本。
	Revision int64 `json:"revision"`
	// InvocationID 是本次变化所属的原生调用身份。
	InvocationID string `json:"invocationId,omitempty"`
	// UpdatedAt 是该变化的原始发生时间。
	UpdatedAt int64 `json:"updatedAt"`
	// Parts 按发生顺序更新明确的部件，不扫描未变化节点。
	Parts []MessagePartIncrement `json:"parts,omitempty"`
	// RunMetrics 非空时替换有界的安全用量摘要。
	RunMetrics *MessageRunMetrics `json:"runMetrics,omitempty"`
}

// ApplyIncrement 在原有投影转换中收集实际变更，不构建前后完整快照或遍历比较部件。
func (p *MessageViewBuilder) ApplyIncrement(record eventlog.Record, messageID string) (string, MessageIncrement, error) {
	if p.increment != nil {
		return "", MessageIncrement{}, status_error.ChatLogError
	}
	change := MessageIncrement{BaseRevision: p.MessageRevision(messageID), InvocationID: record.InvocationID, UpdatedAt: record.RecordedAt}
	p.increment = &change
	id, err := p.Apply(record)
	p.increment = nil
	if err != nil {
		return "", MessageIncrement{}, err
	}
	change.Revision = p.MessageRevision(messageID)
	return id, change, nil
}

func (p *MessageViewBuilder) incrementText(part MessageDisplayPart, text string) {
	if p.increment != nil && text != "" {
		p.increment.Parts = append(p.increment.Parts, MessagePartIncrement{Kind: constants.MessageIncrementTextAppend, PartID: part.ID, PartType: part.Type, Text: text})
	}
}

func (p *MessageViewBuilder) incrementPartType(part MessageDisplayPart) {
	if p.increment != nil {
		p.increment.Parts = append(p.increment.Parts, MessagePartIncrement{Kind: constants.MessageIncrementPartType, PartID: part.ID, PartType: part.Type})
	}
}

func (p *MessageViewBuilder) incrementTool(part MessageDisplayPart) {
	if p.increment == nil {
		return
	}
	// 工具投影自身已执行白名单、长度和路径边界，增量不接触原生输入或输出。
	clone := part
	if part.ToolSafeSummary != nil {
		summary := *part.ToolSafeSummary
		summary.Files = append([]ToolFileChangeSummary(nil), part.ToolSafeSummary.Files...)
		clone.ToolSafeSummary = &summary
	}
	if part.ToolApprovalResult != nil {
		result := *part.ToolApprovalResult
		clone.ToolApprovalResult = &result
	}
	p.increment.Parts = append(p.increment.Parts, MessagePartIncrement{Kind: constants.MessageIncrementPartUpsert, PartID: part.ID, Part: &clone})
}
