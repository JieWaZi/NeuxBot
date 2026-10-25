// Package stdio 为不携带 cwd 的智能体 MCP 配置提供无协议转换的进程启动边界。
package stdio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// Run 在明确插件目录内启动原程序，保留 argv 和三个标准数据流。
func Run(ctx context.Context, directory string, arguments []string, input io.Reader, output, diagnostics io.Writer) int {
	if len(arguments) == 0 || arguments[0] == "" {
		return invalid(diagnostics)
	}
	directory, err := filepath.EvalSymlinks(directory)
	if err != nil || !filepath.IsAbs(directory) {
		return invalid(diagnostics)
	}
	allowed := false
	for _, key := range []string{"PLUGIN_ROOT", "PLUGIN_DATA"} {
		root, err := filepath.EvalSymlinks(os.Getenv(key))
		if err != nil || !filepath.IsAbs(root) {
			continue
		}
		relative, err := filepath.Rel(root, directory)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			allowed = true
			break
		}
	}
	if !allowed {
		return invalid(diagnostics)
	}
	command := exec.CommandContext(ctx, arguments[0], arguments[1:]...)
	command.Dir = directory
	command.Stdin = input
	command.Stdout = output
	command.Stderr = diagnostics
	if err := command.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if code := exit.ExitCode(); code > 0 {
				return code
			}
			return 1
		}
		logx.Errorf("start plugin stdio process failed: %v", err)
		return 127
	}
	return 0
}

// Main 只处理受管子进程启动参数，不启动 Core HTTP 服务或软件能力入口。
func Main(arguments []string, input io.Reader, output, diagnostics io.Writer) int {
	if len(arguments) < 4 || arguments[0] != "--cwd" || arguments[2] != "--" {
		return invalid(diagnostics)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return Run(ctx, arguments[1], arguments[3:], input, output, diagnostics)
}
func invalid(writer io.Writer) int {
	if writer != nil {
		_, _ = fmt.Fprintln(writer, status_error.PluginContentInvalid.Key())
	}
	return 2
}
