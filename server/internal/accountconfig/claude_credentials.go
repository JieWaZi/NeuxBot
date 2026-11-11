package accountconfig

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// ClaudeCredentialSnapshot 保存原生订阅凭据及恢复同一身份所需的原生元数据。
type ClaudeCredentialSnapshot struct {
	// Credential 是 CLI 自己维护的令牌文档，宿主不实现令牌兑换协议。
	Credential json.RawMessage `json:"credential"`
	// Metadata 是新版本 CLI 的身份元数据。
	Metadata json.RawMessage `json:"metadata,omitempty"`
	// LegacyMetadata 是旧版本 CLI 的身份元数据。
	LegacyMetadata json.RawMessage `json:"legacyMetadata,omitempty"`
	// AccountID 来自同一隔离原生客户端的登录状态回执。
	AccountID string `json:"accountId"`
	// OrganizationID 来自同一回执的组织身份。
	OrganizationID string `json:"organizationId"`
}

// Valid 校验快照包含可恢复的原生凭据与明确身份。
func (s ClaudeCredentialSnapshot) Valid() bool {
	return s.AccountID != "" && s.OrganizationID != "" && len(s.Credential) > 2 && json.Valid(s.Credential) &&
		(len(s.Metadata) > 2 && json.Valid(s.Metadata) || len(s.LegacyMetadata) > 2 && json.Valid(s.LegacyMetadata))
}

// ClaudeKeychain 仅操作由受管配置目录派生的一个精确凭据项，不枚举系统账号。
type ClaudeKeychain interface {
	// Read 读取所选受管目录的原生凭据。
	Read(context.Context, string) ([]byte, error)
	// Write 恢复所选受管目录的原生凭据。
	Write(context.Context, string, []byte) error
	// Remove 移除所选受管目录的原生凭据。
	Remove(context.Context, string) error
}

// ClaudeCredentialStore 将原生凭据限制在应用自己的 Claude 账号根目录。
type ClaudeCredentialStore struct {
	// root 是唯一允许读写的受管账号根目录。
	root string
	// keychain 是当前平台的精确项访问器；测试可注入内存实现。
	keychain ClaudeKeychain
	// platform 决定原生凭据存储位置，测试无需访问真实系统密钥链。
	platform string
}

// NewClaudeCredentialStore 创建受管快照搬运器，不读取任何凭据。
func NewClaudeCredentialStore(root string, keychain ClaudeKeychain) *ClaudeCredentialStore {
	return &ClaudeCredentialStore{root: root, keychain: keychain, platform: runtime.GOOS}
}

func (s *ClaudeCredentialStore) directory(home string) (string, error) {
	if s == nil || !filepath.IsAbs(s.root) || !filepath.IsAbs(home) {
		return "", status_error.AIAccountConfigurationInvalid
	}
	root, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return "", status_error.AIAccountWorkspaceError
	}
	relative, err := filepath.Rel(root, filepath.Clean(home))
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", status_error.AIAccountConfigurationInvalid
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", status_error.AIAccountWorkspaceError
		}
	}
	return current, nil
}

func claudeCredentialFile(path string, optional bool) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if optional {
			return nil, nil
		}
		return nil, status_error.RuntimeAuthRequired
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, status_error.AIAccountCredentialError
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, status_error.AIAccountCredentialError
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, status_error.AIAccountCredentialError
	}
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(data) > 1<<20 || !json.Valid(data) {
		clear(data)
		return nil, status_error.AIAccountCredentialError
	}
	return data, nil
}

// Read 只读取已经选定的受管目录，不探测 HOME 或默认 Keychain 项。
func (s *ClaudeCredentialStore) Read(ctx context.Context, home string) (ClaudeCredentialSnapshot, error) {
	home, err := s.directory(home)
	if err != nil {
		return ClaudeCredentialSnapshot{}, err
	}
	var credential []byte
	if s.platform == "darwin" {
		if s.keychain == nil {
			return ClaudeCredentialSnapshot{}, status_error.AIAccountCredentialError
		}
		credential, err = s.keychain.Read(ctx, home)
		if errors.Is(err, os.ErrNotExist) {
			credential, err = claudeCredentialFile(filepath.Join(home, constants.ClaudeCredentialsFileName), false)
		}
	} else {
		credential, err = claudeCredentialFile(filepath.Join(home, constants.ClaudeCredentialsFileName), false)
	}
	if err != nil {
		clear(credential)
		return ClaudeCredentialSnapshot{}, err
	}
	if len(credential) == 0 || len(credential) > 1<<20 || !json.Valid(credential) {
		clear(credential)
		return ClaudeCredentialSnapshot{}, status_error.AIAccountCredentialError
	}
	metadata, err := claudeCredentialFile(filepath.Join(home, constants.ClaudeAccountMetadataFileName), true)
	if err != nil {
		clear(credential)
		return ClaudeCredentialSnapshot{}, err
	}
	legacy, err := claudeCredentialFile(filepath.Join(home, constants.ClaudeLegacyMetadataFileName), true)
	if err != nil {
		clear(credential)
		clear(metadata)
		return ClaudeCredentialSnapshot{}, err
	}
	return ClaudeCredentialSnapshot{Credential: credential, Metadata: metadata, LegacyMetadata: legacy}, nil
}

// Write 仅向已核验的受管目录恢复加密数据库中的原生文档。
func (s *ClaudeCredentialStore) Write(ctx context.Context, home string, snapshot ClaudeCredentialSnapshot) error {
	home, err := s.directory(home)
	if err != nil {
		return err
	}
	if !snapshot.Valid() {
		return status_error.AIAccountCredentialError
	}
	for name, data := range map[string]json.RawMessage{constants.ClaudeAccountMetadataFileName: snapshot.Metadata, constants.ClaudeLegacyMetadataFileName: snapshot.LegacyMetadata} {
		if len(data) == 0 {
			continue
		}
		path := filepath.Join(home, name)
		if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() || err != nil && !errors.Is(err, os.ErrNotExist) {
			return status_error.AIAccountWorkspaceError
		}
		if err := WriteFile(path, data); err != nil {
			return err
		}
	}
	if s.platform == "darwin" {
		if s.keychain == nil {
			return status_error.AIAccountCredentialError
		}
		if err := s.keychain.Write(ctx, home, snapshot.Credential); err == nil {
			return nil
		}
		// 原生 Keychain 容量不足时只能在旧精确项已移除后回退到受管文件，避免读取优先级串号。
		if err := s.keychain.Remove(ctx, home); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	path := filepath.Join(home, constants.ClaudeCredentialsFileName)
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() || err != nil && !errors.Is(err, os.ErrNotExist) {
		return status_error.AIAccountWorkspaceError
	}
	return WriteFile(path, snapshot.Credential)
}

// Remove 删除一个受管配置目录的凭据，其他账号和系统配置保持独立。
func (s *ClaudeCredentialStore) Remove(ctx context.Context, home string) error {
	home, err := s.directory(home)
	if err != nil {
		return err
	}
	if s.platform == "darwin" {
		if s.keychain == nil {
			return status_error.AIAccountCredentialError
		}
		if err := s.keychain.Remove(ctx, home); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for _, name := range []string{constants.ClaudeCredentialsFileName, constants.ClaudeAccountMetadataFileName, constants.ClaudeLegacyMetadataFileName} {
		path := filepath.Join(home, name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return status_error.AIAccountWorkspaceError
		}
		if err := os.Remove(path); err != nil {
			return status_error.AIAccountWorkspaceError
		}
	}
	return nil
}
