package im

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gorilla/websocket"
)

// Wecom 将官方智能机器人长连接协议接到既有 IM 收件和交付边界。
// 官方 Node SDK 的协议来源见 THIRD_PARTY_NOTICES.md；连接重试由 IM Service 统一拥有。
type Wecom struct {
	// mu 保护每个连接配置唯一的活跃传输。
	mu sync.Mutex
	// sessions 仅保存内存传输，不复制聊天授权或投递事实。
	sessions map[string]*wecomSession
	// endpoint 仅供同包测试替换官方 WebSocket 边界。
	endpoint string
	// client 仅供同包测试替换受限媒体 HTTP 边界。
	client *http.Client
}

type wecomFrame struct {
	// Command 区分平台推送与调用命令。
	Command string `json:"cmd,omitempty"`
	// Headers 保存请求关联身份。
	Headers wecomHeaders `json:"headers"`
	// Body 是按命令解释的协议载荷。
	Body json.RawMessage `json:"body,omitempty"`
	// ErrorCode 仅在服务端明确回执时存在。
	ErrorCode *int `json:"errcode,omitempty"`
}

type wecomHeaders struct {
	// RequestID 关联调用与唯一平台回执。
	RequestID string `json:"req_id"`
}

type wecomSession struct {
	// socket 是本次 Run 唯一的连接。
	socket *websocket.Conn
	// write 串行化业务发送与心跳。
	write sync.Mutex
	// mu 保护等待平台回执的调用。
	mu sync.Mutex
	// pending 不超过正在发送的业务调用数。
	pending map[string]chan wecomFrame
	// done 在本次连接退出时解除所有调用等待。
	done chan struct{}
}

// Run 完成真实认证后才公布连接；取消时关闭 socket 并等待唯一读协程退出。
func (w *Wecom) Run(ctx context.Context, connection models.IMConnection, commit BatchCommit, state StateChanged) error {
	if connection.AppID == "" || connection.Credential == "" {
		return status_error.IMLoginRequired
	}
	endpoint := w.endpoint
	if endpoint == "" {
		endpoint = constants.IMWecomWebSocketURL
	}
	socket, response, err := websocket.DefaultDialer.DialContext(ctx, endpoint, nil)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() == nil {
			logx.Errorf("wecom websocket dial failed: connection_id=%s error_type=%T", connection.ID, err)
		}
		return status_error.IMPlatformUnavailable
	}
	session := &wecomSession{socket: socket, pending: map[string]chan wecomFrame{}, done: make(chan struct{})}
	// 即使认证尚未返回，也必须能被 Core 关闭或系统休眠打断。
	stop := context.AfterFunc(ctx, func() { socket.Close() })
	defer stop()
	defer socket.Close()
	socket.SetReadLimit(2 * 1024 * 1024)
	requestID := constants.IMWecomSubscribe + "_" + util.New()
	if err := session.writeFrame(constants.IMWecomSubscribe, requestID, map[string]string{"bot_id": connection.AppID, "secret": connection.Credential}); err != nil {
		return err
	}
	_ = socket.SetReadDeadline(time.Now().Add(10 * time.Second))
	var auth wecomFrame
	if err := socket.ReadJSON(&auth); err != nil {
		if ctx.Err() == nil {
			logx.Errorf("wecom authentication read failed: connection_id=%s error_type=%T", connection.ID, err)
		}
		return status_error.IMPlatformUnavailable
	}
	if auth.Headers.RequestID != requestID || auth.ErrorCode == nil {
		return status_error.IMProtocolInvalid
	}
	if *auth.ErrorCode != 0 {
		return status_error.IMLoginRequired
	}
	w.mu.Lock()
	if w.sessions == nil {
		w.sessions = map[string]*wecomSession{}
	}
	if w.sessions[connection.ID] != nil {
		w.mu.Unlock()
		return status_error.IMConfigurationConflict
	}
	w.sessions[connection.ID] = session
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		delete(w.sessions, connection.ID)
		w.mu.Unlock()
		close(session.done)
	}()
	state(constants.IMConnectionStateConnected, "")
	frames := make(chan wecomFrame, 32)
	readDone := make(chan struct{})
	readCtx, cancel := context.WithCancel(ctx)
	defer func() { cancel(); socket.Close(); <-readDone }()
	go func() {
		defer close(readDone)
		defer close(frames)
		for {
			_ = socket.SetReadDeadline(time.Now().Add(90 * time.Second))
			var frame wecomFrame
			if socket.ReadJSON(&frame) != nil {
				return
			}
			select {
			case frames <- frame:
			case <-readCtx.Done():
				return
			}
		}
	}()
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()
	ping := ""
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-heartbeat.C:
			if ping != "" {
				return status_error.IMPlatformUnavailable
			}
			ping = constants.IMWecomHeartbeat + "_" + util.New()
			if err := session.writeFrame(constants.IMWecomHeartbeat, ping, nil); err != nil {
				return err
			}
		case frame, ok := <-frames:
			if !ok {
				return status_error.IMPlatformUnavailable
			}
			switch frame.Command {
			case constants.IMWecomMessageCallback:
				message, accepted, err := normalizeWecom(frame.Body, connection.AppID)
				if err != nil {
					return err
				}
				if accepted {
					// 企微不提供客户端消费 ACK；只在本地耐久收件后交给既有执行队列。
					if err := commit(ctx, []Message{message}, ""); err != nil {
						return err
					}
				}
			case constants.IMWecomEventCallback:
				var event struct {
					// Event 保存官方事件类型。
					Event struct {
						// Type 区分连接替换等生命周期通知。
						Type string `json:"eventtype"`
					} `json:"event"`
				}
				if json.Unmarshal(frame.Body, &event) != nil {
					return status_error.IMProtocolInvalid
				}
				if event.Event.Type == constants.IMWecomDisconnected {
					return status_error.IMConnectionReplaced
				}
			default:
				if frame.ErrorCode == nil {
					continue
				}
				if frame.Headers.RequestID == ping {
					if *frame.ErrorCode != 0 {
						return status_error.IMPlatformUnavailable
					}
					ping = ""
				}
				session.mu.Lock()
				waiting := session.pending[frame.Headers.RequestID]
				session.mu.Unlock()
				if waiting != nil {
					select {
					case waiting <- frame:
					default:
					}
				}
			}
		}
	}
}

