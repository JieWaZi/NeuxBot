package automatic_tasks

import (
	"context"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	rules "github.com/JieWaZi/NeuxBot/server/internal/integration"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegrationAutomaticTaskIsolationAndKeyDeletion(t *testing.T) {
	h := newHarness(t)
	task := h.save(t, constants.AutomaticTaskTriggerAPI)
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "keys.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB.DB()
	defer pool.Close()
	store := controllers.NewIntegrationKeyController(db.DB)
	for _, id := range []string{"key-a", "key-b"} {
		_, _, err = store.ClaimMutation(t.Context(), models.IntegrationKeyMutation{RequestID: id, Digest: id, KeyID: id, Action: "create"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.CompleteMutation(t.Context(), id, id, models.IntegrationKey{ID: id, Name: id, KeyHash: rules.HashKey(id), Enabled: true, Grants: []models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindAutomaticTask, ResourceIDs: []string{task.TaskID}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityTaskInvoke, constants.IntegrationCapabilityTaskResults}}}}, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	auth := integrationservice.New(integrationservice.Config{Keys: store, Tasks: h.api.config.Tasks})
	handler := NewIntegrationHandler(h.service, h.api.config, auth)
	request := func(method, path, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(`{"input":"附加要求"}`))
		r.Header.Set("X-API-Key", key)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "same-request")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	path := "/api/v1/automatic-tasks/" + task.TaskID + "/runs"
	first := request("POST", path, "key-a")
	if first.Code != 202 {
		t.Fatal(first.Code, first.Body.String())
	}
	var a PublicRun
	if json.Unmarshal(first.Body.Bytes(), &a) != nil || a.RunID == "" {
		t.Fatal(first.Body.String())
	}
	replay := request("POST", path, "key-a")
	if replay.Code != 202 || !strings.Contains(replay.Body.String(), a.RunID) || len(h.boundary.intents) != 1 {
		t.Fatal(replay.Code, replay.Body.String())
	}
	if w := request("GET", path+"/"+a.RunID, "key-b"); w.Code != 404 {
		t.Fatal("cross key run leaked", w.Code, w.Body.String())
	}
	run, err := h.api.config.Tasks.GetRun(t.Context(), a.RunID)
	if err != nil || run.IntegrationInvocation == nil || run.IntegrationInvocation.Identity.KeyID != "key-a" {
		t.Fatal(run, err)
	}
	h.boundary.finish(run.ChatID, constants.ExecutionStatusCompleted, false)
	if w := request("GET", path+"/"+a.RunID, "key-a"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	h.now.Add(2000)
	second := request("POST", path, "key-b")
	if second.Code != 202 || strings.Contains(second.Body.String(), a.RunID) {
		t.Fatal("cross key occurrence collided", second.Code, second.Body.String())
	}
	key, _ := store.Get(t.Context(), "key-a")
	if err = auth.WithAuthorizationWrite(t.Context(), func(ctx context.Context) error {
		return store.Delete(ctx, key.ID, key.Revision)
	}); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", path+"/"+a.RunID, "key-a"); w.Code != http.StatusUnauthorized {
		t.Fatal("deleted key accepted", w.Code)
	}
	retained, err := h.api.config.Tasks.GetRun(t.Context(), a.RunID)
	if err != nil || retained.IntegrationInvocation == nil || retained.IntegrationInvocation.Identity.KeyID != "key-a" {
		t.Fatal("deletion lost original run ownership", retained, err)
	}
	if w := request("GET", path+"/"+a.RunID, "key-b"); w.Code != http.StatusNotFound {
		t.Fatal("another key inherited deleted key results", w.Code)
	}
	// 即使客户端自报身份也不能改变服务端调用主体。
	_ = context.Background()
}
