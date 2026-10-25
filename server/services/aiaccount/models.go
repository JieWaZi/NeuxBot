// Package aiaccount 管理账号生命周期与模型目录发现。
package aiaccount

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const (
	// modelsTimeout 限制目录发现请求的最长等待时间。
	modelsTimeout = 15 * time.Second
	// modelsMaxResponseBytes 防止异常服务返回过大的响应体。
	modelsMaxResponseBytes = 2 << 20
)

// Model 是远端目录返回的模型标识和名称。
type Model struct {
	// ID 是远端服务接受的原始模型标识。
	ID string
	// Name 是远端服务返回的展示名称。
	Name string
}

type accountModelsResponse struct {
	// Data 是兼容模型接口返回的模型条目。
	Data []struct {
		// ID 是服务端接受的模型标识。
		ID string `json:"id"`
		// Name 是 OpenAI 兼容响应中的展示名称。
		Name string `json:"name"`
		// DisplayName 是 Anthropic 模型响应中的展示名称。
		DisplayName string `json:"display_name"`
	} `json:"data"`
}

func DiscoverModels(ctx context.Context, endpoint models.AIAccountEndpoint, apiKey string) ([]Model, error) {
	if endpoint.Protocol == constants.AccountProtocolGoogleGenerativeAI {
		return discoverGoogleModels(ctx, endpoint.BaseURL, apiKey)
	}
	modelsURL, err := ModelsURL(endpoint)
	if err != nil {
		return nil, status_error.AIAccountConfigurationInvalid
	}
	requestCtx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, status_error.AIAccountConfigurationInvalid
	}
	request.Header.Set("Accept", "application/json")
	if endpoint.Protocol == constants.AccountProtocolAnthropicMessages && !isDeepSeekAnthropicModelsEndpoint(endpoint) {
		request.Header.Set("x-api-key", apiKey)
		request.Header.Set("anthropic-version", "2023-06-01")
	} else {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		logx.Errorf("fetch account models failed: error=%v", err)
		return nil, status_error.AIAccountModelsUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed || response.StatusCode == http.StatusNotImplemented {
		return []Model{}, nil
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		logx.Errorf("fetch account models failed: status=%d", response.StatusCode)
		return nil, status_error.AIAccountModelsUnavailable
	}
	var payload accountModelsResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, modelsMaxResponseBytes)).Decode(&payload); err != nil {
		logx.Errorf("decode account models failed: error=%v", err)
		return nil, status_error.AIAccountModelsResponseInvalid
	}
	items := make([]Model, 0, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = strings.TrimSpace(item.DisplayName)
		}
		if name == "" {
			name = id
		}
		items = append(items, Model{ID: id, Name: name})
	}
	return items, nil
}

func ModelsURL(endpoint models.AIAccountEndpoint) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || parsed.Scheme == "" {
		return "", status_error.AIAccountConfigurationInvalid
	}
	if isDeepSeekAnthropicModelsEndpoint(endpoint) {
		parsed.Path = "/models"
		return parsed.String(), nil
	}
	path := strings.TrimRight(parsed.Path, "/")
	for _, suffix := range []string{"/chat/completions", "/responses", "/messages"} {
		if strings.HasSuffix(path, suffix) {
			path = strings.TrimSuffix(path, suffix)
			break
		}
	}
	if endpoint.Protocol == constants.AccountProtocolAnthropicMessages && path == "" {
		path = "/v1"
	}
	if endpoint.Protocol == constants.AccountProtocolAnthropicMessages && !hasVersionPath(path) {
		path += "/v1"
	}
	parsed.Path = path + "/models"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func isDeepSeekAnthropicModelsEndpoint(endpoint models.AIAccountEndpoint) bool {
	if endpoint.Protocol != constants.AccountProtocolAnthropicMessages {
		return false
	}
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/"))
	return err == nil && parsed.Scheme == "https" && parsed.Host == "api.deepseek.com" && parsed.Path == "/anthropic" && parsed.RawQuery == "" && parsed.Fragment == ""
}

func hasVersionPath(path string) bool {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 0 {
		return false
	}
	last := segments[len(segments)-1]
	if len(last) < 2 || last[0] != 'v' {
		return false
	}
	for _, character := range last[1:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
