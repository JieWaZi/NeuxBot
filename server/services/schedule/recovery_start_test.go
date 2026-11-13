package schedule

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestStartAfterRecoveryChecksDurableBarrier(t *testing.T) {
	dir := t.TempDir()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(dir, "schedule.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.DB.Create(&models.AutomaticTask{ID: "task", Deleting: true}).Error; err != nil {
		t.Fatal(err)
	}
	tasks := controllers.NewAutomaticTaskController(db.DB)
	service := New(Config{
		Tasks: tasks,
		APIHandler: func(*Service, Config, string) http.Handler {
			t.Fatal("初始化屏障失败后启动了任务监听器")
			return nil
		},
		DataDirectory: dir,
	})
	t.Cleanup(service.Close)
	barrierFailure := errors.New("reading initialization marker failed")
	err = service.StartAfterRecovery(t.Context(), func(ctx context.Context) error {
		task, err := tasks.Get(ctx, "task")
		if err != nil {
			t.Fatal(err)
		}
		if task.Deleting {
			t.Fatal("调度恢复未完成就调用初始化屏障")
		}
		return barrierFailure
	})
	if !errors.Is(err, barrierFailure) || !errors.Is(service.ctx.Err(), context.Canceled) {
		t.Fatalf("初始化屏障失败未取消任务扫描：%v / %v", err, service.ctx.Err())
	}
}
