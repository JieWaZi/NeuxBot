package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	sdk "github.com/JieWaZi/acp-go/sdk"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestUnknownRuntime(t *testing.T) {
	_, err := NewAgent(context.Background(), AgentConfig{RuntimeID: "missing"})
	var f *Fault
	if !errors.As(err, &f) || f.Code != FaultUnsupported {
		t.Fatalf("error: %v", err)
	}
}
func TestConstructAgents(t *testing.T) {
	for _, descriptor := range Descriptors() {
		id := descriptor.ID
		t.Run(id, func(t *testing.T) {
			dir := t.TempDir()
			executable := filepath.Join(dir, "cli")
			if err := os.WriteFile(executable, []byte("#!/bin/sh\ncase \"$*\" in *--version*) echo \"codex-cli 0.99.0\";; esac\n"), 0755); err != nil {
				t.Fatal(err)
			}
			module := filepath.Join(dir, "module")
			if err := os.WriteFile(module, []byte(""), 0600); err != nil {
				t.Fatal(err)
			}
			a, err := NewAgent(context.Background(), AgentConfig{RuntimeID: id, Executable: executable, PrefixArgs: []string{"--test"}, Environment: []string{"PI_MCP_MODULE_PATH=" + module}, WorkingDirectory: t.TempDir(), StateDirectory: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if err != nil || a == nil {
				t.Fatalf("agent=%v error=%v", a, err)
			}
			t.Cleanup(func() {
				if closer, ok := a.(interface{ Close(context.Context) error }); ok {
					_ = closer.Close(context.Background())
				}
			})
		})
	}
}
func TestEnvironmentValue(t *testing.T) {
	if got := environmentValue([]string{"PI_MCP_MODULE_PATH=old", "OTHER=x", "PI_MCP_MODULE_PATH=a=b"}, "PI_MCP_MODULE_PATH"); got != "a=b" {
		t.Fatalf("value=%q", got)
	}
}

// TestQoderProcessConfig 验证显式环境和启动配置实际传入进程。
func TestQoderProcessConfig(t *testing.T) {
	t.Setenv("NEUX_NATIVE_SENTINEL", "inherited")
	for _, tc := range []struct {
		// name 标识本次显式环境场景。
		name string
		// env 是允许传给受控进程的全部环境。
		env []string
		// want 是进程应观察到的两个环境变量。
		want string
	}{
		{"empty", []string{}, "absent|absent"},
		{"explicit", []string{"NEUX_NATIVE_VALUE=provided"}, "absent|provided"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			out := filepath.Join(dir, "capture")
			cli := filepath.Join(dir, "cli")
			script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\n%%s\n%%s|%%s\n' \"$*\" \"$PWD\" \"${NEUX_NATIVE_SENTINEL-absent}\" \"${NEUX_NATIVE_VALUE-absent}\" > %q\nexec /bin/cat >/dev/null\n", out)
			if err := os.WriteFile(cli, []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			a, err := NewAgent(context.Background(), AgentConfig{RuntimeID: "qoder", Executable: cli, PrefixArgs: []string{"--prefix", "value"}, Environment: tc.env, WorkingDirectory: dir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = a.(interface{ Close(context.Context) error }).Close(context.Background()) })
			var data []byte
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				data, err = os.ReadFile(out)
				// Shell 的 printf 可能分次写入，首行出现不代表三行配置已全部落盘。
				if err == nil && strings.Count(string(data), "\n") >= 3 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			physicalDir, err := filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(lines) != 3 || lines[0] != "--prefix value --acp" || lines[1] != physicalDir || lines[2] != tc.want {
				t.Fatalf("capture=%q", data)
			}
		})
	}
}

// TestConstructionFailureReturnsNil 验证构造失败不会返回带类型的空接口。
func TestConstructionFailureReturnsNil(t *testing.T) {
	for _, descriptor := range Descriptors() {
		id := descriptor.ID
		t.Run(id, func(t *testing.T) {
			a, err := NewAgent(context.Background(), AgentConfig{RuntimeID: id, Executable: filepath.Join(t.TempDir(), "missing"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if err == nil || a != nil {
				t.Fatalf("agent=%v error=%v", a, err)
			}
		})
	}
}

// TestQoderConfigCategories 验证公开工厂将 Qoder 与 Qoder CN 的错误配置类别收回对应适配器。
// 子进程只运行本测试二进制，不启动用户安装的 CLI。
func TestQoderConfigCategories(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"qoder", "qoder-cn"} {
		t.Run(id, func(t *testing.T) {
			a, err := NewAgent(context.Background(), AgentConfig{RuntimeID: id, Executable: binary, PrefixArgs: []string{"-test.run=^TestQoderConfigProcess$", "--"}, Environment: []string{"NEUX_QODER_CONFIG_PROCESS=1"}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = a.(interface{ Close(context.Context) error }).Close(context.Background()) })
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err := a.Initialize(ctx, sdk.InitializeRequest{ProtocolVersion: sdk.ProtocolVersionNumber}); err != nil {
				t.Fatal(err)
			}
			session, err := a.NewSession(ctx, sdk.NewSessionRequest{Cwd: t.TempDir(), McpServers: []sdk.McpServer{}})
			if err != nil {
				t.Fatal(err)
			}
			check := func(options []sdk.SessionConfigOption, reasoning string) {
				t.Helper()
				if len(options) != 1 || options[0].Select == nil || options[0].Select.Category == nil || *options[0].Select.Category != sdk.SessionConfigOptionCategoryThoughtLevel || options[0].Select.Id != "reasoning_effort" || string(options[0].Select.CurrentValue) != reasoning {
					t.Fatalf("Qoder reasoning was exposed as model or lost its native identity: %+v", options)
				}
			}
			check(session.ConfigOptions, "low")
			response, err := a.SetSessionConfigOption(ctx, sdk.SetSessionConfigOptionRequest{ValueId: &sdk.SetSessionConfigOptionValueId{SessionId: session.SessionId, ConfigId: "reasoning_effort", Value: "high"}})
			if err != nil {
				t.Fatal(err)
			}
			check(response.ConfigOptions, "high")
		})
	}
}

// TestQoderConfigProcess 在受控子进程中模拟 Qoder 原生的错误类别与原生 setter。
func TestQoderConfigProcess(t *testing.T) {
	if os.Getenv("NEUX_QODER_CONFIG_PROCESS") != "1" {
		return
	}
	if strings.Contains(strings.Join(os.Args, " "), "--version") {
		fmt.Fprintln(os.Stdout, "qoder 0.1.0")
		os.Exit(0)
	}
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	for {
		var request map[string]json.RawMessage
		if err := decoder.Decode(&request); err != nil {
			os.Exit(0)
		}
		var method string
		_ = json.Unmarshal(request["method"], &method)
		var result any
		switch method {
		case "initialize":
			result = sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber}
		case "session/new", "session/set_config_option":
			value := "low"
			if method == "session/set_config_option" {
				var config sdk.SetSessionConfigOptionRequest
				if err := json.Unmarshal(request["params"], &config); err != nil || config.ValueId == nil || config.ValueId.ConfigId != "reasoning_effort" {
					os.Exit(2)
				}
				value = string(config.ValueId.Value)
			}
			result = map[string]any{"sessionId": "native", "configOptions": []any{map[string]any{"id": "reasoning_effort", "name": "Reasoning", "type": "select", "category": "model", "currentValue": value, "options": []any{map[string]any{"value": "low", "name": "Low"}, map[string]any{"value": "high", "name": "High"}}}}}
		default:
			os.Exit(2)
		}
		if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": result}); err != nil {
			os.Exit(2)
		}
	}
}

// TestDescriptorSnapshotsCannotChangeLaunchPolicy 验证调用方修改目录副本不会污染下一次启动的权限候选或安装入口。
func TestDescriptorSnapshotsCannotChangeLaunchPolicy(t *testing.T) {
	descriptor, ok := Lookup("codex")
	if !ok {
		t.Fatal("Codex descriptor missing")
	}
	descriptor.Modes.Unrestricted[0] = "read-only"
	descriptor.BinaryCandidates[0] = "other"
	for _, item := range Descriptors() {
		if item.ID == "codex" {
			item.Modes.Unrestricted[0] = "other"
		}
	}
	again, _ := Lookup("codex")
	if !reflect.DeepEqual(again.Modes.Unrestricted, []string{"agent-full-access"}) || !reflect.DeepEqual(again.BinaryCandidates, []string{"codex"}) {
		t.Fatalf("descriptor mutation escaped snapshot: %+v", again)
	}
}

// TestNativeConfigurationReachesCLI 验证冻结的插件和设置事实由统一工厂转为真实进程参数。
func TestNativeConfigurationReachesCLI(t *testing.T) {
	for _, tc := range []struct {
		// id 是需要消费原生配置覆盖的适配器。
		id string
		// want 是受控版本进程应收到的完整参数。
		want string
	}{
		{"codex", "--prefix\n-c\na=1\n-c\nz=2\n--version\n"},
		{"claude", "--prefix\n--settings\n/settings.json\n--plugin-dir\n/plugin-a\n--plugin-dir\n/plugin-b\n--version\n"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			dir := t.TempDir()
			capture := filepath.Join(dir, "capture")
			executable := filepath.Join(dir, "cli")
			// 后台 app-server 可以晚于版本探测启动，不能覆盖本断言的版本参数记录。
			script := "#!/bin/sh\nfor argument do last=$argument; done\nif [ \"$last\" = \"--version\" ]; then\n  printf '%s\\n' \"$@\" > \"$CAPTURE\"\nfi\necho 'codex-cli 0.99.0'\n"
			if err := os.WriteFile(executable, []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			agent, err := NewAgent(context.Background(), AgentConfig{RuntimeID: tc.id, Executable: executable, PrefixArgs: []string{"--prefix"}, Environment: []string{"CAPTURE=" + capture}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), CodexConfiguration: map[string]string{"z": "2", "a": "1"}, ClaudeSettingsPath: "/settings.json", NativePluginDirectories: []string{"/plugin-a", "/plugin-b"}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = agent.(interface{ Close(context.Context) error }).Close(context.Background()) })
			got, err := os.ReadFile(capture)
			if err != nil || string(got) != tc.want {
				t.Fatalf("process arguments=%q error=%v, want %q", got, err, tc.want)
			}
		})
	}
}

func TestTaskSystemInstructionsKeepNativeCodexRoleAndSerializeOtherCLIs(t *testing.T) {
	messages := []Message{{Role: "system", Content: "system instruction"}, {Role: "user", Content: "current task"}}
	codex, err := CompileTaskMessages("codex", messages)
	if err != nil {
		t.Fatal(err)
	}
	if codex.Text != "current task" || len(codex.Instructions) != 1 {
		t.Fatalf("codex=%+v", codex)
	}
	workspace := t.TempDir()
	config, err := ConfigureTaskInstructions(AgentConfig{RuntimeID: "codex", WorkingDirectory: workspace, CodexConfiguration: map[string]string{"preserved": "true"}}, codex.Instructions)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "model-instructions.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "system instruction" || config.CodexConfiguration["model_instructions_file"] != strconv.Quote(path) || config.CodexConfiguration["preserved"] != "true" {
		t.Fatalf("native configuration=%+v contents=%s", config.CodexConfiguration, data)
	}
	other, err := CompileTaskMessages("kimi", messages)
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Instructions) != 0 || !strings.Contains(other.Text, "<message role=\"system\">\nsystem instruction") || !strings.Contains(other.Text, "current task") {
		t.Fatalf("other=%+v", other)
	}
}
func TestTaskInstructionConfigurationDoesNotMutateFrozenOverrides(t *testing.T) {
	overrides := map[string]string{"unchanged": "true"}
	_, err := ConfigureTaskInstructions(AgentConfig{RuntimeID: "codex", WorkingDirectory: t.TempDir(), CodexConfiguration: overrides}, []string{"one"})
	if err != nil {
		t.Fatal(err)
	}
	if len(overrides) != 1 {
		t.Fatal("native helper mutated frozen host configuration")
	}
}
func TestNativeSelectedSkillSyntaxComesFromDescriptor(t *testing.T) {
	for id, want := range map[string]string{"codex": "$", "kimi": "/skill:", "pi": "/skill:", "claude": ""} {
		descriptor, ok := Lookup(id)
		if !ok || descriptor.SkillPrefix != want {
			t.Fatalf("%s skill syntax=%q want=%q", id, descriptor.SkillPrefix, want)
		}
	}
}

