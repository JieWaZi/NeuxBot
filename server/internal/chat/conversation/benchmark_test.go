package conversation

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
)

// BenchmarkConversationStorePage measures a warm, fixed 20-message page as history grows.
func BenchmarkConversationStorePage(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			ctx := context.Background()
			root := b.TempDir()
			store, err := NewStore(root)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(store.Close)
			path, err := store.LogPath(chatID)
			if err != nil {
				b.Fatal(err)
			}
			seedPageBenchmark(b, path, n)
			query := agent.MessageQuery{ChatID: chatID, Limit: 20}
			page, err := store.ReadMessages(ctx, query)
			if err != nil || len(page.Messages) != 20 {
				b.Fatalf("prime page: %d, %v", len(page.Messages), err)
			}
			logInfo, err := os.Stat(path)
			if err != nil {
				b.Fatal(err)
			}
			indexInfo, err := os.Stat(filepath.Join(root, indexFilename))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				page, err := store.ReadMessages(ctx, query)
				if err != nil || len(page.Messages) != 20 {
					b.Fatalf("page: %d, %v", len(page.Messages), err)
				}
			}
			b.ReportMetric(float64(logInfo.Size()), "authority-B")
			b.ReportMetric(float64(indexInfo.Size()), "index-B")
		})
	}
}

// seedPageBenchmark uses the production codec and performs a single sync outside the timed region.
func seedPageBenchmark(b *testing.B, path string, count int) {
	b.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		b.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	defer file.Close()
	w := bufio.NewWriter(file)
	var version, offset int64
	emit := func(key string, changes []agent.ConversationChange) {
		b.Helper()
		rows := make([]diskRow, len(changes))
		for i := range changes {
			rows[i].Chat = &changes[i]
		}
		payload, hash, err := marshalRows(rows)
		if err != nil {
			b.Fatal(err)
		}
		header := makeHeader(key, hash, version, len(rows))
		frame, _, err := encodeBatch(header, payload, offset)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := w.Write(frame); err != nil {
			b.Fatal(err)
		}
		version, offset = header.Receipt.Version, offset+int64(len(frame))
	}
	emit("create", []agent.ConversationChange{fact(constants.ChatEventTypeCreated, "", "")})
	for start := 0; start < count; start += 32 {
		var changes []agent.ConversationChange
		for i := start; i < min(start+32, count); i++ {
			changes = append(changes, agent.ConversationChange{Fact: &agent.ChatFact{
				Type: constants.ChatEventTypeUserMessageCreated, MessageID: fmt.Sprintf("message-%032d", i),
				Data: json.RawMessage("{\"content\":\"A synthetic short user message.\"}"),
			}})
		}
		emit(fmt.Sprintf("batch-%d", start), changes)
	}
	if err := w.Flush(); err != nil {
		b.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		b.Fatal(err)
	}
}
