package aiaccount

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// capabilityDetector 记录能力查询是否在租约释放后才启动原生探测。
type capabilityDetector struct {
	// called 通知测试原生探测已实际开始。
	called chan struct{}
}

// retainedCapabilityDetector 把真实准备出的释放端口交回测试，模拟仍由探测消费者持有的资源。
type retainedCapabilityDetector struct {
	// request 保存实际账号工作区及可重试释放端口。
	request runtimeconfig.DetectRequest
}

func (d *retainedCapabilityDetector) Detect(_ context.Context, request runtimeconfig.DetectRequest) runtimeconfig.DetectionResult {
	d.request = request
	return runtimeconfig.DetectionResult{State: runtimeconfig.DetectionStateReady}
}

func TestCapabilityReleaseFailureRetainsAccountAndWorkspace(t *testing.T) {
	service, account, _ := subscriptionFixture(t)
	detector := &retainedCapabilityDetector{}
	_, err := service.DetectModel(context.Background(), detector, account.ID, models.RuntimeEnvironment{RuntimeID: constants.RuntimeIDCodex, BaseRuntimeID: constants.RuntimeIDCodex}, "fixture-model")
	if err != nil {
		t.Fatal(err)
	}
	workspace := detector.request.WorkingDirectory
	defer os.RemoveAll(workspace)
	path := filepath.Join(accountconfig.Home(workspace, constants.RuntimeIDCodex), "auth.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("invalid-native-auth"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := detector.request.ReleaseResources(context.Background()); !errors.Is(err, status_error.AIAccountCredentialError) {
		t.Fatalf("探测回收未报告认证失败: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("认证尚未回收就删除了工作区: %v", err)
	}
	if release, err := service.Exclusive(account.ID); !errors.Is(err, status_error.AIAccountBusy) {
		if release != nil {
			release()
		}
		t.Fatalf("失败的探测回收释放了账号所有权: %v", err)
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := detector.request.ReleaseResources(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("成功回收后仍保留探测目录: %v", err)
	}
	release, err := service.Exclusive(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func (d capabilityDetector) Detect(_ context.Context, request runtimeconfig.DetectRequest) runtimeconfig.DetectionResult {
	if request.ReleaseResources != nil {
		defer request.ReleaseResources(context.Background())
	}
	close(d.called)
	return runtimeconfig.DetectionResult{State: runtimeconfig.DetectionStateReady}
}

func TestCapabilityQueryWaitsForAccountMutationAndCanBeCancelled(t *testing.T) {
	for _, cancelQuery := range []bool{false, true} {
		t.Run(map[bool]string{false: "release", true: "cancel"}[cancelQuery], func(t *testing.T) {
			service, account, _ := subscriptionFixture(t)
			release, err := service.Exclusive(account.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			detector := capabilityDetector{called: make(chan struct{})}
			finished := make(chan error, 1)
			go func() {
				_, err := service.DetectModel(ctx, detector, account.ID, models.RuntimeEnvironment{RuntimeID: constants.RuntimeIDCodex, BaseRuntimeID: constants.RuntimeIDCodex}, "fixture-model")
				finished <- err
			}()
			select {
			case err := <-finished:
				t.Fatalf("read-only capability query failed instead of waiting: %v", err)
			case <-detector.called:
				t.Fatal("query bypassed active account mutation")
			case <-time.After(30 * time.Millisecond):
			}
			if cancelQuery {
				cancel()
			} else {
				release()
			}
			select {
			case err := <-finished:
				if cancelQuery && !errors.Is(err, context.Canceled) || !cancelQuery && err != nil {
					t.Fatalf("unexpected query outcome: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("query did not finish after release or cancellation")
			}
			if cancelQuery {
				if _, err := service.Exclusive(account.ID); !errors.Is(err, status_error.AIAccountBusy) {
					t.Fatal("cancelled query released another execution's lease")
				}
			}
		})
	}
}
