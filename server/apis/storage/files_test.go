package storage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	filesystem "github.com/JieWaZi/NeuxBot/server/internal/storage"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	service "github.com/JieWaZi/NeuxBot/server/services/storage"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

type storageFilesChats struct {
	// Module 保留与本测试无关的会话接口。
	chatservice.Module
	// repository 保存测试会话的活动状态。
	repository controllers.ChatController
}

func (s storageFilesChats) GetChatSummary(ctx context.Context, id string) (chatservice.ChatSummary, error) {
	chat, err := s.repository.Get(ctx, id)
	return chatservice.ChatSummary{Chat: chat}, err
}

func TestStorageFilesScopeAndDelete(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/files", nil).WithContext(t.Context())
	db, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "test.db"), Migrate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	wakers := controllers.NewWakerController(db.DB)
	chats := controllers.NewChatController(db.DB)
	for _, id := range []string{"lumi", "nova"} {
		if err := wakers.Create(ctx, models.Waker{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	for _, chat := range []models.Chat{
		{ID: "first", WakerID: "lumi", Title: "First"},
		{ID: "second", WakerID: "lumi", Title: "Second", ActiveExecutionID: "running"},
		{ID: "foreign", WakerID: "nova", Title: "Foreign"},
		{ID: "internal", WakerID: "lumi", Title: "Internal", HiddenFromWakerHistory: true, ActiveExecutionID: "scheduled"},
		{ID: "legacy", WakerID: "nova", Title: "Legacy", ForkSourceChatID: "deleted-parent"},
	} {
		if err := chats.Create(ctx, chat); err != nil {
			t.Fatal(err)
		}
	}
	managed := t.TempDir()
	if err := workspace.EnsureAgentWorkspace(filepath.Join(managed, "Agents/lumi"), "lumi"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"Agents/lumi/draft.md",
		"Conversations/first/attachments/message/upload.png",
		"Conversations/first/attachments/unowned/untracked.png",
		"Conversations/second/attachments/message/upload.png",
		"Conversations/foreign/attachments/message/upload.png",
		"Conversations/legacy/attachments/ancestor/upload.png",
		"Conversations/legacy/attachments/ancestor/extra.png",
		"Conversations/internal/attachments/message/upload.png",
		"Conversations/first/log/journal.jsonl",
	} {
		target := filepath.Join(managed, path)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"first", "second"} {
		marker, err := json.Marshal(map[string]string{
			"kind": "message-attachments", "chatId": id, "messageId": "message",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(managed, "Conversations", id, "attachments/message/workspace-owner.json"), marker, 0600); err != nil {
			t.Fatal(err)
		}
	}
	current := service.New(t.Context(), service.Config{
		Roots:         filesystem.Roots{Data: t.TempDir(), Managed: managed},
		LookupChats:   chats.ListStorageChats,
		CheckFileIdle: func(context.Context, string, string) error { return nil },
		InheritedAttachments: func(_ context.Context, id string) (map[string]string, error) {
			if id != "legacy" {
				return nil, nil
			}
			return map[string]string{"attachments/ancestor/upload.png": "ancestor"}, nil
		},
	})
	previousChatService := global.ChatService
	global.ChatService = storageFilesChats{repository: chats}
	previous, previousWakers, previousChats := global.StorageService, global.WakerControllerManager, global.ChatControllerManager
	global.StorageService, global.WakerControllerManager, global.ChatControllerManager = current, wakers, chats
	t.Cleanup(func() {
		current.Close()
		global.ChatService = previousChatService
		global.StorageService, global.WakerControllerManager, global.ChatControllerManager = previous, previousWakers, previousChats
	})

	result, err := (&ListStorageFiles{Kind: "wakers", ID: "lumi"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.(StorageFilePage); got.Total != 3 || got.Title != "lumi" || got.TotalBytes != 12 {
		t.Fatalf("waker files=%+v", got)
	}
	result, err = (&ListStorageFiles{Kind: "chats", ID: "first", Search: "PNG"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := result.(StorageFilePage)
	if got.Total != 1 || got.Items[0].ChatTitle != "First" || got.Items[0].MessageID != "message" {
		t.Fatalf("chat files=%+v", got)
	}
	file := got.Items[0]
	if _, err := (&DeleteStorageFile{Data: DeleteStorageFileInput{
		Kind: "wakers", ID: "nova", FileID: file.ID, Version: file.Version,
	}}).Output(ctx); !errors.Is(err, status_error.WorkspaceResourceNotFound) {
		t.Fatalf("cross-owner delete: %v", err)
	}
	result, err = (&ListStorageFiles{Kind: "chats", ID: "second"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	busy := result.(StorageFilePage).Items[0]
	if _, err := (&DeleteStorageFile{Data: DeleteStorageFileInput{
		Kind: "chats", ID: "second", FileID: busy.ID, Version: busy.Version,
	}}).Output(ctx); !errors.Is(err, status_error.ChatHasActiveExecution) {
		t.Fatalf("active chat delete: %v", err)
	}
	if _, err := (&DeleteStorageFile{Data: DeleteStorageFileInput{
		Kind: "chats", ID: "first", FileID: file.ID, Version: file.Version,
	}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	result, err = (&ListStorageFiles{Kind: "chats", ID: "first"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.(StorageFilePage); got.Total != 0 {
		t.Fatalf("stale file list=%+v", got)
	}
	manifest, err := os.ReadFile(filepath.Join(managed, "Conversations/first/removed-chat-files.json"))
	if err != nil || string(manifest) != `["attachments/message/upload.png"]` {
		t.Fatalf("missing attachment marker: %s %v", manifest, err)
	}
	if _, err := os.Stat(filepath.Join(managed, "Conversations/first/log/journal.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Model(&models.Chat{}).Where("id = ?", "second").Update("active_execution_id", "").Error; err != nil {
		t.Fatal(err)
	}
	result, err = (&ListStorageFiles{Kind: "wakers", ID: "lumi", Search: "draft.md"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	draft := result.(StorageFilePage).Items[0]
	if _, err := (&DeleteStorageFile{Data: DeleteStorageFileInput{
		Kind: "wakers", ID: "lumi", FileID: draft.ID, Version: draft.Version,
	}}).Output(ctx); !errors.Is(err, status_error.WakerHasRunningWork) {
		t.Fatalf("active automatic task did not protect partner file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(managed, "Agents/lumi/draft.md")); err != nil {
		t.Fatal(err)
	}
	result, err = (&ListStorageFiles{Kind: "chats", ID: "legacy"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	legacy := result.(StorageFilePage)
	if legacy.Total != 1 || legacy.Items[0].MessageID != "ancestor" || legacy.Items[0].RelativePath != "attachments/ancestor/upload.png" {
		t.Fatalf("legacy fork authorized an untracked file or hid its original upload: %+v", legacy)
	}
	if _, err := (&DeleteStorageFile{Data: DeleteStorageFileInput{
		Kind: "chats", ID: "legacy", FileID: legacy.Items[0].ID, Version: legacy.Items[0].Version,
	}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(managed, "Conversations/legacy/attachments/ancestor/extra.png")); err != nil {
		t.Fatal(err)
	}
}
