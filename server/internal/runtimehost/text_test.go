package runtimehost

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
)

// driverFixture 提供可控制最终消息和资源关闭的技术边界。
type driverFixture struct {
	// id 是被测用户 Profile 身份。
	id string
	// opened 保存冻结的技术配置与环境。
	opened c.OpenRequest
	// promptCalls 统计真正提交的执行次数。
	promptCalls int
	// closeCalls 统计清理重试。
	closeCalls int
	// allowClose 控制原生进程是否成功退出。
	allowClose bool
	// failure 是执行完成前返回的故障。
	failure *c.Fault
	// entered 通知已经真正提交 Prompt。
	entered chan struct{}
	// input 保存真正交付的本次任务文本。
	input string
}

func (d *driverFixture) ID() string { return d.id }
func (d *driverFixture) Capabilities() c.DriverCapabilities {
	return c.DriverCapabilities{Version: "fixture"}
}
func (d *driverFixture) Open(_ context.Context, request c.OpenRequest) (c.DriverSession, error) {
	d.opened = request
	return d, nil
}
func (d *driverFixture) Close(context.Context) error {
	d.closeCalls++
	if !d.allowClose {
		return status_error.RuntimeShutdownFailed
	}
	return nil
}
func (d *driverFixture) Prompt(ctx context.Context, request c.TurnRequest, sink c.DriverSink) c.DriverResult {
	d.promptCalls++
	d.input = request.Input.Text
	_ = sink.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
	if d.entered != nil {
		close(d.entered)
		<-ctx.Done()
		return c.DriverResult{Status: c.StatusCancelled, Fault: &c.Fault{Code: c.FaultCancelled, Stage: "prompt", Cause: ctx.Err()}}
	}
	_ = sink.Accept(ctx, c.DriverEvent{Kind: c.EventTool, Tool: &c.ToolEvent{ID: "native-tool", Name: "shell", Status: "completed"}})
	_ = sink.Accept(ctx, c.DriverEvent{Kind: c.EventAssistantMessage, Message: &c.AssistantMessage{MessageID: "commentary", Phase: c.PhaseCommentary, Delta: "process", Complete: true}})
	if d.failure != nil {
		return c.DriverResult{Status: c.StatusFailed, Fault: d.failure}
	}
	_ = sink.Accept(ctx, c.DriverEvent{Kind: c.EventAssistantMessage, Message: &c.AssistantMessage{MessageID: "final", Phase: c.PhaseFinal, Delta: "trusted final", Complete: true}})
	return c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopMaxTokens, Usage: c.Usage{Known: true, InputTokens: 3, CachedReadTokens: 5, OutputTokens: 7}, Configuration: c.Configuration{ActualReasoning: "low", ReasoningFallback: "thinking_off_unsupported"}}
}

func hostFixture(t *testing.T, base string, d *driverFixture) (*Host, *int) {
	t.Helper()
	released := new(int)
	host := New(Config{Resolve: func(context.Context, TextRequest) (Environment, error) {
		return Environment{BaseRuntimeID: base, Target: runtimes.RuntimeTarget{RuntimeID: d.id, Binary: "/selected/codex", PrefixArgs: []string{"--profile", "selected"}, ConfigHome: "/selected/home"}}, nil
	}, Prepare: func(_ context.Context, _ TextRequest, env Environment, _ string) (runtimes.RuntimeTarget, func(context.Context) error, error) {
		target := env.Target
		target.Isolated = true
		target.Environment = map[string]string{"CODEX_HOME": "/managed/home", "OPENAI_API_KEY": "selected-account"}
		return target, func(context.Context) error { *released++; return nil }, nil
	}, Launch: func(target runtimes.RuntimeTarget, _ runtimes.DetectionResult) (runtimes.LaunchFacts, error) {
		return runtimes.LaunchFacts{BaseRuntimeID: base, Executable: target.Binary, PrefixArgs: target.PrefixArgs, Environment: environmentList(target.Environment)}, nil
	}, Driver: func(Environment, acpdriver.Factory) (c.Driver, error) { return d, nil }})
	return host, released
}

func TestCodexProfileFinalSurvivesCleanupAndNeverReplays(t *testing.T) {
	driver := &driverFixture{id: "custom-codex"}
	host, released := hostFixture(t, runtimes.RuntimeIDCodex, driver)
	result, err := host.GenerateText(context.Background(), TextRequest{RuntimeID: driver.id, AccountID: "account", Model: "chosen-model", Reasoning: "high", Messages: []Message{{Role: "system", Content: "task rules"}, {Role: "user", Content: "exact input"}}})
	if err != nil || result.Text != "trusted final" || result.StopReason != "max_tokens" || result.Usage == nil || result.Usage.CacheReadTokens != 5 || driver.promptCalls != 1 || *released != 0 {
		t.Fatalf("result=%+v err=%v prompt=%d release=%d", result, err, driver.promptCalls, *released)
	}
	if driver.opened.Profile.Model != "chosen-model" || driver.opened.Policies.Tools.Mode != c.ToolsAllowAll || len(driver.opened.Contributions.Instructions) != 0 || len(driver.opened.Contributions.MCP) != 0 || driver.opened.Credential.Environment["OPENAI_API_KEY"] != "selected-account" {
		t.Fatalf("open=%+v", driver.opened)
	}
	if _, err := os.Stat(driver.opened.Workspace.Root); err != nil {
		t.Fatalf("workspace released before native close: %v", err)
	}
	driver.allowClose = true
	if err := host.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if driver.promptCalls != 1 || *released != 1 {
		t.Fatalf("cleanup replayed prompt or released twice: prompt=%d release=%d", driver.promptCalls, *released)
	}
	if _, err := os.Stat(driver.opened.Workspace.Root); !os.IsNotExist(err) {
		t.Fatalf("workspace retained: %v", err)
	}
}

