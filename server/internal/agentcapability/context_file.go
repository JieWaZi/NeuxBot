package agentcapability

import (
	"encoding/json"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// Connection 是只供 CLI 进程读取的受管连接描述，不进入模型指令。
type Connection struct {
	// ProtocolVersion 验证 CLI 与 Core 同包协议。
	ProtocolVersion int `json:"protocolVersion"`
	// Endpoint 是经过回环与固定路径校验的 Core 地址。
	Endpoint string `json:"endpoint"`
	// CoreEpoch 区分不同 Core 生命周期。
	CoreEpoch string `json:"coreEpoch"`
	// GrantID 是当前真实执行授权身份。
	GrantID string `json:"grantId"`
	// Token 是只保存在受管文件和请求头中的凭据。
	Token string `json:"token"`
}

// Validate 验证连接地址不能把执行令牌转交外部地址。
func (c Connection) Validate() error {
	u, err := url.Parse(c.Endpoint)
	if err != nil || c.ProtocolVersion != constants.AgentCLIProtocolVersion || c.Token == "" || c.GrantID == "" || c.CoreEpoch == "" || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != constants.AgentCLIPath {
		return status_error.AgentConnectionUnavailable
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || u.Port() == "" {
		return status_error.AgentConnectionUnavailable
	}
	return nil
}

// ReadConnection 固定读取一次完整描述文件；在途命令不能换用下一轮身份。
func ReadConnection(path string) (Connection, error) {
	if !filepath.IsAbs(path) {
		return Connection{}, status_error.AgentContextStorageError
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || !privateFile(info) {
		return Connection{}, status_error.AgentContextStorageError
	}
	file, err := os.Open(path)
	if err != nil {
		return Connection{}, status_error.AgentContextStorageError
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return Connection{}, status_error.AgentContextStorageError
	}
	data, err := io.ReadAll(io.LimitReader(file, constants.AgentCLIContextMaximumBytes+1))
	if err != nil || len(data) > constants.AgentCLIContextMaximumBytes {
		return Connection{}, status_error.AgentContextStorageError
	}
	var result Connection
	if json.Unmarshal(data, &result) != nil {
		return Connection{}, status_error.AgentContextStorageError
	}
	return result, result.Validate()
}

// WriteConnection 在应用独占目录发布文件，拒绝父目录或目标的符号链接替换。
func WriteConnection(directory, filename string, value Connection) (string, error) {
	if !filepath.IsAbs(directory) || !filepath.IsLocal(filename) || strings.ContainsAny(filename, "/\\") {
		return "", status_error.AgentContextStorageError
	}
	if err := value.Validate(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		logx.Errorf("create agent context directory failed: error=%v", err)
		return "", status_error.AgentContextStorageError
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(directory) {
		return "", status_error.AgentContextStorageError
	}
	if err := protectDirectory(directory); err != nil {
		return "", status_error.AgentContextStorageError
	}
	path := filepath.Join(directory, filename)
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return "", status_error.AgentContextStorageError
	} else if err != nil && !os.IsNotExist(err) {
		return "", status_error.AgentContextStorageError
	}
	file, err := os.CreateTemp(directory, "connection-staging-")
	if err != nil {
		return "", status_error.AgentContextStorageError
	}
	defer os.Remove(file.Name())
	data, err := json.Marshal(value)
	if err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return "", status_error.AgentContextStorageError
	}
	if err := replaceConnection(file.Name(), path); err != nil {
		logx.Errorf("publish agent context file failed: error=%v", err)
		return "", status_error.AgentContextStorageError
	}
	return path, nil
}
