package mainconversation

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/sqlite"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func newTestService(t *testing.T) (*Service, controllers.WakerController, controllers.ChannelController, controllers.ChatController, *events.Hub) {
	t.Helper()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "main.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := sqlite.ProtectTransactions(db.DB); err != nil {
		t.Fatal(err)
	}
	hub := events.New()
	database := controllers.WithEventPublisher(db.DB, hub)
	wakers, teams := controllers.NewWakerController(database), controllers.NewChannelController(database)
	store := controllers.NewMainConversationController(database)
	tx := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database))
	return New(store, tx, wakers, teams), wakers, teams, controllers.NewChatController(database), hub
}

func TestConcurrentOpenReturnsStableSubjectConversation(t *testing.T) {
	service, wakers, _, _, _ := newTestService(t)
	ctx := context.Background()
	if err := wakers.Create(ctx, models.Waker{ID: "p", Name: "P"}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	ids := make([]string, 2)
	var group sync.WaitGroup
	for i := range ids {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			conv, err := service.Ensure(ctx, "waker", "p")
			if err != nil {
				t.Errorf("open main: %v", err)
				return
			}
			ids[i] = conv.ID
		}()
	}
	close(start)
	group.Wait()
	if ids[0] == "" || ids[0] != ids[1] {
		t.Fatalf("public addresses: %v", ids)
	}
}
