package integration_knowledge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/JieWaZi/neux-knowledge/remote"
)

// contractTransport 按固定协议样本响应远端客户端，不启动外部服务。
type contractTransport struct {
	// responses 是按真实请求路径保存的响应正文。
	responses map[string]json.RawMessage
	// testing 记录认证或目标路径错误。
	testing *testing.T
}

func (c contractTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.testing.Helper()
	if r.Method != http.MethodGet || r.Header.Get("X-API-Key") != "fixture-only" || r.Header.Get("Authorization") != "" {
		c.testing.Fatalf("unexpected authentication or method: %s %s", r.Method, r.URL.Path)
	}
	body, ok := c.responses[r.URL.Path]
	if !ok {
		c.testing.Fatalf("unexpected target: %s", r.URL.Path)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
}

// TestWeKnoraContractFixtures 防止兼容合同改变包装、库身份或 Wiki 路径后仍被客户端当作成功。
func TestWeKnoraContractFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/weknora_contracts.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		// Responses 是固定提交中已核对的只读响应。
		Responses map[string]json.RawMessage `json:"responses"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	client, err := remote.NewWeKnora(remote.Options{BaseURL: "https://example.test", Token: "fixture-only", AuthMode: "api_key", HTTPClient: &http.Client{Transport: contractTransport{responses: fixtures.Responses, testing: t}}})
	if err != nil {
		t.Fatal(err)
	}
	library, err := client.GetLibrary(context.Background(), "library-a")
	if err != nil || library.ID != "library-a" || library.Name != "合同示例" {
		t.Fatalf("library: %#v %v", library, err)
	}
	page, err := client.GetPage(context.Background(), "library-a", "主题/入门")
	if err != nil || page.KnowledgeBaseID != "library-a" || page.Title != "入门" {
		t.Fatalf("wiki: %#v %v", page, err)
	}
	if _, err := client.GetLibrary(context.Background(), "malformed"); err == nil {
		t.Fatal("unknown response was accepted as an empty library")
	}
}
