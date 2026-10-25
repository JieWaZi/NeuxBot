package im

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Weixin 使用腾讯当前 iLink 协议，不依赖社区 SDK 的过期二维码流程。
type Weixin struct {
	// Client 允许测试替换 HTTP 边界，生产使用受限重定向客户端。
	Client *http.Client
}

// WeixinQR 保存登录进度，不对管理端返回认证令牌。
type WeixinQR struct {
	// Code 是服务器二维码轮询身份。
	Code string `json:"qrcode"`
	// Content 是交给前端二维码展示的编码内容。
	Content string `json:"qrcode_img_content"`
	// Status 是腾讯当前登录阶段。
	Status string `json:"status"`
	// Token 是确认后的机器人凭据，仅用于服务内保存。
	Token string `json:"bot_token"`
	// BotID 是已登录机器人身份。
	BotID string `json:"ilink_bot_id"`
	// OwnerID 是扫码用户身份。
	OwnerID string `json:"ilink_user_id"`
	// BaseURL 是服务器返回的实际 API 地址。
	BaseURL string `json:"baseurl"`
	// RedirectHost 是跨域扫码流程指定的后续地址。
	RedirectHost string `json:"redirect_host"`
}

// weixinResponse 保存每个接口共同的业务返回及消息批次。
type weixinResponse struct {
	// Ret 是主要业务返回码。
	Ret int `json:"ret"`
	// ErrorCode 是附加业务失败码。
	ErrorCode int `json:"errcode"`
	// Messages 是本轮新输入。
	Messages []weixinMessage `json:"msgs"`
	// Cursor 是下轮长轮询游标。
	Cursor string `json:"get_updates_buf"`
	// TimeoutMillis 是服务器建议的下一次等待时长。
	TimeoutMillis int64 `json:"longpolling_timeout_ms"`
	// UploadURL 是服务器返回的实际媒体上传地址。
	UploadURL string `json:"upload_full_url"`
	// UploadParam 是没有完整地址时的媒体上传参数。
	UploadParam string `json:"upload_param"`
}

// weixinMessage 只解析真实输入和发送所需的协议字段。
type weixinMessage struct {
	// ID 是服务端消息身份。
	ID json.Number `json:"message_id,omitempty"`
	// From 是真实用户身份；机器人发送时为空。
	From string `json:"from_user_id"`
	// To 是接收方身份。
	To string `json:"to_user_id"`
	// ClientID 是发送的幂等标识。
	ClientID string `json:"client_id,omitempty"`
	// Type 区分用户输入与机器人消息。
	Type int `json:"message_type"`
	// State 是完整消息阶段。
	State int `json:"message_state"`
	// CreatedAt 是平台生成的发送时间。
	CreatedAt int64 `json:"create_time_ms,omitempty"`
	// GroupID 只用于识别并拒绝当前协议未支持的群聊。
	GroupID string `json:"group_id,omitempty"`
	// Context 是对该单聊回复需要的令牌。
	Context string `json:"context_token,omitempty"`
	// Items 是规范化前的文本及媒体内容。
	Items []weixinItem `json:"item_list"`
}

// weixinItem 保留当前客户端支持的文本及媒体对象。
type weixinItem struct {
	// Type 是腾讯协议的内容类型。
	Type int `json:"type"`
	// Text 保存文本内容。
	Text *weixinText `json:"text_item,omitempty"`
	// Image 保存图片资源。
	Image *weixinMediaItem `json:"image_item,omitempty"`
	// File 保存文件资源。
	File *weixinMediaItem `json:"file_item,omitempty"`
	// Voice 保存语音及平台转写。
	Voice *weixinMediaItem `json:"voice_item,omitempty"`
	// Video 保存视频资源。
	Video *weixinMediaItem `json:"video_item,omitempty"`
}

// weixinText 表示一段普通文本。
type weixinText struct {
	// Text 是原始内容。
	Text string `json:"text"`
}

