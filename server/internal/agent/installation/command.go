package installation

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type commandRunner struct{}

type boundedOutput struct {
	// text retains a bounded diagnostic/version response without blocking process pipes.
	text strings.Builder
}

func (b *boundedOutput) Write(data []byte) (int, error) {
	if remaining := (64 << 10) - b.text.Len(); remaining > 0 {
		_, _ = b.text.Write(data[:min(remaining, len(data))])
	}
	return len(data), nil
}

func (commandRunner) Run(ctx context.Context, input Command) (string, error) {
	command := exec.CommandContext(ctx, input.Binary, input.Args...)
	command.Dir, command.Env, command.WaitDelay = input.Dir, input.Env, 2*time.Second
	configureProcess(command)
	output := &boundedOutput{}
	command.Stdout, command.Stderr = output, output
	err := command.Run()
	return output.text.String(), err
}

func privateEnvironment(root string) []string {
	// Never expose provider credentials or use the user's real home, npm config, or cache.
	values := make([]string, 0, 15)
	for _, name := range []string{"PATH", "SystemRoot", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value := os.Getenv(name); value != "" {
			values = append(values, name+"="+value)
		}
	}
	for _, pair := range [][2]string{{"HOME", "home"}, {"USERPROFILE", "home"}, {"APPDATA", "home"}, {"LOCALAPPDATA", "cache"}, {"XDG_CONFIG_HOME", "home"}, {"XDG_CACHE_HOME", "cache"}, {"TMPDIR", "tmp"}, {"TMP", "tmp"}, {"TEMP", "tmp"}, {"NPM_CONFIG_CACHE", "cache"}} {
		values = append(values, pair[0]+"="+filepath.Join(root, pair[1]))
	}
	return append(values, "CI=1", "NO_COLOR=1", "NPM_CONFIG_UPDATE_NOTIFIER=false", "NPM_CONFIG_REGISTRY=https://registry.npmjs.org", "NPM_CONFIG_USERCONFIG="+filepath.Join(root, ".npmrc"), "NPM_CONFIG_GLOBALCONFIG="+filepath.Join(root, "global.npmrc"))
}

func (i *Installer) installNPM(ctx context.Context, artifact artifact, downloaded, payload, staging string) error {
	node, err := i.lookPath("node")
	if err != nil {
		return failure("Node.js prerequisite missing", err)
	}
	npm, err := i.lookPath("npm")
	if err != nil {
		return failure("npm prerequisite missing", err)
	}
	for _, name := range []string{"home", "cache", "tmp"} {
		if err := os.MkdirAll(filepath.Join(staging, name), 0o700); err != nil {
			return failure("create private install environment", err)
		}
	}
	if err := os.WriteFile(filepath.Join(staging, ".npmrc"), []byte("registry=https://registry.npmjs.org\nignore-scripts=true\naudit=false\nfund=false\n"), 0o600); err != nil {
		return failure("write private npm configuration", err)
	}
	if err := os.WriteFile(filepath.Join(staging, "global.npmrc"), nil, 0o600); err != nil {
		return failure("write private global npm configuration", err)
	}
	environment := privateEnvironment(staging)
	version, err := i.runner.Run(ctx, Command{Binary: node, Args: []string{"--version"}, Dir: staging, Env: environment})
	if ctx.Err() != nil {
		return canceled(ctx)
	}
	major, parseErr := strconv.Atoi(strings.Split(strings.TrimPrefix(strings.TrimSpace(version), "v"), ".")[0])
	if err != nil || parseErr != nil || major < 20 {
		return failure("Node.js 20 or newer required", err)
	}
	args := []string{"install", "--prefix", payload, "--ignore-scripts", "--omit=optional", "--no-audit", "--no-fund", "--no-package-lock", "--registry=https://registry.npmjs.org", downloaded}
	if i.os == "windows" {
		// Execute npm through Node; cmd.exe would reinterpret spaces and shell metacharacters.
		npmEntry := filepath.Join(filepath.Dir(npm), "node_modules", "npm", "bin", "npm-cli.js")
		if _, err := os.Stat(npmEntry); err != nil {
			return failure("npm Node entry unavailable", err)
		}
		args, npm = append([]string{npmEntry}, args...), node
	}
	if _, err := i.runner.Run(ctx, Command{Binary: npm, Args: args, Dir: staging, Env: environment}); err != nil {
		if ctx.Err() != nil {
			return canceled(ctx)
		}
		return failure("install official npm package dependencies", err)
	}
	return nil
}
