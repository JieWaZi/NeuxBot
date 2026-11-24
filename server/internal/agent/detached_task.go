package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

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

// TextRequest specifies a detached text invocation without application resources.
type TextRequest struct {
	// BeforeSubmit authorizes the detached input again after acquiring the process and account.
	BeforeSubmit func(context.Context) error
	// Ephemeral keeps bodies out of Core history and applies available native restrictions.
	Ephemeral bool
	// ScratchRoot is the Core-owned directory for recoverable ephemeral scratch.
	ScratchRoot string
	// UsageWakerID attributes this detached invocation to a partner.
	UsageWakerID string
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
	// Text is the answer from a completed native invocation.
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

// GenerateText runs a fresh, detached generation through the regular Turn lifecycle.
func (l *ThreadLifecycle) GenerateText(ctx context.Context, request TextRequest) (TextResult, error) {
	var result TextResult
	if request.MaxTokens != 0 {
		result.Limitations = append(result.Limitations, "max_tokens_unsupported")
	}
	if request.Temperature != 0 {
		result.Limitations = append(result.Limitations, "temperature_unsupported")
	}
	instructions, input, err := generationInput(request.Messages)
	if err != nil {
		return result, err
	}
	provider := request.Connection.Account.Provider
	resources := request.Connection.Resources
	if resources.Skills != nil || len(resources.Plugins) != 0 || len(resources.ConnectorIDs) != 0 ||
		!SupportsProvider(provider) || (!request.Ephemeral && !SupportsDetachedPolicy(provider)) {
		return result, status_error.RuntimePolicyUnsupported
	}
	source := request.UsageSource
	if source == "" {
		source = "generate_text"
	}
	input.UsageSource = source
	input.UsageWakerID = request.UsageWakerID
	launch := &request.Connection.Launch
	launch.RequireNoTools = SupportsDetachedPolicy(provider)
	var mode string
	lifecycle := l
	var privateStore *ephemeralTextStore
	if request.Ephemeral {
		if !filepath.IsAbs(request.ScratchRoot) {
			return result, status_error.RuntimePolicyUnsupported
		}
		launch.EphemeralText = true
		launch.CursorPermissionMode, launch.ProcessPermissionMode = "default", "default"
		if provider == constants.RuntimeIDCodex {
			mode = "read-only"
		}
		if provider != constants.RuntimeIDCodex && !launch.usesEphemeralClaude(provider) {
			result.Limitations = append(result.Limitations, "native_history_not_disabled")
		}
		if !launch.RequireNoTools {
			result.Limitations = append(result.Limitations, "native_tools_not_disabled")
			rule := "Use only the supplied text. Return the requested output without tools, file access, or network access."
			if instructions != nil {
				rule += "\n\n" + *instructions
			}
			instructions = &rule
		}
		privateStore = &ephemeralTextStore{ConversationStore: l.store, results: make(map[NativeResultKey]NativeResult)}
		lifecycle = NewThreadLifecycle(l.provider, privateStore)
		defer func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = lifecycle.Close(cleanup)
		}()
	}
	ports := TurnPorts{}
	if request.BeforeSubmit != nil {
		ports.Authorize = func(ctx context.Context, _ TurnAdmission, _ CommitReceipt) (func(), error) {
			return nil, request.BeforeSubmit(ctx)
		}
	}
	turn, err := lifecycle.prepareDetached(ctx, ThreadRequest{
		Connection: request.Connection, Settings: request.Settings, Selection: request.Selection,
		Session: AgentSessionOptions{DeveloperInstructions: instructions}, Mode: mode,
	}, input, ports, request.ResolveAccount, request.ScratchRoot)
	if err != nil {
		return result, err
	}
	result.ToolsDisabled = launch.RequireNoTools
	result.Turn = turn.Run(ctx)
	outcome := result.Turn.Outcome
	if outcome != nil && outcome.FinalMessage != nil &&
		outcome.Turn.Status == acpmeta.NativeTurnCompleted && outcome.NativeFailure == nil {
		result.Text = outcome.FinalMessage.Text
	}
	if err = turnExecutionError(result.Turn); err != nil {
		return result, err
	}
	if outcome != nil && outcome.FinalMessage != nil {
		return result, nil
	}
	if outcome != nil && privateStore != nil {
		key := NativeResultKey{Session: result.Turn.Binding.Session, InvocationID: outcome.Turn.InvocationID}
		if answer := privateStore.streamedText(key); answer != "" {
			result.Text = answer
			return result, nil
		}
	}
	return result, status_error.RuntimeExecutionFailed
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
	turn, err := l.prepareDetached(ctx, request.Thread, request.Input, request.Ports, request.ResolveAccount, "")
	if err != nil {
		return TurnResult{}, err
	}
	result := turn.Run(ctx)
	return result, turnExecutionError(result)
}

func (l *ThreadLifecycle) prepareDetached(ctx context.Context, thread ThreadRequest, input TurnRequest, ports TurnPorts, resolve DetachedAccountResolver, scratchRoot string) (*Turn, error) {
	if scratchRoot != "" {
		if err := os.MkdirAll(scratchRoot, 0700); err != nil {
			return nil, status_error.RuntimeConfigurationInvalid
		}
	}
	cwd, err := os.MkdirTemp(scratchRoot, detachedScratchPrefix)
	if err != nil {
		return nil, status_error.RuntimeConfigurationInvalid
	}
	closeFiles := func(context.Context) error { return os.RemoveAll(cwd) }
	if scratchRoot != "" {
		closeFiles = func(context.Context) error { return l.provider.closeEphemeralScratch(cwd) }
	}
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

func generationInput(messages []TextMessage) (*string, TurnRequest, error) {
	var system []string
	i := 0
	for ; i < len(messages) && messages[i].Role == "system"; i++ {
		system = append(system, messages[i].Content)
	}
	remaining := messages[i:]
	if len(remaining) == 0 || remaining[len(remaining)-1].Role != "user" || strings.TrimSpace(remaining[len(remaining)-1].Content) == "" {
		return nil, TurnRequest{}, status_error.AgentInputInvalid
	}
	for _, message := range remaining {
		if message.Role != "user" && message.Role != "assistant" {
			return nil, TurnRequest{}, status_error.AgentInputInvalid
		}
	}
	input := TurnRequest{Text: remaining[len(remaining)-1].Content}
	if len(remaining) > 1 {
		system = append(system, TurnContextInstructions)
		input.Context = []TurnFact{{Name: "Recent conversation history", Value: slices.Clone(remaining[:len(remaining)-1])}}
	}
	var instructions *string
	if len(system) > 0 {
		value := strings.Join(system, "\n\n")
		instructions = &value
	}
	return instructions, input, nil
}
