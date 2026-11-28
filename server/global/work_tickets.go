package global

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
)

// WorkTicketService 管理原会话交办、尝试与工单报告。
var WorkTicketService *workticket.Service

func loadWorkTickets() {
	hub := EventHub
	WorkTicketService = workticket.New(controllers.NewWorkTicketController(DB), TransactionControllerManager, func(_ context.Context, address workticket.Address) {
		resource := events.Resource{Kind: string(address.Kind), ID: address.ID}
		_ = events.Emit(hub, constants.WorkTicketEventTopic, constants.WorkTicketChangedEvent, resource, struct{}{})
	}, MainConversationService.PublishDeletedOriginReport)
}

// ValidateWorkTicketOrigin 核验 HTTP 与事件订阅引用的原会话仍可访问。
func ValidateWorkTicketOrigin(ctx context.Context, address workticket.Address) error {
	switch address.Kind {
	case constants.WorkTicketOriginChat:
		chat, err := ChatControllerManager.Get(ctx, address.ID)
		if err == nil && chat.HiddenFromWakerHistory {
			return status_error.ChatNotFound.WithParams(map[string]interface{}{"ChatID": address.ID})
		}
		return err
	case constants.WorkTicketOriginMain:
		conversation, err := MainConversationControllerManager.Get(ctx, address.ID)
		if err != nil {
			return err
		}
		switch conversation.SubjectType {
		case "waker":
			_, err = WakerControllerManager.Get(ctx, conversation.SubjectID)
		case "team":
			_, err = ChannelControllerManager.Get(ctx, conversation.SubjectID)
		default:
			return status_error.BadRequest
		}
		return err
	default:
		return status_error.BadRequest
	}
}
