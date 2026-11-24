package mcpconnector

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/mcpclient"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const (
	managementListLimit    = 24
	managementProbeTimeout = 20 * time.Second
)

type managementInput struct {
	// Query 按名称或描述检索当前可见的连接器。
	Query string `json:"query"`
	// ConnectorID 来自已读取的连接器标识。
	ConnectorID string `json:"connectorId"`
	// ExpectedRevision 标识用户确认及执行时依据的配置。
	ExpectedRevision string `json:"expectedRevision"`
	// Name 指定新名称；编辑时省略则保留。
	Name *string `json:"name"`
	// Description 指定新描述；编辑时省略则保留。
	Description *string `json:"description"`
	// Transport 指定传输方式。
	Transport *constants.MCPTransport `json:"transport"`
	// Command 指定 STDIO 本地可执行命令。
	Command *string `json:"command"`
	// Arguments 指定完整参数列表；编辑时省略则保留。
	Arguments *[]string `json:"arguments"`
	// URL 指定远程 MCP 地址。
	URL *string `json:"url"`
	// ConnectionTimeoutMS 指定 MCP 连接超时，与命令调用超时独立。
	ConnectionTimeoutMS *int64 `json:"connectionTimeoutMs"`
}

// ManagementBindings 绑定当前执行可管理的连接器与实际用户确认入口。
func (s *Service) ManagementBindings(wakerID, executionID string, approve func(context.Context, string) error, changed func(context.Context, string, capability.Result)) []capability.Binding {
	if s == nil || wakerID == "" || executionID == "" {
		return nil
	}
	bindings := make([]capability.Binding, 0, 7)
	scope := wakerID + ":" + executionID
	for _, action := range []string{"list", "get", "create", "update", "test", "detach", "delete"} {
		operation := "mcp.connectors." + action
		switch action {
		case "list", "get", "test":
			bindings = append(bindings, capability.Binding{Operation: operation, Call: func(ctx context.Context, request capability.Request) (capability.Result, error) {
				var input managementInput
				if err := capability.DecodeParams(request.Params, &input); err != nil {
					return capability.Result{}, err
				}
				data, err := s.read(ctx, wakerID, action, input, approve)
				return capability.Result{Data: data}, err
			}})
		default:
			if s.config.Commands == nil || approve == nil {
				continue
			}
			bindings = append(bindings, capability.Binding{
				Operation: operation,
				Call: func(ctx context.Context, request capability.Request) (capability.Result, error) {
					var input managementInput
					if err := capability.DecodeParams(request.Params, &input); err != nil {
						return capability.Result{}, err
					}
					result, err := capability.Execute(ctx, s.config.Commands, scope, request, func(ctx context.Context) error {
						return s.mutate(ctx, wakerID, action, input, approve)
					})
					if err == nil && changed != nil {
						changed(ctx, action, result)
					}
					return result, err
				},
				Receipt: func(ctx context.Context, requestID string) (capability.Result, error) {
					result, err := capability.Lookup(ctx, s.config.Commands, scope, operation, requestID)
					if err == nil && changed != nil {
						changed(ctx, action, result)
					}
					return result, err
				},
			})
		}
	}
	return bindings
}

func (s *Service) read(ctx context.Context, wakerID, action string, input managementInput, approve func(context.Context, string) error) (map[string]any, error) {
	if action == "list" {
		items, err := s.config.Connectors.ListForWakerSummary(ctx, wakerID, input.Query, managementListLimit+1)
		if err != nil {
			return nil, err
		}
		listed := make([]map[string]any, 0, min(len(items), managementListLimit))
		for _, item := range items[:min(len(items), managementListLimit)] {
			listed = append(listed, publicConnector(item))
		}
		return map[string]any{"items": listed, "truncated": len(items) > managementListLimit}, nil
	}
	current, err := s.Visible(ctx, wakerID, input.ConnectorID)
	if err != nil {
		return nil, err
	}
	if action == "get" {
		return publicConnector(current), nil
	}
	if approve == nil {
		return nil, status_error.AgentOperationForbidden
	}
	if !Trusted(current.Connector) {
		return nil, status_error.MCPConnectorTrustRequired
	}
	if err := approve(ctx, approvalDescription("Test MCP connection", current.Connector, nil)); err != nil {
		return nil, err
	}
	fresh, err := s.Visible(ctx, wakerID, input.ConnectorID)
	if err != nil {
		return nil, err
	}
	if fresh.Connector.UpdatedAt != current.Connector.UpdatedAt {
		return nil, status_error.Conflict
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	probeContext, cancelProbe := context.WithTimeout(ctx, managementProbeTimeout)
	defer cancelProbe()
	tested, err := s.probe(probeContext, fresh.Connector)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"connectorId": fresh.Connector.ID, "name": fresh.Connector.Name,
		"status": tested.Status, "toolCount": len(tested.Tools), "durationMs": tested.DurationMS,
	}, nil
}

