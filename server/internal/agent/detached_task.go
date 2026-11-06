package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

// TextMessage is a role message in a detached generation.
type TextMessage struct {
	// Role is system, user, or assistant.
	Role string `json:"role"`
	// Content is the role's message body.
	Content string `json:"content"`
}

// DetachedAccountResolver freezes the selected account's actual credential identity in an isolated working directory.
type DetachedAccountResolver func(context.Context, AccountRequest, string) (AccountRequest, error)

// TextRequest specifies a detached text invocation without application resources or tools.
type TextRequest struct {
	// Connection specifies the native account, executable, and model.
	Connection ProviderConnectionRequest
	// ResolveAccount resolves the account after preparing the isolated working directory.
	ResolveAccount DetachedAccountResolver
	// Messages passes system instructions and messages in order.
	Messages []TextMessage
	// Settings passes native options explicitly specified by the caller.
	Settings []SessionSetting
	// Selection specifies reasoning options.
	Selection *TurnSelection
	// UsageSource records the invocation's business source.
	UsageSource string
	// MaxTokens is reported as an unsupported generation limit when nonzero.
	MaxTokens int
	// Temperature is reported as an unsupported generation limit when nonzero.
	Temperature float64
}

// TextResult returns natively confirmed text, terminal state, and usage evidence.
type TextResult struct {
	// Text is returned only when the native client confirms a complete message.
	Text string
	// Turn holds native terminal state and independent host failures.
	Turn TurnResult
	// ToolsDisabled reports whether the runtime actually confirmed startup without tools.
	ToolsDisabled bool
	// Limitations records requested controls that could not be applied.
	Limitations []string
}

// TaskRequest executes a detached task with explicitly selected resources and permissions.
type TaskRequest struct {
	// Thread freezes account, work scope, and capability selections.
	Thread ThreadRequest
	// ResolveAccount resolves the account after preparing the isolated working directory.
	ResolveAccount DetachedAccountResolver
	// Input is the detached task input.
	Input TurnRequest
	// Ports supplies explicit resource and interaction grants.
	Ports TurnPorts
}

// SupportsDetachedPolicy reports whether the adapter can enforce the permission boundary of a detached invocation.
func SupportsDetachedPolicy(provider string) bool {
	return provider == constants.RuntimeIDClaude
}

// GenerateText runs detached generation without tools through a regular Turn.
func (l *ThreadLifecycle) GenerateText(ctx context.Context, request TextRequest) (TextResult, error) {
	var result TextResult
	if request.MaxTokens != 0 {
		result.Limitations = append(result.Limitations, "max_tokens_unsupported")
	}
	if request.Temperature != 0 {
		result.Limitations = append(result.Limitations, "temperature_unsupported")
	}
	instructions, text, err := generationInput(request.Messages)
	if err != nil {
		return result, err
	}
	if !SupportsDetachedPolicy(request.Connection.Account.Provider) || len(request.Connection.Resources.Plugins) != 0 || len(request.Connection.Resources.ConnectorIDs) != 0 {
		return result, status_error.RuntimePolicyUnsupported
	}
	source := request.UsageSource
	if source == "" {
		source = "generate_text"
	}
	request.Connection.Launch.RequireNoTools = true
	turn, err := l.prepareDetached(ctx, ThreadRequest{
		Connection: request.Connection, Settings: request.Settings, Selection: request.Selection,
		Session: AgentSessionOptions{DeveloperInstructions: instructions},
	}, TurnRequest{Text: text, UsageSource: source}, TurnPorts{}, request.ResolveAccount)
	if err != nil {
		return result, err
	}
	result.ToolsDisabled = true
	result.Turn = turn.Run(ctx)
	if outcome := result.Turn.Outcome; outcome != nil && outcome.FinalMessage != nil &&
		outcome.Turn.Status == acpmeta.NativeTurnCompleted && outcome.NativeFailure == nil {
		result.Text = outcome.FinalMessage.Text
	}
	err = turnExecutionError(result.Turn)
	if err == nil && (result.Turn.Outcome == nil || result.Turn.Outcome.FinalMessage == nil) {
		err = status_error.RuntimeExecutionFailed
	}
	return result, err
}

