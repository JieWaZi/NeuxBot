package controllers

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"github.com/shrewx/ginx/pkg/statuserror"
	"gorm.io/gorm"
)

func TestWakerControllerCRUD(t *testing.T) {
	db := newTestDB(t)
	controller := NewWakerController(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	waker := models.Waker{
		ID:       "11111111111111111111111111111111",
		Name:     "小瓦",
		RoleName: "产品设计师",
		Bio:      "负责验证伙伴 CRUD 纵向链路",
		Expertise: []profile.Expertise{{
			Title:       "交互设计",
			Description: "设计清晰易用的交互流程",
		}},
		WorkStyle: []string{"先验证"},
		WorkMethods: []profile.WorkMethod{{
			Name:  "设计验证",
			Steps: []string{"建立假设", "验证"},
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := controller.Create(ctx, waker); err != nil {
		t.Fatalf("create waker: %v", err)
	}
	got, err := controller.Get(ctx, waker.ID)
	if err != nil {
		t.Fatalf("get waker: %v", err)
	}
	if got.Name != waker.Name ||
		len(got.WorkMethods) != 1 ||
		got.WorkMethods[0].Name != "设计验证" {
		t.Fatalf("unexpected waker: %+v", got)
	}

	if err := controller.Update(ctx, waker.ID, models.Waker{
		Name:      "小瓦更新",
		RoleName:  waker.RoleName,
		Bio:       waker.Bio,
		UpdatedAt: now + 1,
	}); err != nil {
		t.Fatalf("update waker: %v", err)
	}
	if err := controller.UpdateProfileProjection(ctx, waker.ID, profile.Projection{
		Expertise: []profile.Expertise{{
			Title:       "需求分析",
			Description: "识别目标与约束",
		}},
		WorkStyle:   []string{"先验证"},
		WorkMethods: []profile.WorkMethod{{Name: "复盘", Steps: []string{"记录"}}},
	}, now+1); err != nil {
		t.Fatalf("update waker projection: %v", err)
	}
	list, err := controller.List(
		ctx,
		"更新",
		50,
		0,
	)
	if err != nil {
		t.Fatalf("list wakers: %v", err)
	}
	if len(list) != 1 ||
		len(list[0].Expertise) != 1 ||
		list[0].Expertise[0].Title != "需求分析" ||
		len(list[0].WorkMethods) != 1 {
		t.Fatalf("unexpected list: %+v", list)
	}
	count, err := controller.Count(
		ctx,
		"产品",
	)
	if err != nil {
		t.Fatalf("count wakers: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	if err := controller.Delete(ctx, waker.ID); err != nil {
		t.Fatalf("delete waker: %v", err)
	}
	if _, err := controller.Get(ctx, waker.ID); err != nil {
		var commonErr statuserror.CommonError
		if !errors.As(err, &commonErr) ||
			commonErr.Code() != status_error.WakerNotFound.Code() {
			t.Fatalf("deleted waker error = %v, want WakerNotFound", err)
		}
	} else {
		t.Fatal("deleted waker lookup succeeded")
	}
}

func TestWakerControllerMapsDuplicateNameToBusinessError(t *testing.T) {
	db := newTestDB(t)
	controller := NewWakerController(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	first := models.Waker{
		ID:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Name:        "Alice",
		RoleName:    "设计师",
		Expertise:   []profile.Expertise{},
		WorkStyle:   []string{},
		WorkMethods: []profile.WorkMethod{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := controller.Create(ctx, first); err != nil {
		t.Fatalf("create first waker: %v", err)
	}
	second := first
	second.ID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := controller.Create(ctx, second); !errors.Is(err, status_error.WakerNameConflict) {
		t.Fatalf("duplicate create error = %v, want WakerNameConflict", err)
	}
}

func TestSQLiteUsesDefaultJournalMode(t *testing.T) {
	db := newTestDB(t)
	var journalMode string
	if err := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatalf("read journal mode: %v", err)
	}
	if journalMode != "delete" {
		t.Fatalf("journal_mode = %q, want delete", journalMode)
	}
}

func newTestDB(t testing.TB) *gorm.DB {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "ally-test.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	return database.DB
}
