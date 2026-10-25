package plugininstallation

import (
	"context"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// ResolveBoundAuthentication 只向当前交付且精确匹配声明的连接申请秘密。
func ResolveBoundAuthentication(ctx context.Context, bridge *commandbridge.Driver, id string, declarations []plugins.AuthenticationBinding, native bool, connectors []models.MCPConnector, environment map[string]string) error {
	allowed := map[string][]plugins.AuthenticationBinding{}
	components := []commandbridge.Component{}
	for _, declaration := range declarations {
		componentID := plugins.ComponentID(id, "mcp:"+declaration.Server)
		if !native && !slices.ContainsFunc(connectors, func(connector models.MCPConnector) bool {
			return connector.ID == componentID && connector.URL == declaration.URL
		}) {
			continue
		}
		if len(allowed[componentID]) == 0 {
			components = append(components, commandbridge.Component{ComponentID: componentID, Key: declaration.Server, Target: "mcp", URL: declaration.URL})
		}
		allowed[componentID] = append(allowed[componentID], declaration)
	}
	if len(components) == 0 {
		return nil
	}
	resolved, err := bridge.Resolve(ctx, components)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, binding := range resolved {
		declarations, exists := allowed[binding.ComponentID]
		if !exists || seen[binding.ComponentID] || len(binding.Environment) > 0 || len(binding.Headers) != len(declarations) {
			return status_error.PluginForbidden
		}
		seen[binding.ComponentID] = true
		for _, declaration := range declarations {
			value := ""
			matches := 0
			for key, header := range binding.Headers {
				if strings.EqualFold(key, declaration.Header) {
					value = header
					matches++
				}
			}
			if matches != 1 || strings.TrimSpace(value) == "" {
				return status_error.PluginNotReady
			}
			if native {
				environment[plugins.AuthenticationEnvironmentKey(id, declaration)] = value
				continue
			}
			for index := range connectors {
				if connectors[index].ID != binding.ComponentID {
					continue
				}
				connectors[index].Headers = slices.DeleteFunc(connectors[index].Headers, func(header models.MCPSecretDefinition) bool {
					return strings.EqualFold(header.Name, declaration.Header)
				})
				connectors[index].Headers = append(connectors[index].Headers, models.MCPSecretDefinition{Name: declaration.Header, Value: value, Configured: true})
			}
		}
	}
	if len(seen) != len(allowed) {
		return status_error.PluginNotReady
	}
	return nil
}
