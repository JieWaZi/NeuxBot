package im

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// roundTripFunc 用确定的协议响应替换 HTTP 传输，不接触真实账号。
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
func responseJSON(text string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(text)), Header: make(http.Header)}
}

func TestWeixinQRUsesCurrentPostProtocolWithoutCredentials(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Header.Get("Authorization") != "" {
			t.Fatal("QR bootstrap sent credentials")
		}
		switch calls {
		case 1:
			if request.Method != http.MethodPost || request.URL.Query().Get("bot_type") != "3" || request.Header.Get("iLink-App-Id") != "bot" {
				t.Fatalf("wrong QR request: %s %s", request.Method, request.URL)
			}
			var body map[string]any
			if json.NewDecoder(request.Body).Decode(&body) != nil || body["local_token_list"] == nil {
				t.Fatal("missing current token-list body")
			}
			return responseJSON(`{"qrcode":"code","qrcode_img_content":"https://official/qr"}`), nil
		case 2:
			if request.Method != http.MethodGet || request.URL.Query().Get("verify_code") != "123456" || request.URL.Query().Get("qrcode") != "code" {
				t.Fatal("wrong status query")
			}
			return responseJSON(`{"status":"need_verifycode"}`), nil
		default:
			t.Fatal("extra request")
			return nil, nil
		}
	})}
	weixin := &Weixin{Client: client}
	qr, err := weixin.QR(context.Background())
	if err != nil || qr.Code != "code" {
		t.Fatal(err)
	}
	status, err := weixin.QRStatus(context.Background(), constants.IMWeixinBaseURL, qr.Code, "123456")
	if err != nil || status.Status != "need_verifycode" {
		t.Fatal(err)
	}
}

func TestWeixinPollingWaitsForDurabilityBeforeAdvancingCursor(t *testing.T) {
	calls := 0
	commitFailure := status_error.IMServiceUnavailable
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls > 1 {
			t.Fatal("poll cursor advanced before durable commit")
		}
		if request.Header.Get("Authorization") != "Bearer token" || request.Header.Get("AuthorizationType") != "ilink_bot_token" {
			t.Fatal("wrong current authentication")
		}
		return responseJSON(`{"ret":0,"get_updates_buf":"next","msgs":[{"message_id":42,"from_user_id":"owner","message_type":1,"context_token":"context","item_list":[{"type":1,"text_item":{"text":"开始工作"}}]},{"message_id":43,"from_user_id":"owner","message_type":2,"item_list":[]}]}`), nil
	})}
	weixin := &Weixin{Client: client}
	err := weixin.Run(context.Background(), models.IMConnection{Credential: "token"}, func(_ context.Context, messages []Message, cursor string) error {
		if len(messages) != 1 || messages[0].ID != "42" || messages[0].SenderID != "owner" || messages[0].Content != "开始工作" || cursor != "next" {
			t.Fatalf("normalization: %+v %s", messages, cursor)
		}
		return commitFailure
	}, func(constants.IMConnectionState, string) { t.Fatal("connection claimed readiness before commit") })
	if !errors.Is(err, commitFailure) || calls != 1 {
		t.Fatalf("durability boundary: %v", err)
	}
}

func TestWeixinMediaAESMatchesKnownVectorAndRejectsInvalidPadding(t *testing.T) {
	key, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	input, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	encrypted, err := cryptWeixinMedia(input, key, true)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(encrypted[:16]) != "69c4e0d86a7b0430d8cdb78070b4c55a" {
		t.Fatal("AES did not match the NIST vector")
	}
	decrypted, err := cryptWeixinMedia(encrypted, key, false)
	if err != nil || string(decrypted) != string(input) {
		t.Fatalf("media cipher roundtrip: %s %v", decrypted, err)
	}
	if _, err := cryptWeixinMedia([]byte("invalid"), key, false); !errors.Is(err, status_error.IMProtocolInvalid) {
		t.Fatal("invalid ciphertext accepted")
	}
	if _, err := cryptWeixinMedia(input, []byte("short"), true); !errors.Is(err, status_error.IMProtocolInvalid) {
		t.Fatal("invalid key accepted")
	}
	for _, url := range []string{"https://evil.example", "http://ilinkai.weixin.qq.com", "https://ilinkai.weixin.qq.com.evil.example", "https://user@ilinkai.weixin.qq.com", "https://ilinkai.weixin.qq.com:443"} {
		if OfficialURL(url, false) {
			t.Fatalf("untrusted endpoint accepted: %s", url)
		}
	}
	if !OfficialURL(constants.IMWeixinBaseURL, false) || !OfficialURL(constants.IMWeixinCDNBaseURL, true) {
		t.Fatal("official endpoint rejected")
	}
}

func TestFeishuOnlyAcceptsRealUserDMOrBotMentionAndPreservesQuote(t *testing.T) {
	var event larkim.P2MessageReceiveV1
	payload := `{"event":{"sender":{"sender_type":"user","sender_id":{"open_id":"member"}},"message":{"message_id":"message","chat_id":"group","chat_type":"group","message_type":"text","parent_id":"quoted","content":"{\"text\":\"@_user_1 整理结果\"}","mentions":[{"key":"@_user_1","id":{"open_id":"other-bot"}}]}}}`
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatal(err)
	}
	if _, accepted, err := normalizeFeishu(&event, "ally-bot"); err != nil || accepted {
		t.Fatal("unmentioned group input accepted")
	}
	actualBot := "ally-bot"
	event.Event.Message.Mentions[0].Id.OpenId = &actualBot
	message, accepted, err := normalizeFeishu(&event, "ally-bot")
	if err != nil || !accepted || message.Content != "整理结果" || message.SenderID != "member" || message.QuoteID != "quoted" || message.Kind != constants.IMChatKindGroup {
		t.Fatalf("group normalization: %+v %v", message, err)
	}
	botSender := "app"
	event.Event.Sender.SenderType = &botSender
	if _, accepted, err := normalizeFeishu(&event, "ally-bot"); err != nil || accepted {
		t.Fatal("bot echo accepted")
	}
}
