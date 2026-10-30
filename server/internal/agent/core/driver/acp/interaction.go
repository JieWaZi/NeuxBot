package acp

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// interact 将当前原生请求的撤销与整轮撤销分别交给同一个人工端口。
func (s *session) interact(ctx context.Context, kind contract.InteractionKind, prompt string, approval *contract.ApprovalRequest, input *contract.InputRequest) (contract.InteractionResponse, error) {
	s.mu.Lock()
	turn := s.turn
	if turn == nil || ctx.Err() != nil || turn.ctx.Err() != nil {
		s.mu.Unlock()
		return contract.InteractionResponse{}, context.Canceled
	}
	turn.interactionSeq++
	sequence := turn.interactionSeq
	if turn.pendingInput == 0 {
		turn.inputDone = make(chan struct{})
	}
	turn.pendingInput++
	s.mu.Unlock()
	s.markActivity(turn)
	defer s.setPendingInput(turn, -1)
	interactionCtx, cancel := context.WithCancel(turn.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	request := contract.InteractionRequest{Invocation: turn.request.Invocation, ID: string(kind) + "-" + strconv.FormatUint(sequence, 10), Kind: kind, Prompt: prompt, Approval: approval, Input: input}
	response, err := turn.sink.Interact(interactionCtx, request)
	if ctx.Err() != nil {
		return contract.InteractionResponse{}, ctx.Err()
	}
	if turn.ctx.Err() != nil {
		return contract.InteractionResponse{}, turn.ctx.Err()
	}
	if err != nil {
		s.failTurn(turn, err)
		return contract.InteractionResponse{}, err
	}
	if response.Invocation != request.Invocation || response.ID != request.ID {
		err = fault(contract.FaultProtocol, "interaction", "ACP interaction response identity mismatch", nil)
		s.failTurn(turn, err)
		return contract.InteractionResponse{}, err
	}
	return response, nil
}

// RequestPermission 保留原生选项身份与未知类别，只回传本次请求允许的明确选项。
func (s *session) RequestPermission(ctx context.Context, request sdk.RequestPermissionRequest) (sdk.RequestPermissionResponse, error) {
	if request.SessionId != s.nativeID {
		return permissionCancelled(), nil
	}
	approval := &contract.ApprovalRequest{ToolCallID: string(request.ToolCall.ToolCallId), Locations: canonicalLocations(request.ToolCall.Locations)}
	if request.ToolCall.Title != nil {
		approval.Title = *request.ToolCall.Title
	}
	if request.ToolCall.Kind != nil {
		approval.Kind = string(*request.ToolCall.Kind)
	}
	for _, block := range canonicalToolContent(request.ToolCall.Content) {
		if block.Text != "" {
			if approval.Summary != "" {
				approval.Summary += "\n"
			}
			approval.Summary += block.Text
		}
	}
	if args, ok := request.ToolCall.RawInput.(map[string]any); ok {
		if command, ok := args["command"].(string); ok {
			approval.Command = command
		}
	}
	if approval.Summary == "" {
		approval.Summary = approval.Title
	}
	for _, option := range request.Options {
		if option.OptionId != "" {
			approval.Options = append(approval.Options, contract.ApprovalOption{ID: string(option.OptionId), Name: option.Name, Kind: string(option.Kind)})
		}
	}
	if len(approval.Options) == 0 {
		return permissionCancelled(), nil
	}
	response, err := s.interact(ctx, contract.InteractionApproval, approval.Title, approval, nil)
	if err != nil || response.Action == contract.InteractionCancel {
		return permissionCancelled(), nil
	}
	for _, option := range request.Options {
		if string(option.OptionId) == response.OptionID && response.Action == contract.InteractionAccept {
			return sdk.RequestPermissionResponse{Outcome: sdk.RequestPermissionOutcome{Selected: &sdk.RequestPermissionOutcomeSelected{OptionId: option.OptionId}}}, nil
		}
	}
	return permissionCancelled(), nil
}

func permissionCancelled() sdk.RequestPermissionResponse {
	return sdk.RequestPermissionResponse{Outcome: sdk.RequestPermissionOutcome{Cancelled: &sdk.RequestPermissionOutcomeCancelled{}}}
}

// UnstableCreateElicitation 将公开表单或 URL 输入交给宿主，不解析 CLI 私有问题协议。
func (s *session) UnstableCreateElicitation(ctx context.Context, request sdk.UnstableCreateElicitationRequest) (sdk.UnstableCreateElicitationResponse, error) {
	var prompt string
	input := &contract.InputRequest{}
	if form := request.Form; form != nil {
		prompt = form.Message
		input.Mode = "form"
		data, err := json.Marshal(form.RequestedSchema)
		if err != nil || json.Unmarshal(data, &input.RequestedSchema) != nil {
			return sdk.NewUnstableCreateElicitationResponseCancel(), nil
		}
	} else if url := request.Url; url != nil {
		prompt = url.Message
		input.Mode = "url"
		input.URL = url.Url
	} else {
		return sdk.NewUnstableCreateElicitationResponseCancel(), nil
	}
	if strings.TrimSpace(prompt) == "" || input.Mode == "url" && strings.TrimSpace(input.URL) == "" {
		return sdk.NewUnstableCreateElicitationResponseCancel(), nil
	}
	response, err := s.interact(ctx, contract.InteractionInput, prompt, nil, input)
	if err != nil {
		return sdk.NewUnstableCreateElicitationResponseCancel(), nil
	}
	switch response.Action {
	case contract.InteractionAccept:
		result := sdk.NewUnstableCreateElicitationResponseAccept()
		result.Accept.Content = response.Content
		return result, nil
	case contract.InteractionDecline:
		return sdk.NewUnstableCreateElicitationResponseDecline(), nil
	default:
		return sdk.NewUnstableCreateElicitationResponseCancel(), nil
	}
}

// UnstableCompleteElicitation 接收公开 URL 完成通知；持久化与交互展示由宿主拥有。
func (*session) UnstableCompleteElicitation(context.Context, sdk.UnstableCompleteElicitationNotification) error {
	return nil
}
