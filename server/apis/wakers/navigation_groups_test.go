package wakers

import (
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestWakerNavigationGroupsKeepEmptyGroupsAfterMovesAndDeletion(t *testing.T) {
	wakers, changes := navigationGroupsFixture(t)
	assertNavigationGroups(t, nil)
	for _, body := range []string{`{"name":"  Research  "}`, `{"name":"Research"}`, `{"name":"Updates"}`, `{"name":""}`} {
		if _, err := executeNavigationGroupRequest(&MoveWakerToNavigationGroup{}, body, "owner"); err != nil {
			t.Fatal(err)
		}
	}
	if len(changes.Events) != 3 {
		t.Fatalf("move changes = %d, want 3", len(changes.Events))
	}
	if _, err := executeNavigationGroupRequest(&MoveWakerToNavigationGroup{}, `{"name":"Research"}`, "member"); err != nil {
		t.Fatal(err)
	}
	member, err := wakers.Get(t.Context(), "member")
	if err != nil || member.NavigationGroupID == "" || member.UpdatedAt != 12 {
		t.Fatalf("moving member changed profile time or lost membership: %+v, %v", member, err)
	}
	if err := wakers.Create(t.Context(), models.Waker{ID: "copy", Name: "Copy", NavigationGroupID: member.NavigationGroupID}); err != nil {
		t.Fatal(err)
	}
	assertNavigationGroups(t, []navigationGroupResult{{"Research", []string{"member", "copy"}}, {"Updates", []string{}}})
	for _, id := range []string{"member", "copy"} {
		if err := wakers.Delete(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	assertNavigationGroups(t, []navigationGroupResult{{"Research", []string{}}, {"Updates", []string{}}})
	if len(changes.Events) != 7 {
		t.Fatalf("total change events = %d, want 7", len(changes.Events))
	}
	for len(changes.Events) > 0 {
		if event := <-changes.Events; event.Type != "entities.changed" {
			t.Fatalf("unexpected event %+v", event)
		}
	}
}

func TestWakerNavigationGroupImportSeedsOnceAndFiltersMembership(t *testing.T) {
	_, changes := navigationGroupsFixture(t)
	first := `{"groups":[{"name":"  Updates ","wakerIds":["missing","owner","owner"]},{"name":"Updates","wakerIds":["member"]},{"name":"Research","wakerIds":["owner","member"]},{"name":"Empty","wakerIds":[]}]}`
	if _, err := executeNavigationGroupRequest(&ImportWakerNavigationGroups{}, first, ""); err != nil {
		t.Fatal(err)
	}
	second := `{"groups":[{"name":"Later","wakerIds":["member"]}]}`
	if _, err := executeNavigationGroupRequest(&ImportWakerNavigationGroups{}, second, ""); err != nil {
		t.Fatal(err)
	}
	assertNavigationGroups(t, []navigationGroupResult{{"Updates", []string{"owner"}}, {"Research", []string{"member"}}, {"Empty", []string{}}})
	if len(changes.Events) != 1 {
		t.Fatalf("change events = %d, want one for the whole import", len(changes.Events))
	}
}

func TestWakerNavigationGroupWritesRejectInvalidInputBeforeCreatingGroups(t *testing.T) {
	_, changes := navigationGroupsFixture(t)
	for _, body := range []string{`{}`, `{"name":null}`, `{"name":5}`} {
		if _, err := executeNavigationGroupRequest(&MoveWakerToNavigationGroup{}, body, "owner"); err == nil {
			t.Fatalf("accepted move body %s", body)
		}
	}
	for _, body := range []string{`{}`, `{"groups":null}`, `{"groups":[{"name":"Valid","wakerIds":[]},{"name":" ","wakerIds":[]}]}`, `{"groups":[{"name":"Valid"}]}`, `{"groups":[{"name":"Valid","wakerIds":[null]}]}`} {
		if _, err := executeNavigationGroupRequest(&ImportWakerNavigationGroups{}, body, ""); err == nil {
			t.Fatalf("accepted import body %s", body)
		}
	}
	if _, err := executeNavigationGroupRequest(&MoveWakerToNavigationGroup{}, `{"name":"Not created"}`, "missing"); !util.HasStatusCode(err, status_error.WakerNotFound) {
		t.Fatalf("move missing Waker error = %v, want WakerNotFound", err)
	}
	for _, body := range []string{`{"placement":"before"}`, `{"targetWakerId":"member","placement":"around"}`, `{"targetWakerId":"owner","placement":"after"}`} {
		if _, err := executeNavigationGroupRequest(&PlaceWakerInNavigation{}, body, "owner"); err == nil {
			t.Fatalf("accepted placement body %s", body)
		}
	}
	if _, err := executeNavigationGroupRequest(&PlaceWakerInNavigation{}, `{"targetWakerId":"missing","placement":"after"}`, "owner"); !util.HasStatusCode(err, status_error.WakerNotFound) {
		t.Fatalf("place next to missing Waker error = %v, want WakerNotFound", err)
	}
	assertNavigationGroups(t, nil)
	assertUngroupedWakers(t, []string{"owner", "member"})
	if len(changes.Events) != 0 {
		t.Fatal("rejected group write published a change")
	}
}

func TestConcurrentWakerNavigationGroupMovesShareTheSameGroup(t *testing.T) {
	_, changes := navigationGroupsFixture(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range []string{"owner", "member"} {
		go func(id string) {
			<-start
			_, err := executeNavigationGroupRequest(&MoveWakerToNavigationGroup{}, `{"name":"Updates"}`, id)
			results <- err
		}(id)
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Errorf("concurrent move error = %v", err)
		}
	}
	value, err := executeNavigationGroupRequest(&ListWakerNavigationGroups{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	groups := value.(ListWakerNavigationGroupsResponse).Groups
	if len(groups) != 1 || groups[0].Name != "Updates" {
		t.Fatalf("concurrent moves created different groups: %#v", groups)
	}
	members := slices.Clone(groups[0].WakerIDs)
	slices.Sort(members)
	if !slices.Equal(members, []string{"member", "owner"}) {
		t.Fatalf("concurrent move membership = %#v", members)
	}
	if len(changes.Events) != 2 {
		t.Fatalf("change events = %d, want two committed moves", len(changes.Events))
	}
}

func TestWakerNavigationPlacementPersistsOrderAcrossGroups(t *testing.T) {
	wakers, changes := navigationGroupsFixture(t)
	for _, waker := range []models.Waker{{ID: "third", Name: "Third", CreatedAt: 3}, {ID: "fourth", Name: "Fourth", CreatedAt: 4}} {
		if err := wakers.Create(t.Context(), waker); err != nil {
			t.Fatal(err)
		}
	}
	assertUngroupedWakers(t, []string{"owner", "member", "third", "fourth"})
	if _, err := executeNavigationGroupRequest(&ImportWakerNavigationGroups{}, `{"groups":[{"name":"Research","wakerIds":["third","owner","member"]}]}`, ""); err != nil {
		t.Fatal(err)
	}
	assertNavigationGroups(t, []navigationGroupResult{{"Research", []string{"third", "owner", "member"}}})
	place := func(source, body string) {
		t.Helper()
		if _, err := executeNavigationGroupRequest(&PlaceWakerInNavigation{}, body, source); err != nil {
			t.Fatal(err)
		}
	}
	place("member", `{"targetWakerId":"third","placement":"before"}`)
	assertNavigationGroups(t, []navigationGroupResult{{"Research", []string{"member", "third", "owner"}}})
	place("fourth", `{"targetWakerId":"third","placement":"after"}`)
	assertNavigationGroups(t, []navigationGroupResult{{"Research", []string{"member", "third", "fourth", "owner"}}})
	assertUngroupedWakers(t, []string{})
	if _, err := executeNavigationGroupRequest(&MoveWakerToNavigationGroup{}, `{"name":""}`, "third"); err != nil {
		t.Fatal(err)
	}
	place("owner", `{"targetWakerId":"third","placement":"before"}`)
	assertNavigationGroups(t, []navigationGroupResult{{"Research", []string{"member", "fourth"}}})
	assertUngroupedWakers(t, []string{"owner", "third"})
	if len(changes.Events) != 7 {
		t.Fatalf("change events = %d, want 7", len(changes.Events))
	}
}

func TestWakerNavigationImportPreservesUngroupedOrderWithoutNamedGroups(t *testing.T) {
	wakers, changes := navigationGroupsFixture(t)
	if _, err := executeNavigationGroupRequest(&ImportWakerNavigationGroups{}, `{"groups":[],"wakerOrder":["missing","member","member","owner"]}`, ""); err != nil {
		t.Fatal(err)
	}
	assertUngroupedWakers(t, []string{"member", "owner"})
	if err := wakers.Create(t.Context(), models.Waker{ID: "new", Name: "New", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := executeNavigationGroupRequest(&ImportWakerNavigationGroups{}, `{"groups":[{"name":"Ignored","wakerIds":["member"]}],"wakerOrder":["owner","member"]}`, ""); err != nil {
		t.Fatal(err)
	}
	assertNavigationGroups(t, nil)
	assertUngroupedWakers(t, []string{"member", "owner", "new"})
	if err := wakers.Delete(t.Context(), "new"); err != nil {
		t.Fatal(err)
	}
	assertUngroupedWakers(t, []string{"member", "owner"})
	if len(changes.Events) != 3 {
		t.Fatalf("change events = %d, want 3", len(changes.Events))
	}
}

func assertUngroupedWakers(t *testing.T, want []string) {
	t.Helper()
	value, err := executeNavigationGroupRequest(&ListWakerNavigationGroups{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	response := value.(ListWakerNavigationGroupsResponse)
	if !reflect.DeepEqual(response.UngroupedWakerIDs, want) {
		t.Fatalf("ungrouped = %#v, want %#v", response.UngroupedWakerIDs, want)
	}
}

type navigationGroupResult struct {
	// Name 是返回的分组名称。
	Name string
	// WakerIDs 是返回的伙伴标识数组。
	WakerIDs []string
}

func assertNavigationGroups(t *testing.T, want []navigationGroupResult) {
	t.Helper()
	value, err := executeNavigationGroupRequest(&ListWakerNavigationGroups{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	response, ok := value.(ListWakerNavigationGroupsResponse)
	if !ok || response.Groups == nil {
		t.Fatalf("list response did not include a group array: %#v", value)
	}
	var actual []navigationGroupResult
	for _, group := range response.Groups {
		actual = append(actual, navigationGroupResult{group.Name, group.WakerIDs})
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("groups = %#v, want %#v", actual, want)
	}
}

func executeNavigationGroupRequest(operator ginx.HandleOperator, body, wakerID string) (interface{}, error) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(operator.Method(), operator.Path(), strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "wakerId", Value: wakerID}}
	if err := ginx.ParameterBinding(ctx, operator, ginx.GetOperatorTypeInfo(reflect.TypeOf(operator))); err != nil {
		return nil, err
	}
	if err := operator.Validate(ctx); err != nil {
		return nil, err
	}
	return operator.Output(ctx)
}

func navigationGroupsFixture(t *testing.T) (controllers.WakerController, events.Subscription) {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "navigation-groups.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	hub := events.New()
	t.Cleanup(hub.Close)
	db := controllers.WithEventPublisher(database.DB, hub)
	wakers := controllers.NewWakerController(db)
	previousWakers, previousTransactions := global.WakerControllerManager, global.TransactionControllerManager
	global.WakerControllerManager = wakers
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db))
	t.Cleanup(func() {
		global.WakerControllerManager, global.TransactionControllerManager = previousWakers, previousTransactions
	})
	for _, waker := range []models.Waker{{ID: "owner", Name: "Owner", CreatedAt: 1, UpdatedAt: 11}, {ID: "member", Name: "Member", CreatedAt: 2, UpdatedAt: 12}} {
		if err := wakers.Create(t.Context(), waker); err != nil {
			t.Fatal(err)
		}
	}
	changes := hub.Subscribe("entities", events.Resource{Kind: "waker_navigation_groups", ID: "navigation"})
	t.Cleanup(changes.Close)
	return wakers, changes
}
