package usage

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog_next"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestExactNativeAmountsSurviveProjection(t *testing.T) {
	root := t.TempDir()
	store, err := eventlog_next.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "usage.sqlite")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.UsageRecord{}, &models.UsageCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	session := agent.NativeSessionKey{Provider: "codex", Subject: "original", SessionID: "native"}
	for _, observation := range []struct {
		// id identifies a distinct canonical invocation.
		id string
		// amount is the native exact decimal representation.
		amount json.Number
		// currency keeps distinct amounts separate.
		currency string
	}{
		{"tiny", "1e-128", "USD"},
		{"zero", "0", "USD"},
		{"large", "1e128", "CNY"},
	} {
		result := agent.NativeResult{
			Key: agent.NativeResultKey{Session: session, InvocationID: observation.id},
			Turn: acpmeta.NativeTurn{InvocationID: observation.id, Origin: acpmeta.NativeTurnOriginProvider, Status: acpmeta.NativeTurnCompleted,
				Usage: &acpmeta.TurnUsage{Cost: &acpmeta.UsageCost{Amount: observation.amount, Currency: observation.currency, Source: "native",
					Scope: "turn", Aggregation: "turn_delta", Valuation: "reported"}}}}
		if err := store.CommitNativeResult(t.Context(), result); err != nil {
			t.Fatal(err)
		}
	}
	filter := Filter{From: time.Now().Add(-time.Hour).UnixMilli(), To: time.Now().Add(time.Hour).UnixMilli(), Timezone: "UTC"}
	summary, err := NewService(store, controllers.NewUsageController(db)).Summary(t.Context(), filter, "")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Metrics.UnpricedInvocations != 0 || len(summary.Metrics.Amounts) != 2 ||
		summary.Metrics.Amounts[0].Currency != "CNY" ||
		summary.Metrics.Amounts[0].Amount != "1"+strings.Repeat("0", 128) ||
		summary.Metrics.Amounts[1].Currency != "USD" || summary.Metrics.Amounts[1].Invocations != 2 ||
		summary.Metrics.Amounts[1].Amount != "0."+strings.Repeat("0", 127)+"1" {
		t.Fatalf("accepted cost projected as unknown: %+v", summary.Metrics)
	}
}
