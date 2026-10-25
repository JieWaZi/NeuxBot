package controllers

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestKnowledgeTaskConfigurationPersistsFirstSelectionWithoutChat(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	controller := NewKnowledgeController(db)
	first := models.KnowledgeTaskConfiguration{ID: "task", Backend: "local", ConnectionID: "local", KnowledgeBaseID: "library", RuntimeID: "codex", AccountID: "account", Model: "first", Message: "按主题归纳"}
	frozen, err := controller.FreezeTaskConfiguration(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	changed := first
	changed.Model = "later"
	replay, err := NewKnowledgeController(db).FreezeTaskConfiguration(ctx, changed)
	if err != nil || replay.Model != frozen.Model || replay.AccountID != first.AccountID {
		t.Fatalf("首次模型选择漂移: %+v %v", replay, err)
	}
	changed.KnowledgeBaseID = "other-library"
	if _, err := controller.FreezeTaskConfiguration(ctx, changed); !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatalf("配置重放更换知识库: %v", err)
	}
	var chats int64
	if err := db.Model(&models.Chat{}).Count(&chats).Error; err != nil || chats != 0 {
		t.Fatalf("冻结模型选择创建聊天: %d %v", chats, err)
	}
}
