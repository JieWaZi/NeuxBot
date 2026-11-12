package work_board

import (
	"errors"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetWorkBoardLiveTarget 校验系统提醒定位的原目标。
type GetWorkBoardLiveTarget struct {
	// MethodGet 声明只读请求。
	ginx.MethodGet
	// WorkKind 是原公开工作类型。
	WorkKind constants.WorkOwnerKind `in:"query" name:"workKind" validate:"required"`
	// WorkID 是原公开工作标识。
	WorkID string `in:"query" name:"workId" validate:"required,len=32"`
	// Operation 是原实时事件操作类型。
	Operation string `in:"query" name:"operation" validate:"required,oneof=completed failed request_opened"`
	// ChatID 是实际产生消息或请求的会话。
	ChatID string `in:"query" name:"chatId" validate:"omitempty,len=32"`
	// MessageID 是已提交的原公开消息标识。
	MessageID string `in:"query" name:"messageId" validate:"omitempty,max=128"`
	// RequestID 是准确的原待办请求标识。
	RequestID string `in:"query" name:"requestId" validate:"omitempty,max=128"`
	// RequestKind 是原交互类型。
	RequestKind string `in:"query" name:"requestKind" validate:"omitempty,oneof=approval input review"`
	// ExecutionID 定位实际等待的会话执行或工作流运行。
	ExecutionID string `in:"query" name:"executionId" validate:"omitempty,len=32"`
	// RequestFence 是原团队验收围栏。
	RequestFence int64 `in:"query" name:"requestFence" validate:"min=0"`
	// IncludePreview 表示需要有界公开预览。
	IncludePreview bool `in:"query" name:"includePreview"`
}

// WorkBoardLiveTarget 返回有效原目标的安全导航信息。
type WorkBoardLiveTarget struct {
	// Valid 表示原目标仍可打开。
	Valid bool `json:"valid"`
	// Work 是实际可打开的原工作。
	Work *models.WorkRef `json:"work,omitempty"`
	// Title 是公开工作标题。
	Title string `json:"title,omitempty"`
	// Preview 是可选的公开短文本。
	Preview string `json:"preview,omitempty"`
	// WakerID 是实际伙伴标识。
	WakerID string `json:"wakerId,omitempty"`
	// ChannelID 是原团队标识。
	ChannelID string `json:"channelId,omitempty"`
	// ThreadID 是原团队对话标识。
	ThreadID string `json:"threadId,omitempty"`
	// DefinitionID 是原工作流定义标识。
	DefinitionID string `json:"definitionId,omitempty"`
	// RunID 是实际工作流运行标识。
	RunID string `json:"runId,omitempty"`
	// TaskID 是原团队任务标识。
	TaskID string `json:"taskId,omitempty"`
	// InstanceID 是原工作流步骤实例标识。
	InstanceID string `json:"instanceId,omitempty"`
	// AutomaticTaskID 是独立自动机会的任务标识。
	AutomaticTaskID string `json:"automaticTaskId,omitempty"`
	// ChatID 是导航的实际会话标识。
	ChatID string `json:"chatId,omitempty"`
}

func (*GetWorkBoardLiveTarget) Path() string { return "/live-target" }
func (d *GetWorkBoardLiveTarget) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	work := models.WorkRef{Kind: d.WorkKind, ID: d.WorkID}
	if d.Operation == "request_opened" {
		if d.RequestKind == "" || d.RequestID == "" {
			return nil, ginx.WithStack(status_error.BadRequest)
		}
		lookup := models.WorkBoardLiveEvent{Work: work, ChatID: d.ChatID, RequestID: d.RequestID, RequestKind: d.RequestKind, ExecutionID: d.ExecutionID, RequestFence: d.RequestFence}
		target, err := global.WorkBoardControllerManager.FindPendingRequest(request, lookup)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if target.ID == "" || (target.Kind != "review" && target.ChatID != "" && global.ChatService != nil &&
			global.ChatService.ReadingIncompleteReason([]string{target.ChatID}) == "isolated") {
			return WorkBoardLiveTarget{}, nil
		}
		value := WorkBoardLiveTarget{Valid: true, Work: &models.WorkRef{Kind: target.OwnerKind, ID: target.OwnerID},
			Title: target.WorkTitle, ChatID: target.ChatID, WakerID: target.WakerID, ChannelID: target.ChannelID,
			DefinitionID: target.WorkflowID, RunID: target.RunID, TaskID: target.TaskID, InstanceID: target.InstanceID}
		if target.OwnerKind == constants.WorkOwnerKindTeamThread {
			value.ThreadID = target.OwnerID
		}
		if d.IncludePreview {
			value.Preview = livePreview(target.Title)
		}
		return value, nil
	}
	var owner models.WorkBoardEntry
	var err error
	if d.WorkKind == constants.WorkOwnerKindChat {
		if d.ChatID != d.WorkID {
			return WorkBoardLiveTarget{}, nil
		}
		owner, err = global.WorkBoardControllerManager.ResolveChatOutcome(request, d.ChatID)
	} else {
		owner, err = global.WorkBoardControllerManager.ResolveWorkRef(request, work)
	}
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if owner.OwnerID == "" {
		return WorkBoardLiveTarget{}, nil
	}
	value := WorkBoardLiveTarget{Work: &models.WorkRef{Kind: owner.OwnerKind, ID: owner.OwnerID}, Title: owner.Title, ChatID: owner.ChatID, WakerID: owner.WakerID, ChannelID: owner.ChannelID, DefinitionID: owner.WorkflowID, AutomaticTaskID: owner.AutomaticTaskID}
	switch owner.OwnerKind {
	case constants.WorkOwnerKindTeamThread, constants.WorkOwnerKindChat:
		if owner.OwnerKind == constants.WorkOwnerKindTeamThread {
			value.ThreadID = owner.OwnerID
		}
		if d.ChatID != owner.ChatID || d.MessageID == "" || (global.ChatService != nil &&
			global.ChatService.ReadingIncompleteReason([]string{d.ChatID}) == "isolated") {
			return WorkBoardLiveTarget{}, nil
		}
		if d.ExecutionID != "" && global.ExecutionService != nil && global.ExecutionService.IsExecutionActive(d.ChatID, d.ExecutionID) {
			return WorkBoardLiveTarget{}, nil
		}
		found, err := global.ChatReadingControllerManager.HasMessage(request, d.ChatID, d.MessageID, d.ExecutionID, d.Operation == "failed")
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		value.Valid = found
		if !found || !d.IncludePreview {
			return value, nil
		}
		message, err := global.ChatService.PublicReadingMessage(request, d.ChatID, d.MessageID)
		if errors.Is(err, status_error.ChatMessageNotFound) || errors.Is(err, status_error.ChatNotFound) {
			return WorkBoardLiveTarget{}, nil
		}
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if message == nil || !message.Readable || message.ExecutionID != d.ExecutionID {
			return WorkBoardLiveTarget{}, nil
		}
		value.Preview = livePreview(message.Content)
		if value.Preview == "" {
			value.Preview = livePreview(message.ErrorMessage)
		}
	case constants.WorkOwnerKindWorkflowRun, constants.WorkOwnerKindAutomaticRun:
		state, err := global.RunViewingControllerManager.Get(request, constants.LocalReaderID, models.RunOwnerRef{OwnerKind: constants.RunOwnerKind(d.WorkKind), OwnerID: d.WorkID})
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		value.Valid = state.Viewable && (d.Operation == "failed") == (owner.LastStatus == "failed")
		if d.WorkKind == constants.WorkOwnerKindWorkflowRun {
			value.RunID = owner.OwnerID
		}
	}
	return value, nil
}

func livePreview(raw string) string {
	value := []rune(strings.Join(strings.Fields(raw), " "))
	return string(value[:min(len(value), 160)])
}