// weixinMediaItem 描述入站或出站媒体引用。
type weixinMediaItem struct {
	// Media 保存 CDN 下载和解密参数。
	Media weixinMedia `json:"media"`
	// Name 是平台提供的文件名称。
	Name string `json:"file_name,omitempty"`
	// AESKey 是图片可选的十六进制密钥。
	AESKey string `json:"aeskey,omitempty"`
	// Text 是语音的已有转写，不发起额外识别任务。
	Text string `json:"text,omitempty"`
	// Length 是文件原始字节数的十进制编码。
	Length string `json:"len,omitempty"`
	// Size 是图片密文字节数。
	Size int `json:"mid_size,omitempty"`
}

// weixinMedia 表示腾讯 CDN 加密媒体引用。
type weixinMedia struct {
	// Query 是下载所需的不可推断定位参数。
	Query string `json:"encrypt_query_param"`
	// AESKey 是协议允许的 base64 编码媒体密钥。
	AESKey string `json:"aes_key"`
	// Type 是当前协议的加密类型。
	Type int `json:"encrypt_type"`
	// URL 是服务器可选返回的完整官方媒体地址。
	URL string `json:"full_url,omitempty"`
}

func (w *Weixin) client() *http.Client {
	if w.Client != nil {
		return w.Client
	}
	return HTTPClient(false)
}

// QR 使用当前 POST 协议获取新的登录二维码。
func (w *Weixin) QR(ctx context.Context) (WeixinQR, error) {
	var qr WeixinQR
	_, err := w.request(ctx, constants.IMWeixinBaseURL, "/ilink/bot/get_bot_qrcode?bot_type=3", http.MethodPost, "", map[string]any{"local_token_list": []string{}}, &qr)
	if err == nil && (qr.Code == "" || qr.Content == "") {
		err = status_error.IMProtocolInvalid
	}
	return qr, err
}

// QRStatus 轮询同一次扫码流程，并传递用户明确输入的验证代码。
func (w *Weixin) QRStatus(ctx context.Context, base, code, verification string) (WeixinQR, error) {
	var result WeixinQR
	query := url.Values{"qrcode": []string{code}}
	if verification != "" {
		query.Set("verify_code", verification)
	}
	_, err := w.request(ctx, base, "/ilink/bot/get_qrcode_status?"+query.Encode(), http.MethodGet, "", nil, &result)
	return result, err
}

func (w *Weixin) request(ctx context.Context, base, path, method, token string, input, output any) (bool, error) {
	if !OfficialURL(base, false) {
		return true, status_error.IMProtocolInvalid
	}
	var body io.Reader
	if input != nil {
		value, err := json.Marshal(input)
		if err != nil {
			return true, status_error.IMProtocolInvalid
		}
		body = bytes.NewReader(value)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, body)
	if err != nil {
		return true, status_error.IMProtocolInvalid
	}
	req.Header.Set("iLink-App-Id", "bot")
	req.Header.Set("iLink-App-ClientVersion", "132105")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("AuthorizationType", "ilink_bot_token")
		var random [4]byte
		if _, err := rand.Read(random[:]); err != nil {
			return true, status_error.IMCredentialError
		}
		req.Header.Set("X-WECHAT-UIN", base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(binary.BigEndian.Uint32(random[:])), 10))))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	response, err := w.client().Do(req)
	if err != nil {
		logx.Errorf("weixin request failed: endpoint=%s error_type=%T", strings.SplitN(path, "?", 2)[0], err)
		return false, status_error.IMPlatformUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return true, status_error.IMLoginRequired
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode < 500, status_error.IMPlatformUnavailable
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2*1024*1024))
	decoder.UseNumber()
	if err := decoder.Decode(output); err != nil {
		logx.Errorf("weixin response decode failed: endpoint=%s error_type=%T", strings.SplitN(path, "?", 2)[0], err)
		return false, status_error.IMProtocolInvalid
	}
	return true, nil
}

func weixinBase(connection models.IMConnection) string {
	if connection.BaseURL != "" {
		return connection.BaseURL
	}
	return constants.IMWeixinBaseURL
}
func weixinInfo() map[string]string {
	return map[string]string{"channel_version": "2.4.9", "bot_agent": "NeuxBot/0.1"}
}

