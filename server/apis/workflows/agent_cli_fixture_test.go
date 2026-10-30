package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
)

// fixtureCLIBinary 是本测试进程从当前源码构建的真实可执行文件。
var fixtureCLIBinary string

func runCLITestSuite(m *testing.M) int {
	directory, err := os.MkdirTemp("", "allycli-integration-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fixtureCLIBinary = filepath.Join(directory, "allycli")
	if os.PathSeparator == '\\' {
		fixtureCLIBinary += ".exe"
	}
	command := exec.Command("go", "build", "-o", fixtureCLIBinary, "../../cmd/allycli")
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		_ = os.RemoveAll(directory)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(directory)
	return code
}

func fixtureCLI(ctx context.Context, request fixtureInvocation, arguments []string, input map[string]any) (map[string]any, error) {
	return fixtureCLIEnvironment(ctx, request.Workspace.Path, request.Resources.Target.Environment, arguments, input)
}

// fixtureCLIEnvironment 让外部 Driver 使用真实启动环境访问同一 allycli 授权入口。
func fixtureCLIEnvironment(ctx context.Context, directory string, environment map[string]string, arguments []string, input map[string]any) (map[string]any, error) {
	args := append([]string(nil), arguments...)
	var body []byte
	if input != nil {
		body, _ = json.Marshal(input)
		args = append(args, "--input", "-")
	}
	command := exec.CommandContext(ctx, fixtureCLIBinary, args...)
	command.Dir = directory
	command.Env = append(os.Environ(), constants.AgentCLIContextEnvironment+"="+environment[constants.AgentCLIContextEnvironment])
	command.Stdin = bytes.NewReader(body)
	var output, failure bytes.Buffer
	command.Stdout, command.Stderr = &output, &failure
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("CLI failed: %w: %s", err, failure.String())
	}
	var response agentcapability.Response
	if err := json.Unmarshal(output.Bytes(), &response); err != nil || !response.OK {
		return nil, fmt.Errorf("invalid CLI response: %s", output.String())
	}
	value, _ := response.Data.(map[string]any)
	return value, nil
}
