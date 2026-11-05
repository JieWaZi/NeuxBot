package usage

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog_next"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

func TestCanonicalUsageRebuildAndSnapshot(t *testing.T) {
	for _, provider := range []string{"codex", "claude", "cursor"} {
		t.Run(provider, func(t *testing.T) { verifyCanonicalUsageRebuildAndSnapshot(t, provider) })
	}
}

func verifyCanonicalUsageRebuildAndSnapshot(t *testing.T, provider string) {
	t.Helper()
	root := t.TempDir()
	store, err := eventlog_next.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	ctx := t.Context()
	zero, input, output, read, thought := int64(0), int64(7), int64(11), int64(3), int64(2)
	session := agent.NativeSessionKey{Provider: provider, Subject: "frozen-subject", SessionID: "native"}
	first := agent.NativeResult{
		Key: agent.NativeResultKey{Session: session, InvocationID: "p1"},
		Turn: acpmeta.NativeTurn{InvocationID: "p1", Origin: acpmeta.NativeTurnOriginProvider, Status: acpmeta.NativeTurnCompleted,
			Usage: &acpmeta.TurnUsage{Source: provider, Coverage: "last_request",
				InputTokens: &input, OutputTokens: &output, CacheReadTokens: &read, CacheWriteTokens: &zero, ReasoningTokens: &thought,
				Observations: []acpmeta.UsageObservation{{Coverage: "overlap", Tokens: acpmeta.ModelUsage{InputTokens: &input}}}}},
	}
	for range 2 {
		if err := store.CommitNativeResult(ctx, first); err != nil {
			t.Fatal(err)
		}
	}
	rangeFilter := Filter{From: time.Now().Add(-time.Hour).UnixMilli(), To: time.Now().Add(time.Hour).UnixMilli(), Timezone: "Asia/Shanghai"}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "usage.sqlite")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.UsageRecord{}, &models.UsageCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	projection := controllers.NewUsageController(db)
	service := NewService(store, projection)
	summary, err := service.Summary(ctx, rangeFilter, "")
	if err != nil {
		t.Fatal(err)
	}
	m := summary.Metrics
	if m.Invocations != 1 || m.IncompleteUsageInvocations != 1 || m.MissingUsageInvocations != 0 ||
		m.Tokens.Input != nil || m.KnownTokens.Input == nil || *m.KnownTokens.Input != 7 ||
		m.KnownTotalWithCacheRead == nil || *m.KnownTotalWithCacheRead != 21 ||
		m.KnownTotalWithoutCacheRead == nil || *m.KnownTotalWithoutCacheRead != 18 {
		t.Fatalf("unexpected partial metrics: %+v", m)
	}
	second := first
	second.Key.InvocationID, second.Turn.InvocationID, second.Turn.Usage = "p2", "p2", nil
	if err := store.CommitNativeResult(ctx, second); err != nil {
		t.Fatal(err)
	}
	groups, err := service.Groups(ctx, rangeFilter, summary.Snapshot, "day", "", 1)
	if err != nil || groups.TotalGroups != 1 || groups.Groups[0].Metrics.Invocations != 1 {
		t.Fatalf("snapshot changed after terminal commit: %+v %v", groups, err)
	}
	store.Close()
	store, err = eventlog_next.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	// Preserve existing rows and checkpoints across supported projection upgrades.
	previous := "providers-v2:fixture"
	if provider == "codex" {
		previous = "providers-v1:fixture"
		if err := db.Exec("ALTER TABLE usage_records DROP COLUMN provider").Error; err != nil {
			t.Fatal(err)
		}
	}
	if provider == "cursor" {
		previous = "providers-v3:fixture"
		// Older SQLite databases can retain both the amount column and a cost-only row.
		if err := db.Exec("ALTER TABLE usage_records ADD COLUMN amount text").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`INSERT INTO usage_records
			(native_id, scope, recorded_at, waker_id, chat_id, provider, account_id, source, model, amount)
			VALUES (CAST('legacy' AS BLOB), ?, ?, '', '', ?, '', 'provider', 'legacy-cost-only', '0.10')`,
			models.UsageScopeModelOnly, time.Now().UnixMilli(), provider).Error; err != nil {
			t.Fatal(err)
		}
	} else if err := db.Exec("ALTER TABLE usage_records DROP COLUMN scope").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.UsageCheckpoint{}).Where("shard = -1").Update("generation", previous).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.UsageRecord{}, &models.UsageCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	service = NewService(store, projection)
	rangeFilter.Provider = provider
	summary, err = service.Summary(ctx, rangeFilter, "")
	if err != nil || summary.Metrics.Invocations != 2 || summary.Metrics.MissingUsageInvocations != 1 {
		t.Fatalf("cold read lost terminal usage: %+v %v", summary, err)
	}
	if provider == "cursor" {
		page, err := service.Groups(ctx, rangeFilter, summary.Snapshot, "model", "", 2)
		if err != nil || page.TotalGroups != 1 || len(page.Groups) != 1 || page.Groups[0].Key != "" {
			t.Fatalf("legacy cost-only group survived projection replay: %+v %v", page, err)
		}
	}
}

