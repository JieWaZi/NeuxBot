package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/packagefiles"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/plugininstallation"
)

// preparedPlugins 是原生消费者租约保护的能力投射，不创建独立 Skill/MCP 资源。
type preparedPlugins struct {
	// native 是消费者独占的完整原生包，不能同时展开其 Skill/MCP。
	native []plugins.NativeTarget
	// skills 是可按需读取的原始插件技能。
	skills []runtimecontract.Skill
	// connectors 是经过配置和传输能力检查的派生连接器。
	connectors []models.MCPConnector
	// environment 是包内桥接及原生目标交付的私有进程变量。
	environment map[string]string
	// snapshots 记录实际交付组件与版本事实。
	snapshots []plugins.Snapshot
	// updateUsage 随每轮开始和结束更新真实执行引用，不改变消费者租约。
	updateUsage func(...authorization.Usage)
	// release 在真实原生消费者关闭后释放共享身份租约。
	release func()
}

// pluginResourceSelection 冻结插件交付约束，不持有消息、附件或本轮执行能力。
type pluginResourceSelection struct {
	// selectedPluginIDs 是必须成功交付的显式插件身份。
	selectedPluginIDs []string
	// retry 表示必须重现原执行的插件版本与配置。
	retry bool
	// snapshots 是失败重试固定的原始资源事实。
	snapshots []plugins.Snapshot
	// allowedConnectorIDs 是交付连接器的冻结白名单。
	allowedConnectorIDs *[]string
	// allowedAgentDomains 是入口冻结的软件能力范围。
	allowedAgentDomains *[]string
}

// frozenPluginResources 固定廉价仓储事实，资源方法只消费这些描述值。
type frozenPluginResources struct {
	// pluginIDs 是冷准备占位期间即保护管理回收的候选插件身份。
	pluginIDs []string
	// key 仅包含安装、版本、配置与交付限制，不包含消息或授权令牌。
	key string
	// selected 保存当前消息明确引用的公开资源名称。
	selected []plugins.Snapshot
	// selection 是固定的插件交付与重试约束。
	selection pluginResourceSelection
	// detection 是本次选用的原生能力快照。
	detection runtimeconfig.DetectionResult
	// declarations 仅持有当前伙伴启用的版本、信任与配置修订，不携带实际配置和包。
	declarations []models.PluginExecutionDeclaration
}

func pluginSelection(request pendingMessage) pluginResourceSelection {
	return pluginResourceSelection{
		selectedPluginIDs: slices.Clone(request.message.PluginIDs), retry: request.retry,
		snapshots: slices.Clone(request.pluginSnapshots), allowedConnectorIDs: cloneConnectorAllowlist(request.allowedConnectorIDs),
		allowedAgentDomains: cloneConnectorAllowlist(request.allowedAgentDomains),
	}
}

func pluginTargetDigest(sourceRoot, targetRoot, verifiedDigest string) (string, error) {
	if filepath.Clean(sourceRoot) == filepath.Clean(targetRoot) && verifiedDigest != "" {
		return verifiedDigest, nil
	}
	return plugins.ContentDigest(targetRoot)
}

