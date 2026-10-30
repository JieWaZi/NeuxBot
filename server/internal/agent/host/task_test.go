package runtimehost

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
)

// taskResourceFixture 核验任务资源与短期授权拥有不同释放边界。
type taskResourceFixture struct {
	root     string      // root 是引擎提供的受管工作目录。
	revoked  atomic.Bool // revoked 表示调用授权是否已撤销。
	closed   int         // closed 统计成功释放资源的次数。
	acquired int         // acquired 统计资源物化次数。
}

func (r *taskResourceFixture) Acquire(_ context.Context, request c.ResourceRequest) (c.ResourceLease, error) {
	r.root = request.Workspace.Root
	r.acquired++
	if err := os.Mkdir(filepath.Join(r.root, "ally"), 0700); err != nil {
		return nil, err
	}
	return r, nil
}
func (r *taskResourceFixture) Resources() []c.ResourceBinding {
	return []c.ResourceBinding{{ID: "ally", Path: filepath.Join(r.root, "ally")}}
}
func (r *taskResourceFixture) Close(context.Context) error {
	r.closed++
	return os.RemoveAll(filepath.Join(r.root, "ally"))
}
func (r *taskResourceFixture) Revoke() { r.revoked.Store(true) }
func (r *taskResourceFixture) LaunchEnvironment(base map[string]string) map[string]string {
	base["ALLY_CONTEXT_PATH"] = "task-connection"
	base["PATH"] = "task-bin:" + base["PATH"]
	return base
}

// taskDriverFixture 在原生关闭时验证领域授权已撤销。
type taskDriverFixture struct {
	*driverFixture                      // driverFixture 复用共享宿主的原生边界替身。
	resources      *taskResourceFixture // resources 是本次调用拥有的任务资源。
	t              *testing.T           // t 接收关闭顺序断言。
	openError      error                // openError 模拟创建原生会话后返回的启动故障。
}

func (d *taskDriverFixture) Open(_ context.Context, request c.OpenRequest) (c.DriverSession, error) {
	d.opened = request
	return d, d.openError
}
func (d *taskDriverFixture) Close(ctx context.Context) error {
	if !d.resources.revoked.Load() {
		d.t.Error("native close began before invocation grant revocation")
	}
	return d.driverFixture.Close(ctx)
}

func TestTaskResourcesAndAccountRemainOwnedUntilNativeClose(t *testing.T) {
	resources := &taskResourceFixture{}
	driver := &taskDriverFixture{driverFixture: &driverFixture{id: "custom-codex"}, resources: resources, t: t}
	host, released := hostFixture(t, runtimeconfig.RuntimeIDCodex, driver.driverFixture)
	host.config.Driver = func(Environment, acpdriver.Factory) (c.Driver, error) { return driver, nil }
	result, err := host.ExecuteTask(context.Background(), TaskRequest{TextRequest: TextRequest{RuntimeID: driver.id, AccountID: "account", Messages: []Message{{Role: "user", Content: "maintain"}}}, Contributions: c.Contributions{Instructions: []c.Instruction{{Name: "wiki", Text: "task rules"}}, Skills: []c.Skill{{Name: "ally", ResourceID: "ally"}}}, Resources: resources})
	if err != nil || result.Text != "trusted final" || resources.acquired != 1 || resources.closed != 0 || *released != 0 || !resources.revoked.Load() {
		t.Fatalf("result=%+v err=%v acquired=%d resources=%d account=%d revoked=%v", result, err, resources.acquired, resources.closed, *released, resources.revoked.Load())
	}
	if len(driver.opened.Resources) != 1 || len(driver.opened.Contributions.Instructions) != 1 || driver.opened.Policies.Tools.Mode != c.ToolsAllowAll || driver.opened.Session.Mode != c.SessionNew || driver.opened.Credential.Environment["ALLY_CONTEXT_PATH"] != "task-connection" || driver.opened.Credential.Environment["OPENAI_API_KEY"] != "selected-account" {
		t.Fatalf("open=%+v", driver.opened)
	}
	if _, err := os.Stat(resources.root); err != nil {
		t.Fatalf("live resources removed before native exit: %v", err)
	}
	driver.allowClose = true
	if err := host.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if resources.closed != 1 || *released != 1 || driver.promptCalls != 1 {
		t.Fatalf("close replayed effects: resource=%d account=%d prompt=%d", resources.closed, *released, driver.promptCalls)
	}
	if _, err := os.Stat(resources.root); !os.IsNotExist(err) {
		t.Fatalf("workspace retained after close: %v", err)
	}
}

