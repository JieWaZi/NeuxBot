package storage

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	filesystem "github.com/JieWaZi/NeuxBot/server/internal/storage"
	service "github.com/JieWaZi/NeuxBot/server/services/storage"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

func TestStorageUsageOperatorMeasuresActualRoots(t *testing.T) {
	data, managed := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(data, "bot.db"), []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(managed, "Conversations", "chat", "attachments")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "image.png"), []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}
	previous := global.StorageService
	current := service.New(t.Context(), service.Config{Roots: filesystem.Roots{Data: data, Managed: managed}})
	global.StorageService = current
	t.Cleanup(func() { current.Close(); global.StorageService = previous })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/usage?force=true", nil)
	operator := &GetStorageUsage{}
	if err := ginx.ParameterBinding(ctx, operator, ginx.GetOperatorTypeInfo(reflect.TypeOf(operator))); err != nil {
		t.Fatal(err)
	}
	if !operator.Force {
		t.Fatal("force not bound")
	}
	result, err := operator.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := result.(UsageResponse)
	if got.TotalBytes != 8 || !got.Complete || len(got.Categories) != 2 {
		t.Fatalf("usage=%+v", got)
	}
	if got.Categories[0].Category != constants.StorageCategoryAppData || got.Categories[0].Bytes != 3 ||
		got.Categories[1].Category != constants.StorageCategoryConversations || got.Categories[1].Bytes != 5 {
		t.Fatalf("categories=%+v", got.Categories)
	}
}

func TestStoragePolicyPreviewBindingAcceptsOmittedChatIDs(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/expired-chats/preview", strings.NewReader(`{"retentionDays":15}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	operator := &PreviewExpiredChats{}
	if err := ginx.ParameterBinding(ctx, operator, ginx.GetOperatorTypeInfo(reflect.TypeOf(operator))); err != nil {
		t.Fatal(err)
	}
	if operator.Data.RetentionDays != 15 || len(operator.Data.ChatIDs) != 0 {
		t.Fatalf("bound preview=%+v", operator.Data)
	}
}
