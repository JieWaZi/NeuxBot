package integrations

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/services/schedule"
)

func TestPublishedRouterExcludesDesktopManagementAndServesOnlyPublicContract(t *testing.T) {
	handler := NewPublishedHandler(nil, schedule.Config{}, "127.0.0.1:8431")
	for _, path := range []string{"/api/v1/integrations/keys", "/api/v1/chats", "/api/v1/knowledge/connections", "/openapi.json", "/api/v1/runtime-options"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-API-Key", "invalid")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code < 400 {
			t.Fatalf("management route %s exposed with %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/integration-openapi.json", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var doc struct { // Paths 是公开文档实际暴露的路径。
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Paths) == 0 {
		t.Fatal("empty public contract")
	}
	for path := range doc.Paths {
		if strings.Contains(path, "/integrations/") || strings.Contains(path, "/chats") || strings.Contains(path, "/connections") {
			t.Fatalf("management schema leaked: %s", path)
		}
	}
}
