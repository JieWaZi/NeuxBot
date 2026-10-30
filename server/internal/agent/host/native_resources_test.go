package runtimehost

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/agent/environment/catalog"
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"
	ginxlogx "github.com/shrewx/ginx/pkg/logx"
	"github.com/sirupsen/logrus"
)

// hostTestLogx 在任何后台输出开始前取得实例，避免并发绕过项目的入口锁。
var hostTestLogx = ginxlogx.Instance()

// hostLogCapture 接收后台原生诊断及当前断言的并发输出。
type hostLogCapture struct {
	// mutex 保护日志缓冲区。
	mutex sync.Mutex
	// buffer 保存统一输出后的诊断文本。
	buffer bytes.Buffer
}

func (capture *hostLogCapture) Write(data []byte) (int, error) {
	capture.mutex.Lock()
	defer capture.mutex.Unlock()
	return capture.buffer.Write(data)
}

func (capture *hostLogCapture) text() string {
	capture.mutex.Lock()
	defer capture.mutex.Unlock()
	return capture.buffer.String()
}

// TestHostNativeLoggerAlwaysReachesLogx 防止宿主受到任意全局 slog 默认输出替换的影响。
func TestHostNativeLoggerAlwaysReachesLogx(t *testing.T) {
	registry, err := catalog.New(catalog.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close(context.Background()) })
	output, level := hostTestLogx.Out, hostTestLogx.GetLevel()
	capture := &hostLogCapture{}
	hostTestLogx.SetOutput(capture)
	hostTestLogx.SetLevel(logrus.WarnLevel)
	t.Cleanup(func() { hostTestLogx.SetOutput(output); hostTestLogx.SetLevel(level) })
	previousDefault := slog.Default()
	unrelated := &hostLogCapture{}
	slog.SetDefault(slog.New(slog.NewTextHandler(unrelated, nil)))
	t.Cleanup(func() { slog.SetDefault(previousDefault) })
	config := hostNativeConfig(runtimeconfig.RuntimeTarget{}, runtimeconfig.LaunchFacts{}, "", "", runtimeconfig.PermissionLevelDefault)
	config.Logger.Warn("host-native-logx-sentinel")
	if strings.Count(capture.text(), "host-native-logx-sentinel") != 1 || strings.Contains(unrelated.text(), "host-native-logx-sentinel") {
		t.Fatalf("native diagnostics escaped logx: logx=%q unrelated=%q", capture.text(), unrelated.text())
	}
}
