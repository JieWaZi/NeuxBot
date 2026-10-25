package im

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	protocol "github.com/JieWaZi/NeuxBot/server/internal/im"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// capabilityLease 保存执行或诊断授权，权限和文件范围不接受模型覆盖。
type capabilityLease struct {
	// chatID 限定可访问的来源对话。
	chatID string
	// executionID 限定当前真实执行。
	executionID string
	// wakerID 限定伙伴和所属团队获准目标。
	wakerID string
	// roots 是本次 Runtime 冻结的文件范围。
	roots []string
}

func (s *Service) callCapability(ctx context.Context, lease capabilityLease, workflow bool, runID, definitionID string, input protocol.ToolInput) (map[string]any, error) {
	targets, err := s.Targets(ctx, lease.wakerID, workflow)
	if err != nil {
		return nil, err
	}
	if input.Action == constants.IMActionListTargets {
		return map[string]any{"targets": targets}, nil
	}
	allowed := func(id string) bool {
		for _, t := range targets {
			if t.ID == id {
				return true
			}
		}
		return false
	}
	switch input.Action {
	case constants.IMActionCreateDelivery:
		if workflow {
			if err := s.ValidateWorkflowTarget(ctx, input.TargetID); err != nil {
				return nil, err
			}
		}
		if !allowed(input.TargetID) || input.RequestID == "" || len(input.RequestID) > 100 {
			return nil, status_error.IMChatNotAuthorized
		}
		scope := lease.executionID
		if workflow {
			scope = runID
		}
		delivery, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "tool:" + scope + ":" + input.RequestID, TargetID: input.TargetID, Content: input.Content, Files: input.Files, AllowedRoots: lease.roots, ChatID: lease.chatID, ExecutionID: lease.executionID, WorkflowRunID: runID, WorkflowDefinitionID: definitionID})
		if err != nil {
			return nil, err
		}
		return DeliverySummary(delivery), nil
	case constants.IMActionGetDelivery:
		delivery, err := s.config.Store.Delivery(ctx, input.DeliveryID)
		if err != nil {
			return nil, err
		}
		same := delivery.ExecutionID == lease.executionID && delivery.ChatID == lease.chatID && lease.executionID != ""
		if workflow {
			same = delivery.WorkflowRunID == runID && runID != ""
		}
		if !same || !allowed(delivery.BindingID) {
			return nil, status_error.IMChatNotAuthorized
		}
		return DeliverySummary(delivery), nil
	default:
		return nil, status_error.IMConfigurationInvalid
	}
}

// WorkflowCall 在 Runner 的冻结范围内调用同一交付能力，创建动作等待独立发送终态。
func (s *Service) WorkflowCall(ctx context.Context, run models.WorkflowRun, call models.WorkflowActionCall) (any, error) {
	if call.ToolName != constants.IMBuiltinToolName {
		return nil, status_error.WorkflowCapabilityUnsupported
	}
	encoded, err := json.Marshal(call.Input)
	if err != nil {
		return nil, status_error.IMConfigurationInvalid
	}
	input, err := protocol.DecodeToolInput(encoded)
	if err != nil {
		return nil, status_error.IMConfigurationInvalid
	}
	// Runner 调用身份拥有幂等键，模型无法在重启时换键重发。
	input.RequestID = call.ID
	var data map[string]any
	existing := false
	if input.Action == constants.IMActionCreateDelivery {
		_, existing, err = s.config.Store.FindDelivery(ctx, "tool:"+run.ID+":"+call.ID)
		if err == nil && existing {
			// 恢复已经登记的调用只核验原载荷，不把之后的权限变化写成已完成发送的失败。
			// 未完成的投递仍由发送循环按最新授权决定是否发送下一片段。
			var delivery models.IMDelivery
			delivery, err = s.CreateDelivery(ctx, DeliveryRequest{RequestID: "tool:" + run.ID + ":" + call.ID, TargetID: input.TargetID, Content: input.Content, Files: input.Files, AllowedRoots: []string{run.ScopeBinding.WorkingDirectory}, WorkflowRunID: run.ID, WorkflowDefinitionID: run.DefinitionID})
			if err == nil {
				data = DeliverySummary(delivery)
			}
		}
	}
	if err == nil && !existing {
		data, err = s.callCapability(ctx, capabilityLease{roots: []string{run.ScopeBinding.WorkingDirectory}}, true, run.ID, run.DefinitionID, input)
	}
	if err != nil {
		cause := map[string]any{"code": errorCode(err)}
		var detail *statuserror.StatusErr
		if errors.As(err, &detail) {
			for _, key := range []string{"targetId", "hint", "field", "reason"} {
				if value := detail.Fields[key]; value != "" {
					cause[key] = value
				}
			}
		}
		params := map[string]any{"targetId": input.TargetID, "action": input.Action, "cause": cause}
		if input.Action == constants.IMActionCreateDelivery {
			if previous, found, lookupErr := s.config.Store.FindDelivery(ctx, "tool:"+run.ID+":"+call.ID); lookupErr == nil && found {
				data = DeliverySummary(previous)
				params["delivery"] = data
			}
		}
		return data, status_error.WorkflowEffectFailed.WithField("code", "TOOL_FAILED").WithField("reason", errorCode(err)).WithParams(params)
	}
	if input.Action == constants.IMActionCreateDelivery {
		id, _ := data["deliveryId"].(string)
		if s.config.Events == nil {
			return data, status_error.IMServiceUnavailable
		}
		subscription := s.config.Events.Subscribe("im", events.Resource{Kind: "delivery", ID: id})
		defer subscription.Close()
		for {
			delivery, err := s.config.Store.Delivery(ctx, id)
			if err != nil {
				return data, err
			}
			data = DeliverySummary(delivery)
			switch delivery.State {
			case constants.IMDeliveryStateSent:
				return data, nil
			case constants.IMDeliveryStateFailed, constants.IMDeliveryStateUnknown:
				return data, status_error.WorkflowEffectFailed.WithField("code", "TOOL_FAILED").WithParams(map[string]any{"delivery": data})
			}
			select {
			case <-ctx.Done():
				return data, ctx.Err()
			case _, open := <-subscription.Events:
				if !open {
					return data, status_error.IMServiceUnavailable
				}
			}
		}
	}
	return data, nil
}