func TestUsageFiltersDSTPaginationAndMeasuredZero(t *testing.T) {
	ctx := t.Context()
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
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := db.AutoMigrate(&models.UsageRecord{}, &models.UsageCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	projection := controllers.NewUsageController(db)
	checkpoints, err := projection.Checkpoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	generation := checkpoints[0].Generation
	at := func(value string) int64 {
		t.Helper()
		instant, err := time.Parse(time.RFC3339, value)
		if err != nil {
			t.Fatal(err)
		}
		return instant.UnixMilli()
	}
	zero, one := int64(0), int64(1)
	row := func(id, time, waker, model string) models.UsageRecord {
		return models.UsageRecord{NativeID: []byte(id), RecordedAt: at(time), WakerID: waker, Model: model, AccountID: "original",
			Source: "chat", Input: &zero, CacheRead: &zero, CacheWrite: &zero, Output: &one, Complete: true}
	}
	rows := []models.UsageRecord{
		row("a", "2026-03-08T04:59:59Z", "w", "ignore"),
		row("b", "2026-03-08T05:00:00Z", "w", "b"),
		row("c", "2026-03-09T03:59:59Z", "w", "a"),
		row("d", "2026-03-09T04:00:00Z", "w", "a"),
		row("e", "2026-03-09T04:00:01Z", "other", "ignore"),
		row("f", "2026-03-10T04:00:00Z", "w", "ignore"),
	}
	if err := projection.Project(ctx, generation, 0, 0, rows); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, projection)
	filter := Filter{From: at("2026-03-08T05:00:00Z"), To: at("2026-03-10T04:00:00Z"), Timezone: "America/New_York", WakerIDs: []string{"w"}}
	summary, err := service.Summary(ctx, filter, "")
	if err != nil || summary.Metrics.Invocations != 3 ||
		summary.Metrics.Tokens.Input == nil || *summary.Metrics.Tokens.Input != 0 ||
		summary.Metrics.TotalWithCacheRead == nil || *summary.Metrics.TotalWithCacheRead != 3 {
		t.Fatalf("incorrect grouped scope or complete Token counts: %+v %v", summary, err)
	}
	day, err := service.Groups(ctx, filter, summary.Snapshot, "day", "", 1)
	if err != nil || !day.HasMore || day.TotalGroups != 2 || day.Groups[0].Key != "2026-03-08" || day.Groups[0].Metrics.Invocations != 2 {
		t.Fatalf("DST day one: %+v %v", day, err)
	}
	day2, err := service.Groups(ctx, filter, summary.Snapshot, "day", day.NextCursor, 1)
	if err != nil || day2.HasMore || day2.TotalGroups != 2 || day2.Groups[0].Key != "2026-03-09" || day2.Groups[0].Metrics.Invocations != 1 {
		t.Fatalf("DST day two: %+v %v", day2, err)
	}
	model, err := service.Groups(ctx, filter, summary.Snapshot, "model", "", 1)
	if err != nil || model.TotalGroups != 2 || !model.HasMore || model.Groups[0].Key != "a" {
		t.Fatalf("model first page: %+v %v", model, err)
	}
	model2, err := service.Groups(ctx, filter, summary.Snapshot, "model", model.NextCursor, 1)
	if err != nil || model2.HasMore || model2.Groups[0].Key != "b" {
		t.Fatalf("model second page: %+v %v", model2, err)
	}
	if _, err := service.Groups(ctx, filter, summary.Snapshot, "day", model.NextCursor, 1); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("unexpected cross-group cursor result: %v", err)
	}
	filter.Timezone = "UTC"
	if _, err := service.Groups(ctx, filter, summary.Snapshot, "day", "", 1); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("unexpected cross-filter snapshot result: %v", err)
	}
	filter.Timezone = "America/New_York"
	if err := projection.ResetProjection(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Groups(ctx, filter, summary.Snapshot, "day", "", 1); !errors.Is(err, status_error.Conflict) {
		t.Fatalf("unexpected reset snapshot result: %v", err)
	}

	type planRow struct {
		// Detail is the SQLite planner's index access description.
		Detail string
	}
	for _, tc := range []struct {
		// name identifies the expected index.
		name string
		// sql is the corresponding production range query.
		sql string
	}{
		{"idx_usage_time", "SELECT * FROM usage_records WHERE recorded_at >= 1 AND recorded_at < 200 AND id <= 30 ORDER BY recorded_at, id LIMIT 256"},
		{"idx_usage_waker_time", "SELECT * FROM usage_records WHERE waker_id IN ('w') AND recorded_at >= 1 AND recorded_at < 200 AND id <= 30 ORDER BY recorded_at, id LIMIT 256"},
		{"idx_usage_time", "SELECT model AS key, provider, MIN(id) AS row_id FROM usage_records WHERE recorded_at >= 1 AND recorded_at < 200 AND id <= 30 AND provider = 'codex' AND scope IN (0, 2) AND (model, provider) > ('a', 'codex') GROUP BY model, provider ORDER BY model, provider LIMIT 2"},
	} {
		var plan []planRow
		if err := db.Raw("EXPLAIN QUERY PLAN " + tc.sql).Scan(&plan).Error; err != nil || len(plan) == 0 || !strings.Contains(plan[0].Detail, tc.name) {
			t.Fatalf("query plan for %s: %+v %v", tc.name, plan, err)
		}
		t.Log(plan[0].Detail)
	}
}

