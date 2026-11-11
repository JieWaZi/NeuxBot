package workflows

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
