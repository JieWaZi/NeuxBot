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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/plugininstallation"
)

// preparedPlugins 是原生消费者租约保护的能力投射，不创建独立 Skill/MCP 资源。
type preparedPlugins struct {
	// skills 是可按需读取的原始插件技能。
	skills []agentapi.Skill
	// connectors 是经过配置和传输能力检查的派生连接器。
	connectors []models.MCPConnector
	// environment 是技能授权投射使用的私有进程变量。
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
	// allowedConnectorIDs 是交付连接器的冻结白名单。
	allowedConnectorIDs *[]string
	// allowedAgentDomains 是入口冻结的软件能力范围。
	allowedAgentDomains *[]string
}

// frozenPluginResources 固定廉价仓储事实，资源方法只消费这些描述值。
type frozenPluginResources struct {
	// wakerID 是声明所属伙伴，冷获取时沿同一安装范围复核。
	wakerID string
	// pluginIDs 是冷准备占位期间即保护管理回收的候选插件身份。
	pluginIDs []string
	// key 仅包含安装、版本、配置与交付限制，不包含消息或授权令牌。
	key string
	// selected 保存当前消息明确引用的公开资源名称。
	selected []plugins.Snapshot
	// selection 是固定的插件交付约束。
	selection pluginResourceSelection
	// detection 提供已注册目标的资源目录与版本适配信息。
	detection agentapi.DetectionResult
	// declarations 持有启用版本的目标、普通配置与秘密存在性，不携带明文秘密或授权材料。
	declarations []models.PluginExecutionDeclaration
}

