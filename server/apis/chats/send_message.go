package chats

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/apis/uploads"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 续发消息请求
type SendMessageRequest struct {
	// 输入提交标识
	SubmissionID string `json:"submissionId,omitempty" validate:"max=64"`
	// 引用消息标识
	ReplyToMessageID string `json:"replyToMessageId,omitempty" validate:"max=32"`
	// 创建工单
	StartWork bool `json:"startWork,omitempty"`
	// 消息内容
	MessageRequest
}

// WorkInput 在普通消息内容之外保留来源内提交与引用身份。
func (r SendMessageRequest) WorkInput() (execution.WorkInput, execution.WorkOptions) {
	input, options := r.MessageRequest.WorkInput()
	input.SubmissionID = strings.TrimSpace(r.SubmissionID)
	input.ReplyToMessageID = strings.TrimSpace(r.ReplyToMessageID)
	return input, options
}

// 发送消息
type SendMessage struct {
	// 通过嵌入契约声明该接口使用 HTTP POST 方法。
	ginx.MethodPost
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required,max=32"`
	// 请求体
	Body SendMessageRequest `in:"body"`
}

// Path 返回已有对话发送消息接口路径。
func (s *SendMessage) Path() string { return "/:chatId/messages" }

// Output 调用 Chat 续发用例并返回启动后的执行标识。
// [MessageAttachmentInvalid][40000000017][消息附件不合法或不可读取]
// [ChatReplyTargetUnavailable][40400007202][当前对话中没有可引用的这条消息]
// @err[WorkTicketCannotContinue][40900007211][工单当前无法继续]
// @err[WorkTicketAttemptUnavailable][40900007212][这轮执行已无权更新工单]
func (s *SendMessage) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	input, options := s.Body.WorkInput()
	chatID := strings.TrimSpace(s.ChatID)
	var identity requestidempotency.Identity
	if input.SubmissionID != "" && global.WorkTicketService != nil {
		unlock, err := lockOrdinarySubmission(request, chatID, input.SubmissionID)
		if err != nil {
			return nil, err
		}
		defer unlock()
		identity, err = requestidempotency.NewSubmission(input.SubmissionID, []any{input, options, s.Body.StartWork})
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		replay, found, err := replayTransferredWork(request, chatID, identity)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if found {
			return replay, nil
		}
	}
	if input.ReplyToMessageID != "" && global.WorkTicketService != nil {
		origin := workticket.Address{Kind: constants.WorkTicketOriginChat, ID: chatID}
		ticket, found, err := global.WorkTicketService.ForMessage(request, origin, input.ReplyToMessageID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if found && ticket.Transferred {
			previous, err := global.ChatService.ReadSubmission(request, chatID, input.SubmissionID)
			if err != nil {
				return nil, ginx.WithStack(err)
			}
			if previous == nil {
				if s.Body.StartWork || s.Body.StartNewSession {
					return nil, status_error.BadRequest
				}
				if input.AttachmentOnly || len(input.Attachments) != 0 {
					return nil, status_error.MessageAttachmentInvalid
				}
				result, err := continueTransferredWork(request, origin, ticket, input, identity)
				if err != nil {
					return nil, ginx.WithStack(err)
				}
				return result, nil
			}
		}
	}
	release, err := uploads.ClaimWork(ctx, &input)
	if err != nil {
		return nil, err
	}
	accepted := false
	defer func() { release(accepted) }()
	intent := execution.WorkIntent{
		Chat:      execution.ChatTarget{ChatID: chatID},
		Input:     input,
		Ticket:    execution.WorkTicketInput{Start: s.Body.StartWork},
		Options:   options,
		Admission: constants.ExecutionAdmissionCommitOnRuntimeAccept,
	}
	result, err := global.ExecutionService.Start(ctx, intent)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	accepted = true
	return result, nil
}
