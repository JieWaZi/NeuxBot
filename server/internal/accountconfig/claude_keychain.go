package accountconfig

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"golang.org/x/text/unicode/norm"
)

// claudeNativeKeychain 仅访问显式原生配置目录派生的精确 macOS 凭据项。
type claudeNativeKeychain struct {
	// run 隔离系统工具边界；凭据仅通过 stdin 和私有 stdout 传输。
	run func(context.Context, []string, []byte) ([]byte, error)
	// username 与原生 CLI 使用相同的 Keychain account 身份。
	username func() (string, error)
}

// NewClaudeKeychain 创建不读取凭据的原生 Keychain 访问器；受管目录授权由 ClaudeCredentialStore 校验。
func NewClaudeKeychain() ClaudeKeychain {
	return &claudeNativeKeychain{run: runClaudeSecurity, username: claudeKeychainUsername}
}

func claudeKeychainUsername() (string, error) {
	if name := os.Getenv("USER"); name != "" {
		return name, nil
	}
	current, err := user.Current()
	if err != nil || current.Username == "" {
		return "", status_error.AIAccountCredentialError
	}
	return current.Username, nil
}

func claudeKeychainService(home string) (string, error) {
	// 使用传给 CLI 的确切路径，只做其原生 NFC 规范化；不展开、清理或解析为另一个根。
	if !filepath.IsAbs(home) || strings.ContainsAny(home, "\x00\r\n") {
		return "", status_error.AIAccountConfigurationInvalid
	}
	digest := sha256.Sum256([]byte(norm.NFC.String(home)))
	return "Claude Code-credentials-" + hex.EncodeToString(digest[:4]), nil
}

func (k *claudeNativeKeychain) item(home string) (string, string, error) {
	service, err := claudeKeychainService(home)
	if err != nil {
		return "", "", err
	}
	name, err := k.username()
	if err != nil || name == "" || strings.ContainsAny(name, "\x00\r\n") {
		return "", "", status_error.AIAccountCredentialError
	}
	return service, name, nil
}

func (k *claudeNativeKeychain) Read(ctx context.Context, home string) ([]byte, error) {
	service, name, err := k.item(home)
	if err != nil {
		return nil, err
	}
	output, err := k.run(ctx, []string{"find-generic-password", "-a", name, "-w", "-s", service}, nil)
	if err != nil {
		clear(output)
		return nil, err
	}
	result := bytes.TrimSpace(output)
	if len(result) == 0 || len(result) > 1<<20 || !json.Valid(result) {
		clear(output)
		return nil, status_error.AIAccountCredentialError
	}
	return result, nil
}

func claudeSecurityQuote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

func (k *claudeNativeKeychain) Write(ctx context.Context, home string, credential []byte) error {
	service, name, err := k.item(home)
	if err != nil {
		return err
	}
	if len(credential) == 0 || len(credential) > 1<<20 || !json.Valid(credential) {
		return status_error.AIAccountCredentialError
	}
	// security -i 有固定命令缓冲上限；过大文档由上层采用原生受管文件回退，绝不放到 argv。
	command := []byte("add-generic-password -U -a " + claudeSecurityQuote(name) + " -s " + claudeSecurityQuote(service) + " -X \"" + hex.EncodeToString(credential) + "\"\n")
	defer clear(command)
	if len(command) > 4032 {
		return status_error.AIAccountCredentialError
	}
	output, err := k.run(ctx, []string{"-i"}, command)
	clear(output)
	return err
}

func (k *claudeNativeKeychain) Remove(ctx context.Context, home string) error {
	service, name, err := k.item(home)
	if err != nil {
		return err
	}
	output, err := k.run(ctx, []string{"delete-generic-password", "-a", name, "-s", service}, nil)
	clear(output)
	return err
}

func runClaudeSecurity(ctx context.Context, args []string, input []byte) ([]byte, error) {
	command := exec.CommandContext(ctx, "/usr/bin/security", args...)
	command.Stdin, command.Stderr = bytes.NewReader(input), io.Discard
	output, err := command.StdoutPipe()
	if err != nil {
		return nil, status_error.AIAccountCredentialError
	}
	if err := command.Start(); err != nil {
		return nil, status_error.AIAccountCredentialError
	}
	data, readErr := io.ReadAll(io.LimitReader(output, (1<<20)+1))
	if readErr != nil || len(data) > 1<<20 {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if ctx.Err() != nil {
		clear(data)
		return nil, status_error.OperationCanceled
	}
	if readErr != nil || len(data) > 1<<20 {
		clear(data)
		return nil, status_error.AIAccountCredentialError
	}
	if waitErr != nil {
		clear(data)
		var exit *exec.ExitError
		if errors.As(waitErr, &exit) && exit.ExitCode() == 44 {
			return nil, os.ErrNotExist
		}
		return nil, status_error.AIAccountCredentialError
	}
	return data, nil
}
