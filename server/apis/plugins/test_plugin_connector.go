package plugins

import (
	"errors"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/apis/mcp"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/mcpclient"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/plugininstallation"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 测试插件连接器
type TestPluginConnector struct {
	ginx.MethodPost
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 固定版本
	VersionID string `in:"query" name:"versionId" validate:"required"`
	// 组件标识
	Key string `in:"query" name:"key" validate:"required"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
	// 来源提交
	SourceCommit string `in:"query" name:"sourceCommit"`
	// 压缩摘要
	ArchiveSHA256 string `in:"query" name:"archiveSHA256"`
}

// Path 返回显式协议探测入口；普通查看不执行此用例。
func (*TestPluginConnector) Path() string { return "/:pluginId/connector/test" }

// Output 在内容与共享身份占用内完成 MCP 初始化和工具发现，返回结果供当前详情展示。
func (o *TestPluginConnector) Output(ctx *gin.Context) (interface{}, error) {
	visible := GetPluginConnector{PluginID: o.PluginID, VersionID: o.VersionID, Key: o.Key, WakerID: o.WakerID}
	value, err := visible.Output(ctx)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	result := value.(mcp.ConnectorResponse)
	version, err := global.PluginControllerManager.Version(ctx, o.VersionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if (o.SourceCommit != "" && version.Provenance["sourceCommit"] != o.SourceCommit) || (o.ArchiveSHA256 != "" && version.Provenance["archiveSHA256"] != o.ArchiveSHA256) {
		return nil, ginx.WithStack(status_error.PluginVersionConflict)
	}
	if version.Package.RequiresTrust() && version.TrustedAt == 0 {
		return nil, ginx.WithStack(status_error.MCPConnectorTrustRequired)
	}
	config, err := global.PluginControllerManager.Configuration(ctx, o.PluginID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	source, err := packageplugin.ContentPath(global.Config.DataDirectory, version.ContentPath)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	data := filepath.Join(global.Config.DataDirectory, "plugins", "data", o.PluginID)
	if err := os.MkdirAll(data, 0700); err != nil {
		return nil, ginx.WithStack(status_error.PluginStorageError)
	}
	if global.PluginContents != nil {
		release, err := global.PluginContents.Acquire(source, data)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		defer release()
	}
	if err := packageplugin.VerifyContent(source, version.Digest); err != nil {
		return nil, ginx.WithStack(err)
	}
	// 选择与只读展示一致的业务组件；明确的授权绑定只借用匹配目标的身份。
	target := "codex"
	hasOwned := slices.ContainsFunc(version.Package.Servers, func(s packageplugin.Server) bool { return s.Key == o.Key })
	if !hasOwned {
		target = ""
		for _, candidate := range []string{"codex", "claude"} {
			if slices.ContainsFunc(version.Package.NativeImplementations[candidate].Servers, func(s packageplugin.NativeServerSummary) bool { return s.Key == o.Key }) {
				target = candidate
				break
			}
		}
		if target == "" {
			return nil, ginx.WithStack(status_error.PluginContentInvalid)
		}
	} else if auth := version.Package.Integration.Authentication; auth != nil && len(auth.Bindings) > 0 {
		target = ""
		for _, binding := range auth.Bindings {
			if binding.Server == o.Key {
				target = binding.Runtime
				break
			}
		}
		if target == "" {
			return nil, ginx.WithStack(status_error.PluginForbidden)
		}
	}
	selection := version.Package
	if hasOwned {
		// 自有组件始终使用逻辑包；原生引用仅服务原生摘要组件，不能劫持同名连接。
		selection.Integration.NativePlugins = nil
	}
	pkg, root, err := packageplugin.SelectTarget(source, selection, target)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	extension, err := packageplugin.ClientExtension(pkg)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	secrets := secretValues(config)
	targetValues, targetSecrets := map[string]string{}, map[string]string{}
	for _, field := range extension.Config {
		if field.Secret {
			if value, ok := secrets[field.Key]; ok {
				targetSecrets[field.Key] = value
			}
		} else if value, ok := config.Values[field.Key]; ok {
			targetValues[field.Key] = value
		}
	}
	if err := packageplugin.ValidateValues(pkg, targetValues, targetSecrets); err != nil {
		return nil, ginx.WithStack(err)
	}
	values := maps.Clone(config.Values)
	if values == nil {
		values = map[string]string{}
	}
	for _, field := range extension.Config {
		if _, exists := values[field.Key]; !exists && !field.Secret {
			values[field.Key] = field.Default
		}
	}
	maps.Copy(values, secrets)
	var server *packageplugin.Server
	for _, candidate := range pkg.Servers {
		if candidate.Key == o.Key {
			server = &candidate
			break
		}
	}
	if server == nil {
		return nil, ginx.WithStack(status_error.PluginContentInvalid)
	}
	resolved, err := packageplugin.ResolveServer(pkg, *server, root, data, values)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	connector := models.MCPConnector{ID: result.Connector.ID, Name: o.Key, Transport: result.Connector.Transport, Command: resolved.Server.Command, Arguments: resolved.Server.Args, URL: resolved.Server.URL, TimeoutMS: constants.MCPConnectorDefaultTimeoutMS}
	for name, value := range resolved.Server.Env {
		if connector.Transport == constants.MCPTransportStdio {
			connector.Environment = append(connector.Environment, models.MCPSecretDefinition{Name: name, Value: value, Configured: value != ""})
		}
	}
	for name, value := range resolved.Server.Headers {
		connector.Headers = append(connector.Headers, models.MCPSecretDefinition{Name: name, Value: value, Configured: value != ""})
	}
	diagnostic := connectorDiagnostic{Revision: config.Revision}
	logical := version
	if logical.Package.Integration.Authentication == nil && pkg.Integration.Authentication != nil {
		logical.Package = pkg
		logical.Package.SourcePath, err = filepath.Rel(source, root)
		if err != nil {
			return nil, ginx.WithStack(status_error.PluginContentInvalid)
		}
	}
	if logical.Package.Integration.Authentication != nil {
		auth, bridge, release, err := installationService().BridgeLease(logical)
		if errors.Is(err, status_error.PluginBusy) {
			// 同版本桥接可直接共享；仅入口需切换且被占用时回收闲置消费者，活动工作仍受保护。
			if err := closeIdlePlugin(ctx, o.PluginID); err != nil {
				return nil, ginx.WithStack(err)
			}
			auth, bridge, release, err = installationService().BridgeLease(logical)
		}
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		defer release()
		account, err := auth.Status(ctx, true)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if !account.Available {
			return nil, ginx.WithStack(status_error.PluginNotReady)
		}
		for _, scope := range logical.Package.Integration.Authentication.Scopes {
			if !auth.ScopeGranted(account.Scopes, scope) {
				return nil, ginx.WithStack(status_error.PluginNotReady)
			}
		}
		diagnostic.BridgeDigest = logical.Digest + ":" + filepath.ToSlash(logical.Package.SourcePath)
		diagnostic.AccountID, diagnostic.Scopes = account.ID, slices.Clone(account.Scopes)
		bindings := packageplugin.TargetAuthenticationBindings(logical.Package, target)
		if len(logical.Package.Integration.Authentication.Bindings) > 0 && len(bindings) == 0 {
			return nil, ginx.WithStack(status_error.PluginForbidden)
		}
		if len(bindings) > 0 {
			connectors := []models.MCPConnector{connector}
			if err := plugininstallation.ResolveBoundAuthentication(ctx, bridge, o.PluginID, bindings, false, connectors, nil); err != nil {
				return nil, ginx.WithStack(err)
			}
			connector = connectors[0]
		} else {
			bindings, err := bridge.Resolve(ctx, []commandbridge.Component{{ComponentID: connector.ID, Key: o.Key, Target: "mcp", URL: connector.URL}})
			if err != nil {
				return nil, ginx.WithStack(err)
			}
			for _, binding := range bindings {
				for name, value := range binding.Headers {
					connector.Headers = slices.DeleteFunc(connector.Headers, func(d models.MCPSecretDefinition) bool { return strings.EqualFold(d.Name, name) })
					connector.Headers = append(connector.Headers, models.MCPSecretDefinition{Name: name, Value: value, Configured: true})
				}
				if connector.Transport == constants.MCPTransportStdio {
					for name, value := range binding.Environment {
						connector.Environment = append(connector.Environment, models.MCPSecretDefinition{Name: name, Value: value, Configured: true})
					}
				}
			}
		}
	}
	probe := mcpclient.TestInDirectory(ctx, connector, resolved.WorkingDirectory)
	// 配置或伙伴版本变化后，迟到的结果不能覆盖当前详情。
	latest, err := global.PluginControllerManager.Configuration(ctx, o.PluginID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if latest.Revision != config.Revision {
		return nil, ginx.WithStack(status_error.PluginConfigurationConflict)
	}
	if _, err := visible.Output(ctx); err != nil {
		return nil, ginx.WithStack(err)
	}
	result.Connector.TestTools = []models.MCPToolSummary{}
	result.Connector.TestToolCount = 0
	result.Connector.TestStatus = probe.Status
	result.Connector.TestedAt = time.Now().UnixMilli()
	result.Connector.TestDurationMS = probe.DurationMS
	result.Connector.TimeoutMS = connector.TimeoutMS
	scrub := diagnosticRedactor(connector, secrets)
	result.Connector.TestError = scrub(probe.Error)
	if probe.Status == constants.MCPTestStatusSucceeded {
		result.Connector.TestToolCount = len(probe.Tools)
		for _, tool := range probe.Tools[:min(len(probe.Tools), constants.MCPConnectorMaximumToolCount)] {
			result.Connector.TestTools = append(result.Connector.TestTools, models.MCPToolSummary{Name: scrub(tool.Name), Description: scrub(tool.Description)})
		}
	}
	diagnostic.Connector = result.Connector
	pluginConnectorDiagnostics.put(connectorDiagnosticKey(o.PluginID, o.VersionID, o.Key), diagnostic)
	// 再按当前配置与公开身份读取，不能把已失效身份的迟到结果直接交付界面。
	confirmed, err := visible.Output(ctx)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	result = confirmed.(mcp.ConnectorResponse)
	return result, nil
}

// diagnosticRedactor 保证服务错误与工具声明不会回显本次连接使用的凭据。
func diagnosticRedactor(connector models.MCPConnector, values map[string]string) func(string) string {
	secrets := []string{}
	for _, argument := range connector.Arguments {
		if argument != "" {
			secrets = append(secrets, argument)
		}
		if _, value, ok := strings.Cut(argument, "="); ok && value != "" {
			secrets = append(secrets, value)
		}
	}
	if address, err := url.Parse(connector.URL); err == nil {
		for _, values := range address.Query() {
			for _, value := range values {
				if value != "" {
					secrets = append(secrets, value, url.QueryEscape(value))
				}
			}
		}
	}
	for _, definition := range append(slices.Clone(connector.Headers), connector.Environment...) {
		if definition.Value != "" {
			secrets = append(secrets, definition.Value)
			if strings.HasPrefix(definition.Value, "Bearer ") {
				secrets = append(secrets, strings.TrimPrefix(definition.Value, "Bearer "))
			}
		}
	}
	for _, value := range values {
		if value != "" {
			secrets = append(secrets, value)
		}
	}
	if connector.URL != publicConnectorURL(connector.URL) {
		secrets = append(secrets, connector.URL)
	}
	slices.SortFunc(secrets, func(a, b string) int { return len(b) - len(a) })
	return func(value string) string {
		for _, secret := range secrets {
			value = strings.ReplaceAll(value, secret, "••••")
		}
		runes := []rune(value)
		return string(runes[:min(len(runes), constants.MCPConnectorMaximumSummaryLength)])
	}
}
