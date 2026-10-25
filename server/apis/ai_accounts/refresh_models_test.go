package ai_accounts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
)

func TestAccountModelsURL(t *testing.T) {
	tests := []struct {
		// name 标识模型列表地址的测试场景。
		name string
		// protocol 指定账号使用的请求协议。
		protocol constants.AccountProtocol
		// baseURL 是账号配置的服务根地址。
		baseURL string
		// want 是按协议组合后的预期模型列表地址。
		want string
	}{
		{
			name:     "openai base",
			protocol: constants.AccountProtocolOpenAIResponses,
			baseURL:  "https://api.example.com/v1",
			want:     "https://api.example.com/v1/models",
		},
		{
			name:     "anthropic root",
			protocol: constants.AccountProtocolAnthropicMessages,
			baseURL:  "https://api.anthropic.com",
			want:     "https://api.anthropic.com/v1/models",
		},
		{
			name:     "anthropic provider root",
			protocol: constants.AccountProtocolAnthropicMessages,
			baseURL:  "https://api.kimi.com/coding",
			want:     "https://api.kimi.com/coding/v1/models",
		},
		{
			name:     "DeepSeek Anthropic service uses the shared model catalog",
			protocol: constants.AccountProtocolAnthropicMessages,
			baseURL:  "https://api.deepseek.com/anthropic",
			want:     "https://api.deepseek.com/models",
		},
		{
			name:     "complete endpoint",
			protocol: constants.AccountProtocolOpenAICompletions,
			baseURL:  "https://api.example.com/v1/chat/completions",
			want:     "https://api.example.com/v1/models",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := aiaccount.ModelsURL(models.AIAccountEndpoint{Protocol: test.protocol, BaseURL: test.baseURL})
			if err != nil || got != test.want {
				t.Fatalf("aiaccount.ModelsURL() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestFetchAccountModelsTreatsUnsupportedCatalogAsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			t.Fatalf("unexpected model request path: %s", request.URL.Path)
		}
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	items, err := aiaccount.DiscoverModels(context.Background(), models.AIAccountEndpoint{
		Protocol: constants.AccountProtocolAnthropicMessages,
		BaseURL:  server.URL,
	}, "test-key")
	if err != nil || len(items) != 0 {
		t.Fatalf("unsupported model catalog returned items=%v, error=%v", items, err)
	}
}

func TestFetchAccountModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" || request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected model request: %s %s", request.URL, request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":[{"id":"model-b"},{"id":"model-a","name":"Model A"}]}`))
	}))
	defer server.Close()

	items, err := aiaccount.DiscoverModels(context.Background(), models.AIAccountEndpoint{
		Protocol: constants.AccountProtocolOpenAIResponses,
		BaseURL:  server.URL + "/v1",
	}, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "model-b" || items[1].Name != "Model A" {
		t.Fatalf("unexpected models: %#v", items)
	}
}
