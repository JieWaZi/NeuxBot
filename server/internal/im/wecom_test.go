package im

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gorilla/websocket"
)

func TestWecomQRUsesOfficialGETAndKeepsPrivatePollingCode(t *testing.T) {
	calls := 0
	adapter := &Wecom{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodGet || request.URL.Host != "work.weixin.qq.com" || request.Header.Get("Authorization") != "" {
			t.Errorf("invalid QR boundary: %s %s", request.Method, request.URL.Host)
		}
		switch calls {
		case 1:
			if request.URL.Path != "/ai/qc/generate" || request.URL.Query().Get("source") != "neuxally" || request.URL.Query().Get("plat") == "" {
				t.Error("missing official QR source/platform")
			}
			return responseJSON(`{"data":{"scode":"private/code","auth_url":"https://work.weixin.qq.com/ai/qc/auth?token=qr"}}`), nil
		case 2:
			if request.URL.Path != "/ai/qc/query_result" || request.URL.Query().Get("scode") != "private/code" {
				t.Error("polling code was not encoded")
			}
			return responseJSON(`{"data":{"status":"success","bot_info":{"botid":"bot","secret":"private-secret"}}}`), nil
		default:
			t.Error("unexpected request")
			return responseJSON(`{}`), nil
		}
	})}}
	qr, err := adapter.QR(context.Background())
	if err != nil || qr.Code != "private/code" {
		t.Fatalf("QR generation: %+v %v", qr, err)
	}
	result, err := adapter.QRStatus(context.Background(), qr.Code)
	if err != nil || result.Bot.ID != "bot" || result.Bot.Secret != "private-secret" {
		t.Fatalf("QR confirmation: %v", err)
	}
	adapter.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return responseJSON(`{"data":{"scode":"private","auth_url":"https://work.weixin.qq.com.evil.example/qr"}}`), nil
	})
	if _, err := adapter.QR(context.Background()); !errors.Is(err, status_error.IMProtocolInvalid) {
		t.Fatalf("accepted foreign QR origin: %v", err)
	}
}

// newWecomPeer 以本地 WebSocket 替换外部平台，所有协议读取只由 handler 拥有。
func newWecomPeer(t *testing.T, handler func(*websocket.Conn)) *Wecom {
	t.Helper()
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		socket, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer socket.Close()
		_ = socket.SetReadDeadline(time.Now().Add(5 * time.Second))
		handler(socket)
	}))
	t.Cleanup(server.Close)
	return &Wecom{endpoint: "ws" + strings.TrimPrefix(server.URL, "http")}
}

func wecomReply(t *testing.T, socket *websocket.Conn, request wecomFrame, code int, body any) {
	t.Helper()
	value, err := json.Marshal(body)
	if err != nil {
		t.Error(err)
		return
	}
	if err := socket.WriteJSON(wecomFrame{Headers: request.Headers, ErrorCode: &code, Body: value}); err != nil {
		t.Error(err)
	}
}

func wecomAuth(t *testing.T, socket *websocket.Conn) bool {
	t.Helper()
	var frame wecomFrame
	if err := socket.ReadJSON(&frame); err != nil {
		t.Error(err)
		return false
	}
	var credentials map[string]string
	if json.Unmarshal(frame.Body, &credentials) != nil || frame.Command != constants.IMWecomSubscribe || credentials["bot_id"] != "bot" || credentials["secret"] != "secret" {
		t.Error("invalid official authentication frame")
		return false
	}
	wecomReply(t, socket, frame, 0, nil)
	return true
}

func wecomConnection() models.IMConnection {
	return models.IMConnection{ID: "connection", AppID: "bot", Credential: "secret", Platform: constants.IMPlatformWecom}
}