func TestTaskCancellationRevokesBeforeFailedNativeClose(t *testing.T) {
	resources := &taskResourceFixture{}
	driver := &taskDriverFixture{driverFixture: &driverFixture{id: "custom-codex", entered: make(chan struct{})}, resources: resources, t: t}
	host, released := hostFixture(t, runtimeconfig.RuntimeIDCodex, driver.driverFixture)
	host.config.Driver = func(Environment, acpdriver.Factory) (c.Driver, error) { return driver, nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := host.ExecuteTask(ctx, TaskRequest{TextRequest: TextRequest{RuntimeID: driver.id, Messages: []Message{{Role: "user", Content: "maintain"}}}, Resources: resources})
		done <- err
	}()
	<-driver.entered
	cancel()
	if err := <-done; !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("cancel=%v", err)
	}
	if !resources.revoked.Load() || resources.closed != 0 || *released != 0 {
		t.Fatalf("cancel ownership revoked=%v resource=%d account=%d", resources.revoked.Load(), resources.closed, *released)
	}
	driver.allowClose = true
	if err := host.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if driver.promptCalls != 1 || resources.acquired != 1 || resources.closed != 1 || *released != 1 {
		t.Fatal("cleanup repeated task or released leases twice")
	}
}

// TestCredentialCleanupDoesNotRepeatSuccessfulAccountRelease 核验目录回收失败不重复释放已成功归还的账号租约。
func TestCredentialCleanupDoesNotRepeatSuccessfulAccountRelease(t *testing.T) {
	released := 0
	workspace := string([]byte{0})
	resources := &nativeResources{snapshot: runtimecontract.SessionResources{Close: func(context.Context) error { released++; return nil }}, releaseWorkspace: func(context.Context) error { return os.RemoveAll(workspace) }}
	if err := resources.Close(context.Background()); err == nil {
		t.Fatal("invalid directory unexpectedly removed")
	}
	workspace = t.TempDir()
	if err := resources.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if released != 1 {
		t.Fatalf("successful account release repeated: %d", released)
	}
}

func TestTaskOpenFailureRevokesAndRetainsReturnedSession(t *testing.T) {
	resources := &taskResourceFixture{}
	driver := &taskDriverFixture{driverFixture: &driverFixture{id: "custom-codex"}, resources: resources, t: t, openError: status_error.RuntimeProtocolIncompatible}
	host, released := hostFixture(t, runtimeconfig.RuntimeIDCodex, driver.driverFixture)
	host.config.Driver = func(Environment, acpdriver.Factory) (c.Driver, error) { return driver, nil }
	_, err := host.ExecuteTask(context.Background(), TaskRequest{TextRequest: TextRequest{RuntimeID: driver.id, Messages: []Message{{Role: "user", Content: "maintain"}}}, Resources: resources})
	if !errors.Is(err, status_error.RuntimeProtocolIncompatible) || !resources.revoked.Load() || resources.closed != 0 || *released != 0 || driver.promptCalls != 0 {
		t.Fatalf("open failure lost ownership: err=%v revoked=%v resource=%d account=%d prompt=%d", err, resources.revoked.Load(), resources.closed, *released, driver.promptCalls)
	}
	driver.allowClose = true
	if err := host.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if resources.closed != 1 || *released != 1 || driver.promptCalls != 0 {
		t.Fatal("open failure cleanup replayed invocation")
	}
}

// nativeTaskResources 在资源物化后才能提供本次调用的真实连接路径与 CLI 路径。
type nativeTaskResources struct {
	// taskResourceFixture 复用任务授权与文件租约生命周期。
	*taskResourceFixture
}

