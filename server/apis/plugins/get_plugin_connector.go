package plugins

import (
	"maps"
	"net/url"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/apis/mcp"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 查看插件连接器
type GetPluginConnector struct {
	ginx.MethodGet
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 固定版本
	VersionID string `in:"query" name:"versionId" validate:"required"`
	// 组件标识
	Key string `in:"query" name:"key" validate:"required"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
}

// Path 返回插件组件的只读入口，不建立独立连接器。
func (*GetPluginConnector) Path() string { return "/:pluginId/connector" }

// Output 读取固定版本保存的公开连接信息，不执行内容或触发授权。
func (o *GetPluginConnector) Output(ctx *gin.Context) (interface{}, error) {
	resource, err := visiblePlugin(ctx, o.PluginID, o.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if o.WakerID != "" {
		installed, err := global.PluginControllerManager.Installations(ctx, resource.ID, o.WakerID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if len(installed) == 0 {
			return nil, ginx.WithStack(status_error.PluginForbidden)
		}
		if installed[0].VersionID != o.VersionID {
			return nil, ginx.WithStack(status_error.PluginVersionConflict)
		}
	}
	version, err := global.PluginControllerManager.Version(ctx, o.VersionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if version.PluginID != resource.ID {
		return nil, ginx.WithStack(status_error.PluginVersionConflict)
	}
	result, err := pluginConnectorResponse(version.Package, o.Key)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	result.Connector.ID = packageplugin.ComponentID(resource.ID, "mcp:"+o.Key)
	if err := pluginConnectorDiagnostics.apply(ctx, version, o.Key, &result); err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}

// pluginConnectorResponse 与插件能力列表保持相同的优先级，只输出保存的脱敏快照。
func pluginConnectorResponse(pkg packageplugin.Package, key string) (mcp.ConnectorResponse, error) {
	var selected *packageplugin.Server
	native := false
	for _, server := range pkg.Servers {
		if server.Key == key {
			selected = &server
			break
		}
	}
	name := key
	if selected == nil {
		for _, target := range []string{"codex", "claude"} {
			for _, server := range pkg.NativeImplementations[target].Servers {
				if server.Key != key {
					continue
				}
				selected = &packageplugin.Server{Key: key, Type: server.Transport, URL: server.Endpoint}
				name = server.Name
				native = true
				break
			}
			if selected != nil {
				break
			}
		}
	}
	if selected == nil {
		return mcp.ConnectorResponse{}, status_error.PluginContentInvalid
	}
	transport := constants.MCPTransport(selected.Type)
	switch selected.Type {
	case "http", "streamable-http":
		transport = constants.MCPTransportStreamableHTTP
	case "stdio", "streamable_http", "sse":
	default:
		return mcp.ConnectorResponse{}, status_error.PluginContentInvalid
	}
	environment, headers := maps.Clone(selected.Env), maps.Clone(selected.Headers)
	if environment == nil {
		environment = map[string]string{}
	}
	if headers == nil {
		headers = map[string]string{}
	}
	extension, err := packageplugin.ClientExtension(pkg)
	if err != nil {
		return mcp.ConnectorResponse{}, err
	}
	for _, binding := range extension.Bindings {
		if binding.Server != key {
			continue
		}
		if binding.Target == constants.PluginBindingTargetEnvironment {
			environment[binding.Name] = ""
		} else {
			headers[binding.Name] = ""
		}
	}
	if authentication := pkg.Integration.Authentication; authentication != nil {
		for _, binding := range authentication.Bindings {
			if binding.Server != key {
				continue
			}
			for name := range headers {
				if strings.EqualFold(name, binding.Header) {
					delete(headers, name)
				}
			}
			headers[binding.Header] = ""
		}
	}
	if name == "" {
		name = key
	}
	connector := mcp.ConnectorResource{
		ID: key, Name: name, ActivationPolicy: constants.MCPActivationPolicyPerExecution, Transport: transport, Command: selected.Command,
		URL: publicConnectorURL(selected.URL), Arguments: []string{},
		Environment: connectorDefinitions(environment), Headers: connectorDefinitions(headers),
		TestStatus: constants.MCPTestStatusUntested, TestTools: []models.MCPToolSummary{},
	}
	// 参数可能含包内常量凭据；只表达参数存在，不向查看接口交付原值。
	for range selected.Args {
		connector.Arguments = append(connector.Arguments, "••••")
	}
	compatibility := mcp.ConnectorCompatibility(transport)
	if native {
		for index, item := range compatibility {
			found := false
			for _, server := range pkg.NativeImplementations[item.RuntimeID].Servers {
				if server.Key == key {
					found = true
					break
				}
			}
			compatibility[index].Supported = item.Supported && found
		}
	}
	return mcp.ConnectorResponse{Connector: connector, Compatibility: compatibility}, nil
}

func publicConnectorURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	parsed.User, parsed.RawQuery, parsed.Fragment, parsed.RawFragment = nil, "", "", ""
	parsed.ForceQuery = false
	return parsed.String()
}

func connectorDefinitions(values map[string]string) []mcp.SecretDefinition {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]mcp.SecretDefinition, 0, len(names))
	for _, name := range names {
		result = append(result, mcp.SecretDefinition{Name: name, Configured: strings.TrimSpace(values[name]) != ""})
	}
	return result
}
