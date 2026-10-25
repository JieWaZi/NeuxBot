package events

import (
	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	eventhub "github.com/JieWaZi/NeuxBot/server/internal/notification/events"
)

func validateResource(ctx context.Context, command control) error {
	resource := command.Resource
	if resource.Kind == "global" {
		return nil
	}
	var err error
	switch command.Topic {
	case "knowledge":
		if resource.ParentID == "local" {
			if global.KnowledgeEngine == nil {
				return status_error.KnowledgeUnsupported
			}
			_, err = global.KnowledgeEngine.GetLibrary(ctx, resource.ID)
		} else {
			if global.KnowledgeConnectionControllerManager == nil {
				return status_error.KnowledgeUnsupported
			}
			connection, readErr := global.KnowledgeConnectionControllerManager.Get(ctx, resource.ParentID)
			if readErr != nil {
				return readErr
			}
			if !connection.Enabled {
				return status_error.KnowledgeForbidden
			}
		}
	case "resources":
		switch resource.Kind {
		case "skill":
			_, err = global.SkillControllerManager.GetPackage(ctx, resource.ID)
		case "connector":
			_, err = global.MCPConnectorControllerManager.Get(ctx, resource.ID)
		case "waker":
			_, err = global.WakerControllerManager.Get(ctx, resource.ID)
		}
	case "plugins":
		_, err = global.PluginControllerManager.Get(ctx, resource.ID)
	case "chat":
		_, err = global.ChatControllerManager.Get(ctx, resource.ID)
	case "team":
		thread, readErr := global.ChannelThreadControllerManager.Get(ctx, resource.ID)
		err = readErr
		if err == nil && thread.ChannelID != resource.ParentID {
			err = status_error.ChannelThreadNotFound
		}
	case "workflow":
		_, err = global.WorkflowControllerManager.GetDefinition(ctx, resource.ID)
		if err == nil && resource.ParentID != "" {
			run, readErr := global.WorkflowRunControllerManager.GetRun(ctx, resource.ParentID)
			err = readErr
			if err == nil && run.DefinitionID != resource.ID {
				err = status_error.WorkflowNotFound
			}
		}
	case "work-board", "work-results":
		switch resource.Kind {
		case "chat":
			_, err = global.ChatControllerManager.Get(ctx, resource.ID)
		case "team_thread":
			_, err = global.ChannelThreadControllerManager.Get(ctx, resource.ID)
		case "workflow_run":
			_, err = global.WorkflowRunControllerManager.GetRun(ctx, resource.ID)
		}
	case "entities", "automatic-tasks":
		switch resource.Kind {
		case "waker":
			_, err = global.WakerControllerManager.Get(ctx, resource.ID)
		case "channel":
			_, err = global.ChannelControllerManager.Get(ctx, resource.ID)
		case "automatic_task":
			_, err = global.AutomaticTaskControllerManager.Get(ctx, resource.ID)
		}
	case "im":
		switch resource.Kind {
		case "connection":
			_, err = global.IMControllerManager.Connection(ctx, resource.ID)
		case "binding":
			_, err = global.IMControllerManager.Binding(ctx, resource.ID)
		case "conversation":
			_, err = global.IMControllerManager.Link(ctx, resource.ID)
		case "login":
			_, err = global.IMService.Login(resource.ID)
		}
	case "runtimes":
		_, err = global.RuntimeEnvironmentControllerManager.Get(ctx, resource.ID)
	case "account-login":
		_, err = global.AIAccountService.Get(ctx, resource.ID)
	}
	return err
}
func newSource(ctx context.Context, command control) (source, error) {
	if err := validateResource(ctx, command); err != nil {
		return source{}, err
	}
	switch command.Topic {
	case "chat":
		return chatSource(ctx, command)
	case "team":
		return teamSource(ctx, command)
	case "workflow":
		return workflowSource(ctx, command)
	default:
		return stateSource(ctx, command)
	}
}
func encodedFrame(kind string, resource eventhub.Resource, payload any) (frame, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return frame{}, status_error.InternalServerError
	}
	return frame{Type: kind, Resource: resource, Payload: encoded}, nil
}
func fromHub(value eventhub.Event) frame {
	result := frame{Type: value.Type, Resource: value.Resource, EventID: value.EventID, Transient: value.Transient, Payload: value.Payload}
	if value.SourceCursor != "" {
		result.SourceCursor = stringJSON(value.SourceCursor)
	}
	return result
}
func chatSource(ctx context.Context, command control) (source, error) {
	after, err := numberCursor(command.After)
	if err != nil {
		return source{}, err
	}
	subscription, err := global.ChatService.SubscribeEvents(ctx, command.Resource.ID, after)
	if err != nil {
		return source{}, err
	}
	return source{close: subscription.Close, run: func(ctx context.Context, emit func(frame, bool) bool) {
		for _, value := range subscription.Backlog {
			output, err := encodedFrame("chat.event", command.Resource, value)
			if err != nil {
				return
			}
			output.Transient = value.Transient
			if !value.Transient {
				output.SourceCursor = numericJSON(value.Sequence)
			}
			if !emit(output, true) {
				return
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case value, open := <-subscription.Events:
				if !open {
					emit(resyncFrame(command, "gap"), false)
					return
				}
				output, err := encodedFrame("chat.event", command.Resource, value)
				if err != nil {
					emit(resyncFrame(command, "gap"), false)
					return
				}
				output.Transient = value.Transient
				if !value.Transient {
					output.SourceCursor = numericJSON(value.Sequence)
				}
				if !emit(output, false) {
					return
				}
			}
		}
	}}, nil
}
func teamSource(ctx context.Context, command control) (source, error) {
	after, err := stringCursor(command.After)
	if err != nil {
		return source{}, err
	}
	subscription, err := global.ChannelService.SubscribeEvents(ctx, command.Resource.ID, after)
	if err != nil {
		return source{}, err
	}
	return source{close: subscription.Close, run: func(ctx context.Context, emit func(frame, bool) bool) {
		for _, value := range subscription.Backlog {
			output, err := encodedFrame("team.event", command.Resource, value)
			if err != nil {
				return
			}
			output.SourceCursor = stringJSON(value.ID)
			if !emit(output, true) {
				return
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case value, open := <-subscription.Events:
				if !open {
					emit(resyncFrame(command, "gap"), false)
					return
				}
				output, err := encodedFrame("team.event", command.Resource, value)
				if err != nil {
					emit(resyncFrame(command, "gap"), false)
					return
				}
				output.SourceCursor = stringJSON(value.ID)
				if !emit(output, false) {
					return
				}
			}
		}
	}}, nil
}
func resyncFrame(command control, reason string) frame {
	value, _ := encodedFrame("resync_required", command.Resource, map[string]string{"reason": reason})
	return value
}
func workflowSource(ctx context.Context, command control) (source, error) {
	after, err := numberCursor(command.After)
	if err != nil {
		return source{}, err
	}
	streamID := command.Resource.ID
	if command.Resource.ParentID != "" {
		streamID = command.Resource.ParentID
	}
	// 先监听提交后唤醒，再补历史；此处没有任何周期事件表查询。
	subscription := global.EventHub.Subscribe("workflow", eventhub.Resource{Kind: "workflow", ID: streamID})
	return source{close: subscription.Close, run: func(ctx context.Context, emit func(frame, bool) bool) {
		cursor := after
		read := func() bool {
			for {
				records, err := global.WorkflowControllerManager.ListEvents(ctx, streamID, cursor, 500)
				if err != nil {
					emit(resyncFrame(command, "gap"), false)
					return false
				}
				for _, value := range records {
					if value.Sequence <= cursor {
						continue
					}
					output, err := encodedFrame("workflow.event", command.Resource, value)
					if err != nil {
						return false
					}
					output.SourceCursor = numericJSON(value.Sequence)
					output.EventID = value.EventID
					if !emit(output, true) {
						return false
					}
					cursor = value.Sequence
				}
				if len(records) < 500 {
					return true
				}
			}
		}
		if !read() {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case _, open := <-subscription.Events:
				if !open {
					emit(resyncFrame(command, "gap"), false)
					return
				}
				if !read() {
					return
				}
			}
		}
	}}, nil
}
func stateSource(ctx context.Context, command control) (source, error) {
	topic := command.Topic
	resource := command.Resource
	if topic == "system" {
		topic = "im"
		resource = eventhub.Resource{Kind: "global"}
	}
	subscription := global.EventHub.Subscribe(topic, resource)
	initial, err := initialState(ctx, command)
	if err != nil {
		subscription.Close()
		return source{}, err
	}
	return source{close: subscription.Close, run: func(ctx context.Context, emit func(frame, bool) bool) {
		for _, value := range initial {
			if !emit(value, true) {
				return
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case event, open := <-subscription.Events:
				if !open {
					emit(resyncFrame(command, "gap"), false)
					return
				}
				if command.Topic == "system" {
					values, err := initialState(ctx, command)
					if err != nil {
						emit(resyncFrame(command, "gap"), false)
						return
					}
					for _, value := range values {
						if !emit(value, false) {
							return
						}
					}
					continue
				}
				if command.Topic == "work-results" {
					value, err := resultFrame(ctx, event.Resource)
					if err != nil {
						emit(resyncFrame(command, "gap"), false)
						return
					}
					if !emit(value, false) {
						return
					}
					continue
				}
				if !emit(fromHub(event), false) {
					return
				}
			}
		}
	}}, nil
}
func initialState(ctx context.Context, command control) ([]frame, error) {
	var value frame
	var err error
	switch command.Topic {
	case "notifications":
		snapshot, readErr := global.NotificationService.Snapshot()
		err = readErr
		if err == nil {
			value, err = encodedFrame("notifications.snapshot", command.Resource, map[string]any{"snapshot": snapshot})
		}
	case "work-results":
		value, err = resultFrame(ctx, command.Resource)
	case "system":
		awake, readErr := global.IMService.KeepAwake(ctx)
		err = readErr
		if err == nil {
			value, err = encodedFrame("system.updated", command.Resource, map[string]bool{"keepAwake": awake})
		}
	case "im":
		if command.Resource.Kind == "login" {
			login, readErr := global.IMService.Login(command.Resource.ID)
			err = readErr
			if err == nil {
				value, err = encodedFrame("im.login.updated", command.Resource, map[string]any{"login": login})
			}
		}
	case "account-login":
		login, readErr := global.AIAccountService.GetLogin(ctx, command.Resource.ID, command.Resource.ParentID)
		err = readErr
		if err == nil {
			value, err = encodedFrame("account-login.updated", command.Resource, map[string]any{"login": login})
		}
	}
	if err != nil {
		return nil, err
	}
	if value.Type == "" {
		// 已建立提交监听后再告知界面读取权威事实，覆盖首次 GET 与注册之间的变化。
		value, err = encodedFrame(command.Topic+".ready", command.Resource, struct{}{})
		if err != nil {
			return nil, err
		}
	}
	return []frame{value}, nil
}
func resultFrame(ctx context.Context, resource eventhub.Resource) (frame, error) {
	if resource.Kind == "global" {
		return encodedFrame("work-results.changed", resource, map[string]bool{"reset": true})
	}
	receipt, found, err := global.WorkResultControllerManager.Get(ctx, constants.WorkResultOwnerKind(resource.Kind), resource.ID)
	if err != nil {
		return frame{}, err
	}
	payload := map[string]any{"ownerKind": resource.Kind, "ownerId": resource.ID}
	if found {
		payload["receipt"] = receipt
	} else {
		payload["deleted"] = true
	}
	return encodedFrame("work-results.changed", resource, payload)
}
