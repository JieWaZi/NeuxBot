package channels

import (
	"context"
	"encoding/json"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// appendChannelEvent 在当前用例事务中追加一项结构化团队事件。
func appendChannelEvent(
	ctx context.Context,
	channelID string,
	threadID string,
	eventType constants.ChannelEventType,
	payload interface{},
) (models.ChannelEvent, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		logx.Errorf(
			"marshal channel API event payload failed: channel_id=%s thread_id=%s event_type=%s error=%v",
			channelID,
			threadID,
			eventType,
			err,
		)
		return models.ChannelEvent{}, status_error.InternalServerError
	}
	return global.ChannelThreadControllerManager.AppendEvent(ctx, models.ChannelEvent{
		ID:        util.New(),
		ChannelID: channelID,
		ThreadID:  threadID,
		Type:      eventType,
		Payload:   data,
		CreatedAt: time.Now().UnixMilli(),
	})
}
