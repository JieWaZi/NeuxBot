package memory

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// embedRequestPayload 是测试服务器捕获的请求体。
type embedRequestPayload struct {
	// Model 是请求携带的模型名。
	Model string `json:"model"`
	// Input 是请求携带的文本列表。
	Input []string `json:"input"`
}

// newEmbeddingServer 创建返回固定向量的假嵌入服务，并记录请求。
func newEmbeddingServer(
	t *testing.T,
	dimensions int,
	captured *embedRequestPayload,
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/embeddings" {
			http.NotFound(writer, request)
			return
		}
		if request.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(writer, `{"error":{"message":"unauthorized"}}`, http.StatusUnauthorized)
			return
		}
		if err := json.NewDecoder(request.Body).Decode(captured); err != nil {
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		vector := make([]float32, dimensions)
		for index := range vector {
			vector[index] = 0.1
		}
		data := make([]map[string]any, 0, len(captured.Input))
		for range captured.Input {
			data = append(data, map[string]any{"embedding": vector})
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"data": data})
	}))
}

func TestOpenAIEmbedderEmbed(t *testing.T) {
	var captured embedRequestPayload
	server := newEmbeddingServer(t, 3, &captured)
	defer server.Close()

	embedder := NewOpenAIEmbedder(EmbeddingConfig{
		BaseURL: server.URL + "/v1",
		APIKey:  "test-key",
		Model:   "text-embedding-test",
	})
	vector, err := embedder.Embed(context.Background(), "测试文本", constants.MemoryEmbeddingActionAdd)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vector) != 3 || vector[0] != 0.1 {
		t.Fatalf("unexpected vector: %v", vector)
	}
	if captured.Model != "text-embedding-test" || len(captured.Input) != 1 ||
		captured.Input[0] != "测试文本" {
		t.Fatalf("unexpected request payload: %+v", captured)
	}

	// 批量嵌入一次请求完成。
	if _, err := embedder.EmbedBatch(
		context.Background(),
		[]string{"第一条", "第二条"},
		constants.MemoryEmbeddingActionAdd,
	); err != nil {
		t.Fatalf("embed batch: %v", err)
	}
	if len(captured.Input) != 2 {
		t.Fatalf("expected batch input of 2, got %+v", captured.Input)
	}
}

func TestOpenAIEmbedderErrors(t *testing.T) {
	var captured embedRequestPayload
	server := newEmbeddingServer(t, 3, &captured)
	defer server.Close()

	// 鉴权失败映射为 EmbeddingUnavailable。
	embedder := NewOpenAIEmbedder(EmbeddingConfig{
		BaseURL: server.URL + "/v1",
		APIKey:  "wrong-key",
		Model:   "text-embedding-test",
	})
	if _, err := embedder.Embed(context.Background(), "测试", constants.MemoryEmbeddingActionSearch); err == nil {
		t.Fatalf("expected error for unauthorized")
	} else if !errors.Is(err, status_error.MemoryEmbeddingUnavailable) {
		t.Fatalf("expected embedding-unavailable error, got %v", err)
	}

	// 不可达地址返回错误。
	unreachable := NewOpenAIEmbedder(EmbeddingConfig{
		BaseURL: "http://127.0.0.1:1",
		APIKey:  "k",
		Model:   "m",
	})
	if _, err := unreachable.Embed(context.Background(), "测试", constants.MemoryEmbeddingActionAdd); err == nil {
		t.Fatalf("expected error for unreachable endpoint")
	}
	invalid := NewOpenAIEmbedder(EmbeddingConfig{BaseURL: "://invalid", APIKey: "k", Model: "m"})
	if _, err := invalid.Embed(context.Background(), "测试", constants.MemoryEmbeddingActionAdd); err != status_error.MemoryEmbeddingRequestFailed {
		t.Fatalf("expected original request-failed error, got %v", err)
	}

	// 调用取消返回独立的取消语义。
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := embedder.Embed(canceledContext, "测试", constants.MemoryEmbeddingActionSearch); err == nil {
		t.Fatal("expected canceled request to fail")
	} else if !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("expected operation-canceled error, got %v", err)
	}
}

func TestOpenAIEmbedderMatchesVectorsToInputIndices(t *testing.T) {
	cases := []struct {
		// name 标识服务响应与输入的映射场景。
		name string
		// payload 是服务端实际返回的 JSON。
		payload string
		// invalid 表示响应不能确定每个输入的唯一向量。
		invalid bool
	}{
		{"reordered", `{"data":[{"index":1,"embedding":[0,1]},{"index":0,"embedding":[1,0]}]}`, false},
		{"duplicate", `{"data":[{"index":0,"embedding":[1,0]},{"index":0,"embedding":[0,1]}]}`, true},
		{"outside", `{"data":[{"index":0,"embedding":[1,0]},{"index":2,"embedding":[0,1]}]}`, true},
		{"mixed", `{"data":[{"index":0,"embedding":[1,0]},{"embedding":[0,1]}]}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.payload)
			}))
			defer server.Close()
			embedder := NewOpenAIEmbedder(EmbeddingConfig{BaseURL: server.URL, Model: "model-a", APIKey: "test-key"})
			vectors, err := embedder.EmbedBatch(context.Background(), []string{"first", "second"}, constants.MemoryEmbeddingActionAdd)
			if tc.invalid {
				if !errors.Is(err, status_error.MemoryEmbeddingResponseInvalid) {
					t.Fatalf("ambiguous input mapping accepted: %v %v", vectors, err)
				}
				return
			}
			if err != nil || len(vectors) != 2 || vectors[0][0] != 1 || vectors[1][1] != 1 {
				t.Fatalf("response indices ignored: %v %v", vectors, err)
			}
		})
	}
}
