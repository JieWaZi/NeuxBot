package chats

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/apis/uploads"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 发送消息
type SendMessage struct {
	// 通过嵌入契约声明该接口使用 HTTP POST 方法。
	ginx.MethodPost
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required,max=32"`
	// 请求体
	Body MessageRequest `in:"body"`
}

// Path 返回已有对话发送消息接口路径。
func (s *SendMessage) Path() string { return "/:chatId/messages" }

// Output 调用 Chat 续发用例并返回启动后的执行标识。
func (s *SendMessage) Output(ctx *gin.Context) (interface{}, error) {
	input, options := s.Body.workInput()
	release, err := uploads.ClaimWork(ctx, &input)
	if err != nil {
		return nil, err
	}
	accepted := false
	defer func() { release(accepted) }()
	intent := execution.WorkIntent{
		Chat:      execution.ChatTarget{ChatID: strings.TrimSpace(s.ChatID)},
		Input:     input,
		Options:   options,
		Admission: constants.ExecutionAdmissionCommitOnRuntimeAccept,
	}
	if len(s.Body.WorkflowIDs) > 0 {
		bound, err := global.WorkflowService.BindConsumer(ctx, intent, s.Body.WorkflowIDs)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		intent = bound
	}
	result, err := global.ExecutionService.StartChat(ctx, intent)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	accepted = true
	return result, nil
}
