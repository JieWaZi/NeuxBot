package chats

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// 消息请求
type MessageRequest struct {
	// AccountID 是本次冻结的账号标识；空值使用智能体默认配置。
	AccountID string `json:"accountId,omitempty" validate:"max=32"`
	// 消息正文
	Text string `json:"text" validate:"required,max=1048576"`
	// 运行时标识
	RuntimeID string `json:"runtimeId" validate:"required,max=120"`
	// 模型
	Model string `json:"model" validate:"max=200"`
	// 推理强度
	Reasoning string `json:"reasoning" validate:"max=80"`
	// 权限等级
	PermissionLevel constants.PermissionLevel `json:"permissionLevel" validate:"required"`
	// 是否新建原生会话
	StartNewSession bool `json:"startNewSession,omitempty"`
	// 最近对话轮数
	HistoryTurns int `json:"historyTurns,omitempty" validate:"min=0,max=20"`
	// 新会话取得上下文的方式，空值使用近期对话。
	HistoryMode constants.HistoryMode `json:"historyMode,omitempty" validate:"omitempty,oneof=recent summary"`
	// 自动总结旧会话时使用的模型。
	SummaryModel string `json:"summaryModel,omitempty" validate:"max=200"`
	// 自动总结旧会话时使用的推理强度。
	SummaryReasoning string `json:"summaryReasoning,omitempty" validate:"max=80"`
	// 附件列表
	Attachments []MessageAttachment `json:"attachments" validate:"max=100,dive"`
	// 技能列表
	SkillIDs []string `json:"skillIds" validate:"max=100,dive,max=32"`
	// 插件列表
	PluginIDs []string `json:"pluginIds,omitempty" validate:"max=100,dive,max=32"`
	// 连接器列表
	ConnectorIDs []string `json:"connectorIds" validate:"max=100,dive,max=32"`
	// 工作流列表
	WorkflowIDs []string `json:"workflowIds,omitempty" validate:"max=100,dive,max=32"`
	// 代码引用
	CodeReferences []MessageCodeReference `json:"codeReferences" validate:"max=100,dive"`
}

// 消息附件
type MessageAttachment struct {
	// 本地路径
	SourcePath string `json:"sourcePath" validate:"required"`
}

// 代码引用
type MessageCodeReference struct {
	// 路径
	Path string `json:"path" validate:"required,max=4096"`
	// 起始行号
	StartLine int `json:"startLine" validate:"min=0"`
	// 结束行号
	EndLine int `json:"endLine" validate:"min=0"`
	// 文件内容
	Content string `json:"content" validate:"max=1048576"`
}

func (r MessageRequest) workInput() (execution.WorkInput, execution.WorkOptions) {
	sessionPolicy := constants.RuntimeSessionPolicyContinue
	if r.StartNewSession {
		sessionPolicy = constants.RuntimeSessionPolicyNew
	}
	attachments := make([]execution.AttachmentInput, 0, len(r.Attachments))
	for _, attachment := range r.Attachments {
		attachments = append(attachments, execution.AttachmentInput{
			SourcePath: strings.TrimSpace(attachment.SourcePath),
		})
	}
	references := make([]execution.CodeReferenceInput, 0, len(r.CodeReferences))
	for _, reference := range r.CodeReferences {
		references = append(references, execution.CodeReferenceInput{
			Path:      strings.TrimSpace(reference.Path),
			StartLine: reference.StartLine,
			EndLine:   reference.EndLine,
			Content:   reference.Content,
		})
	}
	return execution.WorkInput{
			Actor:          "user",
			Content:        r.Text,
			Attachments:    attachments,
			SkillIDs:       append([]string(nil), r.SkillIDs...),
			ConnectorIDs:   append([]string(nil), r.ConnectorIDs...),
			PluginIDs:      append([]string(nil), r.PluginIDs...),
			CodeReferences: references,
		}, execution.WorkOptions{
			AccountID: strings.TrimSpace(r.AccountID),
			RuntimeID: strings.TrimSpace(r.RuntimeID),
			Model:     strings.TrimSpace(r.Model),
			Reasoning: strings.TrimSpace(r.Reasoning),
			PermissionLevel: constants.PermissionLevel(
				strings.TrimSpace(string(r.PermissionLevel)),
			),
			SessionPolicy:    sessionPolicy,
			HistoryTurns:     r.HistoryTurns,
			HistoryMode:      r.HistoryMode,
			SummaryModel:     strings.TrimSpace(r.SummaryModel),
			SummaryReasoning: strings.TrimSpace(r.SummaryReasoning),
		}
}
