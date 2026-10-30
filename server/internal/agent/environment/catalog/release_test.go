package catalog

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// TestDetectRetainsFailedEarlyRelease 验证原生启动前失败也保留未能释放的账号租约。
func TestDetectRetainsFailedEarlyRelease(t *testing.T) {
	catalog, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	attempts := 0
	result := catalog.Detect(context.Background(), runtimeconfig.DetectRequest{RuntimeID: "missing", ReleaseResources: func(context.Context) error {
		attempts++
		if attempts == 1 {
			return status_error.RuntimeShutdownFailed
		}
		return nil
	}})
	if result.State != runtimeconfig.DetectionStateUnavailable || attempts != 1 {
		t.Fatalf("state=%s release attempts=%d", result.State, attempts)
	}
	if err := catalog.Close(context.Background()); err != nil || attempts != 2 {
		t.Fatalf("retry error=%v release attempts=%d", err, attempts)
	}
}