func (s *wecomSession) writeFrame(command, id string, body any) error {
	value, err := json.Marshal(body)
	if err != nil {
		return status_error.IMProtocolInvalid
	}
	s.write.Lock()
	defer s.write.Unlock()
	_ = s.socket.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := s.socket.WriteJSON(wecomFrame{Command: command, Headers: wecomHeaders{RequestID: id}, Body: value}); err != nil {
		logx.Errorf("wecom websocket write failed: command=%s error_type=%T", command, err)
		return status_error.IMPlatformUnavailable
	}
	return nil
}

// request 不重试业务发送；只有非零平台回执或发送前拒绝才能确认未送达。
func (s *wecomSession) request(ctx context.Context, command, id string, body any) (wecomFrame, bool, error) {
	var result wecomFrame
	if ctx.Err() != nil {
		return result, true, status_error.IMServiceUnavailable
	}
	waiting := make(chan wecomFrame, 1)
	s.mu.Lock()
	if s.pending[id] != nil {
		s.mu.Unlock()
		return result, true, status_error.IMConfigurationConflict
	}
	s.pending[id] = waiting
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.pending, id); s.mu.Unlock() }()
	select {
	case <-s.done:
		return result, true, status_error.IMServiceUnavailable
	default:
	}
	if err := s.writeFrame(command, id, body); err != nil {
		return result, false, err
	}
	return s.awaitACK(ctx, waiting)
}

// awaitACK 保留已经交付到等待队列的回执，连接关闭、取消和到期不能覆盖该事实。
func (s *wecomSession) awaitACK(ctx context.Context, waiting <-chan wecomFrame) (wecomFrame, bool, error) {
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	var result wecomFrame
	select {
	case result = <-waiting:
	case <-s.done:
	case <-ctx.Done():
	case <-timer.C:
	}
	if result.ErrorCode == nil {
		// select 在多个分支就绪时随机选择；终止分支也必须先消费已经排队的 ACK。
		select {
		case result = <-waiting:
		default:
			return result, false, status_error.IMPlatformUnavailable
		}
	}
	if result.ErrorCode == nil {
		return result, false, status_error.IMProtocolInvalid
	}
	if *result.ErrorCode != 0 {
		return result, true, status_error.IMPlatformUnavailable
	}
	return result, false, nil
}

// ConnectionReady 直接查询已认证会话，不复制持久连接状态或启动额外轮询。
func (w *Wecom) ConnectionReady(id string) bool {
	w.mu.Lock()
	session := w.sessions[id]
	w.mu.Unlock()
	if session == nil {
		return false
	}
	select {
	case <-session.done:
		return false
	default:
		return true
	}
}

// Send 复用当前长连接，以官方主动推送支持长任务结果及工作流交付。
func (w *Wecom) Send(ctx context.Context, connection models.IMConnection, binding models.IMBinding, id, text string, file *models.IMFile) (string, bool, error) {
	w.mu.Lock()
	session := w.sessions[connection.ID]
	w.mu.Unlock()
	if session == nil {
		return "", true, status_error.IMServiceUnavailable
	}
	body := map[string]any{"chatid": binding.ExternalChatID, "msgtype": "markdown", "markdown": map[string]string{"content": text}}
	if file != nil {
		mediaID, kind, err := session.upload(ctx, *file)
		// 上传不产生聊天消息，即使素材回执丢失，也能确定尚未发送给接收人。
		if err != nil {
			return "", true, err
		}
		body = map[string]any{"chatid": binding.ExternalChatID, "msgtype": kind, kind: map[string]string{"media_id": mediaID}}
	}
	_, definitive, err := session.request(ctx, constants.IMWecomSendMessage, constants.IMWecomSendMessage+"_"+id, body)
	if err != nil {
		return "", definitive, err
	}
	return id, false, nil
}

