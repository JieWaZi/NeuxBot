package controllers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

func TestSQLiteConcurrentSkillVersionsReuseSessionCallbacks(t *testing.T) {
	db := newTestDB(t)
	controller := NewSkillController(db)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const versionCount = 8
	for index := 0; index < versionCount; index++ {
		id := fmt.Sprintf("session-skill-%d", index)
		if err := controller.CreatePackage(ctx, models.SkillPackage{ID: id, Name: id, Scope: constants.SkillScopePublic, Source: constants.SkillSourceZip}); err != nil {
			t.Fatal(err)
		}
	}
	ready := make(chan struct{})
	results := make(chan error, versionCount)
	for index := 0; index < versionCount; index++ {
		go func(index int) {
			<-ready
			results <- controller.CreateVersion(ctx, models.SkillVersion{
				ID: fmt.Sprintf("session-version-%d", index), SkillPackageID: fmt.Sprintf("session-skill-%d", index),
				Version: "1.0.0", ContentPath: fmt.Sprintf("skills/session/%d", index), ContentDigest: "digest",
			}, true, "")
		}(index)
	}
	close(ready)
	for index := 0; index < versionCount; index++ {
		if err := <-results; err != nil {
			t.Errorf("concurrent version creation: %v", err)
		}
	}
	for index := 0; index < versionCount; index++ {
		skill, err := controller.GetPackage(ctx, fmt.Sprintf("session-skill-%d", index))
		if err != nil || skill.CurrentVersionID != fmt.Sprintf("session-version-%d", index) {
			t.Fatalf("version pointer must commit with its version: skill=%+v error=%v", skill, err)
		}
	}
}

func TestSQLiteSessionQueriesRemainConcurrent(t *testing.T) {
	db := newTestDB(t)
	entered := make(chan struct{}, 2)
	proceed := make(chan struct{})
	original := db.Callback().Query().Get("gorm:query")
	// 在重试拥有的 SQL 边界阻塞首个查询，第二个查询仍必须能够进入。
	if err := db.Callback().Query().Replace("gorm:query", func(database *gorm.DB) {
		entered <- struct{}{}
		select {
		case <-proceed:
			original(database)
		case <-database.Statement.Context.Done():
			database.AddError(database.Statement.Context.Err())
		}
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	results := make(chan error, 2)
	for index := 0; index < 2; index++ {
		session := db.Session(&gorm.Session{Context: ctx})
		go func() {
			_, err := NewWakerController(session).List(ctx, "", 1, 0)
			results <- err
		}()
	}
	for index := 0; index < 2; index++ {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Error("independent queries were serialized before their SQL execution")
		}
	}
	close(proceed)
	for index := 0; index < 2; index++ {
		if err := <-results; err != nil {
			t.Errorf("independent query: %v", err)
		}
	}
}

func TestSQLiteStandaloneSubqueriesPreserveSQLAndBindings(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	wakers := NewWakerController(db)
	for _, id := range []string{"subquery-target", "subquery-unrelated"} {
		if err := wakers.Create(ctx, models.Waker{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	matchingIDs := func() *gorm.DB {
		return controllerDB(ctx, db).Model(&models.Waker{}).Select("id").Where("name = ?", "subquery-target")
	}
	var selected []models.Waker
	if err := controllerDB(ctx, db).Where("id IN (?)", matchingIDs()).Find(&selected).Error; err != nil {
		t.Fatalf("independent read with a bound subquery: %v", err)
	}
	if len(selected) != 1 || selected[0].ID != "subquery-target" {
		t.Fatalf("subquery bindings selected the wrong rows: %+v", selected)
	}
	// 派发领取同样将带绑定变量的 COUNT 子查询放在独立 UPDATE 的条件中。
	matchingCount := controllerDB(ctx, db).Model(&models.Waker{}).Select("COUNT(*)").Where("name = ?", "subquery-target")
	update := controllerDB(ctx, db).Model(&models.Waker{}).
		Where("id IN (?)", matchingIDs()).Where("(?) = ?", matchingCount, 1).Update("bio", "updated")
	if update.Error != nil || update.RowsAffected != 1 {
		t.Fatalf("independent update with bound subqueries: rows=%d error=%v", update.RowsAffected, update.Error)
	}
	target, err := wakers.Get(ctx, "subquery-target")
	if err != nil || target.Bio != "updated" {
		t.Fatalf("subquery update did not persist: target=%+v error=%v", target, err)
	}
	deletion := controllerDB(ctx, db).Where("id IN (?)", matchingIDs()).Delete(&models.Waker{})
	if deletion.Error != nil || deletion.RowsAffected != 1 {
		t.Fatalf("independent deletion with a bound subquery: rows=%d error=%v", deletion.RowsAffected, deletion.Error)
	}
	remaining, err := wakers.List(ctx, "", 10, 0)
	if err != nil || len(remaining) != 1 || remaining[0].ID != "subquery-unrelated" || remaining[0].Bio != "" {
		t.Fatalf("subquery operations changed unrelated rows: remaining=%+v error=%v", remaining, err)
	}
}
