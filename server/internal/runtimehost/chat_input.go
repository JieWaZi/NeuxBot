package runtimehost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	"github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp/native"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimeauth"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// chatSpec 将产品已冻结的输入和授权转换为无秘密的独立技术描述。
func chatSpec(r runtimecontract.RunRequest) (c.ExecutionSpec, []c.ResourceBinding, error) {
	base := r.Detection.Runtime.BaseRuntimeID
	if base == "" {
		base = r.Detection.Runtime.ID
	}
	if base == "" {
		base = r.Target.RuntimeID
	}
	mode, err := chatNativeMode(base, r.Policy.Execution)
	if err != nil {
		return c.ExecutionSpec{}, nil, err
	}
	spec := c.ExecutionSpec{Profile: c.Profile{DriverID: r.Target.RuntimeID, CredentialRef: r.Target.AccountID + ":" + r.Target.RuntimeID, Subject: r.Target.AccountID, Model: nativeModelID(base, r.Target.AccountID, r.Policy.Model), Reasoning: r.Policy.Reasoning, NativeMode: mode}, Input: c.Input{Text: r.Message.Text}, Workspace: c.WorkspaceScope{Root: r.Workspace.Path, Directories: append([]string(nil), r.Workspace.AdditionalWritableDirectories...), ReadOnly: r.Policy.Execution.WorkspaceReadOnly}, Policies: c.Policies{Tools: c.ToolPolicy{Mode: c.ToolsAllowAll}, NativeConfig: c.NativeConfigInherit, Interaction: c.InteractionInteractive, Output: c.OutputBestEffort}}
	for _, attachment := range r.Message.Attachments {
		path := attachment.RuntimePath
		if path == "" {
			path = filepath.Join(r.Workspace.Path, attachment.Path)
		}
		spec.Input.Attachments = append(spec.Input.Attachments, c.Attachment{Name: attachment.Name, Path: path, DisplayPath: attachment.Path, MIMEType: attachment.MediaType})
	}
	for _, reference := range r.Message.CodeReferences {
		spec.Input.CodeReferences = append(spec.Input.CodeReferences, c.CodeReference{Path: reference.Path, StartLine: reference.StartLine, EndLine: reference.EndLine, Content: reference.Content})
	}
	for _, instruction := range r.Instructions {
		spec.Contributions.Instructions = append(spec.Contributions.Instructions, c.Instruction{Name: instruction.Name, Text: instruction.Content})
	}
	bindings := make([]c.ResourceBinding, 0, len(r.Skills)+len(r.Connectors))
	for _, skill := range r.Skills {
		id := "skill:" + skill.Name
		spec.Contributions.Skills = append(spec.Contributions.Skills, c.Skill{Name: skill.Name, ResourceID: id, Version: skill.Path})
		bindings = append(bindings, c.ResourceBinding{ID: id, Path: skill.Path})
		if skill.Selected {
			spec.Input.SelectedSkills = append(spec.Input.SelectedSkills, skill.Name)
		}
	}
	for _, connector := range r.Connectors {
		name := connector.RuntimeKey
		if name == "" {
			name = connector.Name
		}
		if name == "" {
			name = connector.ID
		}
		id := "mcp:" + connector.ID
		if connector.ID == "" {
			id = "mcp:" + name
		}
		// 版本只由公开连接配置决定，临时凭据和秘密请求头不进入静态摘要。
		public, _ := json.Marshal([]any{connector.Transport, connector.Command, connector.Arguments, connector.URL})
		version := sha256.Sum256(public)
		spec.Contributions.MCP = append(spec.Contributions.MCP, c.MCP{Name: name, ResourceID: id, Version: hex.EncodeToString(version[:])})
		transport := c.MCPTransport(connector.Transport)
		if connector.Transport == runtimecontract.ConnectorTransportStreamableHTTP {
			transport = c.MCPHTTP
		}
		bindings = append(bindings, c.ResourceBinding{ID: id, MCP: &c.MCPConnection{Transport: transport, Command: connector.Command, Arguments: append([]string(nil), connector.Arguments...), Environment: connector.Environment, URL: connector.URL, Headers: connector.Headers}})
		if connector.Selected {
			spec.Input.SelectedMCP = append(spec.Input.SelectedMCP, name)
		}
	}
	if len(spec.Input.SelectedSkills) > 0 {
		var commands []string
		for _, skill := range spec.Input.SelectedSkills {
			switch base {
			case runtimecontract.RuntimeIDCodex:
				commands = append(commands, "$"+skill)
			case runtimecontract.RuntimeIDKimi, runtimecontract.RuntimeIDPi:
				commands = append(commands, "/skill:"+skill)
			default:
				commands = append(commands, "/"+skill)
			}
		}
		spec.Input.Text = strings.Join(commands, " ") + "\n\n" + spec.Input.Text
	}
	return spec, bindings, nil
}

