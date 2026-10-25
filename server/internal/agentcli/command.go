package agentcli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability/contract"
	"github.com/spf13/cobra"
)

// IO 显式注入进程流与连接文件位置，便于通过同一命令入口验证行为。
type IO struct {
	// In 只在显式 --input - 时读取。
	In io.Reader
	// Out 只输出成功结果或显式帮助。
	Out io.Writer
	// Err 只输出单个结构化失败。
	Err io.Writer
	// ContextFile 是宿主注入的绝对连接文件路径。
	ContextFile string
}

// runner 保存一次命令执行的输出与语法阶段。
type runner struct {
	// streams 是显式进程输入输出。
	streams IO
	// entered 表示 Cobra 已完成路由和参数个数检查。
	entered bool
	// response 是要原样输出的规范回执。
	response agentcapability.Response
	// output 是仅 guide 支持的显式文本格式。
	output string
}

// Execute 运行一个无交互命令并返回退出码，不在内部终止进程。
func Execute(ctx context.Context, args []string, streams IO) int {
	r := &runner{streams: streams, output: "json"}
	root := r.root()
	if streams.In == nil {
		streams.In = root.InOrStdin()
	}
	if streams.Out == nil {
		streams.Out = root.OutOrStdout()
	}
	if streams.Err == nil {
		streams.Err = root.ErrOrStderr()
	}
	r.streams = streams
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(io.Discard)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err != nil {
		if !r.entered {
			r.response = agentcapability.ErrorResponse("", status_error.AgentInputInvalid)
		} else {
			r.response = agentcapability.ErrorResponse(r.response.Operation, err)
		}
		if ctx.Err() != nil && (r.response.Error == nil || r.response.Error.Code != status_error.AgentCommandOutcomeUnknown.Key()) {
			r.response = agentcapability.ErrorResponse(r.response.Operation, status_error.OperationCanceled)
		}
	}
	if r.response.ProtocolVersion == 0 {
		return 0
	}
	if !r.response.OK {
		_ = json.NewEncoder(streams.Err).Encode(r.response)
		if ctx.Err() != nil {
			return 130
		}
		if !r.entered {
			return 2
		}
		return failureExit(r.response.Error)
	}
	if r.output == "text" {
		data, ok := r.response.Data.(map[string]any)
		content, _ := data["content"].(string)
		if !ok || content == "" {
			_ = json.NewEncoder(streams.Err).Encode(agentcapability.ErrorResponse("guide", status_error.AgentConnectionUnavailable))
			return 69
		}
		if _, err := io.WriteString(streams.Out, content+"\n"); err != nil {
			return 70
		}
		return 0
	}
	if json.NewEncoder(streams.Out).Encode(r.response) != nil {
		return 70
	}
	return 0
}

func failureExit(failure *agentcapability.Failure) int {
	if failure == nil {
		return 70
	}
	switch failure.Code {
	case status_error.AgentContextUnauthorized.Key(), status_error.AgentOperationForbidden.Key():
		return 77
	case status_error.AgentConnectionUnavailable.Key():
		return 69
	case status_error.AgentInputInvalid.Key(), status_error.BadRequest.Key():
		return 65
	case status_error.DatabaseBusy.Key():
		return 75
	default:
		return 70
	}
}

func (r *runner) run(ctx context.Context, method, path string, payload any, mutation bool, timeout time.Duration) error {
	connection, err := agentcapability.ReadConnection(r.streams.ContextFile)
	if err != nil {
		return err
	}
	response, err := (client{connection: connection}).request(ctx, method, path, payload, mutation, timeout)
	if err != nil {
		return err
	}
	r.response = response
	return nil
}