func (r *nativeTaskResources) LaunchEnvironment(base map[string]string) map[string]string {
	base[constants.AgentCLIContextEnvironment] = filepath.Join(r.root, "ally", "connection.json")
	base[constants.AgentCLIBinaryEnvironment] = filepath.Join(r.root, "ally", "allycli")
	base["PATH"] = filepath.Join(r.root, "ally") + string(os.PathListSeparator) + base["PATH"]
	return base
}

// TestTaskAuthorizedEnvironmentReachesNativeProcess 覆盖真实工厂边界，防止启动器丢失资源物化后加入的环境。
func TestTaskAuthorizedEnvironmentReachesNativeProcess(t *testing.T) {
	directory := t.TempDir()
	capture := filepath.Join(directory, "environment")
	cli := filepath.Join(directory, "native-fixture")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n%%s\\n%%s\\n%%s\\n%%s\\n' \"$*\" \"${%s-absent}\" \"${%s-absent}\" \"${PATH-absent}\" \"${OPENAI_API_KEY-absent}\" > %q\n", constants.AgentCLIContextEnvironment, constants.AgentCLIBinaryEnvironment, capture)
	script += `while IFS= read -r request; do
 request_id=$(printf '%s' "$request" | /usr/bin/sed -n 's/.*"id":\([^,}]*\).*/\1/p')
 if [ -n "$request_id" ]; then
  printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"controlled initialization rejection"}}\n' "$request_id"
 fi
done
`
	if err := os.WriteFile(cli, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	base := []string{"PATH=account-bin", "OPENAI_API_KEY=selected-account"}
	host := New(Config{
		Resolve: func(context.Context, TextRequest) (Environment, error) {
			return Environment{BaseRuntimeID: runtimeconfig.RuntimeIDQoder}, nil
		},
		Prepare: func(context.Context, TextRequest, Environment, string) (runtimeconfig.RuntimeTarget, func(context.Context) error, error) {
			return runtimeconfig.RuntimeTarget{Isolated: true}, nil, nil
		},
		Launch: func(runtimeconfig.RuntimeTarget, runtimeconfig.DetectionResult) (runtimeconfig.LaunchFacts, error) {
			return runtimeconfig.LaunchFacts{BaseRuntimeID: runtimeconfig.RuntimeIDQoder, Executable: cli, PrefixArgs: []string{"--fixture"}, Environment: base}, nil
		},
	})
	defer host.Close(context.Background())
	resources := &nativeTaskResources{taskResourceFixture: &taskResourceFixture{}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := host.ExecuteTask(ctx, TaskRequest{TextRequest: TextRequest{RuntimeID: runtimeconfig.RuntimeIDQoder, Messages: []Message{{Role: "user", Content: "inspect task environment"}}}, Contributions: c.Contributions{Skills: []c.Skill{{Name: "ally", ResourceID: "ally"}}}, Resources: resources})
	if err == nil {
		t.Fatal("只捕获启动配置的受控进程不应产生模型结果")
	}
	data, readErr := os.ReadFile(capture)
	if readErr != nil {
		t.Fatal(readErr)
	}
	want := []string{"--fixture --acp", filepath.Join(resources.root, "ally", "connection.json"), filepath.Join(resources.root, "ally", "allycli"), filepath.Join(resources.root, "ally") + string(os.PathListSeparator) + "account-bin", "selected-account"}
	if got := strings.Split(strings.TrimSpace(string(data)), "\n"); !reflect.DeepEqual(got, want) {
		t.Fatalf("原生消费者未收到实际任务环境: got=%q want=%q", got, want)
	}
	if !reflect.DeepEqual(base, []string{"PATH=account-bin", "OPENAI_API_KEY=selected-account"}) {
		t.Fatal("任务环境反写了目录提供的基础启动快照")
	}
	if !resources.revoked.Load() || resources.closed != 1 {
		t.Fatalf("受控进程结束后未回收任务资源: revoked=%v closed=%d", resources.revoked.Load(), resources.closed)
	}
}
