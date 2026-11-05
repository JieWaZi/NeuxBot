package usage

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	service "github.com/JieWaZi/NeuxBot/server/services/usage"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shrewx/ginx"
	"gorm.io/gorm"
)

func TestUsageAPIReplaysAfterChatDeletion(t *testing.T) {
	root := t.TempDir()
	store, err := conversation.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
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
	old := global.UsageService
	t.Cleanup(func() { global.UsageService = old })
	global.UsageService = service.NewService(store, projection)

	ctx := t.Context()
	chatID := strings.Repeat("a", 32)
	session := agent.NativeSessionKey{Provider: "codex", Subject: "original", SessionID: "native"}
	first := agent.HostInput{HostInvocationID: "h1", ExecutionID: "e1", AdapterInputID: "input-1", Session: session,
		Attribution: &agent.UsageAttribution{AccountID: "account", WakerID: "waker", Source: "workflow"}}
	second := agent.HostInput{HostInvocationID: "h2", ExecutionID: "e2", AdapterInputID: "input-2", Session: session,
		Attribution: &agent.UsageAttribution{AccountID: "other", WakerID: "steer", Source: "chat"}}
	receipt, err := store.Commit(ctx, agent.ConversationBatch{ChatID: chatID, Key: "input", Changes: []agent.ConversationChange{
		{Fact: &agent.ChatFact{Type: constants.ChatEventTypeCreated}},
		{Binding: &agent.NativeBinding{Session: session}}, {Input: &first}, {Input: &second},
	}})
	if err != nil {
		t.Fatal(err)
	}
	input := int64(30)
	result := agent.NativeResult{Key: agent.NativeResultKey{Session: session, InvocationID: "p"},
		Turn: acpmeta.NativeTurn{InvocationID: "p", Origin: acpmeta.NativeTurnOriginHost, Status: acpmeta.NativeTurnCancelled,
			ConsumedInputIDs: []string{"input-1", "input-2"}, Usage: &acpmeta.TurnUsage{Coverage: "last_request", InputTokens: &input,
				Model: "provider-model", Cost: &acpmeta.UsageCost{Amount: "0.000000000000000001", Currency: "USD",
					Source: "fixture", Scope: "last_request", Aggregation: "turn_delta", Valuation: "reported"}}}}
	published := 0
	store.SetUsageChanged(func() { published++ })
	for range 2 {
		if err := store.CommitNativeResult(ctx, result); err != nil {
			t.Fatal(err)
		}
	}
	if published != 1 {
		t.Fatalf("terminal committed %d times", published)
	}
	if _, err := store.Commit(ctx, agent.ConversationBatch{ChatID: chatID, Key: "consume", ExpectedVersion: receipt.Version,
		Changes: []agent.ConversationChange{{Consumption: &agent.InputConsumption{HostInvocationID: "h1", Result: result.Key}},
			{Consumption: &agent.InputConsumption{HostInvocationID: "h2", Result: result.Key}}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(chatID); err != nil {
		t.Fatal(err)
	}
	if err := projection.ResetProjection(ctx); err != nil {
		t.Fatal(err)
	}
	// Drop the disposable locator as well; native JSONL alone is sufficient.
	index, err := store.IndexPath(chatID)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	store, err = conversation.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	global.UsageService = service.NewService(store, projection)

	query := url.Values{
		"from": {strconv.FormatInt(time.Now().Add(-time.Hour).UnixMilli(), 10)}, "to": {strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)},
		"timezone": {"Asia/Shanghai"}, "wakerIds": {"waker"}, "accountId": {"account"}, "chatId": {chatID}, "source": {"workflow"},
	}
	query.Set("provider", "cursor")
	if unrelated := readUsageOperator(t, &GetUsageSummary{}, query).(service.Summary); unrelated.Metrics.Invocations != 0 {
		t.Fatalf("provider filter matched another backend: %+v", unrelated)
	}
	query.Set("provider", "codex")
	value := readUsageOperator(t, &GetUsageSummary{}, query).(service.Summary)
	m := value.Metrics
	if m.Invocations != 1 || m.KnownTotalWithCacheRead == nil || *m.KnownTotalWithCacheRead != 30 ||
		m.MissingUsageInvocations != 0 || m.IncompleteUsageInvocations != 1 || m.TotalWithCacheRead != nil {
		t.Fatalf("canonical facts changed after deletion: %+v", value)
	}
	assertTokenOnlyJSON(t, value)
	query.Set("groupBy", "model")
	query.Set("snapshot", value.Snapshot)
	page := readUsageOperator(t, &ListUsageGroups{}, query).(service.GroupsPage)
	if page.TotalGroups != 1 || len(page.Groups) != 1 || page.Groups[0].Key != "provider-model" ||
		page.Groups[0].Provider != "codex" || page.Groups[0].Metrics.Invocations != 1 {
		t.Fatalf("unexpected model page: %+v", page)
	}
	assertTokenOnlyJSON(t, page)
	query.Set("groupBy", "provider")
	providers := readUsageOperator(t, &ListUsageGroups{}, query).(service.GroupsPage)
	if providers.TotalGroups != 1 || len(providers.Groups) != 1 || providers.Groups[0].Key != "codex" {
		t.Fatalf("unexpected provider page: %+v", providers)
	}
}

func assertTokenOnlyJSON(t *testing.T, response any) {
	t.Helper()
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"amounts"`, `"comparisonAmounts"`, `"unpricedInvocations"`, `"incompleteCostInvocations"`, `"incomplete_cost"`} {
		if strings.Contains(string(payload), field) {
			t.Errorf("public usage includes %s", field)
		}
	}
}

func readUsageOperator(t *testing.T, op ginx.HandleOperator, query url.Values) any {
	t.Helper()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(op.Method(), "/usage"+op.Path()+"?"+query.Encode(), nil)
	if err := ginx.ParameterBinding(ctx, op, ginx.GetOperatorTypeInfo(reflect.TypeOf(op))); err != nil {
		t.Fatal(err)
	}
	if err := op.Validate(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := op.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
