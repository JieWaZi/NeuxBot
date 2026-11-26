package controllers

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestWakerNavigationGroupImportRollsBackGroupsAndMembership(t *testing.T) {
	db := newTestDB(t)
	hub := events.New()
	t.Cleanup(hub.Close)
	wakers := NewWakerController(WithEventPublisher(db, hub))
	transactions := NewTransactionController(dbhelper.NewGormTransactionManager(db))
	if err := wakers.Create(t.Context(), models.Waker{ID: "owner", Name: "Owner"}); err != nil {
		t.Fatal(err)
	}
	updates := hub.Subscribe("entities", events.Resource{Kind: "waker_navigation_groups", ID: "navigation"})
	t.Cleanup(updates.Close)
	groups := []models.WakerNavigationGroupMembers{{Name: "Updates", WakerIDs: []string{"owner"}}}
	if err := transactions.Transaction(t.Context(), func(ctx context.Context) error {
		if err := wakers.ImportNavigationGroups(ctx, groups, nil); err != nil {
			return err
		}
		if len(updates.Events) != 0 {
			t.Error("group change published before commit")
		}
		return status_error.BadRequest
	}); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("transaction error = %v, want BadRequest", err)
	}
	actual, _, err := wakers.ListNavigationGroups(t.Context())
	if err != nil || len(actual) != 0 {
		t.Fatalf("rolled-back groups = %#v, %v", actual, err)
	}
	owner, err := wakers.Get(t.Context(), "owner")
	if err != nil || owner.NavigationGroupID != "" || len(updates.Events) != 0 {
		t.Fatalf("rollback left group membership or event: group = %q, events = %d, err = %v", owner.NavigationGroupID, len(updates.Events), err)
	}
}

func TestWakerNavigationOrderSurvivesDatabaseReopen(t *testing.T) {
	config := conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "navigation.db"), Migrate: true}
	db, err := dbhelper.NewDB(config)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	wakers := NewWakerController(db.DB)
	for _, waker := range []models.Waker{{ID: "first", Name: "First", CreatedAt: 1}, {ID: "second", Name: "Second", CreatedAt: 2}, {ID: "third", Name: "Third", CreatedAt: 3}} {
		if err := wakers.Create(t.Context(), waker); err != nil {
			t.Fatal(err)
		}
	}
	transactions := NewTransactionController(dbhelper.NewGormTransactionManager(db.DB))
	if err := transactions.Transaction(t.Context(), func(ctx context.Context) error {
		return wakers.ImportNavigationGroups(ctx, nil, []string{"third", "second"})
	}); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := dbhelper.NewDB(config)
	if err != nil {
		t.Fatal(err)
	}
	sqlReopened, err := reopened.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlReopened.Close() })
	wakers = NewWakerController(reopened.DB)
	groups, ungrouped, err := wakers.ListNavigationGroups(t.Context())
	if err != nil || len(groups) != 0 || !slices.Equal(ungrouped, []string{"third", "second", "first"}) {
		t.Fatalf("reopened groups = %#v, ungrouped = %#v, error = %v", groups, ungrouped, err)
	}
}
