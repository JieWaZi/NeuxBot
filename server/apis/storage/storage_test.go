package storage

import (
	"errors"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	filesystem "github.com/JieWaZi/NeuxBot/server/internal/storage"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	service "github.com/JieWaZi/NeuxBot/server/services/storage"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
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

func TestStorageEntityRankingScopesChatPagination(t *testing.T) {
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
	for _, waker := range []models.Waker{{ID: "lumi", Name: "Lumi"}, {ID: "nova", Name: "Nova"}} {
		if err := wakers.Create(t.Context(), waker); err != nil {
			t.Fatal(err)
		}
	}
	managed := t.TempDir()
	for _, chat := range []models.Chat{
		{ID: "a", WakerID: "lumi", Title: "First tie"},
		{ID: "b", WakerID: "lumi", Title: "Second tie"},
		{ID: "c", WakerID: "lumi", Title: "Largest"},
		{ID: "other", WakerID: "nova", Title: "Other"},
		{ID: "hidden", WakerID: "lumi", HiddenFromWakerHistory: true},
		{ID: "unmeasured", WakerID: "lumi"},
	} {
		if err := chats.Create(t.Context(), chat); err != nil {
			t.Fatal(err)
		}
	}
	for id, bytes := range map[string]int{"a": 5, "b": 5, "c": 7, "other": 9, "hidden": 10} {
		directory := filepath.Join(managed, "Conversations", id, "attachments")
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "image.png"), make([]byte, bytes), 0600); err != nil {
			t.Fatal(err)
		}
	}
	fileB := filepath.Join(managed, "Conversations/b/attachments/image.png")
	if err := os.Remove(fileB); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(managed, "Conversations/a/attachments/image.png"), fileB); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	current := service.New(t.Context(), service.Config{
		Roots:       filesystem.Roots{Data: t.TempDir(), Managed: managed},
		LookupChats: chats.ListStorageChats,
		Now:         func() time.Time { return time.Unix(1, 0) },
	})
	previous, previousWakers := global.StorageService, global.WakerControllerManager
	global.StorageService, global.WakerControllerManager = current, wakers
	t.Cleanup(func() {
		current.Close()
		global.StorageService, global.WakerControllerManager = previous, previousWakers
	})

	list := func(query string) (EntityPage, error) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("GET", "/entities?"+query, nil).WithContext(t.Context())
		operator := &ListStorageEntities{}
		if err := ginx.ParameterBinding(ctx, operator, ginx.GetOperatorTypeInfo(reflect.TypeOf(operator))); err != nil {
			return EntityPage{}, err
		}
		if err := operator.Validate(ctx); err != nil {
			return EntityPage{}, err
		}
		result, err := operator.Output(ctx)
		if err != nil {
			return EntityPage{}, err
		}
		return result.(EntityPage), nil
	}

	globalPage, err := list("kind=chats&limit=1")
	if err != nil || globalPage.Total != 4 || len(globalPage.Items) != 1 || globalPage.Items[0].ID != "other" || globalPage.NextCursor == "" {
		t.Fatalf("global page = %+v, %v", globalPage, err)
	}
	globalNext, err := list("kind=chats&limit=1&cursor=" + url.QueryEscape(globalPage.NextCursor))
	if err != nil || globalNext.Total != 4 || len(globalNext.Items) != 1 || globalNext.Items[0].ID != "c" {
		t.Fatalf("next global page = %+v, %v", globalNext, err)
	}
	first, err := list("kind=chats&wakerId=lumi&limit=2")
	if err != nil || first.Total != 3 || len(first.Items) != 2 || first.Items[0].ID != "c" ||
		first.Items[0].Bytes != 7 || first.Items[0].WakerName != "Lumi" || first.Items[1].ID != "a" || first.NextCursor == "" {
		t.Fatalf("scoped first page = %+v, %v", first, err)
	}
	second, err := list("kind=chats&wakerId=lumi&limit=2&cursor=" + url.QueryEscape(first.NextCursor))
	if err != nil || second.Total != 3 || len(second.Items) != 1 || second.Items[0].ID != "b" || second.Items[0].Bytes != 5 || second.NextCursor != "" {
		t.Fatalf("scoped second page = %+v, %v", second, err)
	}
	snapshot, err := current.Usage(t.Context(), false)
	if err != nil || snapshot.Measurement.Bytes != 31 {
		t.Fatalf("physical snapshot should count hard link bytes once: %+v %v", snapshot.Measurement, err)
	}
	scopedCursor := url.QueryEscape(first.NextCursor)
	for _, query := range []string{
		"kind=chats&wakerId=nova&cursor=" + scopedCursor,
		"kind=chats&cursor=" + scopedCursor,
		"kind=chats&wakerId=lumi&cursor=" + url.QueryEscape(globalPage.NextCursor),
		"kind=wakers&wakerId=lumi",
	} {
		if _, err := list(query); !errors.Is(err, status_error.BadRequest) {
			t.Errorf("%s: want bad request, got %v", query, err)
		}
	}
	current.Invalidate()
	if _, err := list("kind=chats&wakerId=lumi&cursor=" + scopedCursor); !errors.Is(err, status_error.StorageSnapshotExpired) {
		t.Errorf("old snapshot: want expired cursor, got %v", err)
	}
}
