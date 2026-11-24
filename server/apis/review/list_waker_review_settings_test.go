package review

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/memory"
	"github.com/JieWaZi/NeuxBot/server/internal/memory/reviewstate"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestListWakerReviewSettingsReadsOnlyPagePreferences(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "wakers.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	service, err := memory.NewService(memory.ServiceConfig{DBPath: filepath.Join(t.TempDir(), "review.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	store, err := reviewstate.Open(service.ReviewDB())
	if err != nil {
		t.Fatal(err)
	}
	wakers, worker := global.WakerControllerManager, global.ReviewWorker
	t.Cleanup(func() {
		global.WakerControllerManager, global.ReviewWorker = wakers, worker
	})
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.ReviewWorker = reviewstate.New(store, reviewstate.Ports{})

	for _, partner := range []models.Waker{
		{ID: "enabled", Name: "配置伙伴甲", Avatar: "avatar-a", UpdatedAt: 300},
		{ID: "default", Name: "配置伙伴乙", Avatar: "avatar-b", UpdatedAt: 200},
		{ID: "outside", Name: "配置伙伴丙", UpdatedAt: 100},
		{ID: "unmatched", Name: "无关伙伴", UpdatedAt: 400},
	} {
		if err := global.WakerControllerManager.Create(t.Context(), partner); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetPartner(t.Context(), "enabled", reviewstate.Partner{MemoryEnabled: true, SkillMode: reviewstate.ModeAsk}); err != nil {
		t.Fatal(err)
	}
	// 搜索命中但不在当前页的记录不可参与设置快照。
	if _, err := service.ReviewDB().ExecContext(t.Context(), "INSERT INTO review_config(id,body,revision) VALUES('waker:outside','{',1)"); err != nil {
		t.Fatal(err)
	}

	ctx, _ := gin.CreateTestContext(nil)
	want := []WakerReviewSettingsItem{
		{WakerID: "enabled", Name: "配置伙伴甲", Avatar: "avatar-a", WakerReviewInput: WakerReviewInput{MemoryEnabled: true, SkillMode: reviewstate.ModeAsk}},
		{WakerID: "default", Name: "配置伙伴乙", Avatar: "avatar-b", WakerReviewInput: WakerReviewInput{SkillMode: reviewstate.ModeOff}},
	}
	for i, expected := range want {
		request := &ListWakerReviewSettings{Query: " 配置 ", PageNum: i + 1, PageSize: 1}
		output, err := request.Output(ctx)
		if err != nil {
			t.Fatal(err)
		}
		page := output.(ListWakerReviewSettingsResponse)
		if page.PageNum != i+1 || page.PageSize != 1 || page.TotalSize != 3 || len(page.Resources) != 1 || page.Resources[0] != expected {
			t.Fatalf("unexpected page %d: %+v", i+1, page)
		}
	}
}

func TestListWakerReviewSettingsValidatesPaging(t *testing.T) {
	request := &ListWakerReviewSettings{}
	if err := request.Validate(nil); err != nil || request.PageNum != 1 || request.PageSize != 50 {
		t.Fatalf("invalid default paging: %+v, %v", request, err)
	}
	for _, request := range []*ListWakerReviewSettings{
		{PageNum: -1}, {PageSize: -1}, {PageSize: 101}, {PageNum: math.MaxInt, PageSize: 100},
	} {
		if err := request.Validate(nil); err == nil {
			t.Fatalf("invalid paging accepted: %+v", request)
		}
	}
}
