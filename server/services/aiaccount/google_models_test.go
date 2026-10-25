package aiaccount

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// TestGoogleModelsDiscovery 验证版本化路径、专属认证、分页与可生成模型过滤。
func TestGoogleModelsDiscovery(t *testing.T) {
	for _, suffix := range []string{"", "/v1beta", "/proxy/v1beta"} {
		t.Run(suffix, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				path := "/v1beta/models"
				if suffix == "/proxy/v1beta" {
					path = "/proxy/v1beta/models"
				}
				if r.URL.Path != path || r.Header.Get("x-goog-api-key") != "google-secret" || r.Header.Get("Authorization") != "" || r.URL.Query().Get("key") != "" {
					t.Errorf("incorrect Google request: %s", r.URL.Path)
				}
				if calls == 1 {
					fmt.Fprint(w, `{"models":[{"name":"models/gemini-2.5-pro","displayName":"Gemini Pro","supportedGenerationMethods":["generateContent"]},{"name":"models/embedding","supportedGenerationMethods":["embedContent"]}],"nextPageToken":"page two"}`)
				} else {
					if r.URL.Query().Get("pageToken") != "page two" {
						t.Error("pagination lost")
					}
					fmt.Fprint(w, `{"models":[{"name":"models/custom/model","supportedGenerationMethods":["generateContent"]}]}`)
				}
			}))
			defer server.Close()
			got, err := DiscoverModels(context.Background(), models.AIAccountEndpoint{Protocol: constants.AccountProtocolGoogleGenerativeAI, BaseURL: server.URL + suffix}, "google-secret")
			if err != nil || calls != 2 || len(got) != 2 || got[0].ID != "gemini-2.5-pro" || got[1].ID != "custom/model" {
				t.Fatalf("got=%v calls=%d err=%v", got, calls, err)
			}
		})
	}
}

// TestGoogleModelsRejectsInvalidPages 保证错误结构与重复分页不会伪装成空目录。
func TestGoogleModelsRejectsInvalidPages(t *testing.T) {
	for _, payload := range []string{`{"data":[]}`, `null`, `{"models":"bad"}`, `{"models":[],"nextPageToken":"repeat"}`, `{"models":[]} trailing`} {
		t.Run(payload, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, payload) }))
			defer server.Close()
			_, err := DiscoverModels(context.Background(), models.AIAccountEndpoint{Protocol: constants.AccountProtocolGoogleGenerativeAI, BaseURL: server.URL}, "key")
			if err == nil || calls > 20 {
				t.Fatalf("invalid response accepted or pagination unbounded: calls=%d err=%v", calls, err)
			}
		})
	}
}
