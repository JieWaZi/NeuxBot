package integration_knowledge

import (
	"encoding/json"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/neux-knowledge/core"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublishedThemeWriteUsesStructureCASAndStableIdentity(t *testing.T) {
	e, lib, auth, refs := publishedFixture(t, constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityIngest)
	h := NewHandler(e, nil, auth, Options{References: refs})
	body := fmt.Sprintf(`{"structure_version":%d,"name":"正文"}`, lib.StructureVersion)
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest("POST", "/api/v1/knowledgebase/"+lib.ID+"/wiki/folders", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-API-Key", "fixture-only")
		r.Header.Set("Idempotency-Key", "folder")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var result struct {
			ID      string        `json:"id"`
			Receipt *core.Receipt `json:"receipt"`
		}
		if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.ID == "" || result.Receipt.RequestID != "folder" {
			t.Fatal(w.Body.String())
		}
	}
	themes, err := e.ListThemes(t.Context(), lib.ID)
	count := 0
	for _, theme := range themes {
		if theme.Name == "正文" {
			count++
		}
	}
	if err != nil || count != 1 {
		t.Fatal(themes, err)
	}
	r := httptest.NewRequest("PUT", "/api/v1/knowledgebase/"+lib.ID+"/wiki/folders/"+func() string {
		for _, theme := range themes {
			if theme.Name == "正文" {
				return theme.ID
			}
		}
		return ""
	}(), strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-API-Key", "fixture-only")
	r.Header.Set("Idempotency-Key", "stale-folder")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
}
