// Package agentfixture replaces the external CLI in host integration tests.
package agentfixture

import (
	"context"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"github.com/google/uuid"
)

// Client supplies native receipts while the real Thread owns admission and storage.
type Client struct {
	// AgentClient leaves operations outside a fixture's scope unimplemented.
	agent.AgentClient
	// Session is the currently bound synthetic native session.
	Session agent.AgentSession
	// Options records the session's actual authorized inputs.
	Options agent.AgentSessionOptions
	// Model is the selected synthetic model ID.
	Model string
	// Reasoning is the last configured native effort.
	Reasoning string
	// OnPrompt processes one durably admitted input.
	OnPrompt func(context.Context, agent.AgentPromptRequest) (*string, error)
	// OnClose observes the release of the adapter itself.
	OnClose func(context.Context) error
}

func (*Client) Initialize(context.Context) error { return nil }
func (c *Client) Close(ctx context.Context) error {
	if c.OnClose != nil {
		return c.OnClose(ctx)
	}
	return nil
}

func (*Client) Capabilities() agent.AgentCapabilities {
	return agent.AgentCapabilities{Version: "fixture", Load: true, Resume: true, AdditionalDirectories: true, MCPHTTP: true, DeveloperInstructionDelivery: "native"}
}

func (c *Client) NewSession(_ context.Context, options agent.AgentSessionOptions) (agent.AgentSession, error) {
	return c.open(uuid.NewString(), options)
}
func (c *Client) ResumeSession(_ context.Context, id string, options agent.AgentSessionOptions) (agent.AgentSession, error) {
	return c.open(id, options)
}

func (c *Client) open(id string, options agent.AgentSessionOptions) (agent.AgentSession, error) {
	c.Options = options
	c.Session = agent.AgentSession{ID: id, Mode: "default", MCPStatus: "not_requested"}
	return c.Session, nil
}

func (c *Client) ConfigureCodexTurn(_ context.Context, _ string, model, effort string, _ *bool) ([]agent.AgentSessionOption, error) {
	if model != "" {
		c.Model = model
	}
	c.Reasoning = effort
	return []agent.AgentSessionOption{{ID: "model", Category: "model", Value: agent.AgentOptionValue{ID: c.Model}}}, nil
}

func (c *Client) SetSessionMode(_ context.Context, _ string, mode string) error {
	c.Session.Mode = mode
	return nil
}

func (c *Client) ReservePrompt(ctx context.Context, request agent.AgentPromptRequest) (func() agent.AgentPromptResult, error) {
	return func() agent.AgentPromptResult {
		input := agent.AgentPromptSubmission{SessionID: request.SessionID, HostInvocationID: request.HostInvocationID, InputID: request.HostInvocationID}
		result := agent.AgentPromptResult{Input: input, Submission: "not_sent", Admission: "not_accepted"}
		if err := request.BeforeSubmit(ctx, input); err != nil {
			result.OperationError = err
			return result
		}
		text, err := c.OnPrompt(ctx, request)
		result.Submission, result.Admission = "sent", "accepted"
		outcome := &agent.AgentNativeOutcome{Turn: acpmeta.NativeTurn{
			InvocationID: "fixture-" + input.InputID, Origin: acpmeta.NativeTurnOriginHost, ConsumedInputIDs: []string{input.InputID}, Status: acpmeta.NativeTurnCompleted,
		}, StopReason: "end_turn"}
		if err != nil {
			outcome.Turn.Status = acpmeta.NativeTurnFailed
			outcome.NativeFailure = err
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				outcome.Turn.Status = acpmeta.NativeTurnCancelled
				outcome.StopReason = "cancelled"
			}
		} else if text != nil {
			outcome.FinalMessage = &agent.AgentFinalMessage{ID: "message-" + input.InputID, Text: *text}
			outcome.ObservedOutput = true
		}
		result.Outcome = outcome
		return result
	}, nil
}

// ResolveAccount fixes the fixture account identity without using real credentials.
func ResolveAccount(_ context.Context, request agent.AccountRequest, _ string) (agent.AccountRequest, error) {
	request.Subject = "fixture:" + request.AccountID
	request.AuthGeneration = 1
	return request, nil
}