func TestWecomAuthenticationMustConfirmBeforeConnectedAndCancelClosesReader(t *testing.T) {
	for _, mode := range []string{"rejected", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			received := make(chan struct{})
			peerDone := make(chan struct{})
			adapter := newWecomPeer(t, func(socket *websocket.Conn) {
				defer close(peerDone)
				var auth wecomFrame
				if err := socket.ReadJSON(&auth); err != nil {
					t.Error(err)
					return
				}
				close(received)
				if mode == "rejected" {
					wecomReply(t, socket, auth, 401, nil)
				}
				_, _, _ = socket.ReadMessage()
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			outcome := make(chan error, 1)
			go func() {
				outcome <- adapter.Run(ctx, wecomConnection(), nil, func(constants.IMConnectionState, string) { t.Error("unauthenticated socket claimed connected") })
			}()
			select {
			case <-received:
			case <-time.After(time.Second):
				t.Fatal("authentication not sent")
			}
			if adapter.ConnectionReady("connection") {
				t.Fatal("authentication did not finish but transport reported ready")
			}
			if mode == "cancelled" {
				cancel()
			}
			select {
			case err := <-outcome:
				if mode == "rejected" && !errors.Is(err, status_error.IMLoginRequired) {
					t.Fatalf("wrong authentication error: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("authentication did not exit")
			}
			select {
			case <-peerDone:
			case <-time.After(time.Second):
				t.Fatal("cancel left socket open")
			}
		})
	}
}

func TestWecomReceiveRetainsSenderAndDurableCommitFailure(t *testing.T) {
	adapter := newWecomPeer(t, func(socket *websocket.Conn) {
		if !wecomAuth(t, socket) {
			return
		}
		payload := json.RawMessage(`{"msgid":"message","aibotid":"bot","chatid":"room","chattype":"group","from":{"userid":"member"},"msgtype":"mixed","mixed":{"msg_item":[{"msgtype":"text","text":{"content":"整理结果"}},{"msgtype":"image","image":{"url":"https://wework.qpic.cn/image","aeskey":"key"}}]}}`)
		if err := socket.WriteJSON(wecomFrame{Command: constants.IMWecomMessageCallback, Body: payload}); err != nil {
			t.Error(err)
			return
		}
		_, _, _ = socket.ReadMessage()
	})
	committed := false
	err := adapter.Run(context.Background(), wecomConnection(), func(_ context.Context, messages []Message, cursor string) error {
		committed = true
		if cursor != "" || len(messages) != 1 || messages[0].ChatID != "room" || messages[0].SenderID != "member" || messages[0].Kind != constants.IMChatKindGroup || messages[0].Content != "整理结果" || len(messages[0].Files) != 1 {
			t.Errorf("wrong incoming normalization: %+v", messages)
		}
		return status_error.DatabaseError
	}, func(constants.IMConnectionState, string) {})
	if !committed || !errors.Is(err, status_error.DatabaseError) {
		t.Fatalf("durable commit failure was hidden: %v", err)
	}
	for _, body := range []string{
		`{"msgid":"m","aibotid":"other-bot","chattype":"single","from":{"userid":"u"},"msgtype":"text","text":{"content":"text"}}`,
		`{"msgid":"m","aibotid":"bot","chattype":"group","from":{"userid":"u"},"msgtype":"text","text":{"content":"text"}}`,
		`{"msgid":"m","aibotid":"bot","chattype":"single","from":{"userid":"u"},"msgtype":"image","image":{"url":"https://evil.example/file","aeskey":"key"}}`,
	} {
		if _, accepted, err := normalizeWecom(json.RawMessage(body), "bot"); accepted || !errors.Is(err, status_error.IMProtocolInvalid) {
			t.Fatalf("accepted cross-bot or malformed message: %v", err)
		}
	}
	message, accepted, err := normalizeWecom(json.RawMessage(`{"msgid":"m","aibotid":"bot","chatid":"ignored-room","chattype":"single","from":{"userid":"member"},"msgtype":"voice","voice":{"content":"语音转写"}}`), "bot")
	if err != nil || !accepted || message.ChatID != "member" || message.SenderID != "member" || message.Content != "语音转写" {
		t.Fatalf("direct sender was not isolated: %+v %v", message, err)
	}
}

func TestWecomDeliveryDistinguishesRejectionFromLostACKAndReplacement(t *testing.T) {
	for _, mode := range []string{"accepted", "rejected", "lost_ack", "replaced"} {
		t.Run(mode, func(t *testing.T) {
			adapter := newWecomPeer(t, func(socket *websocket.Conn) {
				if !wecomAuth(t, socket) {
					return
				}
				var request wecomFrame
				if err := socket.ReadJSON(&request); err != nil {
					t.Error(err)
					return
				}
				var body struct {
					// ChatID 是当前外部聊天身份。
					ChatID string `json:"chatid"`
					// Type 是主动发送消息类型。
					Type string `json:"msgtype"`
				}
				if json.Unmarshal(request.Body, &body) != nil || request.Command != constants.IMWecomSendMessage || body.ChatID != "member" || body.Type != "markdown" {
					t.Error("incorrect active message request")
				}
				switch mode {
				case "accepted":
					wecomReply(t, socket, request, 0, nil)
				case "rejected":
					wecomReply(t, socket, request, 600, nil)
				case "lost_ack":
					return
				case "replaced":
					if err := socket.WriteJSON(wecomFrame{Command: constants.IMWecomEventCallback, Body: json.RawMessage(`{"event":{"eventtype":"disconnected_event"}}`)}); err != nil {
						t.Error(err)
					}
				}
				_, _, _ = socket.ReadMessage()
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ready := make(chan struct{})
			outcome := make(chan error, 1)
			go func() {
				outcome <- adapter.Run(ctx, wecomConnection(), nil, func(constants.IMConnectionState, string) { close(ready) })
			}()
			select {
			case <-ready:
			case <-time.After(time.Second):
				t.Fatal("not authenticated")
			}
			if !adapter.ConnectionReady("connection") {
				t.Fatal("authenticated transport did not report readiness")
			}
			receipt, definitive, err := adapter.Send(ctx, wecomConnection(), models.IMBinding{ExternalChatID: "member"}, "part", "结果", nil)
			if mode == "accepted" {
				if err != nil || receipt != "part" {
					t.Fatalf("successful ACK lost: %v", err)
				}
			} else if err == nil || definitive != (mode == "rejected") {
				t.Fatalf("unsafe delivery classification: definitive=%v err=%v", definitive, err)
			}
			cancel()
			select {
			case err := <-outcome:
				if mode == "replaced" && !errors.Is(err, status_error.IMConnectionReplaced) {
					t.Fatalf("replacement not retained: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("connection reader did not exit")
			}
			adapter.mu.Lock()
			defer adapter.mu.Unlock()
			if len(adapter.sessions) != 0 {
				t.Fatal("connection session leaked after exit")
			}
		})
	}
}

func TestWecomUploadUsesOfficialZeroBasedChunksBeforeSending(t *testing.T) {
	value := bytes.Repeat([]byte("x"), 512*1024+7)
	path := filepath.Join(t.TempDir(), "output.bin")
	if err := os.WriteFile(path, value, 0600); err != nil {
		t.Fatal(err)
	}
	adapter := newWecomPeer(t, func(socket *websocket.Conn) {
		if !wecomAuth(t, socket) {
			return
		}
		expected := []string{constants.IMWecomUploadInit, constants.IMWecomUploadChunk, constants.IMWecomUploadChunk, constants.IMWecomUploadFinish, constants.IMWecomSendMessage}
		var chunks []byte
		for index, command := range expected {
			var request wecomFrame
			if err := socket.ReadJSON(&request); err != nil {
				t.Error(err)
				return
			}
			if request.Command != command {
				t.Errorf("command %d: %s", index, request.Command)
				return
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(request.Body, &body); err != nil {
				t.Error(err)
				return
			}
			response := map[string]string{}
			switch command {
			case constants.IMWecomUploadInit:
				if string(body["total_chunks"]) != "2" || string(body["total_size"]) != "524295" {
					t.Error("incorrect upload size")
				}
				response["upload_id"] = "upload"
			case constants.IMWecomUploadChunk:
				if string(body["chunk_index"]) != string(rune('0'+index-1)) {
					t.Error("chunk index is not zero based")
				}
				var data string
				_ = json.Unmarshal(body["base64_data"], &data)
				chunk, err := base64.StdEncoding.DecodeString(data)
				if err != nil {
					t.Error(err)
					return
				}
				chunks = append(chunks, chunk...)
			case constants.IMWecomUploadFinish:
				if !bytes.Equal(chunks, value) {
					t.Error("uploaded bytes changed")
				}
				response["media_id"] = "media"
			case constants.IMWecomSendMessage:
				if string(body["file"]) != `{"media_id":"media"}` {
					t.Errorf("sent before media was finalized: %s", body["file"])
				}
			}
			wecomReply(t, socket, request, 0, response)
		}
		_, _, _ = socket.ReadMessage()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	outcome := make(chan error, 1)
	go func() {
		outcome <- adapter.Run(ctx, wecomConnection(), nil, func(constants.IMConnectionState, string) { close(ready) })
	}()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("not authenticated")
	}
	_, _, err := adapter.Send(ctx, wecomConnection(), models.IMBinding{ExternalChatID: "member"}, "part", "", &models.IMFile{Name: "output.bin", Path: path, Kind: "file"})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-outcome:
	case <-time.After(time.Second):
		t.Fatal("reader did not exit")
	}
}

func TestWecomMediaDecrypts32BytePaddingAndRejectsCorruption(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	value := bytes.Repeat([]byte("x"), 32)
	plain := append(append([]byte{}, value...), bytes.Repeat([]byte{32}, 32)...)
	block, _ := aes.NewCipher(key)
	encrypted := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(encrypted, plain)
	adapter := &Wecom{client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(encrypted))}, nil
	})}}
	file := models.IMFile{Key: "https://wework.qpic.cn/media", AESKey: base64.RawStdEncoding.EncodeToString(key)}
	result, err := adapter.Download(context.Background(), wecomConnection(), "message", file)
	if err != nil || !bytes.Equal(result, value) {
		t.Fatalf("official media decryption failed: %v", err)
	}
	// 修改倒数第二个密文块，确定性破坏最终填充区。
	encrypted[31] ^= 1
	if _, err := adapter.Download(context.Background(), wecomConnection(), "message", file); !errors.Is(err, status_error.IMProtocolInvalid) {
		t.Fatalf("corrupted padding was accepted: %v", err)
	}
	for _, source := range []string{"http://wework.qpic.cn/file", "https://evil.example/file", "https://wework.qpic.cn.evil.example/file", "https://user@qyapi.weixin.qq.com/file", "https://qyapi.weixin.qq.com:443/file", "https://127.0.0.1/file"} {
		if wecomMediaURL(source) {
			t.Errorf("untrusted media URL accepted: %s", source)
		}
	}
}

func TestWecomQueuedACKWinsOverDisconnectAndCancellation(t *testing.T) {
	for _, cause := range []string{"disconnect", "cancel"} {
		for _, code := range []int{0, 600} {
			t.Run(cause+"_"+string(rune('0'+code/100)), func(t *testing.T) {
				// 两条分支同时就绪，重复验证 Go select 任意选中终止分支时也保留真实回执。
				for attempt := 0; attempt < 64; attempt++ {
					done := make(chan struct{})
					ctx, cancel := context.WithCancel(context.Background())
					waiting := make(chan wecomFrame, 1)
					waiting <- wecomFrame{Headers: wecomHeaders{RequestID: "part"}, ErrorCode: &code}
					if cause == "disconnect" {
						close(done)
					} else {
						cancel()
					}
					session := &wecomSession{done: done}
					result, definitive, err := session.awaitACK(ctx, waiting)
					cancel()
					if result.Headers.RequestID != "part" || definitive != (code != 0) || (code == 0 && err != nil) || (code != 0 && !errors.Is(err, status_error.IMPlatformUnavailable)) {
						t.Fatalf("queued ACK was replaced by termination: code=%d definitive=%v error=%v", code, definitive, err)
					}
				}
			})
		}
	}
	done := make(chan struct{})
	close(done)
	session := &wecomSession{done: done}
	if _, definitive, err := session.awaitACK(context.Background(), make(chan wecomFrame, 1)); err == nil || definitive {
		t.Fatalf("missing ACK became safe to retry: definitive=%v error=%v", definitive, err)
	}
}

func TestWecomUnavailableSessionIsDefinitelyUnsent(t *testing.T) {
	adapter := &Wecom{}
	if adapter.ConnectionReady("connection") {
		t.Fatal("missing session reported ready")
	}
	_, definitive, err := adapter.Send(context.Background(), wecomConnection(), models.IMBinding{ExternalChatID: "member"}, "part", "结果", nil)
	if !definitive || !errors.Is(err, status_error.IMServiceUnavailable) {
		t.Fatalf("missing session did not preserve retryable unsent boundary: %v %v", definitive, err)
	}
	done := make(chan struct{})
	close(done)
	session := &wecomSession{done: done, pending: map[string]chan wecomFrame{}}
	adapter.sessions = map[string]*wecomSession{"connection": session}
	if adapter.ConnectionReady("connection") {
		t.Fatal("closed session reported ready")
	}
	// socket 故意为空；已关闭会话必须在任何网络写入前返回。
	if _, definitive, err := session.request(context.Background(), constants.IMWecomSendMessage, "part", nil); !definitive || !errors.Is(err, status_error.IMServiceUnavailable) {
		t.Fatalf("closed session attempted a network write: %v %v", definitive, err)
	}
}

func TestWecomUploadInterruptionCanRestartWithoutDuplicatingChatMessage(t *testing.T) {
	for _, stage := range []string{constants.IMWecomUploadInit, constants.IMWecomUploadChunk, constants.IMWecomUploadFinish} {
		for _, rejected := range []bool{false, true} {
			label := "disconnect"
			if rejected {
				label = "rejected"
			}
			t.Run(stage+"_"+label, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "output.bin")
				if err := os.WriteFile(path, []byte("frozen file"), 0600); err != nil {
					t.Fatal(err)
				}
				// 原子计数同步重连时的服务端会话和聊天写入，避免测试替身引入竞争。
				var peers atomic.Int32
				var chatWrites atomic.Int32
				adapter := newWecomPeer(t, func(socket *websocket.Conn) {
					peer := peers.Add(1)
					if !wecomAuth(t, socket) {
						return
					}
					for _, command := range []string{constants.IMWecomUploadInit, constants.IMWecomUploadChunk, constants.IMWecomUploadFinish, constants.IMWecomSendMessage} {
						var request wecomFrame
						if err := socket.ReadJSON(&request); err != nil {
							t.Error(err)
							return
						}
						if request.Command != command {
							t.Errorf("unexpected upload recovery order: got=%s want=%s", request.Command, command)
							return
						}
						if peer == 1 && command == stage {
							if rejected {
								wecomReply(t, socket, request, 400, nil)
								_, _, _ = socket.ReadMessage()
							}
							return
						}
						var response any
						switch command {
						case constants.IMWecomUploadInit:
							response = map[string]string{"upload_id": "upload"}
						case constants.IMWecomUploadFinish:
							response = map[string]string{"media_id": "media"}
						case constants.IMWecomSendMessage:
							chatWrites.Add(1)
						}
						wecomReply(t, socket, request, 0, response)
					}
					_, _, _ = socket.ReadMessage()
				})
				run := func() (context.Context, context.CancelFunc, <-chan error) {
					ctx, cancel := context.WithCancel(context.Background())
					ready := make(chan struct{})
					outcome := make(chan error, 1)
					go func() {
						outcome <- adapter.Run(ctx, wecomConnection(), nil, func(constants.IMConnectionState, string) { close(ready) })
					}()
					select {
					case <-ready:
					case <-time.After(time.Second):
						cancel()
						t.Fatal("not authenticated")
					}
					return ctx, cancel, outcome
				}
				ctx, cancel, outcome := run()
				defer cancel()
				file := &models.IMFile{Name: "output.bin", Path: path, Kind: "file"}
				_, definitive, err := adapter.Send(ctx, wecomConnection(), models.IMBinding{ExternalChatID: "member"}, "part", "", file)
				if !definitive {
					t.Fatalf("upload failure became an uncertain chat message: %v", err)
				}
				if rejected {
					if !errors.Is(err, status_error.IMPlatformUnavailable) {
						t.Fatalf("explicit upload rejection became retryable: %v", err)
					}
					cancel()
					select {
					case <-outcome:
					case <-time.After(time.Second):
						t.Fatal("rejected connection did not stop")
					}
					if chatWrites.Load() != 0 {
						t.Fatal("rejected upload sent a chat message")
					}
					return
				}
				if !errors.Is(err, status_error.IMServiceUnavailable) {
					t.Fatalf("interrupted upload did not preserve safe retry: %v", err)
				}
				select {
				case <-outcome:
				case <-time.After(time.Second):
					t.Fatal("disconnected session did not exit")
				}
				if chatWrites.Load() != 0 {
					t.Fatal("interrupted upload sent a chat message")
				}
				cancel()
				ctx, cancel, outcome = run()
				defer cancel()
				receipt, _, err := adapter.Send(ctx, wecomConnection(), models.IMBinding{ExternalChatID: "member"}, "part", "", file)
				if err != nil || receipt != "part" {
					t.Fatalf("upload did not recover after reconnect: %v", err)
				}
				cancel()
				select {
				case <-outcome:
				case <-time.After(time.Second):
					t.Fatal("recovered connection did not stop")
				}
				if peers.Load() != 2 || chatWrites.Load() != 1 {
					t.Fatalf("recovery duplicated chat delivery: peers=%d chat_writes=%d", peers.Load(), chatWrites.Load())
				}
			})
		}
	}
}