func pluginSelection(request pendingMessage) pluginResourceSelection {
	return pluginResourceSelection{
		selectedPluginIDs:   slices.Clone(request.message.PluginIDs),
		allowedConnectorIDs: cloneConnectorAllowlist(request.allowedConnectorIDs),
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
func (m *service) freezePluginResources(ctx context.Context, waker string, request pendingMessage, detection agentapi.DetectionResult) (frozenPluginResources, error) {
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
	identities := []map[string]any{}
	selected := []plugins.Snapshot{}
	pluginIDs := []string{}
	enabled := map[string]bool{}
	fixed := make([]models.PluginExecutionDeclaration, 0, len(declarations))
	for _, declaration := range declarations {

		fixed = append(fixed, declaration)
		enabled[declaration.ID] = true
		pluginIDs = append(pluginIDs, declaration.ID)
		if slices.Contains(request.message.PluginIDs, declaration.ID) {
			selected = append(selected, plugins.Snapshot{ID: declaration.ID, Name: declaration.Name})
		}
		identities = append(identities, map[string]any{"id": declaration.ID, "versionId": declaration.VersionID, "digest": declaration.Digest, "trustedAt": declaration.TrustedAt, "revision": declaration.ConfigurationRevision})
	}
	for _, id := range request.message.PluginIDs {
		if !enabled[id] {
			return frozenPluginResources{}, status_error.PluginNotReady
		}
	}

	cli := detection.Runtime.BaseRuntimeID
	if cli == "" {
		cli = detection.Runtime.ID
	}
	delivery := []any{cli, nativePluginVersionSupported(cli, detection.Version), detection.Runtime.SkillDirectoryName}
	body, err := json.Marshal([]any{waker, delivery, identities, request.allowedConnectorIDs, request.allowedAgentDomains})
	if err != nil {
		return frozenPluginResources{}, status_error.BadRequest
	}
	digest := sha256.Sum256(body)
	return frozenPluginResources{wakerID: waker, pluginIDs: pluginIDs, key: hex.EncodeToString(digest[:]), selected: selected, selection: pluginSelection(request), detection: detection, declarations: fixed}, nil
}

// acquirePluginResources 核对发布声明后取得真实内容与共享身份租约。
func (m *service) acquirePluginResources(ctx context.Context, frozen frozenPluginResources, usage ...authorization.Usage) (preparedPlugins, error) {
	if err := ctx.Err(); err != nil {
		return preparedPlugins{}, err
	}
	// 资源声明发生在输入接纳之前；不能把已经停用或换版的安装当作当前授权。
	if m.pluginController != nil {
		current, err := m.pluginController.ExecutionDeclarations(ctx, frozen.wakerID)
		if err != nil {
			return preparedPlugins{}, err
		}
		if len(current) != len(frozen.declarations) {
			return preparedPlugins{}, status_error.PluginVersionConflict
		}
		byID := make(map[string]models.PluginExecutionDeclaration, len(current))
		for _, declaration := range current {
			byID[declaration.ID] = declaration
		}
		for _, declaration := range frozen.declarations {
			actual, exists := byID[declaration.ID]
			if !exists || actual.VersionID != declaration.VersionID || actual.Digest != declaration.Digest || actual.TrustedAt != declaration.TrustedAt {
				return preparedPlugins{}, status_error.PluginVersionConflict
			}
			if actual.ConfigurationRevision != declaration.ConfigurationRevision {
				return preparedPlugins{}, status_error.PluginConfigurationConflict
			}
		}
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
			for _, release := range slices.Backward(releases) {
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

		if version.ID == "" {
			continue
		}
		required := selected[id]
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
		if err := pluginProjectionError(id, logicalVersion.Package, version.Package, request, detection, m.pluginLauncher); err != nil {
			if required {
				return result, err
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
		if hasAuthentication {
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

		candidateSkills := []agentapi.Skill{}
		candidateConnectors := []models.MCPConnector{}
		available := map[string]bool{}
		if detection.Runtime.SkillDirectoryName != "" {
			for _, skill := range version.Package.Skills {
				candidateSkills = append(candidateSkills, agentapi.Skill{Name: skill.Name, Path: filepath.Join(root, skill.Path), Digest: skill.Digest})
				available[skill.Key] = true
				snapshot.ComponentIDs = append(snapshot.ComponentIDs, plugins.ComponentID(id, "skill:"+skill.Key))
			}
		}
		for _, server := range version.Package.Servers {

			transport := constants.RuntimeConnectorTransport(server.Type)
			if server.Type == "streamable-http" {
				transport = constants.RuntimeConnectorTransportStreamableHTTP
			}
			componentID := plugins.ComponentID(id, "mcp:"+server.Key)
			if request.allowedConnectorIDs != nil && !slices.Contains(*request.allowedConnectorIDs, componentID) {
				continue
			}
			if supported, _ := agentapi.ConnectorCompatibility(cli, transport); !supported {
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
			plugininstallation.ProjectBoundAuthentication(id, authenticationBindings, false, candidateConnectors, candidateEnvironment, bindings)
		} else if hasAuthentication {
			for _, binding := range bindings {
				isSkill := false
				for _, declaration := range version.Package.Skills {
					if plugins.ComponentID(id, "skill:"+declaration.Key) == binding.ComponentID && slices.ContainsFunc(candidateSkills, func(skill agentapi.Skill) bool { return skill.Name == declaration.Name }) {
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

	return result, nil
}

// mergePluginSkills 按发布摘要共用同名技能；实际复制内容由冷发布核验。
func mergePluginSkills(independent, derived []agentapi.Skill) ([]agentapi.Skill, error) {
	result := make([]agentapi.Skill, 0, len(independent)+len(derived))
	indexes := map[string]int{}
	for _, item := range append(append([]agentapi.Skill{}, independent...), derived...) {
		existing, ok := indexes[item.Name]
		if !ok {
			indexes[item.Name] = len(result)
			result = append(result, item)
			continue
		}
		if item.Digest == "" || result[existing].Digest != item.Digest {
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

// nativePluginVersionSupported 标识当前必须完整原生交付的版本组合；执行侧明确拒绝该组合。
func nativePluginVersionSupported(cli, version string) bool {
	minimum := []int{}
	switch cli {
	case agentapi.RuntimeIDCodex:
		minimum = []int{0, 161, 0}
	case agentapi.RuntimeIDClaude:
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

// pluginProjectionError 共用目标交付约束；实际内容和秘密仍由冷获取验证。
func pluginProjectionError(pluginID string, logical, target plugins.Package, request pluginResourceSelection, detection agentapi.DetectionResult, launcher string) error {
	extension, err := plugins.ClientExtension(logical)
	if err != nil {
		return err
	}
	targetExtension, err := plugins.ClientExtension(target)
	if err != nil {
		return err
	}
	cli := detection.Runtime.BaseRuntimeID
	if cli == "" {
		cli = detection.Runtime.ID
	}
	bindings := plugins.TargetAuthenticationBindings(logical, cli)
	auth := logical.Integration.Authentication
	if auth != nil && len(auth.Bindings) > 0 && len(bindings) == 0 {
		return status_error.PluginForbidden
	}
	hasAuthentication := auth != nil || target.Integration.Authentication != nil
	if nativePluginVersionSupported(cli, detection.Version) && request.allowedConnectorIDs == nil && request.allowedAgentDomains == nil && len(extension.Bindings) == 0 && len(targetExtension.Bindings) == 0 && (len(extension.Config) == 0 || target.Format != constants.PluginFormatPortable) && (!hasAuthentication || len(target.Servers) == 0 || len(bindings) > 0) {
		return status_error.PluginNotReady
	}
	if hasAuthentication && (request.allowedConnectorIDs != nil || request.allowedAgentDomains != nil) && len(target.Skills) > 0 {
		return status_error.PluginForbidden
	}
	available := map[string]bool{}
	if detection.Runtime.SkillDirectoryName != "" {
		for _, skill := range target.Skills {
			available[skill.Key] = true
		}
	}
	for _, server := range target.Servers {
		if request.allowedConnectorIDs != nil && !slices.Contains(*request.allowedConnectorIDs, plugins.ComponentID(pluginID, "mcp:"+server.Key)) {
			continue
		}
		transport := constants.RuntimeConnectorTransport(server.Type)
		if server.Type == "streamable-http" {
			transport = constants.RuntimeConnectorTransportStreamableHTTP
		}
		if supported, _ := agentapi.ConnectorCompatibility(cli, transport); !supported || (transport == constants.RuntimeConnectorTransportStdio && launcher == "") {
			continue
		}
		available[server.Key] = true
	}
	for _, key := range extension.RequiredComponents {
		if !available[key] {
			return status_error.PluginNotReady
		}
	}
	if len(available) == 0 {
		return status_error.PluginNotReady
	}
	return nil
}

// selectNativePluginSkills 只从发布的不可变目标与配置声明选择技能，不读取来源正文或秘密。
func (m *service) selectNativePluginSkills(ctx context.Context, frozen frozenPluginResources) ([]agentapi.Skill, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := []agentapi.Skill{}
	cli := frozen.detection.Runtime.BaseRuntimeID
	if cli == "" {
		cli = frozen.detection.Runtime.ID
	}
	for _, declaration := range frozen.declarations {
		required := slices.Contains(frozen.selection.selectedPluginIDs, declaration.ID)
		pkg, relative, targetErr := plugins.DeclaredTarget(declaration.Package, cli)
		if targetErr != nil {
			if required {
				return nil, targetErr
			}
			continue
		}
		values := map[string]string{}
		for _, name := range declaration.ConfiguredSecrets {
			values[name] = "configured"
		}
		readyErr := plugins.ValidateValues(declaration.Package, declaration.Values, values)
		if declaration.Package.RequiresTrust() && declaration.TrustedAt == 0 {
			readyErr = status_error.MCPConnectorTrustRequired
		}
		if readyErr != nil {
			if required {
				return nil, readyErr
			}
			continue
		}
		extension, err := plugins.ClientExtension(pkg)
		if err != nil {
			if required {
				return nil, err
			}
			continue
		}
		targetValues, targetSecrets := map[string]string{}, map[string]string{}
		for _, field := range extension.Config {
			if field.Secret {
				if value, exists := values[field.Key]; exists {
					targetSecrets[field.Key] = value
				}
			} else if value, exists := declaration.Values[field.Key]; exists {
				targetValues[field.Key] = value
			}
		}
		if err := plugins.ValidateValues(pkg, targetValues, targetSecrets); err != nil {
			if required {
				return nil, err
			}
			continue
		}
		if err := pluginProjectionError(declaration.ID, declaration.Package, pkg, frozen.selection, frozen.detection, m.pluginLauncher); err != nil {
			if required {
				return nil, err
			}
			continue
		}
		if frozen.detection.Runtime.SkillDirectoryName == "" {
			continue
		}
		for _, skill := range pkg.Skills {
			result = append(result, agentapi.Skill{Name: skill.Name, Path: filepath.Join(m.skillDataDirectory, declaration.ContentPath, relative, skill.Path), Digest: skill.Digest})
		}
	}
	return result, nil
}
