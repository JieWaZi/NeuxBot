package runtimehost

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp/native"
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
)

// technicalSessionPlan 仅转换静态业务事实；当前消息和实际连接不进入会话声明。
func technicalSessionPlan(r runtimecontract.SessionPlan) (c.SessionPlan, error) {
	base := sessionBase(r)
	mode, err := chatNativeMode(base, r.Policy.Execution)
	if err != nil {
		return c.SessionPlan{}, err
	}
	plan := c.SessionPlan{
		Profile:       c.Profile{DriverID: r.Target.RuntimeID, Subject: r.Target.AccountID, Model: nativeModelID(base, r.Target.AccountID, r.Policy.Model), Reasoning: r.Policy.Reasoning, Thinking: r.Policy.Thinking, NativeMode: mode},
		Workspace:     c.WorkspaceScope{Root: r.Workspace.Path, Directories: append([]string(nil), r.Workspace.AdditionalWritableDirectories...), ReadOnly: r.Policy.Execution.WorkspaceReadOnly},
		Policies:      c.SessionPolicies{Tools: c.ToolPolicy{Mode: c.ToolsAllowAll}, NativeConfig: c.NativeConfigInherit},
		ResourceKey:   r.ResourceKey,
		Configuration: c.SessionConfiguration{Binary: r.Target.Binary, Arguments: r.Target.PrefixArgs, ConfigHome: r.Target.ConfigHome, PluginDirectories: r.Target.NativePluginDirectories, PluginScope: r.Target.NativePluginScope, Options: r.Target.CodexPluginConfiguration, SettingsPath: r.Target.ClaudeSettingsPath},
		Grants:        c.Grants{Directories: append([]string{r.Workspace.Path}, r.Workspace.AdditionalWritableDirectories...), AllowAllTools: true},
		Tags:          []string{"account:" + r.Target.AccountID},
	}
	for _, id := range r.PluginIDs {
		plan.Tags = append(plan.Tags, "plugin:"+id)
	}
	for _, instruction := range r.Instructions {
		plan.Contributions.Instructions = append(plan.Contributions.Instructions, c.Instruction{Name: instruction.Name, Text: instruction.Content})
	}
	for _, skill := range r.Skills {
		id := "skill:" + skill.Name
		plan.Contributions.Skills = append(plan.Contributions.Skills, c.Skill{Name: skill.Name, ResourceID: id, Version: skill.Path})
	}
	for _, connector := range r.Connectors {
		name := connectorName(connector.ID, connector.Name, connector.RuntimeKey)
		id := "mcp:" + connector.ID
		if connector.ID == "" {
			id = "mcp:" + name
		}
		// 临时凭据不进入公开连接版本；兼容性只由固定连接配置确定。
		public, _ := json.Marshal([]any{connector.Transport, connector.Command, connector.Arguments, connector.URL})
		version := sha256.Sum256(public)
		plan.Contributions.MCP = append(plan.Contributions.MCP, c.MCP{Name: name, ResourceID: id, Version: hex.EncodeToString(version[:])})
	}
	return plan, nil
}

// technicalTurnInput 只转换本轮新增内容与明确选择，不重建会话静态描述。
func technicalTurnInput(workspace string, resources runtimecontract.SessionResources, turn runtimecontract.TurnInput) c.Input {
	input := c.Input{Text: turn.Message.Text, SelectedSkills: append([]string(nil), turn.SelectedSkills...)}
	for _, attachment := range turn.Message.Attachments {
		path := attachment.RuntimePath
		if path == "" {
			path = filepath.Join(workspace, attachment.Path)
		}
		input.Attachments = append(input.Attachments, c.Attachment{Name: attachment.Name, Path: path, DisplayPath: attachment.Path, MIMEType: attachment.MediaType})
	}
	for _, reference := range turn.Message.CodeReferences {
		input.CodeReferences = append(input.CodeReferences, c.CodeReference{Path: reference.Path, StartLine: reference.StartLine, EndLine: reference.EndLine, Content: reference.Content})
	}
	selected := make(map[string]bool, len(turn.SelectedConnectors))
	for _, id := range turn.SelectedConnectors {
		selected[id] = true
	}
	for _, connector := range resources.Connectors {
		if selected[connector.ID] {
			input.SelectedMCP = append(input.SelectedMCP, connectorName(connector.ID, connector.Name, connector.RuntimeKey))
		}
	}
	return input
}

func connectorName(id, name, runtimeKey string) string {
	if runtimeKey != "" {
		return runtimeKey
	}
	if name != "" {
		return name
	}
	return id
}
func sessionBase(plan runtimecontract.SessionPlan) string {
	if plan.Detection.Runtime.BaseRuntimeID != "" {
		return plan.Detection.Runtime.BaseRuntimeID
	}
	if plan.Detection.Runtime.ID != "" {
		return plan.Detection.Runtime.ID
	}
	return plan.Target.RuntimeID
}
func chatNativeMode(base string, policy runtimecontract.ExecutionPolicy) (string, error) {
	descriptor, ok := native.Lookup(base)
	if !ok {
		return "", status_error.RuntimeUnavailable
	}
	modes := chatModeCandidates(base, policy)
	if policy.WorkspaceReadOnly && len(modes) == 0 {
		return "", status_error.RuntimePolicyUnsupported
	}
	if !policy.WorkspaceReadOnly && descriptor.Modes.Negotiate {
		return "", nil
	}
	if len(modes) == 0 {
		return "", nil
	}
	return modes[0], nil
}
func chatModeCandidates(base string, policy runtimecontract.ExecutionPolicy) []string {
	descriptor, ok := native.Lookup(base)
	if !ok {
		return nil
	}
	switch {
	case policy.WorkspaceReadOnly:
		return descriptor.Modes.ReadOnly
	case policy.PermissionLevel == runtimeconfig.PermissionLevelFullAccess:
		return descriptor.Modes.Unrestricted
	case policy.PermissionLevel == runtimeconfig.PermissionLevelAuto:
		return descriptor.Modes.Automatic
	default:
		return descriptor.Modes.Default
	}
}
