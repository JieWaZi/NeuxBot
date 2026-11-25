//go:build !windows

package agent

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/JieWaZi/acp-go/pkg/agentclient"
)

// Follow OpenBot's bounded login-shell PATH read while reusing ACP's process owner.
func readHostExecutablePath(ctx context.Context, home string, environment environmentSnapshot) ([]string, bool) {
	var fallback string
	switch runtime.GOOS {
	case "darwin":
		fallback = "/bin/zsh"
	case "linux":
		fallback = "/bin/bash"
		if _, err := validateExecutablePath(fallback); err != nil {
			fallback = "/bin/sh"
		}
	default:
		return nil, false
	}
	shell := environment.get("SHELL")
	switch filepath.Base(shell) {
	case "bash", "fish", "ksh", "sh", "zsh":
		if _, err := validateExecutablePath(shell); err != nil || !filepath.IsAbs(shell) {
			shell = fallback
		}
	default:
		shell = fallback
	}
	flags := "-lic"
	if filepath.Base(shell) == "sh" {
		flags = "-lc"
	}
	const marker = "NEUXBOT_LOGIN_PATH="
	const limit = 64 << 10
	readContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	variables := environment.list()
	// Shell startup uses the host's config roots, independently of native account isolation.
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
		if value, ok := os.LookupEnv(name); ok {
			variables = append(variables, name+"="+value)
		}
	}
	process, err := agentclient.NewProcess(agentclient.ProcessSpec{
		Executable: shell,
		Args:       []string{flags, `printf '\nNEUXBOT_LOGIN_PATH=%s\n' "$PATH"`},
		Env:        variables,
		Dir:        home,
	})
	if err != nil {
		return nil, false
	}
	defer func() {
		// Allow the same process-group shutdown grace as the ACP version probe.
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer stop()
		_ = process.Close(cleanup)
	}()
	if err := process.Start(readContext); err != nil {
		return nil, false
	}
	_ = process.Stdin().Close()
	stdout := process.Stdout()
	stopRead := context.AfterFunc(readContext, func() { _ = stdout.Close() })
	defer stopRead()
	output, err := io.ReadAll(io.LimitReader(stdout, limit+1))
	if err != nil || len(output) > limit {
		return nil, false
	}
	exit, err := process.Wait(readContext)
	if err != nil || exit.Err != nil || exit.Code != 0 {
		return nil, false
	}
	text := string(output)
	if start := strings.LastIndex(text, "\n"+marker); start >= 0 {
		path, _, _ := strings.Cut(text[start+1+len(marker):], "\n")
		if path != "" {
			return filepath.SplitList(path), true
		}
	}
	return nil, false
}
