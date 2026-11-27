package global

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// validateWorkIntent 在执行准备前分别核对主会话来源和知识维护工作的准入。
func validateWorkIntent(ctx context.Context, intent execution.WorkIntent) error {
	if err := MainConversationService.ValidateWorkEntry(ctx, intent.Chat.ChatID, intent.Options.EntryPoint == execution.MainConversationEntryPoint || intent.Options.EntryPoint == execution.MainConversationSourceEntryPoint); err != nil {
		return err
	}
	if KnowledgeService == nil {
		return nil
	}
	return KnowledgeService.ValidateWorkIntent(ctx, intent)
}
