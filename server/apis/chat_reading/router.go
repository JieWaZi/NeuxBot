// Package chat_reading 提供既有公开会话的本机阅读状态。
package chat_reading

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx"
)

// Router 注册会话阅读详情、已读和标未读接口。
var Router = ginx.NewRouter(ginx.Group("/chats"))

func init() {
	Router.Register(&GetChatReading{})
	Router.Register(&MarkChatRead{})
	Router.Register(&MarkChatUnread{})
}

func validateChat(ctx context.Context, id string) error {
	if err := global.ChatService.CatchUpReading(ctx, id); err != nil {
		return err
	}
	if global.ChatService.ReadingIncompleteReason([]string{id}) != "" {
		return status_error.ReadingProjectionIncomplete
	}
	return validatePublicChat(ctx, id)
}

func validatePublicChat(ctx context.Context, id string) error {
	owner, err := global.WorkBoardControllerManager.ResolveWorkRef(ctx, models.WorkRef{Kind: constants.WorkOwnerKindChat, ID: id})
	if err != nil {
		return err
	}
	if owner.ChatID == id {
		return nil
	}
	thread, found, err := global.ChannelThreadControllerManager.FindByChatID(ctx, id)
	if err != nil {
		return err
	}
	if found && thread.LeadChatID == id && thread.ReadyAt != nil {
		return nil
	}
	return status_error.ChatNotFound
}

// decodeReadingCommand 拒绝超出本机阅读契约的字段。
func decodeReadingCommand(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return status_error.BadRequest
	}
	var provided map[string]json.RawMessage
	if err := json.Unmarshal(raw, &provided); err != nil || len(provided["observedUnreadGeneration"]) == 0 || string(provided["observedUnreadGeneration"]) == "null" {
		return status_error.BadRequest
	}
	return nil
}
