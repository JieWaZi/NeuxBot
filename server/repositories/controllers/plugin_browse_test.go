package controllers

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// pluginBrowseLog 记录驱动实际执行的 SQL，避免事务回调重置 GORM Statement 后丢失查询。
type pluginBrowseLog struct {
	// Interface 复用其余日志方法。
	logger.Interface
	// statements 是当前浏览读取实际执行的 SQL。
	statements []string
}

// LogMode 保留本测试的查询采样。
func (l *pluginBrowseLog) LogMode(logger.LogLevel) logger.Interface { return l }

// Trace 读取 GORM 已完成参数转义的真实语句。
func (l *pluginBrowseLog) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	sql, _ := query()
	l.statements = append(l.statements, sql)
}

func TestPluginBrowseNarrowReadsHaveConstantQueryBudgetAndIndexedPlans(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	resources := []models.Plugin{}
	versions := []models.PluginVersion{}
	installations := []models.WakerPlugin{}
	for i := 0; i < 205; i++ {
		id := fmt.Sprintf("p-%03d", i)
		resources = append(resources, models.Plugin{ID: id, Name: id, SourceKey: "source:" + id, UpdatedAt: 1})
		versions = append(versions, models.PluginVersion{ID: "v-" + id, PluginID: id, Version: "1.0.0", CreatedAt: 1, Package: plugins.Package{Manifest: plugins.Manifest{Name: id}, Presentation: plugins.Presentation{Icons: plugins.ThemeValues{Default: "default-" + id, Dark: "dark-" + id}, DisplayNames: map[string]string{"en": id, "zh-CN": "办公" + id}, LocalizedDescriptions: map[string]string{"zh-CN": "说明" + id}}, Servers: []plugins.Server{{Key: "secret-server", Command: "must-not-be-read", Env: map[string]string{"TOKEN": "hidden"}}}}})
		versions = append(versions, models.PluginVersion{ID: "prepared-" + id, PluginID: id, Version: "2.0.0", CreatedAt: 2, Package: plugins.Package{Manifest: plugins.Manifest{Name: "prepared-only"}}})
		installations = append(installations, models.WakerPlugin{ID: "i-" + id, WakerID: "w1", PluginID: id, VersionID: "v-" + id, Enabled: false})
	}
	for _, rows := range []any{&resources, &versions, &installations} {
		if err := db.Create(rows).Error; err != nil {
			t.Fatal(err)
		}
	}
	log := &pluginBrowseLog{Interface: logger.Default}
	measured := db.Session(&gorm.Session{Logger: log})
	c := NewPluginController(measured)
	// 与生产浏览接口相同，关联窗口必须在同一个短读事务中读取。
	if err := NewTransactionController(dbhelper.NewGormTransactionManager(measured)).Transaction(ctx, func(ctx context.Context) error {
		for _, size := range []int{1, 9, 100} {
			log.statements = nil
			items, total, err := c.BrowseInstalled(ctx, "办公", 0, size)
			if err != nil || total != 205 || len(items) != size {
				t.Fatalf("wrong browse window size=%d total=%d err=%v", len(items), total, err)
			}
			ids := []string{}
			keys := []string{}
			for _, item := range items {
				ids = append(ids, item.ID)
				keys = append(keys, item.SourceKey)
			}
			states, err := c.ReadBrowseStates(ctx, ids)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range items {
				state := states[item.ID]
				if state.InstalledCount != 1 || len(state.Versions) != 1 || state.Versions[0].ID != "v-"+item.ID || state.Versions[0].DisplayNames["zh-CN"] != "办公"+item.ID || state.Versions[0].Icons != (plugins.ThemeValues{Default: "default-" + item.ID, Dark: "dark-" + item.ID}) {
					t.Fatalf("narrow version projection failed: %+v", state)
				}
			}
			sources, err := c.InstalledSources(ctx, keys)
			if err != nil || len(sources) != size || len(log.statements) != 5 {
				t.Fatalf("query count grows with window: sources=%d statements=%d err=%v", len(sources), len(log.statements), err)
			}
			for _, statement := range log.statements {
				lower := strings.ToLower(statement)
				if strings.Contains(lower, "plugin_configurations") || strings.Contains(lower, "wakers") || strings.Contains(lower, " join ") {
					t.Fatalf("browse read crossed its persistence boundary: %s", statement)
				}
				if strings.HasPrefix(lower, "select * from `plugin_versions`") {
					t.Fatalf("browse fetched complete package snapshots: %s", statement)
				}
				var plans []struct {
					// Detail 是 SQLite 的真实查询计划说明。
					Detail string
				}
				if err := db.Raw("EXPLAIN QUERY PLAN " + statement).Scan(&plans).Error; err != nil {
					t.Fatal(err)
				}
				joined := ""
				for _, plan := range plans {
					joined += plan.Detail + "\n"
				}
				if strings.Contains(joined, "SCAN plugin_versions") || strings.Contains(joined, "SCAN plugins") {
					t.Fatalf("browse lost indexed resource/version lookup: %s", joined)
				}
				if size == 100 {
					t.Logf("SQL plan: %s", joined)
				}
			}
		}
		_, total, err := c.BrowseInstalled(ctx, "prepared-only", 0, 9)
		if err != nil || total != 0 {
			t.Fatal("prepared history participated in installed search", total, err)
		}
		_, total, err = c.BrowseInstalled(ctx, "%", 0, 9)
		if err != nil || total != 0 {
			t.Fatal("literal search was interpreted as SQL wildcard", total, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
