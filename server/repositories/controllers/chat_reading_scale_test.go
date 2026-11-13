package controllers

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

func TestAttentionQueryPlanAndScale(t *testing.T) {
	db := readingDB(t)
	const largest = 10000
	chats := make([]models.Chat, 0, largest)
	messages := make([]models.ChatReadableMessage, 0, 2*largest)
	cursors := make([]models.ChatReadCursor, 0, 3*largest)
	for i := range largest {
		id := fmt.Sprintf("chat-%05d", i)
		chats = append(chats, models.Chat{ID: id, LatestReadableSequence: 9})
		for _, seq := range []int64{4, 9} {
			messages = append(messages, models.ChatReadableMessage{
				ChatID: id, MessageID: fmt.Sprint(seq), OrderSequence: seq, Kind: "answer",
			})
		}
		sparseThrough := int64(9)
		if i%20 == 0 {
			sparseThrough = 4
		}
		cursors = append(cursors,
			models.ChatReadCursor{ReaderID: "read", ChatID: id, ThroughSequence: 9},
			models.ChatReadCursor{ReaderID: "sparse", ChatID: id, ThroughSequence: sparseThrough},
			models.ChatReadCursor{ReaderID: "dense", ChatID: id},
		)
	}
	if err := db.CreateInBatches(messages, 128).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateInBatches(cursors, 128).Error; err != nil {
		t.Fatal(err)
	}

	var rangeQuery string
	var rangeArgs []any
	var cursorQueries, rangeQueries int
	if err := db.Callback().Query().After("gorm:query").Register("test:count-reading-cursors", func(tx *gorm.DB) {
		if tx.Statement.Table == "chat_read_cursors" {
			cursorQueries++
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().After("gorm:row").Register("test:capture-unread-range", func(tx *gorm.DB) {
		statement := tx.Statement.SQL.String()
		if strings.Contains(statement, "FROM chat_readable_messages WHERE chat_id=? AND order_sequence>?") {
			rangeQueries++
			if rangeQuery == "" {
				rangeQuery = statement
				rangeArgs = append([]any(nil), tx.Statement.Vars...)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{100, 1000, 10000} {
		for _, scenario := range []struct {
			reader          string
			unreadChatCount int
			messagesPerChat int64
		}{
			{reader: "read"},
			{reader: "sparse", unreadChatCount: count / 20, messagesPerChat: 1},
			{reader: "dense", unreadChatCount: count, messagesPerChat: 2},
		} {
			t.Run(fmt.Sprintf("%d/%s", count, scenario.reader), func(t *testing.T) {
				cursorQueries, rangeQueries = 0, 0
				store := NewChatReadingController(db)
				start := time.Now()
				states, err := store.Readings(context.Background(), scenario.reader, chats[:count])
				if err != nil || len(states) != count {
					t.Fatalf("阅读状态行数 = %d，期望 %d，错误 %v", len(states), count, err)
				}
				t.Logf("读取 %d 个 Chat，游标 %d 批，范围 %d 批，耗时 %s", count, cursorQueries, rangeQueries, time.Since(start))
				if cursorQueries != (count+127)/128 || rangeQueries != (scenario.unreadChatCount+127)/128 {
					t.Fatalf("游标和未读范围 SQL 未按 128 个 Chat 分批：%d / %d", cursorQueries, rangeQueries)
				}
				var unreadChats int
				for _, state := range states {
					if state.UnreadMessageCount == 0 {
						continue
					}
					unreadChats++
					expectedFirst := "9"
					if scenario.messagesPerChat == 2 {
						expectedFirst = "4"
					}
					if state.UnreadMessageCount != scenario.messagesPerChat ||
						state.FirstUnreadMessageID != expectedFirst || state.LatestReadableMessageID != "9" {
						t.Fatalf("未读范围错误：%+v", state)
					}
				}
				if unreadChats != scenario.unreadChatCount {
					t.Fatalf("未读 Chat 数 = %d，期望 %d", unreadChats, scenario.unreadChatCount)
				}
			})
		}
	}
	if rangeQuery == "" {
		t.Fatal("未捕获生产未读查询")
	}
	var plan []struct {
		// Detail 是 SQLite 选择的访问路径。
		Detail string
	}
	if err := db.Raw("EXPLAIN QUERY PLAN "+rangeQuery, rangeArgs...).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	var seeks int
	for _, row := range plan {
		if strings.Contains(row.Detail, "chat_readable_messages") {
			seeks++
			if !strings.Contains(row.Detail, "SEARCH") || !strings.Contains(row.Detail, "idx_chat_readable_order") ||
				!strings.Contains(row.Detail, "chat_id=? AND order_sequence>?") {
				t.Fatalf("未读范围缺少复合索引查找：%s", row.Detail)
			}
		}
		if strings.Contains(row.Detail, "TEMP B-TREE") {
			t.Fatalf("未读范围发生排序：%s", row.Detail)
		}
	}
	if seeks == 0 {
		t.Fatal("查询计划没有未读范围查找")
	}
}