type wecomContent struct {
	// Content 是文本或平台语音转写结果。
	Content string `json:"content"`
	// URL 是短期有效的加密媒体地址。
	URL string `json:"url"`
	// AESKey 是每个媒体独立的解密密钥。
	AESKey string `json:"aeskey"`
}

type wecomItem struct {
	// Type 是当前内容类型。
	Type string `json:"msgtype"`
	// Text 保存普通文字。
	Text wecomContent `json:"text"`
	// Voice 保存语音转写。
	Voice wecomContent `json:"voice"`
	// Image 保存图片位置。
	Image wecomContent `json:"image"`
	// File 保存文件位置。
	File wecomContent `json:"file"`
	// Video 保存视频位置。
	Video wecomContent `json:"video"`
}

func normalizeWecom(raw json.RawMessage, botID string) (Message, bool, error) {
	var input struct {
		// ID 是平台消息去重身份。
		ID string `json:"msgid"`
		// BotID 是消息所属机器人。
		BotID string `json:"aibotid"`
		// ChatID 仅群聊返回会话身份。
		ChatID string `json:"chatid"`
		// ChatType 是 single 或 group。
		ChatType string `json:"chattype"`
		// From 保存发起用户身份。
		From struct {
			// UserID 是企业成员身份。
			UserID string `json:"userid"`
		} `json:"from"`
		// CreatedAt 是平台事件秒时间。
		CreatedAt int64 `json:"create_time"`
		// Item 保存当前内容。
		wecomItem
		// Mixed 保存图文混排内容。
		Mixed struct {
			// Items 按平台顺序保存正文与图片。
			Items []wecomItem `json:"msg_item"`
		} `json:"mixed"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return Message{}, false, status_error.IMProtocolInvalid
	}
	if input.ID == "" || input.From.UserID == "" || input.BotID != botID {
		return Message{}, false, status_error.IMProtocolInvalid
	}
	message := Message{ID: input.ID, SenderID: input.From.UserID, Kind: constants.IMChatKindDirect, ChatID: input.From.UserID, SentAt: input.CreatedAt * 1000}
	switch input.ChatType {
	case "single":
	case "group":
		if input.ChatID == "" {
			return Message{}, false, status_error.IMProtocolInvalid
		}
		message.ChatID = input.ChatID
		message.Kind = constants.IMChatKindGroup
	default:
		return Message{}, false, status_error.IMProtocolInvalid
	}
	items := []wecomItem{input.wecomItem}
	if input.Type == "mixed" {
		items = input.Mixed.Items
	}
	var texts []string
	for _, item := range items {
		var media wecomContent
		name := ""
		kind := "file"
		switch item.Type {
		case "text":
			texts = append(texts, item.Text.Content)
		case "voice":
			texts = append(texts, item.Voice.Content)
		case "image":
			media = item.Image
			name = "image.jpg"
			kind = "image"
		case "file":
			media = item.File
			name = "attachment"
		case "video":
			media = item.Video
			name = "video.mp4"
		}
		if name != "" {
			if !wecomMediaURL(media.URL) || media.AESKey == "" {
				return Message{}, false, status_error.IMProtocolInvalid
			}
			message.Files = append(message.Files, models.IMFile{Name: name, Kind: kind, Key: media.URL, AESKey: media.AESKey})
		}
	}
	message.Content = strings.TrimSpace(strings.Join(texts, "\n"))
	if message.Kind == constants.IMChatKindGroup && (input.Type == "text" || input.Type == "mixed") {
		message.Content = stripWecomGroupInvocation(message.Content)
	}
	return message, message.Content != "" || len(message.Files) > 0, nil
}

// stripWecomGroupInvocation 只移除群回调开头的一个 @唤起词；正文内的提及和邮箱保持原样。
// 官方回调不提供昵称或提及身份字段，按首个空白边界规范化；含空格昵称无法可靠区分。
func stripWecomGroupInvocation(text string) string {
	if !strings.HasPrefix(text, "@") {
		return text
	}
	end := strings.IndexFunc(text, unicode.IsSpace)
	if end < 0 {
		end = len(text)
	}
	name := text[1:end]
	if name == "" || strings.ContainsRune(name, '@') {
		return text
	}
	return strings.TrimSpace(text[end:])
}
