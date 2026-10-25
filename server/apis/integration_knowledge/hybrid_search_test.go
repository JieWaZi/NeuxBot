package integration_knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublishedHybridSearchRequiresRetrieveAndRejectsCrossLibraryOverrides(t *testing.T) {
	e, lib, auth, refs := publishedFixture(t, constants.IntegrationCapabilityRetrieve)
	handler := NewHandler(e, nil, auth, Options{References: refs})
	for _, item := range []struct {
		// body 是真实公开检索请求。
		body string
		// status 是明确的合同响应。
		status int
	}{{`{"query_text":"原件检索","match_count":3}`, 200}, {`{"query_text":"原件检索","knowledge_base_ids":["other"]}`, 400}, {`{"query_text":""}`, 400}} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-bases/"+lib.ID+"/hybrid-search", strings.NewReader(item.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-API-Key", "fixture-only")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != item.status {
			t.Fatalf("search %s: %d %s", item.body, w.Code, w.Body.String())
		}
	}
}
