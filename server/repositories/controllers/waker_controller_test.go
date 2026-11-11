package controllers

import (
	"context"
	"errors"
	"fmt"
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

func TestWakerControllerListByIDsKeepsRequestedProfilesAndBatchesReads(t *testing.T) {
	db := newTestDB(t)
	controller := NewWakerController(db)
	ctx := context.Background()
	profiles := make([]models.Waker, 550)
	ids := make([]string, len(profiles))
	for index := range profiles {
		ids[index] = fmt.Sprintf("%032d", index+1)
		profiles[index] = models.Waker{ID: ids[index], Name: fmt.Sprintf("成员-%d", index), RoleName: "当前角色", Bio: "当前简介", Expertise: []profile.Expertise{{Title: "当前专长"}}}
	}
	if err := db.CreateInBatches(profiles, 100).Error; err != nil {
		t.Fatal(err)
	}
	queries := 0
	if err := db.Callback().Query().Before("gorm:query").Register("test_waker_batch_reads", func(*gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		// name 标识空集合、小集合与跨批查询场景。
		name string
		// requested 是实际允许读取的伙伴身份。
		requested []string
		// count 是手工确定的存在伙伴数量。
		count int
		// maximumQueries 限制批量查询次数，防止逐成员查询。
		maximumQueries int
	}{
		{name: "empty", count: 0, maximumQueries: 0},
		{name: "missing_and_duplicate", requested: []string{ids[1], "missing", ids[1]}, count: 1, maximumQueries: 1},
		{name: "many", requested: ids, count: 550, maximumQueries: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			queries = 0
			items, err := controller.ListByIDs(ctx, test.requested)
			if err != nil || len(items) != test.count || queries > test.maximumQueries {
				t.Fatalf("读取获准伙伴 count=%d queries=%d error=%v", len(items), queries, err)
			}
			for _, item := range items {
				if item.RoleName != "当前角色" || item.Bio != "当前简介" || len(item.Expertise) != 1 || item.Expertise[0].Title != "当前专长" {
					t.Fatalf("批量读取丢失当前档案: %+v", item)
				}
			}
		})
	}
	if err := db.Migrator().DropTable(&models.Waker{}); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.ListByIDs(ctx, []string{ids[0]}); !errors.Is(err, status_error.DatabaseError) {
		t.Fatalf("数据库故障被伪装成伙伴缺失: %v", err)
	}
}

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
