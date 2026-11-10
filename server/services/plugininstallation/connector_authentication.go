package plugininstallation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

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
	for _, binding := range plugins.TargetSkillAuthenticationBindings(version.Package, target) {
		keys, paths := slices.Clone(binding.EnvironmentKeys), slices.Clone(binding.PrivatePathKeys)
		slices.Sort(keys)
		slices.Sort(paths)
		endpoints = append(endpoints, strings.Join([]string{"skill", binding.Skill, strings.Join(keys, "\x01"), strings.Join(paths, "\x01")}, "\x00"))
	}
	sort.Strings(endpoints)
	body, _ := json.Marshal([]any{"component-delivery", version.Package.ContractVersion, version.PluginID, version.ID, version.Digest, filepath.ToSlash(version.Package.SourcePath), target, endpoints})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// deliveryEndpoint 与实际连接器使用同一普通配置展开规则，材料不能跨端点交付。
func deliveryEndpoint(pkg plugins.Package, config models.PluginConfiguration, address string) (string, error) {
	if err := plugins.ValidateContract(pkg); err != nil {
		return "", err
	}
	return address, nil
}

// materialScope 限定当前版本和目标的唯一保存槽位，端点变化替换旧材料。
func materialScope(version models.PluginVersion, target string) string {
	body, _ := json.Marshal([]any{version.ID, version.Digest, filepath.ToSlash(version.Package.SourcePath), target})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// ResolveDeliveryMaterials 仅由安装和授权管理调用，严格核验返回组件后生成私有保存材料。
func ResolveDeliveryMaterials(ctx context.Context, driver *commandbridge.Driver, version models.PluginVersion, config models.PluginConfiguration, accountID ...string) ([]models.MCPSecretDefinition, error) {
	if err := plugins.ValidateContract(version.Package); err != nil {
		return nil, err
	}
	declaration := version.Package.Integration.Authentication
	if declaration == nil {
		return nil, nil
	}
	targets := []string{}
	for _, binding := range declaration.Bindings {
		if !slices.Contains(targets, binding.Runtime) {
			targets = append(targets, binding.Runtime)
		}
	}
	for _, binding := range declaration.SkillBindings {
		if !slices.Contains(targets, binding.Runtime) {
			targets = append(targets, binding.Runtime)
		}
	}
	sort.Strings(targets)
	materials := []models.MCPSecretDefinition{}
	for _, target := range targets {
		components := []commandbridge.Component{}
		allowed := map[string][]plugins.AuthenticationBinding{}
		for _, binding := range plugins.TargetAuthenticationBindings(version.Package, target) {
			id := plugins.ComponentID(version.PluginID, "mcp:"+binding.Server)
			if len(allowed[id]) == 0 {
				components = append(components, commandbridge.Component{ComponentID: id, Key: binding.Server, Target: "mcp", URL: binding.URL})
			}
			allowed[id] = append(allowed[id], binding)
		}
		skills := map[string]plugins.SkillAuthenticationBinding{}
		for _, binding := range plugins.TargetSkillAuthenticationBindings(version.Package, target) {
			id := plugins.ComponentID(version.PluginID, "skill:"+binding.Skill)
			components = append(components, commandbridge.Component{ComponentID: id, Key: binding.Skill, Target: "skill", EnvironmentKeys: binding.EnvironmentKeys, PrivatePathKeys: binding.PrivatePathKeys})
			skills[id] = binding
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
			if declaration, ok := skills[binding.ComponentID]; ok {
				if len(binding.Headers) > 0 {
					return nil, status_error.PluginForbidden
				}
				if !skillEnvironmentReady(declaration, binding.Environment) {
					return nil, status_error.PluginNotReady
				}
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

func (s *Service) persistDelivery(ctx context.Context, version models.PluginVersion, driver *commandbridge.Driver, accountID string) (resultErr error) {
	// 外部登录已改变共享身份，任何后续失败都须撤销旧材料；不能假装外部状态回滚。
	// CAS 只允许撤销本次原代次，绝不覆盖较新的账号材料。
	defer func() {
		if resultErr == nil {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(constants.PluginAccountInvalidationTimeoutSeconds)*time.Second)
		defer cancel()
		if err := s.clearDelivery(cleanup, version.PluginID, driver.OperationContext().AccountGeneration); err != nil && err != status_error.PluginAccountConflict {
			resultErr = err
		}
	}()

	config, err := s.config.Store.Configuration(ctx, version.PluginID)
	if err != nil {
		return err
	}

	if config.Revision != driver.OperationContext().ConfigurationRevision {
		return status_error.PluginConfigurationConflict
	}

	relations, err := s.config.Store.Installations(ctx, version.PluginID, "")
	if err != nil {
		return err
	}
	versions := []models.PluginVersion{version}
	for _, relation := range relations {
		if slices.ContainsFunc(versions, func(v models.PluginVersion) bool { return v.ID == relation.VersionID }) {
			continue
		}
		current, err := s.config.Store.Version(ctx, relation.VersionID)
		if err != nil {
			return err
		}
		versions = append(versions, current)
	}
	materials := []models.MCPSecretDefinition{}
	var resolveErr error
	for _, current := range versions {
		if err := plugins.ValidateContract(current.Package); err != nil {
			resolveErr = err
			break
		}
		if err := plugins.ValidateValues(current.Package, config.Values, secretValues(config)); err != nil {
			resolveErr = err
			break
		}
		if current.Package.Integration.Authentication == nil {
			continue
		}
		active := driver
		var release func()
		if current.ID != version.ID {
			root, err := plugins.ContentPath(s.config.DataDirectory, current.ContentPath)
			if err != nil {
				resolveErr = err
				break
			}
			if s.config.Contents != nil {
				release, err = s.config.Contents.Acquire(root)
				if err != nil {
					resolveErr = err
					break
				}
			}
			command, err := s.commandConfig(ctx, current)
			if err != nil {
				if release != nil {
					release()
				}
				resolveErr = err
				break
			}
			expected := driver.OperationContext()
			if command.ConfigurationRevision != expected.ConfigurationRevision {
				if release != nil {
					release()
				}
				resolveErr = status_error.PluginConfigurationConflict
				break
			}
			if command.AccountGeneration != expected.AccountGeneration {
				if release != nil {
					release()
				}
				resolveErr = status_error.PluginAccountConflict
				break
			}
			active = commandbridge.New(command)
		}
		if err := plugins.ValidateValues(current.Package, config.Values, secretValues(config)); err != nil {
			if release != nil {
				release()
			}
			resolveErr = err
			break
		}
		account, err := active.Status(ctx, true)
		if err == nil && (!account.Available || account.ID != accountID) {
			err = status_error.PluginNotReady
		}
		if err == nil {
			for _, scope := range current.Package.Integration.Authentication.Scopes {
				if !slices.Contains(account.Scopes, scope) {
					err = status_error.PluginNotReady
					break
				}
			}
		}
		var resolved []models.MCPSecretDefinition
		if err == nil {
			resolved, err = ResolveDeliveryMaterials(ctx, active, current, config, accountID)
		}
		if release != nil {
			release()
		}
		if err != nil {
			resolveErr = err
			break
		}
		materials = append(materials, resolved...)
	}
	if resolveErr != nil {
		materials = nil
	}

	// 显式授权已经确认共享身份；一次替换全部旧材料，不能让其他版本继续交付前账号。
	// 新身份材料解析失败时同样撤销旧材料，普通状态读取和安装准备不经过此边界。
	operation := driver.OperationContext()
	if s.config.Transactions == nil {
		return status_error.PluginNotReady
	}
	err = s.config.Transactions.Transaction(ctx, func(txctx context.Context) error {
		revision, err := s.config.Store.ConfigurationRevision(txctx, version.PluginID)
		if err != nil {
			return err
		}
		if revision != operation.ConfigurationRevision {
			return status_error.PluginConfigurationConflict
		}
		account, err := s.config.Store.Account(txctx, version.PluginID)
		if err != nil {
			return err
		}
		if account.Generation != operation.AccountGeneration {
			return status_error.PluginAccountConflict
		}
		current, err := s.config.Store.Installations(txctx, version.PluginID, "")
		if err != nil {
			return err
		}
		if len(current) != len(relations) {
			return status_error.PluginVersionConflict
		}
		for _, relation := range relations {
			if !slices.ContainsFunc(current, func(v models.WakerPlugin) bool {
				return v.WakerID == relation.WakerID && v.VersionID == relation.VersionID && v.Enabled == relation.Enabled
			}) {
				return status_error.PluginVersionConflict
			}
		}
		account.DeliveryMaterials = materials
		return s.config.Store.UpdateAccount(txctx, account, account.Generation)
	})
	if err != nil {
		if resolveErr != nil {
			logx.Errorf("resolve plugin delivery before identity material replacement failed: plugin_id=%s error=%v", version.PluginID, resolveErr)
		}
		return err
	}
	return resolveErr
}

func (s *Service) clearDelivery(ctx context.Context, id string, expected int64) error {
	if s.config.Transactions == nil {
		return status_error.PluginNotReady
	}
	return s.config.Transactions.Transaction(ctx, func(txctx context.Context) error {
		account, err := s.config.Store.Account(txctx, id)
		if err != nil {
			return err
		}
		if account.Generation != expected {
			return status_error.PluginAccountConflict
		}
		account.DeliveryMaterials = nil
		return s.config.Store.UpdateAccount(txctx, account, expected)
	})
}

// DeliveryAuthentication 只读取与版本、组件及授权端点匹配的私有材料。
func DeliveryAuthentication(version models.PluginVersion, config models.PluginConfiguration, account models.PluginAccount, target string) ([]commandbridge.Binding, string, error) {
	if err := plugins.ValidateContract(version.Package); err != nil {
		return nil, "", err
	}
	if version.Package.Integration.Authentication == nil {
		return nil, "", nil
	}
	if len(version.Package.Integration.Authentication.Bindings) == 0 && len(version.Package.Integration.Authentication.SkillBindings) == 0 {
		return nil, "", nil
	}
	// 公共组件使用同一账号材料，显式目标差异使用其声明的槽位。
	target = plugins.AuthenticationTargetRuntime(version.Package, target)
	key := materialKey(version, config, target)
	for _, material := range account.DeliveryMaterials {
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

// skillEnvironmentReady 要求账号声明的环境均有值，禁止受管工具回落到继承的账号配置。
func skillEnvironmentReady(declaration plugins.SkillAuthenticationBinding, environment map[string]string) bool {
	for _, key := range declaration.EnvironmentKeys {
		if strings.TrimSpace(environment[key]) == "" {
			return false
		}
	}
	return len(declaration.EnvironmentKeys) > 0
}

// SkillDeliveryReady 复核冷租约中的技能材料，以及受管 PATH 的第一个搜索目录。
func SkillDeliveryReady(id string, declaration plugins.SkillAuthenticationBinding, bindings []commandbridge.Binding, data string) bool {
	for _, binding := range bindings {
		if binding.ComponentID != plugins.ComponentID(id, "skill:"+declaration.Skill) {
			continue
		}
		if !skillEnvironmentReady(declaration, binding.Environment) {
			return false
		}
		for _, key := range declaration.PrivatePathKeys {
			path := binding.Environment[key]
			if key == "PATH" {
				parts := filepath.SplitList(path)
				if len(parts) == 0 {
					return false
				}
				path = parts[0]
			}
			root, err := filepath.EvalSymlinks(data)
			if err != nil {
				return false
			}
			real, err := filepath.EvalSymlinks(path)
			if err != nil {
				return false
			}
			rel, err := filepath.Rel(root, real)
			if err != nil || !filepath.IsLocal(rel) {
				return false
			}
		}
		return true
	}
	return false
}

// MCPDeliveryReady 核实组件要求的每个私有头均已在本版本的授权材料中提供。
func MCPDeliveryReady(id, server string, declarations []plugins.AuthenticationBinding, bindings []commandbridge.Binding) bool {
	component := plugins.ComponentID(id, "mcp:"+server)
	for _, declaration := range declarations {
		if declaration.Server == server && boundHeader(component, declaration.Header, bindings) == "" {
			return false
		}
	}
	return true
}

func boundHeader(component, name string, bindings []commandbridge.Binding) string {
	for _, binding := range bindings {
		if binding.ComponentID == component {
			for key, value := range binding.Headers {
				if strings.EqualFold(key, name) && strings.TrimSpace(value) != "" {
					return value
				}
			}
		}
	}
	return ""
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
		value := boundHeader(componentID, declaration.Header, bindings)
		if value == "" {
			continue
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
func ResolveBoundAuthentication(ctx context.Context, driver *commandbridge.Driver, version models.PluginVersion, config models.PluginConfiguration, declarations []plugins.AuthenticationBinding, native bool, connectors []models.MCPConnector, environment map[string]string) error {
	id := version.PluginID
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
	pkg := version.Package
	if err := plugins.ValidateContract(pkg); err != nil {
		return err
	}
	declaration := *pkg.Integration.Authentication
	declaration.Bindings = selected
	declaration.SkillBindings = nil
	pkg.Integration.Authentication = &declaration
	version.Package = pkg
	materials, err := ResolveDeliveryMaterials(ctx, driver, version, config)
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
