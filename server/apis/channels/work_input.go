package channels

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/apis/chats"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// 团队消息输入
type ChannelWorkInput struct {
	// 消息正文
	Text string `json:"text" validate:"required,max=1048576"`
	// 提及伙伴
	MentionWakerID string `json:"mentionWakerId" validate:"max=32"`
	// 附件列表
	Attachments []chats.MessageAttachment `json:"attachments" validate:"max=100,dive"`
	// 技能列表
	SkillIDs []string `json:"skillIds" validate:"max=100,dive,max=32"`
	// 连接器列表
	PluginIDs []string `json:"pluginIds,omitempty" validate:"max=100,dive,max=32"`
	// 连接器列表
	ConnectorIDs []string `json:"connectorIds" validate:"max=100,dive,max=32"`
	// 代码引用
	CodeReferences []chats.MessageCodeReference `json:"codeReferences" validate:"max=100,dive"`
}

func (i ChannelWorkInput) workInput() execution.WorkInput {
	attachments := make([]execution.AttachmentInput, 0, len(i.Attachments))
	for _, attachment := range i.Attachments {
		attachments = append(attachments, execution.AttachmentInput{
			SourcePath: strings.TrimSpace(attachment.SourcePath),
		})
	}
	references := make([]execution.CodeReferenceInput, 0, len(i.CodeReferences))
	for _, reference := range i.CodeReferences {
		references = append(references, execution.CodeReferenceInput{
			Path:      strings.TrimSpace(reference.Path),
			StartLine: reference.StartLine,
			EndLine:   reference.EndLine,
			Content:   reference.Content,
		})
	}
	return execution.WorkInput{
		Actor:          string(constants.ChannelMessageActorUser),
		Content:        i.Text,
		Attachments:    attachments,
		CodeReferences: references,
		SkillIDs:       append([]string(nil), i.SkillIDs...),
		ConnectorIDs:   append([]string(nil), i.ConnectorIDs...),
		PluginIDs:      append([]string(nil), i.PluginIDs...),
	}
}