// freezePluginResources 仅读取安装事实，不启动桥接或访问插件内容。
func (m *service) freezePluginResources(ctx context.Context, waker string, request pendingMessage, detection runtimeconfig.DetectionResult) (frozenPluginResources, error) {
	var declarations []models.PluginExecutionDeclaration
	if m.pluginController != nil {
		var err error
		declarations, err = m.pluginController.ExecutionDeclarations(ctx, waker)
		if err != nil {
			return frozenPluginResources{}, err
		}
	}
	declarations = slices.Clone(declarations)
	slices.SortFunc(declarations, func(a, b models.PluginExecutionDeclaration) int { return strings.Compare(a.ID, b.ID) })
	wanted := map[string]plugins.Snapshot{}
	for _, snapshot := range request.pluginSnapshots {
		wanted[snapshot.ID] = snapshot
	}
	identities := []map[string]any{}
	selected := []plugins.Snapshot{}
	pluginIDs := []string{}
	enabled := map[string]bool{}
	fixed := make([]models.PluginExecutionDeclaration, 0, len(declarations))
	for _, declaration := range declarations {
		if request.retry {
			if _, exists := wanted[declaration.ID]; !exists {
				continue
			}
		}
		if prior, exists := wanted[declaration.ID]; exists && request.retry {
			if prior.ConfigurationRevision != declaration.ConfigurationRevision {
				return frozenPluginResources{}, status_error.PluginConfigurationConflict
			}
			if prior.VersionID != declaration.VersionID {
				return frozenPluginResources{}, status_error.PluginVersionConflict
			}
		}
		fixed = append(fixed, declaration)
		enabled[declaration.ID] = true
		pluginIDs = append(pluginIDs, declaration.ID)
		if slices.Contains(request.message.PluginIDs, declaration.ID) {
			selected = append(selected, plugins.Snapshot{ID: declaration.ID, Name: declaration.Name})
		}
		identities = append(identities, map[string]any{"id": declaration.ID, "name": declaration.Name, "versionId": declaration.VersionID, "digest": declaration.Digest, "trustedAt": declaration.TrustedAt, "revision": declaration.ConfigurationRevision})
	}
	for _, id := range request.message.PluginIDs {
		if !enabled[id] {
			return frozenPluginResources{}, status_error.PluginNotReady
		}
	}
	for id := range wanted {
		if !enabled[id] {
			return frozenPluginResources{}, status_error.PluginNotReady
		}
	}
	body, err := json.Marshal([]any{waker, detection, identities, request.allowedConnectorIDs, request.allowedAgentDomains})
	if err != nil {
		return frozenPluginResources{}, status_error.BadRequest
	}
	digest := sha256.Sum256(body)
	return frozenPluginResources{pluginIDs: pluginIDs, key: hex.EncodeToString(digest[:]), selected: selected, selection: pluginSelection(request), detection: detection, declarations: fixed}, nil
}

