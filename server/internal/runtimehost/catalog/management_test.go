package catalog

import (
	"context"
	"errors"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
	acp "github.com/JieWaZi/acp-go/sdk"
	"os"
	"testing"
	"time"
)

// TestCloseWaitsForAcceptedManagementPreparation 防止尚在工厂中的管理操作丢失资源所有权。
func TestCloseWaitsForAcceptedManagementPreparation(t *testing.T) {
	backend := newACPBackend()
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	operation, cancelOperation := context.WithTimeout(context.Background(), time.Second)
	defer cancelOperation()
	directory := t.TempDir()
	replaceACPAgentFactory(backend, runtimecontract.RuntimeIDPi, func(context.Context, acpAdapterConfig) (acp.Agent, error) {
		close(started)
		select {
		case <-release:
		case <-operation.Done():
			return nil, operation.Err()
		}
		return testCodexACPAgentWithCatalog(), nil
	})
	go func() {
		_, err := backend.DetectOptions(operation, acpOptionsRequest{RuntimeID: runtimecontract.RuntimeIDPi, WorkingDirectory: directory, Prepared: preparedRuntime{definition: runtimeDefinition{baseRuntimeID: runtimecontract.RuntimeIDPi}}})
		done <- err
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("management ended before factory: %v", err)
	case <-operation.Done():
		t.Fatal("management did not reach factory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := backend.Close(ctx)
	close(release)
	select {
	case <-done:
	case <-operation.Done():
		t.Fatal("management did not return after factory release")
	}
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close must retain an accepted preparation until its owner returns: %v", err)
	}
	if err := backend.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// closeRetryAgent 模拟原生使用者首次关闭未确认、第二次才交还依赖。
type closeRetryAgent struct {
	*fakeCodexACPAgent     // fakeCodexACPAgent 提供无模型输入的标准管理协议。
	closeCalls         int // closeCalls 记录原生关闭确认次数。
}

func (agent *closeRetryAgent) Close(context.Context) error {
	agent.closeCalls++
	if agent.closeCalls == 1 {
		return context.DeadlineExceeded
	}
	return agent.fakeCodexACPAgent.Close(context.Background())
}

// TestDetectionKeepsResourcesUntilCloseRetry 证明关闭失败不会释放账号依赖或探测目录。
func TestDetectionKeepsResourcesUntilCloseRetry(t *testing.T) {
	registry, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	agent := &closeRetryAgent{fakeCodexACPAgent: testCodexACPAgentWithCatalog()}
	directory := ""
	released := 0
	replaceACPAgentFactory(registry.acp.(*acpBackend), runtimecontract.RuntimeIDPi, func(_ context.Context, config acpAdapterConfig) (acp.Agent, error) {
		directory = config.WorkingDirectory
		return agent, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := registry.Detect(ctx, runtimecontract.DetectRequest{RuntimeID: runtimecontract.RuntimeIDPi, BaseEnvironment: os.Environ(), LaunchOverride: runtimecontract.LaunchOverride{Binary: os.Args[0]}, ReleaseResources: func() { released++ }})
	if !result.Capabilities.Has(runtimecontract.CapabilityPartialMessages) || released != 0 || agent.closeCalls != 1 {
		t.Fatalf("unconfirmed close released dependencies: result=%+v released=%d close=%d", result, released, agent.closeCalls)
	}
	if _, err := os.Stat(directory); err != nil {
		t.Fatalf("probe directory disappeared before close confirmation: %v", err)
	}
	if err := registry.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if released != 1 || agent.closeCalls != 2 {
		t.Fatalf("close retry did not release exactly once: release=%d close=%d", released, agent.closeCalls)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("confirmed close retained probe directory: %v", err)
	}
	if err := registry.Close(ctx); err != nil || released != 1 {
		t.Fatalf("repeated close changed released resources: err=%v release=%d", err, released)
	}
}

// TestDetectionReleasesBeforeManagementFailure 证明未交接管理连接的拒绝路径也释放调用者资源。
func TestDetectionReleasesBeforeManagementFailure(t *testing.T) {
	registry, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	released := 0
	registry.Detect(context.Background(), runtimecontract.DetectRequest{RuntimeID: "unregistered", ReleaseResources: func() { released++ }})
	if released != 1 {
		t.Fatalf("rejected discovery retained caller resources: %d", released)
	}
}
