package main_conversations

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/global"
)

// resolveSourceInteraction 核对请求所属工作来源及原执行者当前资格，并在团队成员变更边界内答复。
func resolveSourceInteraction(ctx context.Context, kind, id, chatID string, resolve func(context.Context) error) error {
	ctx, release, err := lockMainConversationTeam(ctx, kind, id)
	if err != nil {
		return err
	}
	defer release()
	source, _, err := sourceChat(ctx, kind, id, chatID)
	if err != nil {
		return err
	}
	if err := global.MainConversationService.AuthorizeWorkActor(ctx, source); err != nil {
		return err
	}
	return resolve(ctx)
}