func TestUsageModelGroupsSeparateProviders(t *testing.T) {
	ctx := t.Context()
	store, err := eventlog_next.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sharedModel := strings.Repeat("a", 1800) + ":model"
	for i, provider := range []string{"claude", "cursor"} {
		key := agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: provider, Subject: "system-subject", SessionID: "native"}, InvocationID: "shared-id"}
		input := agent.HostInput{HostInvocationID: "host", ExecutionID: "execution", AdapterInputID: "input", Session: key.Session,
			Attribution: &agent.UsageAttribution{AccountID: agent.SystemAccountID, WakerID: "waker", Source: "chat"}}
		chatID := strings.Repeat(string(rune('a'+i)), 32)
		if _, err := store.Commit(ctx, agent.ConversationBatch{ChatID: chatID, Key: "input", Changes: []agent.ConversationChange{
			{Fact: &agent.ChatFact{Type: constants.ChatEventTypeCreated}},
			{Binding: &agent.NativeBinding{Session: key.Session}}, {Input: &input},
		}}); err != nil {
			t.Fatal(err)
		}
		if err := store.CommitNativeResult(ctx, agent.NativeResult{Key: key, Turn: acpmeta.NativeTurn{
			InvocationID: key.InvocationID, Origin: acpmeta.NativeTurnOriginHost, Status: acpmeta.NativeTurnCompleted,
			ConsumedInputIDs: []string{input.AdapterInputID}, Usage: &acpmeta.TurnUsage{Source: "fixture", Model: sharedModel},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "usage.sqlite")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := db.AutoMigrate(&models.UsageRecord{}, &models.UsageCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	projection := controllers.NewUsageController(db)
	service := NewService(store, projection)
	filter := Filter{From: time.Now().Add(-time.Hour).UnixMilli(), To: time.Now().Add(time.Hour).UnixMilli(), Timezone: "UTC", AccountID: agent.SystemAccountID}
	summary, err := service.Summary(ctx, filter, "")
	if err != nil || summary.Metrics.Invocations != 2 {
		t.Fatalf("expected two native invocations: %+v %v", summary, err)
	}
	page, err := service.Groups(ctx, filter, summary.Snapshot, "model", "", 1)
	if err != nil || page.TotalGroups != 2 || len(page.Groups) != 1 || !page.HasMore ||
		page.Groups[0].Key != sharedModel || page.Groups[0].Provider != "claude" || page.Groups[0].Metrics.Invocations != 1 {
		t.Fatalf("identical model IDs from separate providers merged: %+v %v", page, err)
	}
	next, err := service.Groups(ctx, filter, summary.Snapshot, "model", page.NextCursor, 1)
	if err != nil || next.TotalGroups != 2 || len(next.Groups) != 1 || next.HasMore ||
		next.Groups[0].Key != sharedModel || next.Groups[0].Provider != "cursor" || next.Groups[0].Metrics.Invocations != 1 {
		t.Fatalf("pagination skipped the second provider's model: %+v %v", next, err)
	}
	providers, err := service.Groups(ctx, filter, summary.Snapshot, "provider", "", 10)
	if err != nil || providers.TotalGroups != 2 || len(providers.Groups) != 2 ||
		providers.Groups[0].Key != "claude" || providers.Groups[0].Metrics.Invocations != 1 ||
		providers.Groups[1].Key != "cursor" || providers.Groups[1].Metrics.Invocations != 1 {
		t.Fatalf("unexpected provider groups: %+v %v", providers, err)
	}
	filter.Provider = "cursor"
	if _, err := service.Summary(ctx, filter, summary.Snapshot); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("unfiltered snapshot accepted a provider-filtered read: %v", err)
	}
	scoped, err := service.Summary(ctx, filter, "")
	if err != nil || scoped.Metrics.Invocations != 1 || scoped.Range.Provider != "cursor" {
		t.Fatalf("unexpected provider-filtered summary: %+v %v", scoped, err)
	}
}

func TestUsageSeparatesPrimaryModelBreakdownFromInvocationTotals(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	store, err := eventlog_next.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	two, three, five, overlap := int64(2), int64(3), int64(5), int64(999)
	rawCost := &acpmeta.UsageCost{Amount: "0.10", Currency: "USD", Source: "claude.result.cost",
		Scope: "query_pipeline", Aggregation: "turn_delta", Valuation: "estimated"}
	session := agent.NativeSessionKey{Provider: "claude", Subject: "subject", SessionID: "native"}
	commit := func(id string, usage *acpmeta.TurnUsage) {
		t.Helper()
		result := agent.NativeResult{Key: agent.NativeResultKey{Session: session, InvocationID: id}, Turn: acpmeta.NativeTurn{
			InvocationID: id, Origin: acpmeta.NativeTurnOriginProvider, Status: acpmeta.NativeTurnCompleted, Usage: usage,
		}}
		if err := store.CommitNativeResult(ctx, result); err != nil {
			t.Fatal(err)
		}
	}
	mixed := &acpmeta.TurnUsage{InputTokens: &five, Coverage: "query_pipeline", Models: map[string]acpmeta.ModelUsage{
		"alpha": {InputTokens: &two}, "beta": {InputTokens: &three}, "unavailable": {}, "cost-only": {Cost: rawCost},
	}, Observations: []acpmeta.UsageObservation{{Coverage: "last_request", Models: map[string]acpmeta.ModelUsage{
		"overlap": {InputTokens: &overlap},
	}}}}
	commit("mixed", mixed)
	commit("mixed", mixed)
	commit("single", &acpmeta.TurnUsage{Model: "alpha", InputTokens: &two})
	commit("unattributed", &acpmeta.TurnUsage{InputTokens: &two})
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "usage.sqlite")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := db.AutoMigrate(&models.UsageRecord{}, &models.UsageCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, controllers.NewUsageController(db))
	filter := Filter{From: time.Now().Add(-time.Hour).UnixMilli(), To: time.Now().Add(time.Hour).UnixMilli(), Timezone: "UTC"}
	summary, err := service.Summary(ctx, filter, "")
	if err != nil || summary.Metrics.Invocations != 3 || summary.Metrics.KnownTokens.Input == nil || *summary.Metrics.KnownTokens.Input != 9 {
		t.Fatalf("summary duplicated a model breakdown: %+v %v", summary.Metrics, err)
	}
	page, err := service.Groups(ctx, filter, summary.Snapshot, "model", "", 3)
	if err != nil || page.TotalGroups != 3 || len(page.Groups) != 3 || page.Metrics.Invocations != 3 ||
		page.Groups[0].Key != "" || page.Groups[1].Key != "alpha" || page.Groups[2].Key != "beta" {
		t.Fatalf("model groups dropped or duplicated primary evidence: %+v %v", page, err)
	}
	unknown, alpha, beta := page.Groups[0].Metrics, page.Groups[1].Metrics, page.Groups[2].Metrics
	if unknown.Invocations != 1 || unknown.KnownTokens.Input == nil || *unknown.KnownTokens.Input != 2 ||
		alpha.Invocations != 2 || alpha.KnownTokens.Input == nil || *alpha.KnownTokens.Input != 4 ||
		beta.Invocations != 1 || beta.KnownTokens.Input == nil || *beta.KnownTokens.Input != 3 || beta.Tokens.Output != nil {
		t.Fatalf("unexpected model counters: alpha %+v beta %+v", alpha, beta)
	}
	providers, err := service.Groups(ctx, filter, summary.Snapshot, "provider", "", 1)
	if err != nil || len(providers.Groups) != 1 || providers.Groups[0].Metrics.Invocations != 3 {
		t.Fatalf("provider group counted model rows: %+v %v", providers, err)
	}
}