// ExecuteTask applies explicit detached tool permissions through a regular Turn.
func (l *ThreadLifecycle) ExecuteTask(ctx context.Context, request TaskRequest) (TurnResult, error) {
	if request.Thread.ChatID != "" || request.Thread.Fork != nil || !SupportsDetachedPolicy(request.Thread.Connection.Account.Provider) {
		return TurnResult{}, status_error.RuntimePolicyUnsupported
	}
	launch := &request.Thread.Connection.Launch
	if !launch.RequireNoTools {
		// Approval callbacks cannot constrain tools executed automatically by the native client, so a native tool allowlist is also required.
		if launch.Claude.Tools == nil || request.Ports.Approve == nil {
			return TurnResult{}, status_error.RuntimePolicyUnsupported
		}
		for _, tool := range launch.Claude.Tools {
			if tool == "" || tool == "default" || strings.ContainsAny(tool, " ,*()\t\r\n") {
				return TurnResult{}, status_error.RuntimeConfigurationInvalid
			}
		}
		launch.Claude.SettingSources = []string{}
	}
	if request.Input.UsageSource == "" {
		request.Input.UsageSource = "background"
	}
	turn, err := l.prepareDetached(ctx, request.Thread, request.Input, request.Ports, request.ResolveAccount)
	if err != nil {
		return TurnResult{}, err
	}
	result := turn.Run(ctx)
	return result, turnExecutionError(result)
}

func (l *ThreadLifecycle) prepareDetached(ctx context.Context, thread ThreadRequest, input TurnRequest, ports TurnPorts, resolve DetachedAccountResolver) (*Turn, error) {
	cwd, err := os.MkdirTemp("", "neux-detached-")
	if err != nil {
		return nil, status_error.RuntimeConfigurationInvalid
	}
	closeFiles := func(context.Context) error { return os.RemoveAll(cwd) }
	account := &thread.Connection.Account
	account.CWD = cwd
	if resolve != nil {
		account.CWD, err = resolveAgentDirectory(cwd)
		if err == nil {
			*account, err = resolve(ctx, *account, thread.Connection.Launch.ConfigHome)
		}
		if err != nil {
			return nil, errors.Join(err, closeFiles(context.WithoutCancel(ctx)))
		}
		if account.ConfigHome != "" {
			thread.Connection.Launch.ConfigHome = account.ConfigHome
		}
	}
	return l.prepare(ctx, thread, input, ports, closeFiles)
}

func turnExecutionError(r TurnResult) error {
	var nativeError error
	if r.Outcome != nil {
		nativeError = r.Outcome.NativeFailure
		if nativeError == nil {
			switch r.Outcome.Turn.Status {
			case acpmeta.NativeTurnCompleted:
			case acpmeta.NativeTurnCancelled:
				nativeError = context.Canceled
			default:
				nativeError = status_error.RuntimeExecutionFailed
			}
		}
	} else if r.OperationError == nil {
		nativeError = status_error.RuntimeExecutionFailed
	}
	return errors.Join(r.OperationError, nativeError, r.DeliveryError, r.StorageError, r.CleanupError)
}

func generationInput(messages []TextMessage) (*string, string, error) {
	var system []string
	i := 0
	for ; i < len(messages) && messages[i].Role == "system"; i++ {
		system = append(system, messages[i].Content)
	}
	var instructions *string
	if len(system) > 0 {
		value := strings.Join(system, "\n\n")
		instructions = &value
	}
	remaining := messages[i:]
	if len(remaining) == 0 || remaining[len(remaining)-1].Role != "user" || strings.TrimSpace(remaining[len(remaining)-1].Content) == "" {
		return nil, "", status_error.AgentInputInvalid
	}
	for _, message := range remaining {
		if message.Role != "user" && message.Role != "assistant" {
			return nil, "", status_error.AgentInputInvalid
		}
	}
	if len(remaining) == 1 {
		return instructions, remaining[0].Content, nil
	}
	content, err := json.Marshal(remaining)
	if err != nil {
		return nil, "", status_error.RuntimeEventEncodingFailed
	}
	return instructions, "Conversation messages in chronological order:\n" + string(content), nil
}
