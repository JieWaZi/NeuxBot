package im

import (
	"context"
	"path/filepath"
	"sort"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// AgentBindings 将 IM 真实范围与耐久投递绑定到统一命令入口。
func (s *Service) AgentBindings(chat models.Chat, executionID string, roots []string, hub *events.Hub, replyLinkID string) []capability.Binding {
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
	readAccess := func(ctx context.Context, delivery models.IMDelivery) error {
		if delivery.ChatID != chat.ID {
			return status_error.IMChatNotAuthorized
		}
		if replyLinkID == "" || delivery.LinkID != replyLinkID {
			return allowed(ctx, delivery.BindingID)
		}
		link, err := s.config.Store.Link(ctx, replyLinkID)
		if err != nil {
			return err
		}
		if link.BindingID != delivery.BindingID {
			return status_error.IMChatNotAuthorized
		}
		_, err = s.authorizedDeliveryBinding(ctx, link.BindingID, false)
		return err
	}
	result := func(delivery models.IMDelivery, replayed bool) capability.Result {
		summary := DeliverySummary(delivery)
		summary["deliveryId"] = delivery.ID
		return capability.Result{CommandID: delivery.ID, Replayed: replayed, Data: summary}
	}
	read := func(ctx context.Context, id string) (models.IMDelivery, error) {
		delivery, err := s.config.Store.Delivery(ctx, id)
		if err != nil {
			return delivery, err
		}
		if err := readAccess(ctx, delivery); err != nil {
			return models.IMDelivery{}, err
		}
		return delivery, nil
	}
	bindings := []capability.Binding{{Operation: "im.targets.list", Call: func(ctx context.Context, input capability.Request) (capability.Result, error) {
		targets, err := s.Targets(ctx, chat.WakerID, false)
		if err != nil {
			return capability.Result{}, err
		}
		sort.SliceStable(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
		page, err := capability.Page(targets, input.Params)
		return capability.Result{Data: page}, err
	}}, {Operation: "im.deliveries.create", Call: func(ctx context.Context, input capability.Request) (capability.Result, error) {
		var params struct {
			// TargetID 是显式接收目标；IM 文件回复可沿用已绑定的聊天。
			TargetID string `json:"targetId"`
			// Content 是要冻结的完整正文。
			Content string `json:"content"`
			// Files 是需要按真实工作范围冻结的原文件。
			Files []string `json:"files"`
		}
		if err := capability.DecodeParams(input.Params, &params); err != nil {
			return capability.Result{}, err
		}
		implicitReply := params.TargetID == ""
		linkID := ""
		if replyLinkID != "" && len(params.Files) > 0 {
			link, err := s.config.Store.Link(ctx, replyLinkID)
			if err != nil && (params.TargetID == "" || !hasCode(err, status_error.IMResourceNotFound)) {
				return capability.Result{}, err
			}
			if err == nil && (params.TargetID == "" || params.TargetID == link.BindingID) {
				params.TargetID, linkID = link.BindingID, link.ID
			}
		}
		if params.TargetID == "" {
			return capability.Result{}, status_error.AgentInputInvalid
		}
		if implicitReply {
			// 原工作已由 IM 接纳；对原会话的答复沿用该授权，不受之后的团队名单变动影响。
			if _, err := s.authorizedDeliveryBinding(ctx, params.TargetID, false); err != nil {
				return capability.Result{}, err
			}
		} else if err := allowed(ctx, params.TargetID); err != nil {
			return capability.Result{}, err
		}
		previous, exists, err := s.config.Store.FindDelivery(ctx, key(input.RequestID))
		if err != nil {
			return capability.Result{}, err
		}
		origin := executionID
		if exists {
			origin = previous.ExecutionID
		}
		delivery, err := s.CreateDelivery(ctx, DeliveryRequest{VerifyFileContent: true, RequestID: key(input.RequestID), TargetID: params.TargetID, Content: params.Content, Files: params.Files, AllowedRoots: roots, AllowedFiles: capability.FilePaths(ctx), ManagedSpace: chat.WorkspaceDirectory, LinkID: linkID, ChatID: chat.ID, ExecutionID: origin})
		return result(delivery, exists), err
	}, Receipt: func(ctx context.Context, requestID string) (capability.Result, error) {
		delivery, exists, err := s.config.Store.FindDelivery(ctx, key(requestID))
		if err != nil {
			return capability.Result{}, err
		}
		if !exists {
			return capability.Result{}, status_error.IMResourceNotFound
		}
		if err := readAccess(ctx, delivery); err != nil {
			return capability.Result{}, err
		}
		return result(delivery, true), nil
	}}, {Operation: "im.deliveries.get", Call: func(ctx context.Context, input capability.Request) (capability.Result, error) {
		id, _ := input.Params["deliveryId"].(string)
		delivery, err := read(ctx, id)
		return result(delivery, false), err
	}}}
	if hub != nil {
		bindings = append(bindings, capability.Binding{Operation: "im.deliveries.wait", Call: func(ctx context.Context, input capability.Request) (capability.Result, error) {
			id, _ := input.Params["deliveryId"].(string)
			return capability.Observe(ctx, input.Params, func() events.Subscription { return hub.Subscribe("im", events.Resource{Kind: "delivery", ID: id}) }, func(ctx context.Context) (any, bool, error) {
				delivery, err := read(ctx, id)
				terminal := delivery.State == constants.IMDeliveryStateSent || delivery.State == constants.IMDeliveryStateFailed || delivery.State == constants.IMDeliveryStateUnknown
				return result(delivery, false).Data, terminal, err
			})
		}})
	}
	return bindings
}
