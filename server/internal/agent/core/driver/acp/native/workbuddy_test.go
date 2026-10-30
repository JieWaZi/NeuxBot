package native

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	sdk "github.com/JieWaZi/acp-go/sdk"
)

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
	agent, err := New(context.Background(), Config{RuntimeID: runtime, Executable: binary, PrefixArgs: []string{"-test.run=^TestWorkBuddyTitleProcess$", "--", "--probe-dir", dir}, Environment: environment, WorkingDirectory: dir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
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