func (r *runner) root() *cobra.Command {
	root := &cobra.Command{Use: "allycli", Short: "Use capabilities authorized for the current NeuxBot execution", SilenceErrors: true, SilenceUsage: true, CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true}}
	root.RunE = func(cmd *cobra.Command, _ []string) error { r.entered = true; return status_error.AgentInputInvalid }
	root.AddCommand(&cobra.Command{Use: "version", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		r.entered = true
		r.response = agentcapability.Response{ProtocolVersion: constants.AgentCLIProtocolVersion, OK: true, Operation: "version", Result: agentcapability.Result{Data: map[string]any{"version": constants.ApplicationVersion, "protocolVersion": constants.AgentCLIProtocolVersion}}}
		return nil
	}})
	root.AddCommand(&cobra.Command{Use: "capabilities", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		r.entered = true
		return r.run(cmd.Context(), http.MethodGet, "/capabilities", nil, false, 35*time.Second)
	}})
	root.AddCommand(&cobra.Command{Use: "doctor", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		r.entered = true
		if err := r.run(cmd.Context(), http.MethodGet, "/capabilities", nil, false, 5*time.Second); err != nil {
			return err
		}
		if r.response.OK {
			r.response.Operation = "doctor"
			r.response.Data = map[string]any{"context": "valid", "core": "reachable", "protocolVersion": constants.AgentCLIProtocolVersion}
		}
		return nil
	}})
	root.AddCommand(&cobra.Command{Use: "describe <operation-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		r.entered = true
		return r.run(cmd.Context(), http.MethodGet, "/operations/"+url.PathEscape(args[0]), nil, false, 35*time.Second)
	}})
	guide := &cobra.Command{Use: "guide <guide-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		r.entered = true
		if r.output != "json" && r.output != "text" {
			return status_error.AgentInputInvalid
		}
		return r.run(cmd.Context(), http.MethodGet, "/guides/"+url.PathEscape(args[0]), nil, false, 35*time.Second)
	}}
	guide.Flags().StringVar(&r.output, "output", "json", "Output format: json or text")
	root.AddCommand(guide)
	receipts := &cobra.Command{Use: "receipts"}
	var operation, requestID string
	receiptGet := &cobra.Command{Use: "get", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		r.entered = true
		if operation == "" || requestID == "" {
			return status_error.AgentInputInvalid
		}
		return r.run(cmd.Context(), http.MethodGet, receiptPath(operation, requestID), nil, false, 35*time.Second)
	}}
	receiptGet.Flags().StringVar(&operation, "operation", "", "Original operation ID")
	receiptGet.Flags().StringVar(&requestID, "request-id", "", "Original request ID")
	receipts.AddCommand(receiptGet)
	root.AddCommand(receipts)
	for _, op := range contract.Operations() {
		parent := root
		parts := op.Command()
		for _, part := range parts[:len(parts)-1] {
			var next *cobra.Command
			for _, child := range parent.Commands() {
				if child.Name() == part {
					next = child
					break
				}
			}
			if next == nil {
				next = &cobra.Command{Use: part}
				parent.AddCommand(next)
			}
			parent = next
		}
		parent.AddCommand(r.operation(op, parts[len(parts)-1]))
	}
	return root
}

func flagName(value string) string {
	var output strings.Builder
	for _, char := range value {
		if unicode.IsUpper(char) {
			output.WriteByte('-')
			char = unicode.ToLower(char)
		}
		output.WriteRune(char)
	}
	return output.String()
}