func (s *Service) mutate(ctx context.Context, wakerID, action string, input managementInput, approve func(context.Context, string) error) error {
	if action == "create" {
		definition, err := input.apply(models.MCPConnector{})
		if err != nil {
			return err
		}
		connector, err := newPublicConnector(definition)
		if err != nil {
			return err
		}
		if err := approve(ctx, approvalDescription("Test and add MCP connector for the current partner", connector, &input)); err != nil {
			return err
		}
		tested, err := s.probeApprovedConfiguration(ctx, connector)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		applyTestSnapshot(&connector, tested)
		// 保存到资源库，只有已通过测试的配置才会进入当前伙伴的下轮执行。
		_, err = s.saveNew(ctx, wakerID, connector, tested.Status == constants.MCPTestStatusSucceeded)
		return err
	}

	current, err := s.Visible(ctx, wakerID, input.ConnectorID)
	if err != nil {
		return err
	}
	connector := current.Connector
	if (input.ExpectedRevision != "" || action == "update") && input.ExpectedRevision != connectorRevision(connector) {
		return status_error.Conflict
	}

	if action == "detach" {
		if connector.Scope != constants.MCPConnectorScopePublic {
			return status_error.AgentOperationForbidden
		}
		return s.remove(ctx, wakerID, connector, true, 0, nil)
	}
	if connector.Scope == constants.MCPConnectorScopeBuiltIn {
		if action == "delete" {
			return status_error.BuiltInMCPConnectorCannotDelete
		}
		return status_error.BuiltInMCPConnectorReadOnly
	}
	var partners []string
	var affected string
	if connector.OwnerWakerID == "" {
		partners, err = s.config.Connectors.InstalledPartnerIDs(ctx, connector.ID)
		if err != nil {
			return err
		}
		affected, err = s.approvalScope(ctx, partners)
		if err != nil {
			return err
		}
	}
	if action == "delete" {
		reason := fmt.Sprintf("Delete MCP connector %q", bounded(connector.Name, 120))
		if partners != nil {
			reason += " from the library and remove access for " + affected
		}
		if err := approve(ctx, reason); err != nil {
			return err
		}
		return s.remove(ctx, wakerID, connector, false, 0, partners)
	}

	definition, err := input.apply(connector)
	if err != nil {
		return err
	}
	prepared, err := prepareConnector(connector, definition, &connector)
	if err != nil {
		return err
	}
	if credentialsWouldMove(connector, prepared) {
		return status_error.MCPConnectorTrustRequired
	}
	newTrustRequired := !Trusted(prepared)
	runtimeChanged := newTrustRequired || connectorConfigurationChanged(connector, prepared)
	probeRequired := (current.Enabled || partners != nil) && runtimeChanged
	requireApproval := partners != nil || newTrustRequired || probeRequired
	if requireApproval {
		reason := approvalDescription("Update MCP connector", prepared, &input)
		if partners != nil {
			reason += " for " + affected
		}
		if err := approve(ctx, reason); err != nil {
			return err
		}
	}
	if probeRequired {
		// 以用户确认的候选配置测试，失败时保留当前启用配置。
		result, err := s.probeApprovedConfiguration(ctx, prepared)
		if err != nil {
			return err
		}
		if result.Status != constants.MCPTestStatusSucceeded {
			return status_error.MCPConnectorProbeFailed
		}
	}
	_, err = s.saveUpdate(ctx, wakerID, current, prepared, requireApproval, partners)
	return err
}

func (s *Service) probeApprovedConfiguration(ctx context.Context, connector models.MCPConnector) (mcpclient.Result, error) {
	probeCtx, cancel := context.WithTimeout(ctx, managementProbeTimeout)
	defer cancel()
	connector.TrustedConfigDigest = trustDigest(connector)
	return s.probe(probeCtx, connector)
}

func (i managementInput) apply(current models.MCPConnector) (Definition, error) {
	definition := Definition{
		Name: current.Name, Description: current.Description, Categories: current.Categories,
		Transport: current.Transport, Command: current.Command, Arguments: current.Arguments,
		URL: current.URL, TimeoutMS: current.TimeoutMS,
		Headers: retainSecretDefinitions(current.Headers), Environment: retainSecretDefinitions(current.Environment),
	}
	if i.Name != nil {
		definition.Name = *i.Name
	}
	if i.Description != nil {
		definition.Description = *i.Description
	}
	if i.Transport != nil {
		definition.Transport = *i.Transport
		if current.Transport == constants.MCPTransportStdio && definition.Transport != current.Transport {
			definition.Command, definition.Arguments, definition.Environment = "", nil, nil
		} else if current.Transport != constants.MCPTransportStdio && definition.Transport == constants.MCPTransportStdio {
			definition.URL, definition.Headers = "", nil
		}
	}
	if i.Command != nil {
		definition.Command = *i.Command
	}
	if i.Arguments != nil {
		if slices.ContainsFunc(*i.Arguments, carriesCredentialArgument) {
			return Definition{}, status_error.AgentInputInvalid
		}
		definition.Arguments = *i.Arguments
	}
	if i.URL != nil {
		if parsed, err := url.Parse(strings.TrimSpace(*i.URL)); err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
			return Definition{}, status_error.AgentInputInvalid
		}
		definition.URL = *i.URL
	}
	if i.ConnectionTimeoutMS != nil {
		definition.TimeoutMS = *i.ConnectionTimeoutMS
	}
	return definition, nil
}

