package oauthrelay

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
)

const (
	authorizationRequiredCode = -32001
	maxErrorBatchMessages     = 64
	maxErrorIDBytes           = 512
)

type rpcMessage struct {
	// Version 是用于识别 JSON-RPC 2.0 消息的协议版本。
	Version string `json:"jsonrpc"`
	// Method 区分请求、通知和由原生客户端发送的响应。
	Method string `json:"method"`
	// ID 保留请求 ID 的原始字符串或数值，避免丢失数值精度。
	ID json.RawMessage `json:"id"`
	// Result 表示原生客户端对服务端请求给出的结果。
	Result json.RawMessage `json:"result"`
	// Error 表示原生客户端对服务端请求给出的错误。
	Error json.RawMessage `json:"error"`
}

// writeAuthorizationRequired 保持已通过本机租约校验的连接使用 MCP 协议，不向客户端发起 OAuth 质询。
func writeAuthorizationRequired(writer http.ResponseWriter, method string, payload []byte) {
	if method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	batch, ids, ok := requestIDs(payload)
	if !ok {
		reject(writer, http.StatusBadRequest)
		return
	}
	if len(ids) == 0 {
		writer.WriteHeader(http.StatusAccepted)
		return
	}
	replies := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		replies = append(replies, map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"error":   map[string]any{"code": authorizationRequiredCode, "message": "MCP authorization unavailable"},
		})
	}
	var response any = replies[0]
	if batch {
		response = replies
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(response)
}

func requestIDs(payload []byte) (batch bool, ids []json.RawMessage, ok bool) {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return false, nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if payload[0] != '[' {
		id, valid := decodeRequestID(decoder)
		if !valid || !atEnd(decoder) {
			return false, nil, false
		}
		if id != nil {
			ids = append(ids, id)
		}
		return false, ids, true
	}
	if _, err := decoder.Token(); err != nil || !decoder.More() {
		return true, nil, false
	}
	for count := 0; decoder.More(); count++ {
		if count == maxErrorBatchMessages {
			return true, nil, false
		}
		id, valid := decodeRequestID(decoder)
		if !valid {
			return true, nil, false
		}
		if id != nil {
			ids = append(ids, id)
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim(']') || !atEnd(decoder) {
		return true, nil, false
	}
	return true, ids, true
}

func atEnd(decoder *json.Decoder) bool {
	_, err := decoder.Token()
	return err == io.EOF
}

func decodeRequestID(decoder *json.Decoder) (json.RawMessage, bool) {
	var message rpcMessage
	if decoder.Decode(&message) != nil || message.Version != "2.0" {
		return nil, false
	}
	if message.Method == "" {
		// JSON-RPC 响应不应再收到 JSON-RPC 响应。
		response := validRPCID(message.ID) && ((message.Result == nil) != (message.Error == nil))
		return nil, response
	}
	if message.ID == nil {
		return nil, true
	}
	return message.ID, validRPCID(message.ID)
}

func validRPCID(id json.RawMessage) bool {
	if len(id) == 0 || len(id) > maxErrorIDBytes {
		return false
	}
	first := id[0]
	// RawMessage 来自成功的 JSON 解码，因此首字符足以区分允许的值类型。
	return first == '"' || first == 'n' || first == '-' || (first >= '0' && first <= '9')
}

// canRetryMCPRequest 只允许在已确认的认证拒绝后重试 SSE 和无工具调用的 MCP 握手或工具发现。
func canRetryMCPRequest(request *http.Request, payload []byte) bool {
	if request.Method == http.MethodGet {
		return true
	}
	if request.Method != http.MethodPost {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || len(request.Header.Values("Content-Type")) != 1 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if start, err := decoder.Token(); err != nil || start != json.Delim('{') {
		return false
	}
	fields := make(map[string]json.RawMessage, 4)
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return false
		}
		name, ok := key.(string)
		if !ok || fields[name] != nil {
			return false
		}
		switch name {
		case "jsonrpc", "method", "id", "params":
		default:
			return false
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return false
		}
		fields[name] = value
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') || !atEnd(decoder) {
		return false
	}
	var version, method string
	id, params := fields["id"], fields["params"]
	if json.Unmarshal(fields["jsonrpc"], &version) != nil || version != "2.0" ||
		json.Unmarshal(fields["method"], &method) != nil || (method != "initialize" && method != "tools/list") ||
		!validRPCID(id) || id[0] == 'n' || (id[0] != '"' && bytes.IndexAny(id, ".eE") >= 0) ||
		(len(params) != 0 && params[0] != '{') {
		return false
	}
	methodHeaders := request.Header.Values("Mcp-Method")
	return len(methodHeaders) == 0 || (len(methodHeaders) == 1 && methodHeaders[0] == method)
}
