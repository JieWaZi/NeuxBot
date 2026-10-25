package im

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

// Feishu 使用官方 SDK 的同步事件分派，持久收件失败时不确认消息。
type Feishu struct{}

// feishuContent 描述首版接收的文字、图片或文件。
type feishuContent struct {
	// Text 是普通文字。
	Text string `json:"text"`
	// FileKey 是文件或语音资源身份。
	FileKey string `json:"file_key"`
	// ImageKey 是图片资源身份。
	ImageKey string `json:"image_key"`
	// Name 是文件名称。
	Name string `json:"file_name"`
}

// Run 在连接真实就绪后更新状态，所有输入先经过调用者耐久收取。
func (*Feishu) Run(ctx context.Context, connection models.IMConnection, commit BatchCommit, state StateChanged) error {
	if connection.AppID == "" || connection.Credential == "" {
		return status_error.IMLoginRequired
	}
	client := lark.NewClient(connection.AppID, connection.Credential, lark.WithLogLevel(larkcore.LogLevelError))
	botID, err := feishuBotID(ctx, client)
	if err != nil {
		return err
	}
	handler := dispatcher.NewEventDispatcher("", "").OnP2MessageReceiveV1(func(_ context.Context, event *larkim.P2MessageReceiveV1) error {
		message, accepted, err := normalizeFeishu(event, botID)
		if err != nil {
			return err
		}
		if !accepted {
			return nil
		}
		// 不使用 SDK 高层异步 channel pipeline：本回调返回即触发平台 ACK。
		return commit(ctx, []Message{message}, "")
	})
	socket := larkws.NewClient(connection.AppID, connection.Credential,
		larkws.WithEventHandler(handler), larkws.WithLogLevel(larkcore.LogLevelError),
		larkws.WithOnReady(func() { state(constants.IMConnectionStateConnected, "") }),
		larkws.WithOnReconnected(func() { state(constants.IMConnectionStateConnected, "") }),
		larkws.WithOnReconnecting(func() { state(constants.IMConnectionStateConnecting, "") }),
		larkws.WithOnDisconnected(func() { state(constants.IMConnectionStateDisconnected, "") }),
	)
	defer socket.Close()
	if err := socket.Start(ctx); err != nil && ctx.Err() == nil {
		logx.Errorf("feishu websocket failed: connection_id=%s error_type=%T", connection.ID, err)
		return status_error.IMPlatformUnavailable
	}
	return nil
}

func feishuBotID(ctx context.Context, client *lark.Client) (string, error) {
	response, err := client.Do(ctx, &larkcore.ApiReq{HttpMethod: "GET", ApiPath: "/open-apis/bot/v3/info", SupportedAccessTokenTypes: []larkcore.AccessTokenType{larkcore.AccessTokenTypeTenant}})
	if err != nil {
		return "", status_error.IMPlatformUnavailable
	}
	var result struct {
		// Code 是飞书业务错误码。
		Code int `json:"code"`
		// Bot 包含机器人身份。
		Bot struct {
			// OpenID 是群提及需要比较的机器人身份。
			OpenID string `json:"open_id"`
		} `json:"bot"`
	}
	if err := json.Unmarshal(response.RawBody, &result); err != nil || result.Code != 0 || result.Bot.OpenID == "" {
		return "", status_error.IMProtocolInvalid
	}
	return result.Bot.OpenID, nil
}

