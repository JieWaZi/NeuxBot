// Package agentcli 实现随应用交付的非交互命令客户端，不访问领域数据库。
package agentcli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
)

// client 固定一次 CLI 进程读取的授权，不在失败后换用下一轮连接文件。
type client struct {
	// connection 是已经验证为本机固定入口的连接信息。
	connection agentcapability.Connection
}

func (c client) request(ctx context.Context, method, path string, payload any, mutation bool, timeout time.Duration) (agentcapability.Response, error) {
	var body bytes.Buffer
	if payload != nil && json.NewEncoder(&body).Encode(payload) != nil {
		return agentcapability.Response{}, status_error.AgentInputInvalid
	}
	request, err := http.NewRequestWithContext(ctx, method, c.connection.Endpoint+path, &body)
	if err != nil {
		return agentcapability.Response{}, status_error.AgentConnectionUnavailable
	}
	request.Header.Set("Authorization", "Bearer "+c.connection.Token)
	request.Header.Set("Content-Type", "application/json")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	httpClient := http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := httpClient.Do(request)
	uncertain := func() (agentcapability.Response, error) {
		if mutation {
			return agentcapability.Response{}, status_error.AgentCommandOutcomeUnknown
		}
		return agentcapability.Response{}, status_error.AgentConnectionUnavailable
	}
	if err != nil {
		return uncertain()
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, constants.AgentCLIRequestMaximumBytes+1))
	if err != nil || len(data) > constants.AgentCLIRequestMaximumBytes {
		return uncertain()
	}
	var result agentcapability.Response
	if json.Unmarshal(data, &result) != nil || result.ProtocolVersion != constants.AgentCLIProtocolVersion {
		return uncertain()
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !result.OK {
		if result.Error == nil {
			return uncertain()
		}
		return result, nil
	}
	if result.Error != nil {
		return uncertain()
	}
	return result, nil
}

func receiptPath(operation, requestID string) string {
	query := url.Values{"operation": []string{operation}, "requestId": []string{requestID}}
	return "/receipts?" + query.Encode()
}