// acquirePluginResources 只从已冻结描述值取得内容与共享身份租约。
func (m *service) acquirePluginResources(ctx context.Context, frozen frozenPluginResources, usage ...authorization.Usage) (preparedPlugins, error) {
	if err := ctx.Err(); err != nil {
		return preparedPlugins{}, err
	}
	request, detection := frozen.selection, frozen.detection
	result := preparedPlugins{environment: map[string]string{}, release: func() {}, updateUsage: func(...authorization.Usage) {}}
	executionID := ""
	if len(usage) > 0 {
		executionID = usage[0].ExecutionID
	}
	logSlowStage := func(id, stage string, started time.Time) {
		if elapsed := time.Since(started); elapsed >= 100*time.Millisecond {
			logx.Warnf("chat plugin preparation stage slow: execution_id=%s plugin_id=%s stage=%s duration=%s", executionID, id, stage, elapsed)
		}
	}
	selected := map[string]bool{}
	for _, id := range request.selectedPluginIDs {
		selected[id] = true
	}
	wanted := map[string]plugins.Snapshot{}
	for _, item := range request.snapshots {
		wanted[item.ID] = item
	}
	found := map[string]bool{}
	releases := []func(){}
	usageUpdates := []func(...authorization.Usage){}
	result.updateUsage = func(uses ...authorization.Usage) {
		for _, update := range usageUpdates {
			update(uses...)
		}
	}
	var once sync.Once
	result.release = func() {
		once.Do(func() {
			for _, release := range releases {
				release()
			}
		})
	}
	for _, declaration := range frozen.declarations {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		id := declaration.ID
		pluginStarted := time.Now()
		prior, isRetryResource := wanted[id]
		if request.retry && !isRetryResource {
			continue
		}
		version, err := m.pluginController.Version(ctx, declaration.VersionID)
		if err != nil {
			return result, err
		}
		if version.PluginID != id || version.Digest != declaration.Digest || version.TrustedAt != declaration.TrustedAt {
			return result, status_error.PluginVersionConflict
		}
		configuration, err := m.pluginController.ExecutionConfiguration(ctx, id)
		if err != nil {
			return result, err
		}
		if configuration.Revision != declaration.ConfigurationRevision {
			return result, status_error.PluginConfigurationConflict
		}
		if isRetryResource {
			if configuration.Revision != prior.ConfigurationRevision {
				return result, status_error.PluginConfigurationConflict
			}
			if version.ID != prior.VersionID {
				return result, status_error.PluginVersionConflict
			}
		}
		if version.ID == "" {
			continue
		}
		required := selected[id] || isRetryResource
		secrets := map[string]string{}
		for _, secret := range configuration.Secrets {
			if secret.Configured {
				secrets[secret.Name] = secret.Value
			}
		}
		readyErr := plugins.ValidateValues(version.Package, configuration.Values, secrets)
		extension, extensionErr := plugins.ClientExtension(version.Package)
		if readyErr == nil {
			readyErr = extensionErr
		}
		if version.Package.RequiresTrust() && version.TrustedAt == 0 {
			readyErr = status_error.MCPConnectorTrustRequired
		}
		if readyErr != nil {
			if required {
				return result, readyErr
			}
			continue
		}
		root, pathErr := plugins.ContentPath(m.skillDataDirectory, version.ContentPath)
		if pathErr != nil {
			if required {
				return result, pathErr
			}
			continue
		}
		data := filepath.Join(m.skillDataDirectory, "plugins", "data", id)
		if err = os.MkdirAll(data, 0700); err != nil {
			return result, status_error.PluginStorageError
		}
		if m.pluginContents != nil {
			started := time.Now()
			release, err := m.pluginContents.Acquire(root, data)
			logSlowStage(id, "content_lease", started)
			if err != nil {
				return result, err
			}
			releases = append(releases, release)
		}
		started := time.Now()
		verifyErr := plugins.VerifyContent(root, version.Digest)
		logSlowStage(id, "content_verify", started)
		if err := verifyErr; err != nil {
			if required {
				return result, err
			}
			continue
		}
		logicalVersion := version
		sourceRoot := root
		cli := detection.Runtime.BaseRuntimeID
		if cli == "" {
			cli = detection.Runtime.ID
		}
		selectedPackage, selectedRoot, selectErr := plugins.SelectTarget(root, version.Package, cli)
		if selectErr != nil {
			if required {
				return result, selectErr
			}
			continue
		}
		root = selectedRoot
		version.Package = selectedPackage
		// 逻辑资源可以存储多个目标的配置，当前交付只校验选中目标实际声明的字段。
		targetExtension, targetErr := plugins.ClientExtension(version.Package)
		if targetErr == nil {
			targetValues, targetSecrets := map[string]string{}, map[string]string{}
			for _, field := range targetExtension.Config {
				if field.Secret {
					if value, ok := secrets[field.Key]; ok {
						targetSecrets[field.Key] = value
					}
				} else if value, ok := configuration.Values[field.Key]; ok {
					targetValues[field.Key] = value
				}
			}
			targetErr = plugins.ValidateValues(version.Package, targetValues, targetSecrets)
		}
		if targetErr != nil {
			if required {
				return result, targetErr
			}
			continue
		}
		values := map[string]string{}
		for _, field := range extension.Config {
			if !field.Secret {
				values[field.Key] = field.Default
			}
		}
		for key, value := range configuration.Values {
			values[key] = value
		}
		for key, value := range secrets {
			values[key] = value
		}
		started = time.Now()
		targetDigest, digestErr := pluginTargetDigest(sourceRoot, root, version.Digest)
		logSlowStage(id, "target_digest", started)
		if digestErr != nil {
			if required {
				return result, digestErr
			}
			continue
		}
		snapshot := plugins.Snapshot{TargetDigest: targetDigest, TargetVersion: version.Package.Manifest.Version, Delivery: constants.PluginDeliveryProjection, LoadStage: constants.PluginLoadStagePrepared, ID: id, Name: declaration.Name, VersionID: version.ID, Version: version.Version, ConfigurationRevision: configuration.Revision, ComponentIDs: []string{}}
		authenticationBindings := plugins.TargetAuthenticationBindings(logicalVersion.Package, cli)
		if declaration := logicalVersion.Package.Integration.Authentication; declaration != nil && len(declaration.Bindings) > 0 && len(authenticationBindings) == 0 {
			// 显式绑定同时限定授权目标；不匹配时不能降级为无范围的旧桥接解析。
			if required {
				return result, status_error.PluginForbidden
			}
			continue
		}
		authVersion := logicalVersion
		if len(authenticationBindings) == 0 {
			authVersion.Package = version.Package
			authVersion.Package.SourcePath, err = filepath.Rel(sourceRoot, root)
			if err != nil {
				return result, status_error.PluginContentInvalid
			}
		}
		hasAuthentication := authVersion.Package.Integration.Authentication != nil
		bridgeRelease := func() {}
		candidateEnvironment := map[string]string{}
		var candidateNative *plugins.NativeTarget
		if hasAuthentication {
			if (request.allowedConnectorIDs != nil || request.allowedAgentDomains != nil) && len(version.Package.Skills) > 0 {
				if required {
					return result, status_error.PluginForbidden
				}
				continue
			}
			if m.pluginInstallations != nil {
				update, retained, authErr := m.pluginInstallations.IdentityLease(id, usage...)
				if authErr != nil {
					return result, authErr
				}
				releases = append(releases, retained)
				usageUpdates = append(usageUpdates, update)
				bridgeRelease = retained
			}
			if m.pluginController != nil {
				current, readErr := m.pluginController.Configuration(ctx, id)
				if readErr != nil {
					return result, readErr
				}
				if current.Revision != configuration.Revision {
					return result, status_error.PluginConfigurationConflict
				}
				configuration.DeliveryMaterials = current.DeliveryMaterials
			}

		}
		nativeEligible := nativePluginVersionSupported(cli, detection.Version) && request.allowedConnectorIDs == nil && request.allowedAgentDomains == nil && len(extension.Bindings) == 0 && len(targetExtension.Bindings) == 0 && (len(extension.Config) == 0 || version.Package.Format != constants.PluginFormatPortable) && (!hasAuthentication || len(version.Package.Servers) == 0 || len(authenticationBindings) > 0)
		candidateSkills := []runtimecontract.Skill{}
		candidateConnectors := []models.MCPConnector{}
		available := map[string]bool{}
		if detection.Capabilities.Has(runtimeconfig.CapabilitySkillDirectory) {
			for _, skill := range version.Package.Skills {
				candidateSkills = append(candidateSkills, runtimecontract.Skill{Name: skill.Name, Path: filepath.Join(root, skill.Path)})
				available[skill.Key] = true
				snapshot.ComponentIDs = append(snapshot.ComponentIDs, plugins.ComponentID(id, "skill:"+skill.Key))
			}
		}
		for _, server := range version.Package.Servers {
			if nativeEligible {
				available[server.Key] = true
				continue
			}
			transport := constants.RuntimeConnectorTransport(server.Type)
			if server.Type == "streamable-http" {
				transport = constants.RuntimeConnectorTransportStreamableHTTP
			}
			componentID := plugins.ComponentID(id, "mcp:"+server.Key)
			if request.allowedConnectorIDs != nil && !slices.Contains(*request.allowedConnectorIDs, componentID) {
				continue
			}
			if !runtimecontract.DetectedConnectorTransportSupported(detection, transport) {
				continue
			}
			resolved, resolveErr := plugins.ResolveServer(version.Package, server, root, data, values)
			if resolveErr != nil {
				if required {
					return result, resolveErr
				}
				continue
			}
			connector := models.MCPConnector{ID: componentID, Name: server.Key, RuntimeKey: "ally_plugin_" + componentID, Transport: constants.MCPTransport(transport), Command: resolved.Server.Command, Arguments: resolved.Server.Args, URL: resolved.Server.URL, TimeoutMS: 30000}
			if transport == constants.RuntimeConnectorTransportStdio {
				if m.pluginLauncher == "" {
					if required {
						return result, status_error.PluginNotReady
					}
					continue
				}
				connector.Command = m.pluginLauncher
				connector.Arguments = append([]string{constants.PluginStdioMode, "--cwd", resolved.WorkingDirectory, "--", resolved.Server.Command}, resolved.Server.Args...)
			}
			if transport == constants.RuntimeConnectorTransportStdio {
				for name, value := range resolved.Server.Env {
					connector.Environment = append(connector.Environment, models.MCPSecretDefinition{Name: name, Value: value, Configured: true})
				}
			}
			for name, value := range resolved.Server.Headers {
				connector.Headers = append(connector.Headers, models.MCPSecretDefinition{Name: name, Value: value, Configured: true})
			}
			candidateConnectors = append(candidateConnectors, connector)
			available[server.Key] = true
			snapshot.ComponentIDs = append(snapshot.ComponentIDs, componentID)
		}
		bindings, accountID, bindErr := plugininstallation.DeliveryAuthentication(authVersion, configuration, cli)
		snapshot.ProviderAccountID = accountID
		if bindErr != nil {
			return result, bindErr
		}
		if hasAuthentication && len(authenticationBindings) > 0 {
			plugininstallation.ProjectBoundAuthentication(id, authenticationBindings, nativeEligible, candidateConnectors, candidateEnvironment, bindings)
		} else if hasAuthentication {
			for _, binding := range bindings {
				isSkill := false
				for _, declaration := range version.Package.Skills {
					if plugins.ComponentID(id, "skill:"+declaration.Key) == binding.ComponentID && slices.ContainsFunc(candidateSkills, func(skill runtimecontract.Skill) bool { return skill.Name == declaration.Name }) {
						isSkill = true
					}
				}
				if isSkill {
					for key, value := range binding.Environment {
						if key == "PATH" {
							candidateEnvironment[key] = plugininstallation.MergeDeliveryPath(candidateEnvironment[key], value, m.skillDataDirectory)
							continue
						}
						if old, ok := candidateEnvironment[key]; ok && old != value {
							return result, status_error.PluginConfigurationConflict
						}
						candidateEnvironment[key] = value
					}
					continue
				}
				for index := range candidateConnectors {
					if candidateConnectors[index].ID != binding.ComponentID {
						continue
					}
					for key, value := range binding.Environment {
						candidateConnectors[index].Environment = append(candidateConnectors[index].Environment, models.MCPSecretDefinition{Name: key, Value: value, Configured: true})
					}
					for key, value := range binding.Headers {
						candidateConnectors[index].Headers = append(candidateConnectors[index].Headers, models.MCPSecretDefinition{Name: key, Value: value, Configured: true})
					}
				}
			}
		}

		if nativeEligible {
			// 只注入当前目标引用的配置，适配器合成的请求头仅在私有进程环境中展开。
			for _, field := range targetExtension.Config {
				if !nativeConfigurationKeyAllowed(field.Key) {
					return result, status_error.PluginForbidden
				}
				if value, ok := values[field.Key]; ok {
					candidateEnvironment[field.Key] = value
				}
			}
			started = time.Now()
			native, nativeErr := plugins.PrepareNativeTargetWithDigest(filepath.Join(m.skillDataDirectory, "plugins", constants.PluginDeliveryNative), id, version.Package, root, cli, targetDigest, authenticationBindings...)
			logSlowStage(id, "native_target", started)
			if nativeErr == nil {
				var configurationEnvironment map[string]string
				configurationEnvironment, nativeErr = native.ConfigurationEnvironment(values)
				for key, value := range configurationEnvironment {
					if existing, exists := candidateEnvironment[key]; exists && existing != value {
						nativeErr = status_error.PluginConfigurationConflict
						break
					}
					candidateEnvironment[key] = value
				}
			}
			if nativeErr != nil {
				if required {
					return result, nativeErr
				}
				bridgeRelease()
				continue
			}
			if nativeErr == nil {
				snapshot.Delivery = constants.PluginDeliveryNative
				snapshot.NativePluginID = native.Name
				if cli == runtimeconfig.RuntimeIDCodex {
					snapshot.NativePluginID += "@" + constants.PluginNativeMarketplace
				}
				snapshot.TargetDigest = native.Digest
				snapshot.ComponentIDs = []string{}
				for _, skill := range version.Package.Skills {
					snapshot.ComponentIDs = append(snapshot.ComponentIDs, plugins.ComponentID(id, "skill:"+skill.Key))
				}
				for _, server := range version.Package.Servers {
					snapshot.ComponentIDs = append(snapshot.ComponentIDs, plugins.ComponentID(id, "mcp:"+server.Key))
				}
				// 原生hooks/commands等完整保留，用插件身份记录整体交付。
				if len(snapshot.ComponentIDs) == 0 {
					snapshot.ComponentIDs = append(snapshot.ComponentIDs, plugins.ComponentID(id, constants.PluginDeliveryNative))
				}
				candidateNative = &native
				candidateSkills = nil
				candidateConnectors = nil
			}
		}
		missingRequired := false
		for _, key := range extension.RequiredComponents {
			if !available[key] {
				missingRequired = true
			}
		}
		if missingRequired || len(snapshot.ComponentIDs) == 0 {
			if required {
				return result, status_error.PluginNotReady
			}
			bridgeRelease()
			continue
		}
		if isRetryResource {
			priorDelivery := prior.Delivery
			if priorDelivery == "" {
				priorDelivery = constants.PluginDeliveryProjection
			}
			priorNativeID := prior.NativePluginID
			if cli == runtimeconfig.RuntimeIDCodex && snapshot.NativePluginID != "" && strings.HasPrefix(priorNativeID, snapshot.NativePluginID+"-") {
				suffix := strings.TrimPrefix(priorNativeID, snapshot.NativePluginID+"-")
				if regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(suffix) {
					priorNativeID = snapshot.NativePluginID
				}
			}
			if !slices.Equal(snapshot.ComponentIDs, prior.ComponentIDs) || snapshot.Delivery != priorDelivery || snapshot.NativePluginID != priorNativeID || (prior.TargetDigest != "" && snapshot.TargetDigest != prior.TargetDigest) {
				return result, status_error.PluginNotReady
			}
			if hasAuthentication && snapshot.ProviderAccountID != prior.ProviderAccountID {
				return result, status_error.PluginConfigurationConflict
			}
		}

		if candidateNative != nil {
			result.native = append(result.native, *candidateNative)
		}
		for key, value := range candidateEnvironment {
			if key == "PATH" && hasAuthentication {
				result.environment[key] = plugininstallation.MergeDeliveryPath(result.environment[key], value, m.skillDataDirectory)
				continue
			}

			if existing, ok := result.environment[key]; ok && existing != value {
				return result, status_error.PluginConfigurationConflict
			}
			result.environment[key] = value
		}
		result.skills = append(result.skills, candidateSkills...)
		result.connectors = append(result.connectors, candidateConnectors...)
		result.snapshots = append(result.snapshots, snapshot)
		found[id] = true
		logSlowStage(id, "total", pluginStarted)
	}
	for id := range selected {
		if !found[id] {
			return result, status_error.PluginNotReady
		}
	}
	for id := range wanted {
		if !found[id] {
			return result, status_error.PluginNotReady
		}
	}
	return result, nil
}

