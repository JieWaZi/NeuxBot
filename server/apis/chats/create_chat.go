package chats

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/apis/uploads"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建对话请求
type CreateChatRequest struct {
	// 客户端首次发送标识
	ClientRequestID string `json:"clientRequestId" validate:"required,max=64"`
	// 伙伴标识
	WakerID string `json:"wakerId" validate:"required,max=32"`
	// 项目标识
	ProjectID string `json:"projectId" validate:"max=32"`
	// 工作目录
	WorkingDirectory string `json:"workingDirectory" validate:"max=4096"`
	// 档案优化首发
	OptimizeProfile bool `json:"optimizeProfile,omitempty"`
	// 首条消息
	MessageRequest
}

// 创建对话
type CreateChat struct {
	// 通过嵌入契约声明该接口使用 HTTP POST 方法。
	ginx.MethodPost
	// 请求体
	Body CreateChatRequest `in:"body"`
}

// Path 返回创建对话接口路径。
func (c *CreateChat) Path() string { return "" }

// Validate 禁止同时选择项目与目录；均为空时使用该会话的受管临时目录。
func (c *CreateChat) Validate(*gin.Context) error {
	hasProject := strings.TrimSpace(c.Body.ProjectID) != ""
	hasDirectory := strings.TrimSpace(c.Body.WorkingDirectory) != ""
	if hasProject && hasDirectory {
		return ginx.WithStack(status_error.BadRequest)
	}
	return nil
}

// Output 调用 Chat 首发用例并返回启动后的对话和执行标识。
// [MessageAttachmentInvalid][40000000017][消息附件不合法或不可读取]
func (c *CreateChat) Output(ctx *gin.Context) (interface{}, error) {
	input, options := c.Body.MessageRequest.workInput()
	if c.Body.OptimizeProfile {
		input.ContextInstructions = []workinput.Instruction{{
			Name:    "Partner profile edit",
			Content: "The user opened this conversation from their partner profile editor. Interpret the first message as a profile edit request. Read the current profile with waker.profile.get and follow the partner profile guide. Ask for clarification if needed.",
		}}
	}
	intent := execution.WorkIntent{
		ClientRequestID: strings.TrimSpace(c.Body.ClientRequestID),
		Chat: execution.ChatTarget{Create: &execution.CreateChatSpec{
			WakerID:          strings.TrimSpace(c.Body.WakerID),
			ProjectID:        strings.TrimSpace(c.Body.ProjectID),
			WorkingDirectory: strings.TrimSpace(c.Body.WorkingDirectory),
		}},
		Input:     input,
		Options:   options,
		Admission: constants.ExecutionAdmissionCommitOnRuntimeAccept,
		ClaimAttachments: func(attachments []execution.AttachmentInput) (func(bool), error) {
			release, err := uploads.ClaimWork(ctx, &execution.WorkInput{Attachments: attachments})
			if err != nil {
				return nil, status_error.MessageAttachmentInvalid
			}
			return release, nil
		},
	}
	result, err := global.ExecutionService.Start(ctx, intent)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
