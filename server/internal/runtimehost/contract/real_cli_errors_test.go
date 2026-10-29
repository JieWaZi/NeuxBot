package contract

import (
	"encoding/json"
	"os"
	"testing"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// realCLIFailureCapture 是真实 CLI 连接本机故障接口后采集的 ACP 终态。
type realCLIFailureCapture struct {
	// CLI 标识实际执行的 CLI。
	CLI string `json:"cli"`
	// Version 固定采集时的程序版本，升级后应重新采集。
	Version string `json:"version"`
	// Scenario 标识认证、限流或上游故障。
	Scenario string `json:"scenario"`
	// Stage 区分创建会话与模型请求阶段。
	Stage string `json:"stage"`
	// HTTPStatus 是本机服务实际注入的状态。
	HTTPStatus int `json:"httpStatus"`
	// RequestCount 证明真实 CLI 曾到达故障接口。
	RequestCount int `json:"requestCount"`
	// AssistantText 用于确认原始错误没有成为普通正文。
	AssistantText string `json:"assistantText"`
	// Error 保留采集到的协议错误，仅将回环端口归一化。
	Error *acp.RequestError `json:"error"`
	// ExpectedCode 是对用户公开的稳定失败类别。
	ExpectedCode FailureCode `json:"expectedCode"`
}

// TestRealCLIFailureCapturesThroughClassifier 用真实输出回放公开故障分类，不以理想化 errorKind 代替原始结构。
func TestRealCLIFailureCapturesThroughClassifier(t *testing.T) {
	data, err := os.ReadFile("testdata/real_cli_errors.json")
	if err != nil {
		t.Fatal(err)
	}
	var captures []realCLIFailureCapture
	if err := json.Unmarshal(data, &captures); err != nil {
		t.Fatal(err)
	}
	coverage := map[string]int{}
	for _, capture := range captures {
		coverage[capture.CLI]++
		t.Run(capture.CLI+"/"+capture.Scenario, func(t *testing.T) {
			if capture.RequestCount == 0 || capture.Version == "" || capture.Error == nil || capture.AssistantText != "" {
				t.Fatalf("缺少真实失败证据：%+v", capture)
			}
			if code := ClassifyFailure(capture.CLI, capture.Error); code != capture.ExpectedCode {
				t.Fatalf("ClassifyFailure = %s, want %s", code, capture.ExpectedCode)
			}
		})
	}
	for _, cli := range []string{RuntimeIDCodex, RuntimeIDClaude, RuntimeIDCursor, RuntimeIDKimi, RuntimeIDPi} {
		if coverage[cli] != 3 {
			t.Fatalf("%s 的真实错误覆盖不完整：%d", cli, coverage[cli])
		}
	}
}
