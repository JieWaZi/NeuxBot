package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/apis/uploads"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	service "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 整理聊天所选附件到远端；不会创建本机模型执行。
type OrganizeRemoteChat struct {
	ginx.MethodPost
	// 聊天标识
	ChatID string `in:"path" name:"chatId" validate:"required,max=32"`
	// 请求体
	Body RemoteChatInput `in:"body"`
}

func (o *OrganizeRemoteChat) Path() string { return "/chat-operations/:chatId" }
func (o *OrganizeRemoteChat) Output(ctx *gin.Context) (interface{}, error) {
	if global.KnowledgeService == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	refs := make([]string, len(o.Body.Attachments))
	for index, attachment := range o.Body.Attachments {
		refs[index] = attachment.SourcePath
	}
	paths, release, err := uploads.Claim(ctx, refs)
	if err != nil {
		return nil, err
	}
	// 远端整理不持久化本机附件，回执重查仍需原字节摘要；保留到用户移除或上传到期。
	defer release(false)
	attachments := make([]workspace.AttachmentInput, 0, len(o.Body.Attachments))
	for _, path := range paths {
		attachments = append(attachments, workspace.AttachmentInput{SourcePath: path})
	}
	result, err := global.KnowledgeService.OrganizeRemoteChat(ctx, service.RemoteChatCommand{RequestID: o.Body.RequestID, ChatID: o.ChatID, Ref: bridge.ResourceRef{Backend: o.Body.Ref.Backend, ConnectionID: o.Body.Ref.ConnectionID, KnowledgeBaseID: o.Body.Ref.KnowledgeBaseID}, ProfileID: o.Body.ProfileID, Attachments: attachments})
	if err != nil {
		return nil, err
	}
	return remoteChatDTO(result), nil
}
func init() { Router.Register(&OrganizeRemoteChat{}) }
