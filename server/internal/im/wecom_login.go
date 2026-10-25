package im

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"runtime"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// WecomQR 描述官方 CLI 使用的二维码会话；认证信息仅在 Core 内部读取。
type WecomQR struct {
	// Code 是私有扫码轮询身份。
	Code string `json:"scode"`
	// Content 是供企业微信扫描的官方授权链接。
	Content string `json:"auth_url"`
	// Status 是平台返回的扫码状态。
	Status string `json:"status"`
	// Bot 保存扫码确认后签发的机器人凭据。
	Bot struct {
		// ID 是完成授权的机器人身份。
		ID string `json:"botid"`
		// Secret 是仅写入加密凭据存储的长连接密钥。
		Secret string `json:"secret"`
	} `json:"bot_info"`
}

// QR 复用官方 wecom-openclaw-cli 的一键创建机器人流程。
func (w *Wecom) QR(ctx context.Context) (WecomQR, error) {
	platform := "0"
	switch runtime.GOOS {
	case "darwin":
		platform = "1"
	case "windows":
		platform = "2"
	case "linux":
		platform = "3"
	}
	result, err := w.qrRequest(ctx, "/generate?source=neuxally&plat="+platform)
	if err != nil {
		return result, err
	}
	u, parseErr := url.Parse(result.Content)
	if result.Code == "" || parseErr != nil || u.Scheme != "https" || u.Host != "work.weixin.qq.com" || u.User != nil {
		return WecomQR{}, status_error.IMProtocolInvalid
	}
	return result, nil
}

// QRStatus 查询当前二维码，不把二维码代码或 Bot Secret 放进日志与 API 响应。
func (w *Wecom) QRStatus(ctx context.Context, code string) (WecomQR, error) {
	return w.qrRequest(ctx, "/query_result?scode="+url.QueryEscape(code))
}

func (w *Wecom) qrRequest(ctx context.Context, path string) (WecomQR, error) {
	var response struct {
		// Data 是官方 CLI 使用的扫码响应载荷。
		Data WecomQR `json:"data"`
	}
	client := w.client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return status_error.IMProtocolInvalid }}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, constants.IMWecomQRBaseURL+path, nil)
	if err != nil {
		return response.Data, status_error.IMProtocolInvalid
	}
	result, err := client.Do(request)
	if err != nil {
		if ctx.Err() == nil {
			// 只记录路径及错误类别，URL 查询和底层错误可能带私有扫码身份。
			logx.Errorf("wecom QR request failed: endpoint=%s error_type=%T", request.URL.Path, err)
		}
		return response.Data, status_error.IMPlatformUnavailable
	}
	defer result.Body.Close()
	if result.StatusCode != http.StatusOK {
		return response.Data, status_error.IMPlatformUnavailable
	}
	value, err := readMediaLimit(result.Body, 64*1024)
	if err != nil {
		return response.Data, err
	}
	if err := json.Unmarshal(value, &response); err != nil {
		logx.Errorf("wecom QR response decode failed: endpoint=%s error_type=%T", request.URL.Path, err)
		return response.Data, status_error.IMProtocolInvalid
	}
	return response.Data, nil
}
