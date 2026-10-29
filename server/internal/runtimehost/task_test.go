package runtimehost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
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
	host, released := hostFixture(t, runtimecontract.RuntimeIDCodex, driver.driverFixture)
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
	host, released := hostFixture(t, runtimecontract.RuntimeIDCodex, driver.driverFixture)
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
	provider := &credentialProvider{workspace: string([]byte{0}), closeLease: func(context.Context) error { released++; return nil }}
	if err := provider.Close(context.Background()); err == nil {
		t.Fatal("invalid directory unexpectedly removed")
	}
	provider.workspace = t.TempDir()
	if err := provider.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if released != 1 {
		t.Fatalf("successful account release repeated: %d", released)
	}
}

func TestTaskOpenFailureRevokesAndRetainsReturnedSession(t *testing.T) {
	resources := &taskResourceFixture{}
	driver := &taskDriverFixture{driverFixture: &driverFixture{id: "custom-codex"}, resources: resources, t: t, openError: status_error.RuntimeProtocolIncompatible}
	host, released := hostFixture(t, runtimecontract.RuntimeIDCodex, driver.driverFixture)
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
