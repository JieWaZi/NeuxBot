package plugininstallation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// deliveryMaterial 是管理操作保存的唯一私有交付与公开主体快照。
type deliveryMaterial struct {
	// AccountID 是同次管理状态核验确认的公开主体。
	AccountID string `json:"accountId,omitempty"`
	// Bindings 是同次严格校验的组件秘密。
	Bindings []commandbridge.Binding `json:"bindings"`
}

// materialKey 使用不可变版本、交付目标和授权端点限定材料；普通配置修订不改变身份。
func materialKey(version models.PluginVersion, config models.PluginConfiguration, target string) string {
	endpoints := []string{}
	declaration := version.Package.Integration.Authentication
	if declaration != nil && len(declaration.Bindings) > 0 {
		for _, binding := range plugins.TargetAuthenticationBindings(version.Package, target) {
			endpoints = append(endpoints, binding.Server+"\x00"+binding.URL+"\x00"+strings.ToLower(binding.Header))
		}
	} else {
		for _, server := range version.Package.Servers {
			address, err := deliveryEndpoint(version.Package, config, server.URL)
			if err != nil {
				address = server.URL
			}
			endpoints = append(endpoints, server.Key+"\x00"+address)
		}
	}
	sort.Strings(endpoints)
	body, _ := json.Marshal([]any{version.ID, version.Digest, filepath.ToSlash(version.Package.SourcePath), target, endpoints})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// deliveryEndpoint 与实际连接器使用同一普通配置展开规则，材料不能跨端点交付。
func deliveryEndpoint(pkg plugins.Package, config models.PluginConfiguration, address string) (string, error) {
	if pkg.Format == constants.PluginFormatPortable {
		return address, nil
	}
	values := map[string]string{}
	extension, err := plugins.ClientExtension(pkg)
	if err != nil {
		return "", err
	}
	for _, field := range extension.Config {
		values[field.Key] = field.Default
	}
	for key, value := range config.Values {
		values[key] = value
	}
	for key, value := range secretValues(config) {
		values[key] = value
	}
	return plugins.Expand(address, values)
}

// materialScope 限定当前版本和目标的唯一保存槽位，端点变化替换旧材料。
func materialScope(version models.PluginVersion, target string) string {
	body, _ := json.Marshal([]any{version.ID, version.Digest, filepath.ToSlash(version.Package.SourcePath), target})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// ResolveDeliveryMaterials 仅由安装和授权管理调用，严格核验返回组件后生成私有保存材料。
func ResolveDeliveryMaterials(ctx context.Context, driver *commandbridge.Driver, version models.PluginVersion, config models.PluginConfiguration, accountID ...string) ([]models.MCPSecretDefinition, error) {
	declaration := version.Package.Integration.Authentication
	if declaration == nil {
		return nil, nil
	}
	targets := []string{"*"}
	if len(declaration.Bindings) > 0 {
		targets = nil
		for _, binding := range declaration.Bindings {
			if !slices.Contains(targets, binding.Runtime) {
				targets = append(targets, binding.Runtime)
			}
		}
		sort.Strings(targets)
	}
	materials := []models.MCPSecretDefinition{}
	for _, target := range targets {
		components := []commandbridge.Component{}
		allowed := map[string][]plugins.AuthenticationBinding{}
		if target != "*" {
			for _, binding := range plugins.TargetAuthenticationBindings(version.Package, target) {
				id := plugins.ComponentID(version.PluginID, "mcp:"+binding.Server)
				if len(allowed[id]) == 0 {
					components = append(components, commandbridge.Component{ComponentID: id, Key: binding.Server, Target: "mcp", URL: binding.URL})
				}
				allowed[id] = append(allowed[id], binding)
			}
		} else {
			for _, skill := range version.Package.Skills {
				components = append(components, commandbridge.Component{ComponentID: plugins.ComponentID(version.PluginID, "skill:"+skill.Key), Key: skill.Key, Target: "skill"})
			}
			for _, server := range version.Package.Servers {
				address, err := deliveryEndpoint(version.Package, config, server.URL)
				if err != nil {
					return nil, err
				}
				components = append(components, commandbridge.Component{ComponentID: plugins.ComponentID(version.PluginID, "mcp:"+server.Key), Key: server.Key, Target: "mcp", URL: address})
			}
		}
		if len(components) == 0 {
			continue
		}
		bindings, err := driver.Resolve(ctx, components)
		if err != nil {
			return nil, err
		}
		if len(bindings) != len(components) {
			return nil, status_error.PluginNotReady
		}
		for _, binding := range bindings {
			declarations := allowed[binding.ComponentID]
			if target == "*" {
				continue
			}
			if len(binding.Environment) > 0 || len(binding.Headers) != len(declarations) {
				return nil, status_error.PluginForbidden
			}
			for _, declaration := range declarations {
				matches := 0
				for key, value := range binding.Headers {
					if strings.EqualFold(key, declaration.Header) {
						if value == "" {
							return nil, status_error.PluginNotReady
						}
						matches++
					}
				}
				if matches != 1 {
					return nil, status_error.PluginForbidden
				}
			}
		}
		material := deliveryMaterial{Bindings: bindings}
		if len(accountID) > 0 {
			material.AccountID = accountID[0]
		}
		body, err := json.Marshal(material)
		if err != nil {
			return nil, status_error.PluginAuthorizationFailed
		}
		key := materialKey(version, config, target)
		materials = append(materials, models.MCPSecretDefinition{ID: materialScope(version, target), Name: key, Configured: true, Value: string(body)})
	}
	return materials, nil
}

func (s *Service) persistDelivery(ctx context.Context, version models.PluginVersion, driver *commandbridge.Driver, accountID string) error {
	config, err := s.config.Store.Configuration(ctx, version.PluginID)
	if err != nil {
		return err
	}
	materials, resolveErr := ResolveDeliveryMaterials(ctx, driver, version, config, accountID)
	// 显式授权已经确认共享身份；一次替换全部旧材料，不能让其他版本继续交付前账号。
	// 新身份材料解析失败时同样撤销旧材料，普通状态读取和安装准备不经过此边界。
	config.DeliveryMaterials = materials
	if err := s.config.Store.UpdateDeliveryMaterials(ctx, config); err != nil {
		if resolveErr != nil {
			logx.Errorf("resolve plugin delivery before identity material replacement failed: plugin_id=%s error=%v", version.PluginID, resolveErr)
		}
		return err
	}
	return resolveErr
}

func (s *Service) clearDelivery(ctx context.Context, id string) error {
	config, err := s.config.Store.Configuration(ctx, id)
	if err != nil {
		return err
	}
	config.DeliveryMaterials = nil
	return s.config.Store.UpdateDeliveryMaterials(ctx, config)
}

// DeliveryAuthentication 只读取与版本、目标及授权端点匹配的已有私有材料；缺失继续交付能力。
func DeliveryAuthentication(version models.PluginVersion, config models.PluginConfiguration, target string) ([]commandbridge.Binding, string, error) {
	if version.Package.Integration.Authentication == nil {
		return nil, "", nil
	}
	if len(version.Package.Integration.Authentication.Bindings) == 0 {
		target = "*"
	}
	key := materialKey(version, config, target)
	for _, material := range config.DeliveryMaterials {
		if material.Name != key || !material.Configured {
			continue
		}
		var saved deliveryMaterial
		if json.Unmarshal([]byte(material.Value), &saved) != nil {
			return nil, "", status_error.PluginForbidden
		}
		return saved.Bindings, saved.AccountID, nil
	}
	return nil, "", nil
}

// ProjectBoundAuthentication 将已保存材料按本次真实交付的连接白名单投射，不启动授权程序。
func ProjectBoundAuthentication(id string, declarations []plugins.AuthenticationBinding, native bool, connectors []models.MCPConnector, environment map[string]string, bindings []commandbridge.Binding) {
	for _, declaration := range declarations {
		componentID := plugins.ComponentID(id, "mcp:"+declaration.Server)
		if !native && !slices.ContainsFunc(connectors, func(connector models.MCPConnector) bool {
			return connector.ID == componentID && connector.URL == declaration.URL
		}) {
			continue
		}
		value := ""
		for _, binding := range bindings {
			if binding.ComponentID == componentID {
				for key, header := range binding.Headers {
					if strings.EqualFold(key, declaration.Header) {
						value = header
					}
				}
			}
		}
		if native {
			environment[plugins.AuthenticationEnvironmentKey(id, declaration)] = value
			continue
		}
		for index := range connectors {
			if connectors[index].ID != componentID {
				continue
			}
			connectors[index].Headers = slices.DeleteFunc(connectors[index].Headers, func(header models.MCPSecretDefinition) bool {
				return strings.EqualFold(header.Name, declaration.Header)
			})
			connectors[index].Headers = append(connectors[index].Headers, models.MCPSecretDefinition{Name: declaration.Header, Value: value, Configured: true})
		}
	}
}

// MergeDeliveryPath 合并管理阶段已核验的程序目录，受管程序始终先于继承的系统目录。
func MergeDeliveryPath(current, incoming, dataDirectory string) string {
	private, inherited := []string{}, []string{}
	seen := map[string]bool{}
	for _, directory := range append(filepath.SplitList(current), filepath.SplitList(incoming)...) {
		if directory == "" || seen[directory] {
			continue
		}
		seen[directory] = true
		relative, err := filepath.Rel(filepath.Join(dataDirectory, "plugins", "data"), directory)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			private = append(private, directory)
		} else {
			inherited = append(inherited, directory)
		}
	}
	return strings.Join(append(private, inherited...), string(filepath.ListSeparator))
}

// ResolveBoundAuthentication 为显式连接测试解析已获准组件；聊天使用已有材料的本地投射。
func ResolveBoundAuthentication(ctx context.Context, driver *commandbridge.Driver, id string, declarations []plugins.AuthenticationBinding, native bool, connectors []models.MCPConnector, environment map[string]string) error {
	selected := []plugins.AuthenticationBinding{}
	for _, declaration := range declarations {
		componentID := plugins.ComponentID(id, "mcp:"+declaration.Server)
		if native || slices.ContainsFunc(connectors, func(connector models.MCPConnector) bool {
			return connector.ID == componentID && connector.URL == declaration.URL
		}) {
			selected = append(selected, declaration)
		}
	}
	if len(selected) == 0 {
		return nil
	}
	pkg := plugins.Package{Integration: plugins.Integration{Authentication: &plugins.Authentication{Bindings: selected}}}
	materials, err := ResolveDeliveryMaterials(ctx, driver, models.PluginVersion{PluginID: id, Package: pkg}, models.PluginConfiguration{})
	if err != nil {
		return err
	}
	for _, material := range materials {
		var saved deliveryMaterial
		if json.Unmarshal([]byte(material.Value), &saved) != nil {
			return status_error.PluginForbidden
		}
		ProjectBoundAuthentication(id, selected, native, connectors, environment, saved.Bindings)
	}
	return nil
}
