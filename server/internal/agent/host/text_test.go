package runtimehost

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp/native"
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
		return Environment{BaseRuntimeID: base, Target: runtimeconfig.RuntimeTarget{RuntimeID: d.id, Binary: "/selected/codex", PrefixArgs: []string{"--profile", "selected"}, ConfigHome: "/selected/home"}}, nil
	}, Prepare: func(_ context.Context, _ TextRequest, env Environment, _ string) (runtimeconfig.RuntimeTarget, func(context.Context) error, error) {
		target := env.Target
		target.Isolated = true
		target.Environment = map[string]string{"CODEX_HOME": "/managed/home", "OPENAI_API_KEY": "selected-account"}
		return target, func(context.Context) error { *released++; return nil }, nil
	}, Launch: func(target runtimeconfig.RuntimeTarget, _ runtimeconfig.DetectionResult) (runtimeconfig.LaunchFacts, error) {
		return runtimeconfig.LaunchFacts{BaseRuntimeID: base, Executable: target.Binary, PrefixArgs: target.PrefixArgs, Environment: environmentList(target.Environment)}, nil
	}, Driver: func(Environment, acpdriver.Factory) (c.Driver, error) { return d, nil }})
	return host, released
}

func TestCodexProfileFinalSurvivesCleanupAndNeverReplays(t *testing.T) {
	driver := &driverFixture{id: "custom-codex"}
	host, released := hostFixture(t, runtimeconfig.RuntimeIDCodex, driver)
	result, err := host.GenerateText(context.Background(), TextRequest{RuntimeID: driver.id, AccountID: "account", Model: "chosen-model", Reasoning: "high", Messages: []Message{{Role: "system", Content: "task rules"}, {Role: "user", Content: "exact input"}}})
	if err != nil || result.Text != "trusted final" || result.StopReason != "max_tokens" || result.Usage == nil || result.Usage.CacheReadTokens != 5 || driver.promptCalls != 1 || *released != 0 {
		t.Fatalf("result=%+v err=%v prompt=%d release=%d", result, err, driver.promptCalls, *released)
	}
	if driver.opened.Profile.Model != "chosen-model" || driver.opened.Policies.Tools.Mode != c.ToolsAllowAll || len(driver.opened.Contributions.Instructions) != 1 || driver.opened.Contributions.Instructions[0].Text != "task rules" || len(driver.opened.Contributions.MCP) != 0 || driver.opened.Credential.Environment["OPENAI_API_KEY"] != "selected-account" {
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
	host, _ := hostFixture(t, runtimeconfig.RuntimeIDCodex, driver)
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
			if !strings.Contains(driver.input, "input") || (!strings.Contains(driver.input, "task rules") && (len(driver.opened.Contributions.Instructions) != 1 || driver.opened.Contributions.Instructions[0].Text != "task rules")) {
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
		host, _ := hostFixture(t, runtimeconfig.RuntimeIDOpenCode, driver)
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
	compiled, err := native.CompileTaskMessages("codex", []native.Message{{Role: "system", Content: " original system\n"}, {Role: "user", Content: " exact user\n"}})
	prompt, systems, limitations := compiled.Text, compiled.Instructions, compiled.Limitations
	if err != nil || prompt != " exact user\n" || !reflect.DeepEqual(systems, []string{" original system\n"}) || len(limitations) != 0 {
		t.Fatalf("prompt=%q systems=%q limitations=%q err=%v", prompt, systems, limitations, err)
	}
	compiled, err = native.CompileTaskMessages("codex", []native.Message{{Role: "user", Content: "first"}, {Role: "assistant", Content: "continuation"}, {Role: "user", Content: "second"}})
	prompt, limitations = compiled.Text, compiled.Limitations
	if err != nil || strings.Index(prompt, "first") > strings.Index(prompt, "continuation") || strings.Index(prompt, "continuation") > strings.Index(prompt, "second") || len(limitations) == 0 {
		t.Fatalf("prompt=%q err=%v", prompt, err)
	}
}

func TestHostCloseCancelsActiveInvocation(t *testing.T) {
	driver := &driverFixture{id: "custom-codex", allowClose: true, entered: make(chan struct{})}
	host, released := hostFixture(t, runtimeconfig.RuntimeIDCodex, driver)
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

// TestHostCleanupRetryKeepsActiveInvocation 验证真实清理周期不会提前关闭仍在执行的独立任务。
func TestHostCleanupRetryKeepsActiveInvocation(t *testing.T) {
	driver := &driverFixture{id: "custom-codex", allowClose: true, entered: make(chan struct{})}
	host, released := hostFixture(t, runtimeconfig.RuntimeIDCodex, driver)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	defer host.Close(context.Background())
	finished := make(chan error, 1)
	go func() {
		_, err := host.GenerateText(ctx, TextRequest{RuntimeID: driver.id, Messages: []Message{{Role: "user", Content: "long task"}}})
		finished <- err
	}()
	select {
	case <-driver.entered:
	case <-ctx.Done():
		t.Fatal("prompt did not start")
	}
	host.Start(ctx)
	select {
	case err := <-finished:
		t.Fatalf("cleanup worker stopped an active invocation: %v", err)
	case <-time.After(11 * time.Second):
	}
	if err := host.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("explicit close did not cancel the active invocation: %v", err)
	}
	if driver.promptCalls != 1 || *released != 1 {
		t.Fatalf("prompt=%d release=%d", driver.promptCalls, *released)
	}
}

// TestHostCloseRetainsLatePartialAcquisition 验证关闭仍取消资源取得，并为迟到部分结果保留同一清理所有者。
func TestHostCloseRetainsLatePartialAcquisition(t *testing.T) {
	driver := &driverFixture{id: "custom-codex", allowClose: true}
	host, _ := hostFixture(t, runtimeconfig.RuntimeIDCodex, driver)
	entered, cancelled := make(chan struct{}), make(chan struct{})
	returnAcquisition, allowRelease := make(chan struct{}), make(chan struct{})
	defer func() {
		for _, signal := range []chan struct{}{returnAcquisition, allowRelease} {
			select {
			case <-signal:
			default:
				close(signal)
			}
		}
		_ = host.Close(context.Background())
	}()
	workspace := ""
	released := 0
	host.config.Prepare = func(ctx context.Context, _ TextRequest, environment Environment, directory string) (runtimeconfig.RuntimeTarget, func(context.Context) error, error) {
		workspace = directory
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-returnAcquisition
		return environment.Target, func(context.Context) error {
			select {
			case <-allowRelease:
				released++
				return nil
			default:
				return status_error.RuntimeShutdownFailed
			}
		}, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := host.GenerateText(ctx, TextRequest{RuntimeID: driver.id, Messages: []Message{{Role: "user", Content: "input"}}})
		finished <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("resource acquisition did not start")
	}
	closeContext, closeCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer closeCancel()
	if err := host.Close(closeContext); !errors.Is(err, status_error.RuntimeShutdownFailed) {
		t.Fatalf("close discarded an acquisition still returning resources: %v", err)
	}
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("host close did not cancel resource acquisition")
	}
	close(returnAcquisition)
	if err := <-finished; !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("acquisition cancellation=%v", err)
	}
	if released != 0 || driver.promptCalls != 0 {
		t.Fatalf("late acquisition prompted or lost failed cleanup: prompt=%d release=%d", driver.promptCalls, released)
	}
	if _, err := os.Stat(workspace); err != nil {
		t.Fatalf("workspace released before the partial resource: %v", err)
	}
	close(allowRelease)
	if err := host.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if released != 1 {
		t.Fatalf("partial resource released %d times", released)
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("workspace retained after resource cleanup: %v", err)
	}
}

func environmentList(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
}
