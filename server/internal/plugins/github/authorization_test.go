package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// 防止未登录时误读机器上已有 gh 账号，以及向伪装官方服务的地址交付凭据。
func TestPrivateProfileAndCredentialDestination(t *testing.T) {
	profile := t.TempDir()
	calls := 0
	auth := New(Config{ProfileDirectory: profile, Run: func(_ context.Context, args []string, _ string, out, _ io.Writer) error {
		calls++
		if reflect.DeepEqual(args, []string{"auth", "token", "--hostname", "github.com"}) {
			_, _ = io.WriteString(out, "private-token\n")
			return nil
		}
		_, _ = fmt.Fprintf(out, `{"hosts":{"github.com":[{"state":"success","active":true,"host":"github.com","login":"octocat","tokenSource":%q,"scopes":"repo, read:org, gist","gitProtocol":"https"}]}}`, filepath.Join(profile, "hosts.yml"))
		return nil
	}})
	account, err := auth.Status(context.Background(), true)
	if err != nil || account.Available || !account.Configured || calls != 0 {
		t.Fatalf("unconfigured profile: %+v %v calls=%d", account, err, calls)
	}
	if err := os.WriteFile(filepath.Join(profile, "hosts.yml"), []byte("github.com: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	account, err = auth.Status(context.Background(), true)
	if err != nil || !account.Available || account.ID != "octocat" {
		t.Fatalf("private account: %+v %v", account, err)
	}
	encoded, _ := json.Marshal(account)
	if strings.Contains(string(encoded), "token") {
		t.Fatalf("credential leaked: %s", encoded)
	}
	for _, url := range []string{"https://evil.example/mcp/", "https://api.githubcopilot.com.evil/mcp/", "https://api.githubcopilot.com/mcp/?redirect=evil", "https://user@api.githubcopilot.com/mcp/", ""} {
		before := calls
		if _, err := auth.AuthorizeServer(context.Background(), url); !errors.Is(err, status_error.PluginForbidden) || calls != before {
			t.Fatalf("destination accepted %q: %v", url, err)
		}
	}
	headers, err := auth.AuthorizeServer(context.Background(), "https://api.githubcopilot.com/mcp/")
	if err != nil || headers["Authorization"] != "Bearer private-token" {
		t.Fatalf("official headers: %+v %v", headers, err)
	}
}

// 防止把设备 URL 当成完整授权而遗漏用户必须手工输入的一次性验证码。
func TestOfficialDeviceFlowAndCancellation(t *testing.T) {
	profile := t.TempDir()
	exited := make(chan struct{})
	auth := New(Config{ProfileDirectory: profile, Run: func(ctx context.Context, args []string, _ string, out, stderr io.Writer) error {
		if len(args) > 1 && args[1] == "login" {
			for _, required := range []string{"--web", "--clipboard=false", "--insecure-storage", "--skip-ssh-key"} {
				if !strings.Contains(strings.Join(args, " "), required) {
					t.Errorf("missing noninteractive option %s", required)
				}
			}
			_, _ = io.WriteString(stderr, "! First copy your one-time code: ABCD-EFGH\nOpen this URL to continue in your web browser: https://github.com/login/device\n")
			<-ctx.Done()
			close(exited)
			return ctx.Err()
		}
		return nil
	}})
	auth.Open(context.Background())
	defer auth.Close()
	session, err := auth.Login(context.Background())
	if err != nil || session.State != "waiting" || session.UserCode != "ABCD-EFGH" || session.VerificationURL != "https://github.com/login/device" {
		t.Fatalf("device: %+v %v", session, err)
	}
	if _, err := auth.Acquire(); !errors.Is(err, status_error.PluginBusy) {
		t.Fatalf("execution acquired during login: %v", err)
	}
	if err := auth.Cancel(session.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("login process not cancelled")
	}
	if snap := auth.Snapshot(); snap.UserCode != "" || snap.VerificationURL != "" || snap.State != "cancelled" {
		t.Fatalf("cancel leaked short-lived details: %+v", snap)
	}
}

// 防止宿主或智能体配置中的 token、profile 与 API 主机覆盖共享身份。
func TestManagedEnvironment(t *testing.T) {
	t.Setenv("GH_TOKEN", "external")
	t.Setenv("GITHUB_TOKEN", "external")
	t.Setenv("GH_CONFIG_DIR", "external")
	env := ManagedEnvironment("/private/profile", map[string]string{"GH_HOST": "evil", "GITHUB_ENTERPRISE_TOKEN": "external", "UNRELATED": "keep"})
	if env["GH_TOKEN"] != "" || env["GITHUB_TOKEN"] != "" || env["GITHUB_ENTERPRISE_TOKEN"] != "" || env["GH_HOST"] != "github.com" || env["GH_CONFIG_DIR"] != "/private/profile" || env["UNRELATED"] != "keep" {
		t.Fatalf("unsafe environment: %+v", env)
	}
}

// 防止清理逻辑只对返回的 map 有效，真实官方子进程仍继承外部凭据。
func TestCommandRunnerOwnsIdentityEnvironment(t *testing.T) {
	t.Setenv("NEUXALLY_GITHUB_ENV_TEST", "1")
	t.Setenv("GH_TOKEN", "external-token")
	t.Setenv("GITHUB_TOKEN", "external-token")
	t.Setenv("GH_CONFIG_DIR", "external-profile")
	t.Setenv("GH_HOST", "external-host")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	profile := t.TempDir()
	var output bytes.Buffer
	err = commandRunner(binary, profile)(context.Background(), []string{"-test.run=^TestGitHubEnvironmentChild$"}, "", &output, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "managed-github-child") {
		t.Fatalf("child did not verify environment: %s", output.String())
	}
}
func TestGitHubEnvironmentChild(t *testing.T) {
	if os.Getenv("NEUXALLY_GITHUB_ENV_TEST") == "" {
		return
	}
	if os.Getenv("GH_TOKEN") != "" || os.Getenv("GITHUB_TOKEN") != "" || os.Getenv("GH_HOST") != "github.com" || os.Getenv("GH_CONFIG_DIR") == "" || os.Getenv("GH_CONFIG_DIR") == "external-profile" || os.Getenv("GH_PROMPT_DISABLED") != "1" {
		t.Fatal("external identity reached child")
	}
	fmt.Fprintln(os.Stdout, "managed-github-child")
}

// 防止拆分到达的官方输出丢失验证码，或异常输出污染已选择的确认地址。
func TestDeviceOutputAcceptsSplitLinesAndRejectsUnexpectedURL(t *testing.T) {
	var got authorization.Device
	output := deviceOutput{ready: func(device authorization.Device) error { got = device; return nil }}
	for _, part := range []string{"! First copy your one-time ", "code: ABCD-EFGH\nOpen this URL to continue in your web browser: https://github.com/", "login/device\n"} {
		if _, err := output.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if got.UserCode != "ABCD-EFGH" || got.VerificationURL != "https://github.com/login/device" {
		t.Fatalf("split output: %+v", got)
	}
	if _, err := output.Write([]byte("Open this URL to continue in your web browser: https://evil.example/\n")); !errors.Is(err, status_error.PluginAuthorizationFailed) {
		t.Fatalf("unofficial confirmation accepted: %v", err)
	}
}

// GitHub 规范化的组织写权限隐含组织读取权限，不能要求服务端重复返回子权限。
func TestNormalizedGitHubScopesRemainAvailable(t *testing.T) {
	profile := t.TempDir()
	if err := os.WriteFile(filepath.Join(profile, "hosts.yml"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	auth := New(Config{ProfileDirectory: profile, Run: func(_ context.Context, _ []string, _ string, out, _ io.Writer) error {
		_, _ = fmt.Fprintf(out, `{"hosts":{"github.com":[{"state":"success","active":true,"host":"github.com","login":"octocat","tokenSource":%q,"scopes":"repo, gist, write:org"}]}}`, filepath.Join(profile, "hosts.yml"))
		return nil
	}})
	account, err := auth.Status(context.Background(), true)
	if err != nil || !account.Available {
		t.Fatalf("normalized permissions rejected: %+v %v", account, err)
	}
}

// 原版 CLI 不能越过运行时的构建身份检查，以免误用尚未隔离的旧开发资源。
func TestCommandRunnerRejectsUnisolatedGitHubBinary(t *testing.T) {
	t.Setenv("NEUXALLY_GITHUB_UNISOLATED_TEST", "1")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	err = commandRunner(binary, t.TempDir())(context.Background(), []string{"auth", "login"}, "", io.Discard, io.Discard)
	if !errors.Is(err, status_error.PluginNotReady) {
		t.Fatalf("original CLI reached authentication command: %v", err)
	}
}

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		if os.Getenv("NEUXALLY_GITHUB_ENV_TEST") == "1" {
			fmt.Fprintln(os.Stdout, "gh version 2.102.0-neuxally.1")
			os.Exit(0)
		}
		if os.Getenv("NEUXALLY_GITHUB_UNISOLATED_TEST") == "1" {
			fmt.Fprintln(os.Stdout, "gh version 2.102.0")
			os.Exit(0)
		}
	}
	if os.Getenv("NEUXALLY_GITHUB_UNISOLATED_TEST") == "1" && len(os.Args) > 1 && os.Args[1] == "auth" {
		os.Exit(17)
	}
	os.Exit(m.Run())
}

// 包含关系只能满足较小权限，不能把只读或公开仓库权限扩张成更高授权。
func TestGitHubScopeContainmentDoesNotGrantBroaderAccess(t *testing.T) {
	adapter := &driver{}
	for _, test := range []struct {
		// granted 是官方已授予权限。
		granted []string
		// required 是本次需要满足的权限。
		required string
		// want 表示官方包含关系是否满足请求。
		want bool
	}{
		{[]string{"admin:org"}, "read:org", true},
		{[]string{"write:org"}, "read:org", true},
		{[]string{"read:org"}, "write:org", false},
		{[]string{"user"}, "user:email", true},
		{[]string{"repo"}, "public_repo", true},
		{[]string{"public_repo"}, "repo", false},
		{[]string{"public_repo"}, "admin:repo_hook", false},
		{[]string{"repo"}, "delete_repo", false},
	} {
		if got := adapter.ScopeGranted(test.granted, test.required); got != test.want {
			t.Errorf("granted=%v required=%s got=%v", test.granted, test.required, got)
		}
	}
}
