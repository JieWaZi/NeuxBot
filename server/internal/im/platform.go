// Package im 封装 IM 平台的协议、登录及媒体传输。
package im

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Message 是平台适配器解析后的输入；平台确认必须晚于调用方耐久提交。
type Message struct {
	// ID 是平台消息身份。
	ID string
	// ChatID 是平台单聊或群聊身份。
	ChatID string
	// Kind 是实际聊天类型。
	Kind constants.IMChatKind
	// SenderID 是平台发起人。
	SenderID string
	// ChatName 是平台能提供的聊天名称。
	ChatName string
	// Content 是交给执行入口的消息正文；提及格式由所属平台规范化。
	Content string
	// QuoteID 是被引用的原消息身份。
	QuoteID string
	// ContextToken 是仅供微信回复的会话上下文。
	ContextToken string
	// Files 是平台媒体资源定位。
	Files []models.IMFile
	// SentAt 是平台提供的 Unix 毫秒发送时间。
	SentAt int64
}

// BatchCommit 必须原子保存整批输入及游标；返回失败时不能确认平台输入。
type BatchCommit func(context.Context, []Message, string) error

// StateChanged 记录适配器的真实连接状态，不携带外部响应或凭据。
type StateChanged func(constants.IMConnectionState, string)

// Platform 是微信长轮询与企业机器人长连接共同使用的传输接口。
type Platform interface {
	// Run 收取平台消息直到取消，提交失败不得发送成功确认。
	Run(context.Context, models.IMConnection, BatchCommit, StateChanged) error
	// Send 发送一个已冻结的正文或文件片段；明确拒绝才返回 definitive=true。
	Send(context.Context, models.IMConnection, models.IMBinding, string, string, *models.IMFile) (receipt string, definitive bool, err error)
	// Download 从所属输入消息下载受限大小的附件。
	Download(context.Context, models.IMConnection, string, models.IMFile) ([]byte, error)
}

// ConnectionReadiness 让依赖活跃会话的传输在领取 Outbox 前报告实际可发送性。
// 不另存连接状态；不实现此端口的 HTTP 传输仍直接使用原发送边界。
type ConnectionReadiness interface {
	// ConnectionReady 表示当前连接存在已认证且尚未退出的传输。
	ConnectionReady(string) bool
}

// StableID 从不可变的平台或投递身份派生无连字符的 32 位标识。
func StableID(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:16])
}

// OfficialURL 只允许腾讯实际使用的 HTTPS API 或 CDN 域名。
func OfficialURL(value string, cdn bool) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if cdn {
		return host == "novac2c.cdn.weixin.qq.com"
	}
	return host == "ilinkai.weixin.qq.com" || strings.HasSuffix(host, ".ilinkai.weixin.qq.com")
}

// HTTPClient 返回禁止跨平台重定向的有界请求客户端。
func HTTPClient(cdn bool) *http.Client {
	return &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || !OfficialURL(req.URL.String(), cdn) {
			return status_error.IMProtocolInvalid
		}
		return nil
	}}
}

// ReadMedia 限量读取平台媒体，超出预算立即拒绝。
func ReadMedia(reader io.Reader) ([]byte, error) {
	return readMediaLimit(reader, constants.IMFileMaximumBytes)
}

func readMediaLimit(reader io.Reader, limit int64) ([]byte, error) {
	value, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, status_error.IMPlatformUnavailable
	}
	if int64(len(value)) > limit {
		return nil, status_error.IMFileInvalid
	}
	return value, nil
}
