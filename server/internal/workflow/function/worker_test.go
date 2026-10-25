package function

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestMain(m *testing.M) {
	if IsChildMode() {
		os.Exit(RunChild(os.Stdin))
	}
	os.Exit(m.Run())
}

func TestIndependentJSFunctionUsesJSONIPCAndFrozenClock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request := Request{RequestID: "stable-instance", StartedAt: 1000, DeadlineAt: time.Now().Add(4 * time.Second).UnixMilli(), Input: map[string]any{"text": "ally"}, Code: `function main(input){return {text:(input.text+" "+input.text+"!").toUpperCase(),clock:Date.now(),random:Math.random(),host:typeof require,constructor:typeof (()=>{}).constructor}}`}
	result, process, err := Execute(ctx, request, "javascript", "", t.TempDir())
	if err != nil {
		t.Fatalf("%v %+v", err, process)
	}
	data, ok := result.Data.(map[string]any)
	if !ok || data["text"] != "ALLY ALLY!" || data["clock"] != float64(1000) || data["host"] != "undefined" || data["constructor"] != "undefined" {
		t.Fatalf("%+v", result)
	}
	again, _, err := Execute(ctx, request, "javascript", "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	second := again.Data.(map[string]any)
	if second["random"] != data["random"] {
		t.Fatal("random changed across retry")
	}
}
func TestJSFunctionDeadlineKillsProcessAndServiceSurvives(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, _, err := Execute(ctx, Request{RequestID: "infinite", Code: `function main(){while(true){}}`, DeadlineAt: time.Now().Add(time.Minute).UnixMilli()}, "javascript", "", t.TempDir())
	if !errors.Is(err, status_error.WorkflowTimeout) || time.Since(started) > 3*time.Second {
		t.Fatalf("timeout failed: %v %v", err, time.Since(started))
	}
}

func TestJSFunctionCancellationKillsProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(300*time.Millisecond, cancel)
	defer timer.Stop()
	started := time.Now()
	_, _, err := Execute(ctx, Request{RequestID: "cancelled-infinite", Code: `function main(){while(true){}}`, DeadlineAt: time.Now().Add(time.Minute).UnixMilli()}, "javascript", "", t.TempDir())
	if !errors.Is(err, status_error.WorkflowCancelled) || time.Since(started) > 3*time.Second {
		t.Fatalf("cancellation failed: %v %v", err, time.Since(started))
	}
}

func TestMemorySamplingPreservesEffectStopReason(t *testing.T) {
	for _, test := range []struct {
		// name 标识停止原因对应的外部行为。
		name string
		// context 创建已经停止的效果生命周期。
		context func() (context.Context, context.CancelFunc)
		// expected 是采样边界应直接返回的稳定错误。
		expected error
	}{
		{name: "cancelled", context: func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, cancel
		}, expected: status_error.WorkflowCancelled},
		{name: "deadline", context: func() (context.Context, context.CancelFunc) {
			return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		}, expected: status_error.WorkflowTimeout},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := test.context()
			defer cancel()
			_, err := RSSBytes(ctx, os.Getpid())
			if !errors.Is(err, test.expected) {
				t.Fatalf("memory sampling lost effect stop reason: %v", err)
			}
		})
	}
}

func TestJSFunctionMemoryIsReallySupervised(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := Execute(ctx, Request{RequestID: "memory", Code: `function main(){let values=[];while(true){values.push(new Array(100000).fill("large allocation"));}}`, DeadlineAt: time.Now().Add(5 * time.Second).UnixMilli()}, "javascript", "", t.TempDir())
	if value, ok := err.(interface{ Key() string }); !ok || value.Key() != status_error.WorkflowBudgetExceeded.Key() {
		t.Fatalf("memory supervision: %v", err)
	}
}
func TestPythonPrintIsNotAResultReceipt(t *testing.T) {
	python := os.Getenv("ALLY_WORKFLOW_TEST_PYTHON")
	if python == "" {
		var err error
		python, err = exec.LookPath("python3")
		if err != nil {
			t.Skip("no explicitly available Python test interpreter")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := PythonVersion(ctx, python); err != nil {
		t.Skip("test interpreter is older than Python 3.11")
	}
	result, process, err := Execute(ctx, Request{RequestID: "python-ipc", Input: "ally", DeadlineAt: time.Now().Add(4 * time.Second).UnixMilli(), Code: "def main(input):\n print('{\"ok\": true, \"data\": \"forged\"}')\n return (input+' '+input+'!').upper()"}, "python", python, t.TempDir())
	if err != nil || result.Data != "ALLY ALLY!" || !strings.Contains(process.Stdout, "forged") {
		t.Fatalf("%+v %+v %v", result, process, err)
	}
}