func TestWecomGroupInvocationDoesNotPolluteMessageBody(t *testing.T) {
	cases := []struct {
		// name 是当前正文边界用例。
		name string
		// chatType 限定只有群消息参与唤起词规范化。
		chatType string
		// kind 区分文本、语音和混排。
		kind string
		// content 是真实平台正文。
		content string
		// want 是交给收件和执行入口的正文。
		want string
		// accepted 表示是否存在可处理正文。
		accepted bool
	}{
		{"screenshot", "group", "text", "@吴俊杰的机器人 你好", "你好", true},
		{"direct", "single", "text", "@吴俊杰的机器人 你好", "@吴俊杰的机器人 你好", true},
		{"voice", "group", "voice", "@吴俊杰的机器人 你好", "@吴俊杰的机器人 你好", true},
		{"mixed", "group", "mixed", "@吴俊杰的机器人 你好", "你好", true},
		{"multiple_mentions", "group", "text", "@机器人 @同事 你好 @其他人", "@同事 你好 @其他人", true},
		{"later_mention", "group", "text", "你好 @机器人", "你好 @机器人", true},
		{"email", "group", "text", "@机器人 发给 me@example.com 并抄送 @同事", "发给 me@example.com 并抄送 @同事", true},
		{"leading_email", "group", "text", "me@example.com 请联系", "me@example.com 请联系", true},
		{"leading_invalid_email", "group", "text", "@me@example.com 请联系", "@me@example.com 请联系", true},
		{"command", "group", "text", "@机器人 /new", "/new", true},
		{"unicode_separator", "group", "text", "@机器人\u2005你好", "你好", true},
		{"mention_only", "group", "text", "@机器人", "", false},
		{"blank_mention", "group", "text", "@ 你好", "@ 你好", true},
		// 协议没有昵称身份字段，含空格名称只能按第一个空白分隔词处理，不能伪称完整识别。
		{"space_in_name_boundary", "group", "text", "@My Bot 你好", "Bot 你好", true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			body := map[string]any{"msgid": "message", "aibotid": "bot", "chatid": "room", "chattype": test.chatType, "from": map[string]string{"userid": "wo-private-sender"}, "msgtype": test.kind}
			if test.kind == "mixed" {
				body["mixed"] = map[string]any{"msg_item": []any{map[string]any{"msgtype": "text", "text": map[string]string{"content": test.content}}, map[string]any{"msgtype": "text", "text": map[string]string{"content": "后续 @保留"}}}}
			} else {
				body[test.kind] = map[string]string{"content": test.content}
			}
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			message, accepted, err := normalizeWecom(raw, "bot")
			want := test.want
			if test.kind == "mixed" {
				want += "\n后续 @保留"
			}
			if err != nil || accepted != test.accepted || message.Content != want || message.SenderID != "wo-private-sender" {
				t.Fatalf("normalization changed body or identity: content=%q accepted=%v sender=%q error=%v", message.Content, accepted, message.SenderID, err)
			}
		})
	}
}

func TestWecomGroupMentionOnlyMixedMessageKeepsAttachments(t *testing.T) {
	raw := json.RawMessage(`{"msgid":"m","aibotid":"bot","chatid":"room","chattype":"group","from":{"userid":"member"},"msgtype":"mixed","mixed":{"msg_item":[{"msgtype":"text","text":{"content":"@机器人 "}},{"msgtype":"image","image":{"url":"https://wework.qpic.cn/image","aeskey":"key"}}]}}`)
	message, accepted, err := normalizeWecom(raw, "bot")
	if err != nil || !accepted || message.Content != "" || len(message.Files) != 1 {
		t.Fatalf("mention-only text discarded mixed attachment: %+v accepted=%v error=%v", message, accepted, err)
	}
}