func chatNativeMode(base string, policy runtimecontract.ExecutionPolicy) (string, error) {
	readonly := policy.WorkspaceReadOnly
	full, automatic := policy.PermissionLevel == runtimecontract.PermissionLevelFullAccess, policy.PermissionLevel == runtimecontract.PermissionLevelAuto
	switch base {
	case runtimecontract.RuntimeIDCodex:
		if readonly || !full && !automatic {
			return "read-only", nil
		}
		if full {
			return "agent-full-access", nil
		}
		return "agent", nil
	case runtimecontract.RuntimeIDClaude:
		if readonly || !full && !automatic {
			return "default", nil
		}
		if full {
			return "bypassPermissions", nil
		}
		return "auto", nil
	case runtimecontract.RuntimeIDKimi:
		if readonly {
			return "plan", nil
		}
		return "", nil
	case runtimecontract.RuntimeIDCursor:
		if readonly {
			return "plan", nil
		}
		return "agent", nil
	case runtimecontract.RuntimeIDGemini:
		if full && !readonly {
			return "yolo", nil
		}
		return "default", nil
	case runtimecontract.RuntimeIDWorkBuddy:
		if readonly {
			return "plan", nil
		}
		if full {
			return "bypassPermissions", nil
		}
		if automatic {
			return "auto", nil
		}
		return "default", nil
	case runtimecontract.RuntimeIDQoder, runtimecontract.RuntimeIDQoderCN:
		if readonly {
			return "", status_error.RuntimePolicyUnsupported
		}
		return "", nil
	case runtimecontract.RuntimeIDPi, runtimecontract.RuntimeIDOpenCode, runtimecontract.RuntimeIDGrok:
		if readonly {
			return "", status_error.RuntimePolicyUnsupported
		}
		return "", nil
	default:
		return "", status_error.RuntimeUnavailable
	}
}

func chatModeCandidates(base string, policy runtimecontract.ExecutionPolicy) []string {
	if base == runtimecontract.RuntimeIDKimi {
		if policy.WorkspaceReadOnly {
			return []string{"plan"}
		}
		if policy.PermissionLevel == runtimecontract.PermissionLevelFullAccess {
			return []string{"yolo"}
		}
		if policy.PermissionLevel == runtimecontract.PermissionLevelAuto {
			return []string{"auto"}
		}
		return []string{"default"}
	}
	if base == runtimecontract.RuntimeIDQoder || base == runtimecontract.RuntimeIDQoderCN {
		if policy.PermissionLevel == runtimecontract.PermissionLevelFullAccess {
			return []string{constants.RuntimeQoderYoloModeID, constants.RuntimeQoderBypassModeID}
		}
		if policy.PermissionLevel == runtimecontract.PermissionLevelAuto {
			return []string{constants.RuntimeQoderAutoModeID}
		}
		return []string{constants.RuntimeQoderDefaultModeID}
	}
	return nil
}

// chatCredentials 消费Execution已冻结的启动事实，不创建第二套账号权威。
type chatCredentials struct {
	// provider 复用原生刷新生命周期，但不拥有普通Chat工作区目录。
	provider *credentialProvider
}

func (p *chatCredentials) Acquire(_ context.Context, _ c.CredentialRequest) (c.CredentialLease, error) {
	values := make(map[string]string, len(p.provider.launch.Environment))
	for _, pair := range p.provider.launch.Environment {
		if key, value, ok := strings.Cut(pair, "="); ok {
			values[key] = value
		}
	}
	p.provider.values = values
	return p, nil
}
func (p *chatCredentials) Credential() c.Credential {
	return c.Credential{Environment: p.provider.values}
}
func (p *chatCredentials) Close(ctx context.Context) error {
	owner := p.provider
	if owner.broker != nil {
		if err := owner.broker.Close(ctx); err != nil {
			return err
		}
		owner.broker = nil
	}
	if owner.sourceAuth != "" {
		unlock, err := runtimeauth.CodexFileCredentialGate(owner.sourceAuth).Lock(ctx)
		if err != nil {
			return err
		}
		_, err = runtimeauth.ReconcileCodexFileAuth(owner.sourceAuth)
		unlock()
		if err != nil {
			return err
		}
		owner.sourceAuth = ""
	}
	return nil
}
func (p *chatCredentials) agent(ctx context.Context, request c.OpenRequest, permission runtimecontract.PermissionLevel) (acp.Agent, error) {
	owner := p.provider
	values := request.Credential.Environment
	if owner.environment.BaseRuntimeID != runtimecontract.RuntimeIDCodex {
		return owner.nativeAgentWithPermission(ctx, request, values, permission)
	}
	refresh := owner.target.RefreshCredentials
	if refresh == nil {
		var err error
		owner.sourceAuth, err = runtimeauth.CodexAuthPathForEnvironment(environmentList(values))
		if err != nil {
			return nil, err
		}
		refresh, err = runtimeauth.CodexFileRefresh(owner.sourceAuth, nil)
		if err != nil {
			return nil, err
		}
	}
	if refresh != nil {
		var err error
		owner.broker, err = runtimeauth.NewCodexRefreshBroker(refresh)
		if err != nil {
			return nil, err
		}
		values[runtimeauth.CodexRefreshURLVariable] = owner.broker.URL()
	}
	args := append([]string(nil), owner.launch.PrefixArgs...)
	keys := make([]string, 0, len(owner.target.CodexPluginConfiguration))
	for key := range owner.target.CodexPluginConfiguration {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "-c", key+"="+owner.target.CodexPluginConfiguration[key])
	}
	return native.New(ctx, native.Config{RuntimeID: owner.environment.BaseRuntimeID, Logger: slog.Default(), Executable: owner.launch.Executable, PrefixArgs: args, Environment: environmentList(values), DefaultModeRequestUserInput: constants.RuntimeCodexDefaultModeRequestUserInput})
}