func (r *runner) operation(op contract.Operation, verb string) *cobra.Command {
	var input, requestID, source, id string
	var timeout time.Duration
	command := &cobra.Command{Use: verb, Short: op.Summary, Args: cobra.NoArgs}
	command.Flags().StringVar(&input, "input", "", "Read JSON parameters from a UTF-8 file; - reads stdin")
	command.Flags().StringVar(&requestID, "request-id", "", "Stable identity required for mutations")
	properties := op.InputSchema["properties"].(map[string]any)
	for name, value := range properties {
		if name == "source" || name == "timeoutMs" {
			continue
		}
		schema := value.(map[string]any)
		kind, _ := schema["type"].(string)
		help := "Set " + name
		if values, ok := schema["enum"].([]string); ok {
			help += "; choices: " + strings.Join(values, ", ")
		}
		if value, ok := schema["default"]; ok {
			encoded, _ := json.Marshal(value)
			help += "; default: " + string(encoded)
		}
		if description, ok := schema["description"].(string); ok {
			help += ". " + description
		}
		if kind == "array" {
			items, _ := schema["items"].(map[string]any)
			if items["type"] == "string" {
				if values, ok := items["enum"].([]string); ok {
					help += "; choices: " + strings.Join(values, ", ")
				}
				command.Flags().StringSlice(flagName(name), []string{}, help+" (comma-separated)")
			}
			continue
		}
		if kind == "object" || kind == "array" || kind == "" {
			continue
		}
		if kind == "boolean" {
			command.Flags().Bool(flagName(name), false, help)
		} else {
			command.Flags().String(flagName(name), "", help)
		}
	}
	if _, ok := properties["source"]; ok {
		command.Flags().StringVar(&source, "source", "", "Read complete UTF-8 source from this file")
	}
	if _, ok := properties["timeoutMs"]; ok {
		command.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Observation window, at most 60s")
	}
	idField := ""
	for _, name := range []string{"deliveryId", "runId", "taskId", "memoryId", "questionId", "definitionId"} {
		if _, ok := properties[name]; ok {
			idField = name
			break
		}
	}
	if idField != "" {
		command.Flags().StringVar(&id, "id", "", "Set "+idField+" without changing authorization scope")
	}
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		r.entered = true
		r.response.Operation = op.ID
		params := map[string]any{}
		if input != "" {
			reader := r.streams.In
			if input != "-" {
				file, err := os.Open(input)
				if err != nil {
					return status_error.AgentInputInvalid
				}
				defer file.Close()
				reader = file
			}
			if reader == nil {
				return status_error.AgentInputInvalid
			}
			data, err := io.ReadAll(io.LimitReader(reader, constants.AgentCLIRequestMaximumBytes+1))
			if err != nil || len(data) > constants.AgentCLIRequestMaximumBytes || !utf8.Valid(data) || json.Unmarshal(data, &params) != nil || params == nil {
				return status_error.AgentInputInvalid
			}
		}
		set := func(name string, value any) error {
			if _, exists := params[name]; exists {
				return status_error.AgentInputInvalid
			}
			params[name] = value
			return nil
		}
		for name, value := range properties {
			flag := flagName(name)
			if name == "source" || name == "timeoutMs" || !cmd.Flags().Changed(flag) {
				continue
			}
			kind, _ := value.(map[string]any)["type"].(string)
			var decoded any
			if kind == "boolean" {
				decoded, _ = cmd.Flags().GetBool(flag)
			} else if kind == "array" {
				values, _ := cmd.Flags().GetStringSlice(flag)
				items := make([]any, 0, len(values))
				for _, value := range values {
					items = append(items, value)
				}
				decoded = items
			} else {
				raw, _ := cmd.Flags().GetString(flag)
				decoded = raw
				if kind == "integer" {
					number, err := strconv.ParseInt(raw, 10, 64)
					if err != nil {
						return status_error.AgentInputInvalid
					}
					decoded = number
				}
			}
			if err := set(name, decoded); err != nil {
				return err
			}
		}
		if cmd.Flags().Changed("id") {
			if err := set(idField, id); err != nil {
				return err
			}
		}
		if source != "" {
			file, err := os.Open(source)
			if err != nil {
				return status_error.AgentInputInvalid
			}
			defer file.Close()
			data, err := io.ReadAll(io.LimitReader(file, constants.AgentCLIRequestMaximumBytes+1))
			if err != nil || len(data) > constants.AgentCLIRequestMaximumBytes || !utf8.Valid(data) {
				return status_error.AgentInputInvalid
			}
			if err := set("source", string(data)); err != nil {
				return err
			}
		}
		window := 35 * time.Second
		if _, ok := properties["timeoutMs"]; ok {
			if cmd.Flags().Changed("timeout") {
				if err := set("timeoutMs", timeout.Milliseconds()); err != nil {
					return err
				}
			} else if _, exists := params["timeoutMs"]; !exists {
				params["timeoutMs"] = timeout.Milliseconds()
			}
			encoded, _ := json.Marshal(params["timeoutMs"])
			var milliseconds int64
			if json.Unmarshal(encoded, &milliseconds) != nil || milliseconds < 1 || milliseconds > 60000 {
				return status_error.AgentInputInvalid
			}
			window = time.Duration(milliseconds)*time.Millisecond + 5*time.Second
		}
		if values, ok := params["files"].([]any); ok {
			for index, value := range values {
				path, ok := value.(string)
				if !ok {
					return status_error.AgentInputInvalid
				}
				absolute, err := filepath.Abs(path)
				if err != nil {
					return status_error.AgentInputInvalid
				}
				values[index] = absolute
			}
		}
		if err := op.Validate(params); err != nil {
			return err
		}
		mutation := op.Effect != constants.AgentCLIEffectRead
		if mutation && requestID == "" {
			return status_error.AgentInputInvalid
		}
		request := agentcapability.Request{Operation: op.ID, RequestID: requestID, Params: params}
		if op.ID == "workflow.runs.start" {
			cwd, err := os.Getwd()
			if err != nil {
				return status_error.AgentInputInvalid
			}
			request.CWD = cwd
		}
		return r.run(cmd.Context(), http.MethodPost, "/commands", request, mutation, window)
	}
	return command
}
