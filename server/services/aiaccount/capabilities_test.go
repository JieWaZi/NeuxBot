package aiaccount

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// capabilityDetector 记录能力查询是否在租约释放后才启动原生探测。
type capabilityDetector struct {
	// called 通知测试原生探测已实际开始。
	called chan struct{}
}

func (d capabilityDetector) Detect(_ context.Context, request runtimecontract.DetectRequest) runtimecontract.DetectionResult {
	if request.ReleaseResources != nil {
		defer request.ReleaseResources()
	}
	close(d.called)
	return runtimecontract.DetectionResult{State: runtimecontract.DetectionStateReady}
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
