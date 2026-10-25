package im

import (
	"context"
	"path/filepath"
	"sort"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// AgentBindings 将 IM 真实范围与耐久投递直接交给统一命令入口，不经过 MCP 传输。
func (s *Service) AgentBindings(chat models.Chat, executionID string, roots []string, hub *events.Hub) []agentcapability.Binding {
	frozenRoots := make([]string, 0, len(roots))
	for _, root := range roots {
		if actual, err := filepath.EvalSymlinks(root); err == nil {
			if absolute, err := filepath.Abs(actual); err == nil {
				frozenRoots = append(frozenRoots, absolute)
			}
		}
	}
	roots = frozenRoots
	key := func(requestID string) string { return "cli:" + chat.ID + ":" + chat.WakerID + ":" + requestID }
	allowed := func(ctx context.Context, targetID string) error {
		targets, err := s.Targets(ctx, chat.WakerID, false)
		if err != nil {
			return err
		}
		for _, target := range targets {
			if target.ID == targetID {
				return nil
			}
		}
		return status_error.IMChatNotAuthorized
	}
	result := func(delivery models.IMDelivery, replayed bool) agentcapability.Result {
		summary := DeliverySummary(delivery)
		summary["deliveryId"] = delivery.ID
		return agentcapability.Result{CommandID: delivery.ID, Replayed: replayed, Data: summary}
	}
	read := func(ctx context.Context, id string) (models.IMDelivery, error) {
		delivery, err := s.config.Store.Delivery(ctx, id)
		if err != nil {
			return delivery, err
		}
		if delivery.ChatID != chat.ID {
			return models.IMDelivery{}, status_error.IMChatNotAuthorized
		}
		return delivery, allowed(ctx, delivery.BindingID)
	}
	bindings := []agentcapability.Binding{{Operation: "im.targets.list", Call: func(ctx context.Context, input agentcapability.Request) (agentcapability.Result, error) {
		targets, err := s.Targets(ctx, chat.WakerID, false)
		if err != nil {
			return agentcapability.Result{}, err
		}
		sort.SliceStable(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
		page, err := agentcapability.Page(targets, input.Params)
		return agentcapability.Result{Data: page}, err
	}}, {Operation: "im.deliveries.create", Call: func(ctx context.Context, input agentcapability.Request) (agentcapability.Result, error) {
		var params struct {
			// TargetID 是本次明确获准的接收目标。
			TargetID string `json:"targetId"`
			// Content 是要冻结的完整正文。
			Content string `json:"content"`
			// Files 是需要按真实工作范围冻结的原文件。
			Files []string `json:"files"`
		}
		if err := agentcapability.DecodeParams(input.Params, &params); err != nil {
			return agentcapability.Result{}, err
		}
		if err := allowed(ctx, params.TargetID); err != nil {
			return agentcapability.Result{}, err
		}
		previous, exists, err := s.config.Store.FindDelivery(ctx, key(input.RequestID))
		if err != nil {
			return agentcapability.Result{}, err
		}
		origin := executionID
		if exists {
			origin = previous.ExecutionID
		}
		delivery, err := s.CreateDelivery(ctx, DeliveryRequest{VerifyFileContent: true, RequestID: key(input.RequestID), TargetID: params.TargetID, Content: params.Content, Files: params.Files, AllowedRoots: roots, ChatID: chat.ID, ExecutionID: origin})
		return result(delivery, exists), err
	}, Receipt: func(ctx context.Context, requestID string) (agentcapability.Result, error) {
		delivery, exists, err := s.config.Store.FindDelivery(ctx, key(requestID))
		if err != nil {
			return agentcapability.Result{}, err
		}
		if !exists {
			return agentcapability.Result{}, status_error.IMResourceNotFound
		}
		if err := allowed(ctx, delivery.BindingID); err != nil {
			return agentcapability.Result{}, err
		}
		return result(delivery, true), nil
	}}, {Operation: "im.deliveries.get", Call: func(ctx context.Context, input agentcapability.Request) (agentcapability.Result, error) {
		id, _ := input.Params["deliveryId"].(string)
		delivery, err := read(ctx, id)
		return result(delivery, false), err
	}}}
	if hub != nil {
		bindings = append(bindings, agentcapability.Binding{Operation: "im.deliveries.wait", Call: func(ctx context.Context, input agentcapability.Request) (agentcapability.Result, error) {
			id, _ := input.Params["deliveryId"].(string)
			return agentcapability.Observe(ctx, input.Params, func() events.Subscription { return hub.Subscribe("im", events.Resource{Kind: "delivery", ID: id}) }, func(ctx context.Context) (any, bool, error) {
				delivery, err := read(ctx, id)
				terminal := delivery.State == constants.IMDeliveryStateSent || delivery.State == constants.IMDeliveryStateFailed || delivery.State == constants.IMDeliveryStateUnknown
				return result(delivery, false).Data, terminal, err
			})
		}})
	}
	return bindings
}