// TestWorkBuddyTitleEnvironment 验证关闭原生标题仅作用于 WorkBuddy 子进程，保留显式与继承环境边界。
func TestWorkBuddyTitleEnvironment(t *testing.T) {
	t.Setenv("NEUX_WORKBUDDY_SENTINEL", "inherited")
	t.Setenv("CODEBUDDY_CODE_DISABLE_TERMINAL_TITLE", "0")
	for _, tc := range []struct {
		// name 标识本次环境继承或覆盖场景。
		name string
		// runtime 是受控原生工厂，不运行用户安装的 CLI。
		runtime string
		// environment 是调用方交付的完整启动环境。
		environment []string
		// wantTitle 是子进程实际观察到的标题开关。
		wantTitle string
		// wantSentinel 是子进程实际观察到的继承标记。
		wantSentinel string
	}{
		{"inherit", "workbuddy", nil, "1", "inherited"},
		{"empty", "workbuddy", []string{}, "1", ""},
		{"explicit", "workbuddy", []string{"NEUX_WORKBUDDY_SENTINEL=explicit"}, "1", "explicit"},
		{"override", "workbuddy", []string{"CODEBUDDY_CODE_DISABLE_TERMINAL_TITLE=0", "NEUX_WORKBUDDY_SENTINEL=explicit", "CODEBUDDY_CODE_DISABLE_TERMINAL_TITLE=false"}, "1", "explicit"},
		{"other-runtime", "qoder", []string{"CODEBUDDY_CODE_DISABLE_TERMINAL_TITLE=0"}, "0", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := slices.Clone(tc.environment)
			agent := workBuddyTitleFixture(t, tc.runtime, tc.environment, t.TempDir())
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			response, err := agent.Initialize(ctx, sdk.InitializeRequest{ProtocolVersion: sdk.ProtocolVersionNumber})
			if err != nil {
				t.Fatal(err)
			}
			if response.Meta["titleDisabled"] != tc.wantTitle || response.Meta["sentinel"] != tc.wantSentinel || response.Meta["hasPath"] != (tc.environment == nil && os.Getenv("PATH") != "") {
				t.Fatalf("child environment=%v", response.Meta)
			}
			if !reflect.DeepEqual(before, tc.environment) || os.Getenv("CODEBUDDY_CODE_DISABLE_TERMINAL_TITLE") != "0" {
				t.Fatal("native factory mutated caller or host environment")
			}
		})
	}
}