func carriesCredentialArgument(argument string) bool {
	key, _, assignment := strings.Cut(strings.TrimSpace(argument), "=")
	if !assignment && !strings.HasPrefix(key, "-") {
		return false
	}
	key = strings.ReplaceAll(strings.ToLower(strings.TrimLeft(key, "-")), "_", "-")
	for _, sensitive := range []string{"api-key", "apikey", "authorization", "credential", "password", "secret", "token"} {
		if key == sensitive || strings.HasSuffix(key, "-"+sensitive) {
			return true
		}
	}
	return key == "header" || key == "headers"
}

func retainSecretDefinitions(values []models.MCPSecretDefinition) []SecretInput {
	retained := make([]SecretInput, 0, len(values))
	for _, value := range values {
		retained = append(retained, SecretInput{ID: value.ID, Name: value.Name})
	}
	return retained
}

func hasCredentials(connector models.MCPConnector) bool {
	configured := func(entry models.MCPSecretDefinition) bool { return entry.Configured }
	if slices.ContainsFunc(connector.Environment, configured) || slices.ContainsFunc(connector.Headers, configured) ||
		slices.ContainsFunc(connector.Arguments, carriesCredentialArgument) {
		return true
	}
	address, err := url.Parse(connector.URL)
	return err == nil && (address.User != nil || address.RawQuery != "")
}

func credentialsWouldMove(current, updated models.MCPConnector) bool {
	if !hasCredentials(current) {
		return false
	}
	if current.Transport != updated.Transport {
		return true
	}
	if current.Transport == constants.MCPTransportStdio {
		return current.Command != updated.Command || !slices.Equal(current.Arguments, updated.Arguments)
	}
	return remoteOrigin(current.URL) == "" || !strings.EqualFold(remoteOrigin(current.URL), remoteOrigin(updated.URL))
}

func remoteOrigin(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func connectorRevision(connector models.MCPConnector) string {
	return strconv.FormatInt(connector.UpdatedAt, 10)
}

func publicConnector(item models.MCPConnectorWithState) map[string]any {
	connector := item.Connector
	public := map[string]any{
		"connectorId": connector.ID, "name": connector.Name, "description": bounded(connector.Description, 240),
		"revision": connectorRevision(connector), "transport": string(connector.Transport),
		"scope": string(connector.Scope), "enabled": item.Enabled, "connectionTimeoutMs": connector.TimeoutMS,
		"testStatus": string(connector.TestStatus), "hasCredentials": hasCredentials(connector),
	}
	if connector.Transport == constants.MCPTransportStdio {
		public["command"] = bounded(filepath.Base(connector.Command), 160)
	} else {
		public["origin"] = remoteOrigin(connector.URL)
	}
	return public
}

func (s *Service) approvalScope(ctx context.Context, ids []string) (string, error) {
	scope := fmt.Sprintf("%d installed partner(s)", len(ids))
	if len(ids) == 0 {
		return scope, nil
	}
	partners, err := s.config.Wakers.ListByIDs(ctx, ids[:min(len(ids), 5)])
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(partners))
	for _, partner := range partners {
		names = append(names, strconv.Quote(bounded(partner.Name, 80)))
	}
	if len(names) != 0 {
		scope += " (" + strings.Join(names, ", ") + ")"
	}
	return scope, nil
}

func approvalDescription(action string, connector models.MCPConnector, proposed *managementInput) string {
	reason := fmt.Sprintf("%s %q", action, bounded(connector.Name, 120))
	if connector.Transport == constants.MCPTransportStdio {
		reason += fmt.Sprintf(" using local command %q", bounded(connector.Command, 320))
		if len(connector.Arguments) > 0 {
			if proposed != nil && proposed.Arguments != nil {
				reason += fmt.Sprintf(" with arguments %q", bounded(strings.Join(connector.Arguments, " "), 320))
			} else {
				reason += " with its existing arguments"
			}
		}
	} else {
		target := remoteOrigin(connector.URL)
		if proposed != nil && proposed.URL != nil {
			// 新提交的地址已经拒绝凭据、查询参数和片段；既有配置仅展示来源。
			if parsed, err := url.Parse(connector.URL); err == nil {
				target += parsed.EscapedPath()
			}
		}
		reason += fmt.Sprintf(" at %q", bounded(target, 500))
	}
	return reason
}
