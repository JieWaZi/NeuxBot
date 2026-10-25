package chatstorage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// ForkOperation 保存从准备到发布之间不可丢失的分叉事实。
type ForkOperation struct {
	// Prepared 表示工作区与全部继承附件已持久化。
	Prepared bool `json:"prepared"`
	// Published 表示子 Chat 已公开；删除子 Chat 后重试不能复活历史。
	Published bool `json:"published"`
	// Cancelled 表示来源已删除且子 Chat 从未公开；保留请求墓碑防止再次分叉。
	Cancelled bool `json:"cancelled,omitempty"`
	// ChatID 是操作开始时分配且不会重用的子 Chat 身份。
	ChatID string `json:"chatId"`
	// SourceChatID 是本次操作锁定的来源 Chat。
	SourceChatID string `json:"sourceChatId"`
	// ExecutionID 是请求选中的来源结束回合。
	ExecutionID string `json:"executionId"`
	// NativeStarted 表示已经越过可能产生外部副作用的边界。
	NativeStarted bool `json:"nativeStarted"`
	// SessionID 是原生分叉已经明确返回且同步落盘的新身份。
	SessionID string `json:"sessionId,omitempty"`
	// Payload 是发布所需的冻结历史及执行配置，不依赖父 Chat 继续存在。
	Payload json.RawMessage `json:"payload"`
}

// ForkDirectory 返回持久请求目录，客户端输入只参与摘要，不进入路径。
func (l *Layout) ForkDirectory(sourceID, requestID string) string {
	sum := sha256.Sum256([]byte(sourceID + "\x00" + requestID))
	return filepath.Join(l.root, constants.ChatForkDirectoryName, hex.EncodeToString(sum[:]))
}

// ReadForkOperation 读取幂等事实；不存在与读取失败严格区分。
func ReadForkOperation(directory string) (ForkOperation, bool, error) {
	data, err := os.ReadFile(filepath.Join(directory, "operation.json"))
	if errors.Is(err, os.ErrNotExist) {
		return ForkOperation{}, false, nil
	}
	if err != nil {
		return ForkOperation{}, false, status_error.ChatLogError
	}
	var operation ForkOperation
	if json.Unmarshal(data, &operation) != nil || !safeSegment(operation.ChatID) || !safeSegment(operation.SourceChatID) {
		return ForkOperation{}, false, status_error.ChatLogCorrupted
	}
	return operation, true, nil
}

// WriteForkOperation 先同步临时文件，再原子替换和同步父目录。
func WriteForkOperation(directory string, operation ForkOperation) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return status_error.ChatLogError
	}
	data, err := json.Marshal(operation)
	if err != nil {
		return status_error.ChatLogError
	}
	temp, err := os.CreateTemp(directory, "operation-*.tmp")
	if err != nil {
		return status_error.ChatLogError
	}
	defer os.Remove(temp.Name())
	_, writeErr := temp.Write(data)
	syncErr := temp.Sync()
	closeErr := temp.Close()
	if errors.Join(writeErr, syncErr, closeErr) != nil {
		return status_error.ChatLogError
	}
	target := filepath.Join(directory, "operation.json")
	if os.Rename(temp.Name(), target) != nil || syncRename(temp.Name(), target) != nil {
		return status_error.ChatLogError
	}
	return nil
}

// PublishForkLog 只在完整 JSONL 已同步后原子公开它；重复调用沿用同一份事实。
func (l *Layout) PublishForkLog(staging *Layout, chatID string) error {
	source, err := staging.ChatLogDirectory(chatID)
	if err != nil {
		return err
	}
	target, err := l.ChatLogDirectory(chatID)
	if err != nil {
		return err
	}
	if _, err = os.Stat(target); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return status_error.ChatLogError
	}
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return status_error.ChatLogError
	}
	if err = os.Rename(source, target); err != nil {
		return status_error.ChatLogError
	}
	if err = syncRename(source, target); err != nil {
		return status_error.ChatLogError
	}
	return nil
}

// CopyForkAttachment 复制单个受管附件，拒绝越界、软链接和目录。
func CopyForkAttachment(sourceRoot, targetRoot, relative string) error {
	relative = filepath.Clean(relative)
	if filepath.IsAbs(relative) || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return status_error.MessageAttachmentInvalid
	}
	source := filepath.Join(sourceRoot, relative)
	canonicalRoot, rootErr := filepath.EvalSymlinks(sourceRoot)
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil || rootErr != nil || filepath.Clean(resolved) != filepath.Join(canonicalRoot, relative) {
		return status_error.MessageAttachmentInvalid
	}
	input, err := os.Open(source)
	if err != nil {
		return status_error.MessageAttachmentInvalid
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return status_error.MessageAttachmentInvalid
	}
	target := filepath.Join(targetRoot, relative)
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return status_error.ManagedWorkspaceError
	}
	// 同目录临时文件同步后原子替换，重试不会接受上一次部分复制的文件。
	output, err := os.CreateTemp(filepath.Dir(target), "fork-attachment-*")
	if err != nil {
		return status_error.ManagedWorkspaceError
	}
	defer os.Remove(output.Name())
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if errors.Join(copyErr, syncErr, closeErr) != nil {
		return status_error.ManagedWorkspaceError
	}
	if os.Rename(output.Name(), target) != nil || syncRename(output.Name(), target) != nil {
		return status_error.ManagedWorkspaceError
	}
	return nil
}