// Run 严格在整批收件和游标已提交后，才进入下一次长轮询。
func (w *Weixin) Run(ctx context.Context, connection models.IMConnection, commit BatchCommit, state StateChanged) error {
	if connection.Credential == "" {
		return status_error.IMLoginRequired
	}
	cursor := connection.Cursor
	wait := 35 * time.Second
	for ctx.Err() == nil {
		requestContext, cancel := context.WithTimeout(ctx, wait+10*time.Second)
		var result weixinResponse
		_, err := w.request(requestContext, weixinBase(connection), "/ilink/bot/getupdates", http.MethodPost, connection.Credential, map[string]any{"get_updates_buf": cursor, "base_info": weixinInfo()}, &result)
		cancel()
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		if result.Ret == -14 || result.ErrorCode == -14 {
			return status_error.IMLoginRequired
		}
		if result.Ret != 0 || result.ErrorCode != 0 {
			return status_error.IMPlatformUnavailable
		}
		messages := []Message{}
		for _, input := range result.Messages {
			if input.Type != 1 || input.GroupID != "" || input.From == "" || input.ID.String() == "" {
				continue
			}
			message := Message{ID: input.ID.String(), ChatID: input.From, Kind: constants.IMChatKindDirect, SenderID: input.From, ContextToken: input.Context, SentAt: input.CreatedAt}
			for _, item := range input.Items {
				if item.Text != nil {
					message.Content += item.Text.Text
				}
				if item.Voice != nil && item.Voice.Text != "" {
					message.Content += item.Voice.Text
				}
				for _, entry := range []struct {
					// item 是当前协议媒体对象。
					item *weixinMediaItem
					// kind 是业务文件类型。
					kind string
				}{{item.Image, "image"}, {item.File, "file"}, {item.Voice, "audio"}, {item.Video, "video"}} {
					if entry.item == nil {
						continue
					}
					media := entry.item.Media
					key := media.URL
					if key == "" {
						key = constants.IMWeixinCDNBaseURL + "/download?encrypted_query_param=" + url.QueryEscape(media.Query)
					}
					aesKey := media.AESKey
					if entry.item.AESKey != "" {
						aesKey = base64.StdEncoding.EncodeToString([]byte(entry.item.AESKey))
					}
					message.Files = append(message.Files, models.IMFile{Name: entry.item.Name, Kind: entry.kind, Key: key, AESKey: aesKey})
				}
			}
			if strings.TrimSpace(message.Content) != "" || len(message.Files) > 0 {
				messages = append(messages, message)
			}
		}
		next := result.Cursor
		if next == "" {
			next = cursor
		}
		if err := commit(ctx, messages, next); err != nil {
			return err
		}
		cursor = next
		state(constants.IMConnectionStateConnected, "")
		if result.TimeoutMillis >= 1000 && result.TimeoutMillis <= 55000 {
			wait = time.Duration(result.TimeoutMillis) * time.Millisecond
		}
	}
	return nil
}

// Send 发出一个稳定 client_id 的文本或媒体片段，并区分明确拒绝与未知发送。
func (w *Weixin) Send(ctx context.Context, connection models.IMConnection, binding models.IMBinding, id, text string, file *models.IMFile) (string, bool, error) {
	if binding.ContextToken == "" {
		return "", true, status_error.IMDeliveryContextUnavailable
	}
	item := weixinItem{Type: 1, Text: &weixinText{Text: text}}
	if file != nil {
		media, err := w.upload(ctx, connection, binding, *file)
		if err != nil {
			return "", true, err
		}
		item = media
	}
	var result weixinResponse
	definitive, err := w.request(ctx, weixinBase(connection), "/ilink/bot/sendmessage", http.MethodPost, connection.Credential, map[string]any{"msg": weixinMessage{To: binding.ExternalChatID, ClientID: id, Type: 2, State: 2, Context: binding.ContextToken, Items: []weixinItem{item}}, "base_info": weixinInfo()}, &result)
	if err != nil {
		return "", definitive, err
	}
	if result.Ret == -14 || result.ErrorCode == -14 {
		return "", true, status_error.IMLoginRequired
	}
	if result.Ret != 0 || result.ErrorCode != 0 {
		return "", true, status_error.IMPlatformUnavailable
	}
	// 腾讯只回传发送成功；client_id 是客户端关联收据，不代表已读或平台消息编号。
	return id, true, nil
}