func TestExecutedFailureCarriesNoRetryAndNeverFallsBack(t *testing.T) {
	driver := &driverFixture{id: "custom-codex", allowClose: true, failure: &c.Fault{Code: c.FaultDriver, Stage: "prompt", Message: "connection timeout", Cause: context.DeadlineExceeded}}
	host, _ := hostFixture(t, runtimes.RuntimeIDCodex, driver)
	defer host.Close(context.Background())
	result, err := host.GenerateText(context.Background(), TextRequest{RuntimeID: driver.id, Messages: []Message{{Role: "user", Content: "input"}}})
	var policy interface{ Retryable() bool }
	if !errors.Is(err, status_error.OperationTimeout) || !errors.As(err, &policy) || policy.Retryable() || result.Text != "" || driver.promptCalls != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestAllCLIProfilesUseOneHostAndReleaseWithoutReplay(t *testing.T) {
	for _, base := range []string{"codex", "claude", "kimi", "cursor", "pi", "opencode", "gemini", "grok", "workbuddy", "qoder", "qoder-cn"} {
		t.Run(base, func(t *testing.T) {
			driver := &driverFixture{id: "profile-" + base, allowClose: true}
			host, released := hostFixture(t, base, driver)
			defer host.Close(context.Background())
			result, err := host.GenerateText(context.Background(), TextRequest{RuntimeID: driver.id, Messages: []Message{{Role: "system", Content: "task rules"}, {Role: "user", Content: "input"}}})
			if err != nil || result.Text != "trusted final" || driver.promptCalls != 1 || *released != 1 {
				t.Fatalf("result=%+v err=%v calls=%d released=%d", result, err, driver.promptCalls, *released)
			}
			if base != "codex" && !strings.Contains(driver.opened.Profile.DriverID, base) {
				t.Fatalf("profile lost: %+v", driver.opened)
			}
			if base != "codex" && (!strings.Contains(driver.input, "task rules") || !strings.Contains(driver.input, "input")) {
				t.Fatalf("task messages lost: %q", driver.input)
			}
		})
	}
}

// TestOpenCodeManagedModelKeepsSystemAndAccountNamespaces 防止系统模型误加受管前缀或账号模型漏选服务商。
func TestOpenCodeManagedModelKeepsSystemAndAccountNamespaces(t *testing.T) {
	for _, tt := range []struct {
		account string // account 是本次已冻结的账号身份。
		want    string // want 是必须送给原生 OpenCode 的完整模型身份。
	}{{"", "model-a"}, {"selected-account", "ally/model-a"}} {
		driver := &driverFixture{id: "opencode-profile", allowClose: true}
		host, _ := hostFixture(t, runtimes.RuntimeIDOpenCode, driver)
		_, err := host.GenerateText(context.Background(), TextRequest{RuntimeID: driver.id, AccountID: tt.account, Model: "model-a", Messages: []Message{{Role: "user", Content: "input"}}})
		if err != nil {
			t.Fatal(err)
		}
		if driver.opened.Profile.Model != tt.want {
			t.Fatalf("account=%q native model=%q want=%q", tt.account, driver.opened.Profile.Model, tt.want)
		}
		if err := host.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTaskMessagesPreserveOriginalSystemAndSoleUser(t *testing.T) {
	prompt, systems, limitations, err := compileMessages([]Message{{Role: "system", Content: " original system\n"}, {Role: "user", Content: " exact user\n"}})
	if err != nil || prompt != " exact user\n" || !reflect.DeepEqual(systems, []string{" original system\n"}) || len(limitations) != 0 {
		t.Fatalf("prompt=%q systems=%q limitations=%q err=%v", prompt, systems, limitations, err)
	}
	prompt, _, limitations, err = compileMessages([]Message{{Role: "user", Content: "first"}, {Role: "assistant", Content: "continuation"}, {Role: "user", Content: "second"}})
	if err != nil || strings.Index(prompt, "first") > strings.Index(prompt, "continuation") || strings.Index(prompt, "continuation") > strings.Index(prompt, "second") || len(limitations) == 0 {
		t.Fatalf("prompt=%q err=%v", prompt, err)
	}
}

func TestHostCloseCancelsActiveInvocation(t *testing.T) {
	driver := &driverFixture{id: "custom-codex", allowClose: true, entered: make(chan struct{})}
	host, released := hostFixture(t, runtimes.RuntimeIDCodex, driver)
	finished := make(chan error, 1)
	go func() {
		_, err := host.GenerateText(context.Background(), TextRequest{RuntimeID: driver.id, Messages: []Message{{Role: "user", Content: "input"}}})
		finished <- err
	}()
	<-driver.entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := host.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("cancel=%v", err)
	}
	if driver.promptCalls != 1 || *released != 1 {
		t.Fatalf("prompt=%d release=%d", driver.promptCalls, *released)
	}
}