func normalizeFeishu(event *larkim.P2MessageReceiveV1, botID string) (Message, bool, error) {
	var result Message
	if event == nil || event.Event == nil || event.Event.Message == nil || event.Event.Sender == nil || event.Event.Sender.SenderId == nil {
		return result, false, status_error.IMProtocolInvalid
	}
	sender := event.Event.Sender
	input := event.Event.Message
	if pointer(sender.SenderType) != "user" || (pointer(input.ChatType) != "p2p" && pointer(input.ChatType) != "group") {
		return result, false, nil
	}
	result = Message{ID: pointer(input.MessageId), ChatID: pointer(input.ChatId), SenderID: pointer(sender.SenderId.OpenId), Kind: constants.IMChatKindDirect, QuoteID: pointer(input.ParentId)}
	if result.ID == "" || result.ChatID == "" || result.SenderID == "" {
		return result, false, status_error.IMProtocolInvalid
	}
	result.SentAt, _ = strconv.ParseInt(pointer(input.CreateTime), 10, 64)
	if pointer(input.ChatType) == "group" {
		result.Kind = constants.IMChatKindGroup
		mentioned := false
		for _, mention := range input.Mentions {
			if mention != nil && mention.Id != nil && pointer(mention.Id.OpenId) == botID {
				mentioned = true
			}
		}
		if !mentioned {
			return result, false, nil
		}
	}
	var content feishuContent
	if err := json.Unmarshal([]byte(pointer(input.Content)), &content); err != nil {
		return result, false, status_error.IMProtocolInvalid
	}
	switch pointer(input.MessageType) {
	case "text":
		result.Content = content.Text
	case "image":
		result.Files = []models.IMFile{{Kind: "image", Key: content.ImageKey, Name: "image.png"}}
	case "file", "audio", "media":
		result.Files = []models.IMFile{{Kind: "file", Key: content.FileKey, Name: content.Name}}
	case "post":
		var post map[string]struct {
			// Title 是富文本标题。
			Title string `json:"title"`
			// Content 是按行排列的富文本内容。
			Content [][]struct {
				// Tag 指明富文本节点类型。
				Tag string `json:"tag"`
				// Text 是文字或链接标题。
				Text string `json:"text"`
				// URL 是可见链接地址，不用于自动请求。
				URL string `json:"href"`
				// ImageKey 是消息所属图片资源。
				ImageKey string `json:"image_key"`
			} `json:"content"`
		}
		if err := json.Unmarshal([]byte(pointer(input.Content)), &post); err != nil {
			return result, false, status_error.IMProtocolInvalid
		}
		// 平台富文本同时含多种语言时，优先中文且只采用一个完整版本。
		selected, exists := post["zh_cn"]
		if !exists {
			selected, exists = post["en_us"]
		}
		if !exists {
			for _, value := range post {
				selected = value
				break
			}
		}
		result.Content = selected.Title
		for _, row := range selected.Content {
			result.Content += "\n"
			for _, node := range row {
				switch node.Tag {
				case "text":
					result.Content += node.Text
				case "a":
					result.Content += node.Text + " (" + node.URL + ")"
				case "img":
					result.Files = append(result.Files, models.IMFile{Kind: "image", Key: node.ImageKey, Name: "image.png"})
				}
			}
		}
	default:
		return result, false, nil
	}
	for _, mention := range input.Mentions {
		if mention != nil && mention.Id != nil && pointer(mention.Id.OpenId) == botID {
			result.Content = strings.ReplaceAll(result.Content, pointer(mention.Key), "")
		}
	}
	result.Content = strings.TrimSpace(result.Content)
	return result, result.Content != "" || len(result.Files) > 0, nil
}

// Send 使用固定 UUID 发送当前冻结片段，不将上传成功当作消息交付。
func (*Feishu) Send(ctx context.Context, connection models.IMConnection, binding models.IMBinding, id, text string, file *models.IMFile) (string, bool, error) {
	client := lark.NewClient(connection.AppID, connection.Credential, lark.WithLogLevel(larkcore.LogLevelError))
	messageType := "text"
	content := map[string]string{"text": text}
	if file != nil {
		source, err := os.Open(file.Path)
		if err != nil {
			return "", true, status_error.IMFileInvalid
		}
		defer source.Close()
		if file.Kind == "image" {
			response, err := client.Im.Image.Create(ctx, larkim.NewCreateImageReqBuilder().Body(larkim.NewCreateImageReqBodyBuilder().ImageType("message").Image(source).Build()).Build())
			if err != nil {
				return "", true, status_error.IMPlatformUnavailable
			}
			if !response.Success() || response.Data == nil || response.Data.ImageKey == nil {
				return "", true, status_error.IMPlatformUnavailable
			}
			messageType = "image"
			content = map[string]string{"image_key": *response.Data.ImageKey}
		} else {
			response, err := client.Im.File.Create(ctx, larkim.NewCreateFileReqBuilder().Body(larkim.NewCreateFileReqBodyBuilder().FileType("stream").FileName(file.Name).File(source).Build()).Build())
			if err != nil {
				return "", true, status_error.IMPlatformUnavailable
			}
			if !response.Success() || response.Data == nil || response.Data.FileKey == nil {
				return "", true, status_error.IMPlatformUnavailable
			}
			messageType = "file"
			content = map[string]string{"file_key": *response.Data.FileKey}
		}
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return "", true, status_error.IMProtocolInvalid
	}
	response, err := client.Im.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().ReceiveIdType("chat_id").Body(larkim.NewCreateMessageReqBodyBuilder().ReceiveId(binding.ExternalChatID).MsgType(messageType).Content(string(encoded)).Uuid(id).Build()).Build())
	if err != nil {
		return "", false, status_error.IMPlatformUnavailable
	}
	if !response.Success() {
		return "", true, status_error.IMPlatformUnavailable
	}
	if response.Data == nil || response.Data.MessageId == nil {
		return "", false, status_error.IMProtocolInvalid
	}
	return *response.Data.MessageId, true, nil
}

// Download 只通过消息资源接口读取输入消息实际拥有的附件。
func (*Feishu) Download(ctx context.Context, connection models.IMConnection, messageID string, file models.IMFile) ([]byte, error) {
	client := lark.NewClient(connection.AppID, connection.Credential, lark.WithLogLevel(larkcore.LogLevelError))
	kind := "file"
	if file.Kind == "image" {
		kind = "image"
	}
	response, err := client.Im.MessageResource.Get(ctx, larkim.NewGetMessageResourceReqBuilder().MessageId(messageID).FileKey(file.Key).Type(kind).Build())
	if err != nil {
		return nil, status_error.IMPlatformUnavailable
	}
	if !response.Success() || response.File == nil {
		return nil, status_error.IMPlatformUnavailable
	}
	if closer, ok := response.File.(io.Closer); ok {
		defer closer.Close()
	}
	return ReadMedia(response.File)
}

func pointer(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
