package runtimes

import (
	"strings"

	"context"
	"errors"
	"fmt"
	"os"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

// Run 执行一次完整 Runtime 调用，并在进程和事件交付收尾后返回唯一终态。
//
// Sink 收到的第一个事件必定是 invocation_started；该事件只会在首条用户输入
// 已经完整交付给 Runtime 后产生。此前的全部失败都只通过 Result 返回。
func (sdk *SDK) Run(
	ctx context.Context,
	request RunRequest,
	sink EventSink,
) Result {
	if ctx == nil {
		return failedRunResult(
			FailureCodeInvalidRequest,
			"validate",
			"context is required",
		)
	}
	if sink == nil {
		return failedRunResult(
			FailureCodeInvalidRequest,
			"validate",
			"event sink is required",
		)
	}
	if err := ctx.Err(); err != nil {
		return failedRunError("validate", runtimeContextStatus(err))
	}
	if strings.TrimSpace(request.Target.RuntimeID) == "" ||
		strings.TrimSpace(request.Message.Text) == "" ||
		request.Session.Mode == "" ||
		strings.TrimSpace(request.Workspace.Path) == "" {
		return failedRunResult(
			FailureCodeInvalidRequest,
			"validate",
			"runtime, workspace, message, and session mode are required",
		)
	}
	if err := validateExecutionPolicy(request.Policy.Execution); err != nil {
		return failedRunError("validate", err)
	}
	if request.Session.Mode != SessionModeNew &&
		request.Session.Mode != SessionModeResume {
		return failedRunResult(
			FailureCodeInvalidRequest,
			"validate",
			"session mode is invalid",
		)
	}
	if request.Session.Mode == SessionModeResume && request.Session.ID == "" {
		return failedRunError("validate", status_error.RuntimeSessionNotFound)
	}
	prepared, err := sdk.prepareFromDetection(
		prepareRequestForRun(sdk, request),
		request.Detection,
	)
	if err != nil {
		return failedRunError(constants.RuntimeRunFailureStagePrepare, err)
	}
	workspace, err := validateWorkspace(request.Workspace)
	if err != nil {
		return failedRunError(constants.RuntimeRunFailureStageValidate, err)
	}
	request.Workspace = workspace
	detection := prepared.detectionResult()
	if sdk.acp == nil {
		return failedRunError(constants.RuntimeRunFailureStagePrepare, status_error.RuntimeUnavailable)
	}
	runtimeFiles, err := sdk.prepareRuntimeFiles(
		detection,
		request.Workspace,
		request.Instructions,
		request.Skills,
	)
	if err != nil {
		return failedRunError(constants.RuntimeRunFailureStageRuntimeFiles, err)
	}
	prompt := compileRunPrompt(request)
	if request.Session.Mode == SessionModeResume {
		prompt = compileCurrentMessage(request.Message)
	}
	return sdk.acp.Run(ctx, acpRunRequest{
		InvocationID: util.New(),
		Prepared:     prepared,
		Request:      request,
		Prompt:       prompt,
		RuntimeFiles: runtimeFiles,
		Sink:         publicEventSink{sink: sink},
	})
}

func cloneConnectors(connectors []Connector) []Connector {
	result := make([]Connector, 0, len(connectors))
	for _, connector := range connectors {
		clone := connector
		clone.Arguments = append([]string(nil), connector.Arguments...)
		clone.Environment = cloneStringMap(connector.Environment)
		clone.Headers = cloneStringMap(connector.Headers)
		result = append(result, clone)
	}
	return result
}

// publicEventSink 只把跨 Runtime 稳定的执行语义暴露给 Chat。
type publicEventSink struct {
	// sink 是调用方提供的公开事件接收器。
	sink EventSink
}

// Accept 丢弃 Runtime 私有状态、Session、诊断和 Raw 事件。
func (s publicEventSink) Accept(ctx context.Context, event Event) error {
	switch event.Type {
	case EventTypeInvocationStarted,
		EventTypeTextDelta,
		EventTypeCommentaryDelta,
		EventTypeThinkingStart,
		EventTypeThinkingDelta,
		EventTypeToolUse,
		EventTypeToolInputSnapshot,
		EventTypeToolUpdate,
		EventTypeToolResult,
		EventTypeUsage:
		return s.sink.Accept(ctx, event)
	default:
		return nil
	}
}

func prepareRequestForRun(sdk *SDK, request RunRequest) DetectRequest {
	environment := make(map[string]string)
	if definition, exists := sdk.definitions[request.Target.RuntimeID]; exists &&
		request.Target.ConfigHome != "" &&
		definition.configHomeEnvironmentVariable != "" {
		environment[definition.configHomeEnvironmentVariable] = request.Target.ConfigHome
	}
	for key, value := range request.Target.Environment {
		environment[key] = value
	}
	baseEnvironment := os.Environ()
	if request.Target.Isolated {
		baseEnvironment = accountconfig.Environment(baseEnvironment)
	}
	homeDirectory, _ := os.UserHomeDir()
	return DetectRequest{
		RuntimeID:       request.Target.RuntimeID,
		BaseEnvironment: baseEnvironment,
		HomeDirectory:   homeDirectory,
		LaunchOverride: LaunchOverride{
			Binary:      request.Target.Binary,
			PrefixArgs:  append([]string(nil), request.Target.PrefixArgs...),
			Environment: environment,
		},
	}
}

func compileRunPrompt(request RunRequest) string {
	var builder strings.Builder
	if len(request.History) > 0 {
		builder.WriteString("Chat history:\n")
		for _, message := range request.History {
			fmt.Fprintf(&builder, "%s: %s\n", message.Role, message.Content)
			appendRunCodeReferences(&builder, message.CodeReferences)
		}
	}
	builder.WriteString(compileCurrentMessage(request.Message))
	return builder.String()
}

func compileCurrentMessage(message Message) string {
	var builder strings.Builder
	builder.WriteString(message.Text)
	if len(message.Attachments) > 0 {
		builder.WriteString("\n\nAttached local files (paths are relative to the working directory):\n")
		for _, attachment := range message.Attachments {
			fmt.Fprintf(&builder, "- %s (%s)\n", attachment.Path, attachment.Name)
		}
	}
	appendRunCodeReferences(&builder, message.CodeReferences)
	return builder.String()
}

func appendRunCodeReferences(builder *strings.Builder, references []CodeReference) {
	wholeFiles := make([]CodeReference, 0, len(references))
	snippets := make([]CodeReference, 0, len(references))
	for _, reference := range references {
		if reference.StartLine == 0 && reference.EndLine == 0 && reference.Content == "" {
			wholeFiles = append(wholeFiles, reference)
			continue
		}
		snippets = append(snippets, reference)
	}
	if len(wholeFiles) > 0 {
		builder.WriteString("\n\nReferenced workspace files (read the current content from these paths):\n")
		for _, reference := range wholeFiles {
			fmt.Fprintf(builder, "- %s\n", reference.Path)
		}
	}
	if len(snippets) == 0 {
		return
	}
	builder.WriteString("\n\nReferenced code snippets (snapshots captured when the message was sent):\n")
	for _, reference := range snippets {
		fmt.Fprintf(
			builder,
			"\n%s:%d-%d\n```\n%s\n```\n",
			reference.Path,
			reference.StartLine,
			reference.EndLine,
			reference.Content,
		)
	}
}

func failedRunError(stage string, err error) Result {
	code := FailureCodeInvalidRequest
	if classified, ok := ErrorCode(err); ok {
		code = classified
	} else if errors.Is(err, context.DeadlineExceeded) {
		code = constants.RuntimeFailureCodeRequestTimeout
	} else if errors.Is(err, context.Canceled) {
		code = FailureCodeContextCanceled
	}
	return failedRunResult(code, stage, err.Error())
}

func failedRunResult(code FailureCode, stage string, message string) Result {
	return Result{TerminationReason: constants.RuntimeTerminationReasonUnknown, ExitCode: -1, Failure: newFailure(code, stage, message)}
}

func defaultExecutionPolicy() ExecutionPolicy {
	return ExecutionPolicy{PermissionLevel: PermissionLevelAuto}
}
