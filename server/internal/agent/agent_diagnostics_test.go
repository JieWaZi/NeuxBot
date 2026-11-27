package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acp "github.com/JieWaZi/acp-go-sdk"
	ginxlogx "github.com/shrewx/ginx/pkg/logx"
	"go.opentelemetry.io/otel/trace"
)

func TestNativePreparationDiagnosticsUseActualOperationAndTrace(t *testing.T) {
	logs, ctx := nativePreparationTestLog(t)
	client := &directAgentClient{
		native:       preparationDiagnosticClient{failingNativeSessionClient{err: io.EOF}},
		capabilities: AgentCapabilities{Resume: true, Load: true},
	}
	_, newErr := client.NewSession(ctx, AgentSessionOptions{})
	_, resumeErr := client.ResumeSession(ctx, "old-session", AgentSessionOptions{})
	_, loadErr := client.LoadSession(ctx, "old-session", AgentSessionOptions{})
	_, modelErr := client.SetSessionOption(ctx, "old-session", "model", AgentOptionValue{ID: "private-model"})
	_, settingErr := client.SetSessionOption(ctx, "old-session", "private-option", AgentOptionValue{ID: "private-value"})
	modeErr := client.SetSessionMode(ctx, "old-session", "private-mode")
	for _, operation := range []struct {
		// name is the emitted adapter operation.
		name string
		// got is the error returned to the existing caller.
		got error
		// want is the unchanged product status for this operation.
		want status_error.StatusError
	}{
		{"initialize", client.Initialize(ctx), status_error.RuntimeUnavailable},
		{"session/new", newErr, status_error.RuntimeSessionStartFailed},
		{"session/resume", resumeErr, status_error.RuntimeSessionRestoreFailed},
		{"session/load", loadErr, status_error.RuntimeSessionRestoreFailed},
		{"session/set_model", modelErr, status_error.RuntimeModelSelectionFailed},
		{"session/set_config_option", settingErr, status_error.RuntimeConfigurationInvalid},
		{"session/set_mode", modeErr, status_error.RuntimeConfigurationInvalid},
	} {
		if !errors.Is(operation.got, operation.want) {
			t.Fatalf("%s returned %v, want %v", operation.name, operation.got, operation.want)
		}
		var matches int
		for _, line := range strings.Split(logs.String(), "\n") {
			if strings.Contains(line, "native agent operation failed") && strings.Contains(line, "operation="+operation.name+" ") &&
				strings.Contains(line, "trace_id=01020304050607080910111213141516") &&
				strings.Contains(line, "error_code="+operation.want.Key()) && strings.Contains(line, "reason=eof") {
				matches++
			}
		}
		if matches != 1 {
			t.Errorf("expected one %s diagnostic, found %d in %q", operation.name, matches, logs.String())
		}
	}
	if strings.Contains(logs.String(), "private-") || strings.Contains(logs.String(), "old-session") {
		t.Fatal("logged native operation inputs")
	}
	logs.Reset()
	client.native = preparationDiagnosticClient{}
	if err := client.Initialize(ctx); err != nil || logs.Len() != 0 {
		t.Fatalf("successful native preparation emitted a failure: %v, %q", err, logs.String())
	}
}

func TestNativePreparationDiagnosticsRetainOnlySafeCauses(t *testing.T) {
	logs, ctx := nativePreparationTestLog(t)
	for _, failure := range []struct {
		// name identifies the kind of upstream failure.
		name string
		// err is returned before product error mapping.
		err error
		// contains lists the expected safe diagnostic fields.
		contains []string
		// omitted lists strings the diagnostic must not include.
		omitted []string
	}{
		{"safe reason", &acp.RequestError{Code: -32603, Message: "ACP agent did not verify restored history", Data: map[string]any{"secret": "never-record-data"}},
			[]string{`native_rpc_code=-32603`, `detail="ACP agent did not verify restored history"`}, []string{"never-record-data"}},
		{"unclassified short sentence", &acp.RequestError{Code: -32603, Message: "Meet Lily in Boston tomorrow"},
			[]string{`native_rpc_code=-32603`, `reason=native_detail_withheld`}, []string{"Meet Lily in Boston tomorrow", `detail=`}},
		{"known kind", &acp.RequestError{Code: -32603, Message: "ACP agent request failed", Data: map[string]any{"classification": "quota_exhausted"}},
			[]string{`native_kind=quota_exhausted`, `reason=quota_exhausted`}, []string{`detail=`}},
		{"missing detail", &acp.RequestError{Code: -32603, Message: "Internal error"},
			[]string{`reason=native_detail_unavailable`}, []string{`detail=`}},
		{"credential", &acp.RequestError{Code: -32603, Message: "Authorization: Bearer credential-fixture"},
			[]string{`native_rpc_code=-32603`}, []string{"credential-fixture", `detail=`}},
		{"multiline", &acp.RequestError{Code: -32603, Message: "Cannot open the session directory\nunsafe echoed sentence"},
			[]string{`native_rpc_code=-32603`}, []string{"unsafe echoed", `detail=`}},
		{"structured", &acp.RequestError{Code: -32603, Message: `{"echo":"private body"}`},
			[]string{`native_rpc_code=-32603`}, []string{"private body", `detail=`}},
		{"prompt", &acp.RequestError{Code: -32603, Message: "Invalid developer prompt private instructions"},
			[]string{`native_rpc_code=-32603`}, []string{"private instructions", `detail=`}},
		{"oversize", &acp.RequestError{Code: -32603, Message: strings.Repeat("private line ", 100)},
			[]string{`native_rpc_code=-32603`}, []string{"private line", `detail=`}},
		{"process exited", errors.New("ACP agent CLI exited with code 17"),
			[]string{`reason=process_exited`, `process_exit_code=17`}, []string{`detail=`, `native_rpc_code=`}},
		{"deadline", context.DeadlineExceeded,
			[]string{`reason=deadline_exceeded`, `error_code=OperationTimeout`}, []string{`detail=`, `native_rpc_code=`}},
		{"codex busy", errors.New("Codex session already has an operation in progress"),
			[]string{`reason=native_busy`, `error_code=RuntimeSessionRestoreFailed`}, []string{`detail=`, `Codex session already`}},
		{"typed process exit", nativePreparationTestExit(t),
			[]string{`reason=process_exited`, `process_exit_code=23`}, []string{"process wrote a private record", `detail=`}},
		{"unclassified transport", errors.New("unstructured private stderr"),
			[]string{`reason=native_detail_unavailable`}, []string{"private stderr", `detail=`, `native_rpc_code=`}},
	} {
		t.Run(failure.name, func(t *testing.T) {
			logs.Reset()
			client := &directAgentClient{
				native:       preparationDiagnosticClient{failingNativeSessionClient{err: failure.err}},
				capabilities: AgentCapabilities{Resume: true},
			}
			if _, err := client.ResumeSession(ctx, "old-session", AgentSessionOptions{}); err == nil {
				t.Fatal("native failure was lost")
			}
			for _, field := range failure.contains {
				if !strings.Contains(logs.String(), field) {
					t.Errorf("missing %q in %q", field, logs.String())
				}
			}
			for _, field := range failure.omitted {
				if strings.Contains(logs.String(), field) {
					t.Errorf("unexpected %q in %q", field, logs.String())
				}
			}
		})
	}
}