// mergePluginSkills 只在同名且完整内容相同的情况下共用一份投射。
func mergePluginSkills(independent, derived []runtimecontract.Skill) ([]runtimecontract.Skill, error) {
	result := make([]runtimecontract.Skill, 0, len(independent)+len(derived))
	indexes := map[string]int{}
	digests := map[string]string{}
	policy := packagefiles.Archive{Invalid: status_error.PluginContentInvalid, Limit: status_error.PluginContentLimitExceeded, IO: status_error.PluginStorageError}
	for _, item := range append(append([]runtimecontract.Skill{}, independent...), derived...) {
		existing, ok := indexes[item.Name]
		if !ok {
			indexes[item.Name] = len(result)
			result = append(result, item)
			continue
		}
		digest, err := policy.Digest(item.Path)
		if err != nil {
			return nil, err
		}
		previous := digests[result[existing].Path]
		if previous == "" {
			previous, err = policy.Digest(result[existing].Path)
			if err != nil {
				return nil, err
			}
			digests[result[existing].Path] = previous
		}
		if previous != digest {
			return nil, status_error.PluginSkillConflict
		}
		// 插件内容已持有执行租约，去重后继续使用受保护的不可变目录。
		result[existing].Path = item.Path
	}
	return result, nil
}

// pluginSelectionHint 将明确插件意图附在本轮消息，保留内部组件的按需发现语义。
func pluginSelectionHint(selected []plugins.Snapshot) string {
	if len(selected) == 0 {
		return ""
	}
	names := make([]string, 0, len(selected))
	for _, item := range selected {
		names = append(names, item.Name+" ("+item.ID+")")
	}
	return "\n\n用户本轮明确选择插件：" + strings.Join(names, "、") + "。请按任务需要使用其可用技能或连接器。"
}