// workBuddyTitleHost 接收正文已交付的同步点，不把正文当作 Prompt 完成。
type workBuddyTitleHost struct {
	// Client 对测试没有提供的宿主能力保持显式失败。
	sdk.Client
	// messages 在原生正文经 SDK 交付后唤醒测试。
	messages chan struct{}
}

func (h *workBuddyTitleHost) SessionUpdate(_ context.Context, request sdk.SessionNotification) error {
	if request.Update.AgentMessageChunk != nil {
		h.messages <- struct{}{}
	}
	return nil
}

// TestWorkBuddyTitlePolicyKeepsPromptReceipt 验证热连接连续两轮仍等真实回执，正文与标题策略不能伪造终态。
func TestWorkBuddyTitlePolicyKeepsPromptReceipt(t *testing.T) {
	dir := t.TempDir()
	agent := workBuddyTitleFixture(t, "workbuddy", []string{}, dir)
	serverInput, clientOutput := io.Pipe()
	clientInput, serverOutput := io.Pipe()
	defer serverInput.Close()
	defer clientOutput.Close()
	defer clientInput.Close()
	defer serverOutput.Close()
	connection := sdk.NewAgentSideConnection(agent, serverOutput, serverInput)
	agent.(interface {
		SetAgentConnection(*sdk.AgentSideConnection)
	}).SetAgentConnection(connection)
	host := &workBuddyTitleHost{messages: make(chan struct{}, 1)}
	peer := sdk.NewClientSideConnection(host, clientOutput, clientInput)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := peer.Initialize(ctx, sdk.InitializeRequest{ProtocolVersion: sdk.ProtocolVersionNumber}); err != nil {
		t.Fatal(err)
	}
	session, err := peer.NewSession(ctx, sdk.NewSessionRequest{Cwd: dir, McpServers: []sdk.McpServer{}})
	if err != nil {
		t.Fatal(err)
	}
	for round := 1; round <= 2; round++ {
		result := make(chan error, 1)
		go func() {
			response, err := peer.Prompt(ctx, sdk.PromptRequest{SessionId: session.SessionId, Prompt: []sdk.ContentBlock{sdk.TextBlock("synthetic input")}})
			if err == nil && response.StopReason != sdk.StopReasonMaxTokens {
				err = fmt.Errorf("native stop reason was replaced: %q", response.StopReason)
			}
			result <- err
		}()
		select {
		case <-host.messages:
		case <-ctx.Done():
			t.Fatal("native text was not delivered")
		}
		select {
		case err := <-result:
			t.Fatalf("Prompt completed before its native receipt: %v", err)
		default:
		}
		if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(round)), nil, 0600); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("native Prompt receipt was not delivered")
		}
	}
}

