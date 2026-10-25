package aiaccount

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// googleModelsPage 严格承载 Google 官方分页模型目录，不把 thinking 布尔值推断为档位。
type googleModelsPage struct {
	// Models 是本页模型；指针区分缺少字段与真实空目录。
	Models *[]struct {
		// Name 是 models/ 开头的资源名称。
		Name string `json:"name"`
		// DisplayName 是服务提供的展示名称。
		DisplayName string `json:"displayName"`
		// SupportedGenerationMethods 声明模型实际接受的生成方法。
		SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	} `json:"models"`
	// NextPageToken 指向下一页目录，空值表示完成。
	NextPageToken string `json:"nextPageToken"`
}

// discoverGoogleModels 使用专属请求头与有界分页读取可执行 GenerateContent 的模型。
func discoverGoogleModels(ctx context.Context, endpoint, apiKey string) ([]Model, error) {
	base, version := accountconfig.GoogleEndpoint(endpoint)
	parsed, err := url.Parse(base + "/" + version + "/models")
	if err != nil || parsed.Host == "" {
		return nil, status_error.AIAccountConfigurationInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	// 禁止自动跨站重定向传播 Google Key。
	client := *http.DefaultClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	result := []Model{}
	seenPages := map[string]bool{}
	seenModels := map[string]bool{}
	for range 20 {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return nil, status_error.AIAccountConfigurationInvalid
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("x-goog-api-key", apiKey)
		response, err := client.Do(request)
		if err != nil {
			logx.Errorf("fetch Google account models failed: error=%v", err)
			return nil, status_error.AIAccountModelsUnavailable
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, modelsMaxResponseBytes+1))
		response.Body.Close()
		if readErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
			logx.Errorf("read Google account models failed: status=%d error=%v", response.StatusCode, readErr)
			return nil, status_error.AIAccountModelsUnavailable
		}
		var page googleModelsPage
		if len(data) > modelsMaxResponseBytes {
			return nil, status_error.AIAccountModelsResponseInvalid
		}
		if err := json.Unmarshal(data, &page); err != nil {
			logx.Errorf("decode Google account models failed: error=%v", err)
			return nil, status_error.AIAccountModelsResponseInvalid
		}
		if page.Models == nil {
			return nil, status_error.AIAccountModelsResponseInvalid
		}
		for _, model := range *page.Models {
			if !slices.Contains(model.SupportedGenerationMethods, "generateContent") {
				continue
			}
			id := strings.TrimPrefix(model.Name, "models/")
			if id == "" || id == model.Name || strings.TrimSpace(id) != id {
				return nil, status_error.AIAccountModelsResponseInvalid
			}
			if seenModels[id] {
				continue
			}
			seenModels[id] = true
			name := strings.TrimSpace(model.DisplayName)
			if name == "" {
				name = id
			}
			result = append(result, Model{ID: id, Name: name})
		}
		if page.NextPageToken == "" {
			return result, nil
		}
		if len(page.NextPageToken) > 4096 || seenPages[page.NextPageToken] {
			return nil, status_error.AIAccountModelsResponseInvalid
		}
		seenPages[page.NextPageToken] = true
		query := parsed.Query()
		query.Set("pageToken", page.NextPageToken)
		parsed.RawQuery = query.Encode()
	}
	return nil, status_error.AIAccountModelsResponseInvalid
}