// nativePluginScope 只比较会改变原生会话能力的固定事实，顺序变化不触发重建。
func nativePluginScope(snapshots []plugins.Snapshot) string {
	entries := []string{}
	for _, snapshot := range snapshots {
		if snapshot.Delivery == constants.PluginDeliveryNative {
			body, _ := json.Marshal([]any{snapshot.ID, snapshot.VersionID, snapshot.TargetDigest, snapshot.ConfigurationRevision, snapshot.ProviderAccountID})
			entries = append(entries, string(body))
		}
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n")
}

// nativePluginVersionSupported 仅使用已验证启动契约的CLI代际，未知或更旧版本保留Skill/MCP投射。
func nativePluginVersionSupported(cli, version string) bool {
	minimum := []int{}
	switch cli {
	case runtimeconfig.RuntimeIDCodex:
		minimum = []int{0, 161, 0}
	case runtimeconfig.RuntimeIDClaude:
		minimum = []int{2, 1, 159}
	default:
		return false
	}
	match := regexp.MustCompile(`([0-9]+)\.([0-9]+)\.([0-9]+)`).FindStringSubmatch(version)
	if len(match) != 4 {
		return false
	}
	for index, wanted := range minimum {
		value, err := strconv.Atoi(match[index+1])
		if err != nil {
			return false
		}
		if value != wanted {
			return value > wanted
		}
	}
	return true
}

// nativeConfigurationKeyAllowed 防止原生模板变量改变宿主身份、模型账号或程序加载路径。
func nativeConfigurationKeyAllowed(key string) bool {
	upper := strings.ToUpper(key)
	if len(accountconfig.Environment([]string{key + "=probe"})) == 0 {
		return false
	}
	for _, prefix := range []string{"ALLY", "NEUXALLY_", "CODEX", "CLAUDE", "NODE_", "LD_", "DYLD_", "PYTHON", "XDG_"} {
		if strings.HasPrefix(upper, prefix) {
			return false
		}
	}
	switch upper {
	case "HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "PATH", "PWD", "OLDPWD", "SHELL", "ENV", "BASH_ENV", "ZDOTDIR", "TMPDIR", "TMP", "TEMP", "BUN_OPTIONS":
		return false
	}
	return true
}
