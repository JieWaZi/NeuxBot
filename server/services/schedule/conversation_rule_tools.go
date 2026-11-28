package schedule

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
)

// ConversationRuleBindings 只为已核验的会话与执行签发规则命令。
func (s *Service) ConversationRuleBindings(store capability.Store, origin string, scope func(context.Context) (RuleScope, error)) []capability.Binding {
	var bindings []capability.Binding
	for _, operation := range []string{capability.RuleList, capability.RuleGet, capability.RuleCreate, capability.RuleUpdate, capability.RuleDelete} {
		binding := capability.Binding{Operation: operation}
		binding.Call = func(ctx context.Context, request capability.Request) (capability.Result, error) {
			owner, err := scope(ctx)
			if err != nil {
				return capability.Result{}, err
			}
			taskID, _ := request.Params["taskId"].(string)
			switch operation {
			case capability.RuleList:
				rules, err := s.ListConversationRules(ctx, owner)
				return capability.Result{Data: map[string]any{"rules": rules}}, err
			case capability.RuleGet:
				rule, err := s.GetConversationRule(ctx, owner, taskID)
				return capability.Result{Data: rule}, err
			}
			return capability.Execute(ctx, store, origin, request, func(ctx context.Context) error {
				if operation == capability.RuleDelete {
					return s.DeleteConversationRule(ctx, owner, taskID)
				}
				patch, err := decodeRulePatch(request.Params)
				if err != nil {
					return err
				}
				if operation == capability.RuleCreate {
					_, err = s.CreateConversationRule(ctx, owner, patch)
				} else {
					_, err = s.UpdateConversationRule(ctx, owner, taskID, patch)
				}
				return err
			})
		}
		if operation != capability.RuleList && operation != capability.RuleGet {
			binding.Receipt = func(ctx context.Context, requestID string) (capability.Result, error) {
				if _, err := scope(ctx); err != nil {
					return capability.Result{}, err
				}
				return capability.Lookup(ctx, store, origin, operation, requestID)
			}
		}
		bindings = append(bindings, binding)
	}
	return bindings
}

func decodeRulePatch(params map[string]any) (ConversationRulePatch, error) {
	input := make(map[string]any, len(params))
	for key, value := range params {
		if key != "taskId" {
			input[key] = value
		}
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return ConversationRulePatch{}, status_error.AgentInputInvalid
	}
	var patch ConversationRulePatch
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&patch); err != nil {
		return ConversationRulePatch{}, status_error.AgentInputInvalid
	}
	return patch, nil
}
