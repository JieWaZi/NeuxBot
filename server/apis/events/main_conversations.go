package events

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	eventhub "github.com/JieWaZi/NeuxBot/server/internal/notification/events"
)

// validMainConversationSubject 限定每个主会话订阅只有一个确切主体。
func validMainConversationSubject(resource eventhub.Resource) bool {
	return resource.ID != "" && resource.ParentID == "" && (resource.Kind == "waker" || resource.Kind == "team")
}

// validateMainConversationSubject 检查订阅对应的伙伴或团队，订阅不创建会话。
func validateMainConversationSubject(ctx context.Context, resource eventhub.Resource) error {
	switch resource.Kind {
	case "waker":
		_, err := global.WakerControllerManager.Get(ctx, resource.ID)
		return err
	case "team":
		_, err := global.ChannelControllerManager.Get(ctx, resource.ID)
		return err
	default:
		return status_error.BadRequest
	}
}