func workBuddyTitleFixture(t *testing.T, runtime string, environment []string, dir string) sdk.Agent {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	agent, err := NewAgent(context.Background(), AgentConfig{RuntimeID: runtime, Executable: binary, PrefixArgs: []string{"-test.run=^TestWorkBuddyTitleProcess$", "--", "--probe-dir", dir}, Environment: environment, WorkingDirectory: dir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.(interface{ Close(context.Context) error }).Close(context.Background()) })
	return agent
}

// TestWorkBuddyTitleProcess 只模拟真实 stdio 边界，正文后等待测试释放，再发不同于默认 end_turn 的回执。
func TestWorkBuddyTitleProcess(t *testing.T) {
	index := slices.Index(os.Args, "--probe-dir")
	if index < 0 {
		return
	}
	if slices.Contains(os.Args, "--version") {
		fmt.Fprintln(os.Stdout, "codebuddy 2.151.0")
		os.Exit(0)
	}
	dir := os.Args[index+1]
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	round := 0
	for {
		var request map[string]json.RawMessage
		if err := decoder.Decode(&request); err != nil {
			os.Exit(0)
		}
		var method string
		_ = json.Unmarshal(request["method"], &method)
		var result any
		switch method {
		case "initialize":
			result = sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber, AgentInfo: &sdk.Implementation{Name: "title-probe", Version: "2.151.0"}, Meta: map[string]any{"titleDisabled": os.Getenv("CODEBUDDY_CODE_DISABLE_TERMINAL_TITLE"), "sentinel": os.Getenv("NEUX_WORKBUDDY_SENTINEL"), "hasPath": os.Getenv("PATH") != ""}}
		case "session/new":
			result = map[string]any{"sessionId": "title-probe"}
		case "session/prompt":
			round++
			if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "title-probe", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "complete-looking reply"}}}}); err != nil {
				os.Exit(2)
			}
			for {
				if _, err := os.Stat(filepath.Join(dir, strconv.Itoa(round))); err == nil {
					break
				}
				time.Sleep(time.Millisecond)
			}
			result = sdk.PromptResponse{StopReason: sdk.StopReasonMaxTokens}
		case "session/cancel":
			continue
		default:
			os.Exit(2)
		}
		if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": result}); err != nil {
			os.Exit(2)
		}
	}
}
