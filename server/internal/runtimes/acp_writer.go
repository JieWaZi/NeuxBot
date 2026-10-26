package runtimes

import (
	"encoding/json"
	"io"
	"sync"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// acpPromptWriter 在不重写 ACP framing 的前提下观察 Prompt 写入成功边界。
type acpPromptWriter struct {
	// output 是 acp-go-sdk 唯一写入的 Client→Agent Pipe。
	output io.Writer
	// mutex 保护 waiters。
	mutex sync.Mutex
	// waiters 按 ACP Session ID 保存当前唯一 Prompt 写入等待者。
	waiters map[string]*acpPromptWriteWaiter
}

// acpPromptWriteWaiter 保存一条 Prompt 请求的写入结果。
type acpPromptWriteWaiter struct {
	// result 接收成功空值或底层 Pipe 写错误。
	result chan error
	// once 保证写入结果只发布一次。
	once sync.Once
}

// newACPPromptWriter 创建没有活动等待者的协议写入观察器。
func newACPPromptWriter(output io.Writer) *acpPromptWriter {
	return &acpPromptWriter{
		output:  output,
		waiters: make(map[string]*acpPromptWriteWaiter),
	}
}

// observe 为同一 Session 注册本次 Prompt 写入屏障和幂等清理函数。
func (writer *acpPromptWriter) observe(
	sessionID string,
) (<-chan error, func()) {
	waiter := &acpPromptWriteWaiter{result: make(chan error, 1)}
	writer.mutex.Lock()
	writer.waiters[sessionID] = waiter
	writer.mutex.Unlock()
	cleanup := func() {
		writer.mutex.Lock()
		if writer.waiters[sessionID] == waiter {
			delete(writer.waiters, sessionID)
		}
		writer.mutex.Unlock()
	}
	return waiter.result, cleanup
}

// Write 先完成原始 ACP 写入，再唤醒匹配 Session 的 Prompt 屏障。
func (writer *acpPromptWriter) Write(data []byte) (int, error) {
	sessionID, isPrompt := acpPromptSessionID(data)
	written, err := writer.output.Write(data)
	if !isPrompt {
		return written, err
	}
	writer.mutex.Lock()
	waiter := writer.waiters[sessionID]
	writer.mutex.Unlock()
	if waiter != nil {
		waiter.once.Do(func() {
			waiter.result <- err
		})
	}
	return written, err
}

// acpPromptSessionID 从单条 ACP JSON-RPC 帧提取 session/prompt 的 Session ID。
func acpPromptSessionID(data []byte) (string, bool) {
	var message struct {
		// Method 是 ACP JSON-RPC 方法名。
		Method string `json:"method"`
		// Params 保存 Prompt 请求中的 Session ID。
		Params struct {
			// SessionID 是本次 Prompt 的 ACP Session 标识。
			SessionID string `json:"sessionId"`
		} `json:"params"`
	}
	if json.Unmarshal(data, &message) != nil ||
		message.Method != acp.AgentMethodSessionPrompt ||
		message.Params.SessionID == "" {
		return "", false
	}
	return message.Params.SessionID, true
}