func TestNativePreparationDiagnosticsKeepCodexModelRefusal(t *testing.T) {
	logs, ctx := nativePreparationTestLog(t)
	request := providerTestRequest(t, "codex")
	runtime := NewProviderRuntime(ProviderRuntimeOptions{AcquireAccount: providerTestAccount})
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	conn, err := runtime.Open(ctx, request, ProviderConnectionPorts{})
	if err != nil {
		t.Fatal(err)
	}
	client, err := conn.Client()
	if err != nil {
		t.Fatal(err)
	}
	codex := client.(*directAgentClient)
	session, err := codex.ForkSession(ctx, AgentForkRequest{
		SourceSessionID: "fixture-source", SourceCWD: request.Account.CWD,
		Position: "exact-turn", ReceiptDirectory: filepath.Join(request.Account.CWD, "fork-receipt"),
	})
	if err != nil || logs.Len() != 0 {
		t.Fatalf("successful native session = %+v, error = %v, logs = %q", session, err, logs.String())
	}
	_, err = codex.ConfigureCodexTurn(ctx, session.ID, "private-requested-model", "", nil)
	if !errors.Is(err, status_error.RuntimeConfigurationInvalid) ||
		!strings.Contains(logs.String(), "operation=session/configure_codex_turn") ||
		!strings.Contains(logs.String(), "native_rpc_code=-32602") ||
		!strings.Contains(logs.String(), "reason=model_unavailable") ||
		strings.Contains(logs.String(), "private-requested-model") {
		t.Fatalf("model rejection changed or did not record safe evidence: %v, %q", err, logs.String())
	}
}

func TestNativePreparationDiagnosticsDoNotInferCauseFromFallback(t *testing.T) {
	logs, ctx := nativePreparationTestLog(t)
	native := &acp.RequestError{Code: -32603, Message: "Internal error"}
	mapped := nativePreparationError(ctx, "provider/credential_proof", native, status_error.RuntimeAuthRequired)
	if !errors.Is(mapped, status_error.RuntimeAuthRequired) ||
		!strings.Contains(logs.String(), "error_code=RuntimeAuthRequired") ||
		!strings.Contains(logs.String(), "native_rpc_code=-32603") ||
		!strings.Contains(logs.String(), "reason=native_detail_unavailable") {
		t.Fatalf("provider fallback was mislabeled as an actual denial: %v, %q", mapped, logs.String())
	}
}

func TestNativePreparationDiagnosticExitChild(t *testing.T) {
	if os.Getenv("NEUX_TEST_NATIVE_PREPARATION_EXIT") != "1" {
		return
	}
	fmt.Fprintln(os.Stderr, "process wrote a private record")
	os.Exit(23)
}

func nativePreparationTestExit(t *testing.T) error {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestNativePreparationDiagnosticExitChild$")
	cmd.Env = append(os.Environ(), "NEUX_TEST_NATIVE_PREPARATION_EXIT=1")
	_, err = cmd.Output()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 || !strings.Contains(string(exit.Stderr), "process wrote a private record") {
		t.Fatalf("fixture did not return an exit with stderr: %v", err)
	}
	return fmt.Errorf("child operation failed: %w", exit)
}

func nativePreparationTestLog(t *testing.T) (*bytes.Buffer, context.Context) {
	t.Helper()
	var logs bytes.Buffer
	logger := ginxlogx.Instance()
	previous := logger.Out
	logger.SetOutput(&logs)
	t.Cleanup(func() { logger.SetOutput(previous) })
	traceID, _ := trace.TraceIDFromHex("01020304050607080910111213141516")
	span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: [8]byte{1}})
	return &logs, trace.ContextWithSpanContext(t.Context(), span)
}

type preparationDiagnosticClient struct {
	// failingNativeSessionClient supplies the native session failures and embeds unused operations.
	failingNativeSessionClient
}

func (c preparationDiagnosticClient) Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersionNumber}, c.err
}

func (c preparationDiagnosticClient) SetSessionConfigOption(context.Context, acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	return acp.SetSessionConfigOptionResponse{}, c.err
}

func (c preparationDiagnosticClient) SetSessionMode(context.Context, acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, c.err
}
