package work_tickets

import (
	"context"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 查询伙伴或团队的工单活动
type ListWorkTicketActivity struct {
	ginx.MethodGet
	// 主会话的主体类型。
	SubjectType string `in:"query" name:"subjectType" validate:"required,oneof=waker team"`
	// 主会话的主体标识。
	SubjectID string `in:"query" name:"subjectId" validate:"required,max=32"`
}

type workTicketActivity struct {
	// TicketID 是跨入口稳定的工单标识。
	TicketID string `json:"ticketId"`
	// ParentTicketID 是这项工作向其回件的直接工单。
	ParentTicketID string `json:"parentTicketId,omitempty"`
	// SourceSubjectType 标记根工单的公开主体类型。
	SourceSubjectType string `json:"sourceSubjectType"`
	// SourceSubjectID 标记根工单的公开主体。
	SourceSubjectID string `json:"sourceSubjectId"`
	// SourceMessageID 是根工单对应的公开交办消息。
	SourceMessageID string `json:"sourceMessageId"`
	// ExecutorWakerID 是当前实际责任伙伴。
	ExecutorWakerID string `json:"executorWakerId"`
	// Title 是工作标题或精简的交办标题。
	Title string `json:"title"`
	// State 用业务状态反映已接纳的工作，未启动的工作标为 queued。
	State string `json:"state"`
	// RuntimeID 仅在已有执行记录时返回其真实运行时。
	RuntimeID string `json:"runtimeId,omitempty"`
	// WorkChatID 是经过工作会话授权检查的来源。
	WorkChatID string `json:"workChatId,omitempty"`
	// WorkSubjectType 与 WorkSubjectID 共同定位来源所属会话。
	WorkSubjectType string `json:"workSubjectType,omitempty"`
	// WorkSubjectID 是工作来源的可访问主体。
	WorkSubjectID string `json:"workSubjectId,omitempty"`
	// Received 表明这行是按接收伙伴投影的受托工作。
	Received bool `json:"received"`
}

// Path 返回受权工单活动查询入口。
func (*ListWorkTicketActivity) Path() string { return "/activity" }

// Output 根据工单真实归属投影任务，不公开子单对话内容或回件。
func (o *ListWorkTicketActivity) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	var out []workTicketActivity
	err := global.TransactionControllerManager.Transaction(request, func(tx context.Context) error {
		var err error
		switch o.SubjectType {
		case "waker":
			_, err = global.WakerControllerManager.Get(tx, o.SubjectID)
		case "team":
			_, err = global.ChannelControllerManager.Get(tx, o.SubjectID)
		}
		if err != nil {
			return err
		}
		store := controllers.NewWorkTicketController(global.DB)
		conversationID, err := global.MainConversationControllerManager.SubjectConversationID(tx, o.SubjectType, o.SubjectID)
		if err != nil {
			return err
		}
		rows, err := store.ActivityCandidates(tx, conversationID, o.SubjectType, o.SubjectID)
		if err != nil {
			return err
		}
		out = make([]workTicketActivity, 0, len(rows))
		for _, row := range rows {
			root := row
			for depth := 0; root.ParentTicketID != "" && depth < 12; depth++ {
				root, err = store.Ticket(tx, root.ParentTicketID)
				if err != nil {
					return err
				}
				if root.ID == "" {
					break
				}
			}
			if root.ID == "" || root.ParentTicketID != "" || root.OriginKind != constants.WorkTicketOriginMain {
				continue
			}
			received := root.OriginID != conversationID
			if received && (o.SubjectType != "waker" || row.OwnerWakerID != o.SubjectID || row.ParentTicketID == "") {
				continue
			}
			if err := global.ValidateWorkTicketOrigin(tx, workticket.Address{Kind: root.OriginKind, ID: root.OriginID}); err != nil {
				continue
			}
			source, err := global.MainConversationControllerManager.Get(tx, root.OriginID)
			if err != nil {
				return err
			}
			canOpen := true
			if row.ParentTicketID != "" && source.SubjectType == "team" {
				canOpen, err = store.AuthorizesTeamDelegate(tx, source.SubjectID, row.OwnerWakerID)
				if err != nil {
					return err
				}
				if received && !canOpen {
					continue
				}
			}
			view := workTicketActivity{TicketID: row.ID, ParentTicketID: row.ParentTicketID, SourceSubjectType: source.SubjectType, SourceSubjectID: source.SubjectID, SourceMessageID: root.ID, ExecutorWakerID: row.OwnerWakerID, State: string(row.Status), Received: received}
			if row.Status == constants.WorkTicketStatusActive && row.CurrentExecutionID == "" {
				view.State = "queued"
			}
			if row.DelegationFailed {
				view.State = "failed"
			}
			if row.ParentTicketID != "" {
				view.Title = activityTitle(row.DelegationContent)
			}
			workOwner, err := global.MainConversationControllerManager.Get(tx, row.OriginID)
			if err == nil && canOpen {
				chat, chatErr := global.ChatControllerManager.Get(tx, row.WorkChatID)
				if chatErr == nil {
					work, workErr := global.MainConversationService.Source(tx, workOwner.SubjectType, workOwner.SubjectID, row.WorkChatID)
					if workErr == nil && work.WakerID == row.OwnerWakerID && global.MainConversationService.AuthorizeWorkActor(tx, chat) == nil {
						view.WorkChatID, view.WorkSubjectType, view.WorkSubjectID = row.WorkChatID, workOwner.SubjectType, workOwner.SubjectID
						if view.Title == "" {
							view.Title = activityTitle(chat.Title)
						}
						view.RuntimeID = chat.LastRuntimeID
						if row.Status == constants.WorkTicketStatusActive && chat.ActiveExecutionID == row.CurrentExecutionID && row.CurrentExecutionID != "" {
							switch {
							case chat.WaitingApproval:
								view.State = "waiting_approval"
							case chat.WaitingElicitation:
								view.State = "waiting_input"
							}
						}
					}
				}
			}
			out = append(out, view)
		}
		return nil
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TicketID < out[j].TicketID })
	return out, nil
}

func activityTitle(title string) string {
	title, _, _ = strings.Cut(strings.TrimSpace(title), "\n")
	runes := []rune(strings.TrimSpace(title))
	if len(runes) > 96 {
		return string(runes[:96]) + "…"
	}
	return string(runes)
}
