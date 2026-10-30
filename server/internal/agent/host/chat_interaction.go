package runtimehost

import (
	"context"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
)

// chatInteraction 复用Execution的审批与输入协调器，准确保留原生选择身份。
type chatInteraction struct {
	// approval 保存本轮工具审批入口。
	approval runtimecontract.ApprovalHandler
	// input 保存本轮结构化输入入口。
	input runtimecontract.ElicitationHandler
	// readOnly 禁止只读工作越过原生审批写权限。
	readOnly bool
}

func (h chatInteraction) Handle(ctx context.Context, request c.InteractionRequest) (c.InteractionResponse, error) {
	response := c.InteractionResponse{Invocation: request.Invocation, ID: request.ID, Action: c.InteractionCancel}
	switch request.Kind {
	case c.InteractionApproval:
		if h.approval == nil || request.Approval == nil || h.readOnly {
			return response, nil
		}
		approval := request.Approval
		product := runtimecontract.ApprovalRequest{InvocationID: request.Invocation.ID, ToolCallID: approval.ToolCallID, Title: approval.Title, Kind: approval.Kind, Summary: approval.Summary, Command: approval.Command}
		for _, option := range approval.Options {
			product.Options = append(product.Options, runtimecontract.ApprovalOption{ID: option.ID, Name: option.Name, Kind: option.Kind})
		}
		for _, location := range approval.Locations {
			product.Locations = append(product.Locations, runtimecontract.ApprovalLocation{Path: location.Path, Line: location.Line})
		}
		decision, err := h.approval.Request(ctx, product)
		if err != nil {
			return response, err
		}
		if !decision.Cancelled {
			response.Action = c.InteractionAccept
			response.OptionID = decision.OptionID
		}
	case c.InteractionInput:
		if h.input == nil || request.Input == nil {
			return response, nil
		}
		input := request.Input
		decision, err := h.input.Request(ctx, runtimecontract.ElicitationRequest{InvocationID: request.Invocation.ID, Mode: input.Mode, Message: request.Prompt, RequestedSchema: input.RequestedSchema, URL: input.URL})
		if err != nil {
			return response, err
		}
		if decision.Accepted {
			response.Action = c.InteractionAccept
			response.Content = decision.Content
		} else if decision.Declined {
			response.Action = c.InteractionDecline
		}
	}
	return response, nil
}
