package agent

import (
	"context"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	native "github.com/JieWaZi/NeuxBot/server/internal/agent/native"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
	acp "github.com/JieWaZi/acp-go/sdk"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// WorkspacePreparer 根据目录注册中的文件约定物化产品受管文件，不拥有CLI或账号。
type WorkspacePreparer struct {
	// runtimes 保存当前产品注册中可物化的指令与技能目录约定。
	runtimes []RuntimeSummary
}

// New 冻结注册元数据供切换智能体时清除旧文件。
func NewWorkspacePreparer(runtimes []RuntimeSummary) *WorkspacePreparer {
	return &WorkspacePreparer{runtimes: append([]RuntimeSummary(nil), runtimes...)}
}

// PrepareWorkspace 为注入的执行宿主物化原生指令与技能，不创建进程或提交输入。
func (p *WorkspacePreparer) Prepare(request SessionPlan) error {
	return p.prepareRuntimeFiles(request.Detection, request.Workspace, request.Instructions, request.Skills)
}

// prepareRuntimeFiles 把当前指令和技能准备成所选 Runtime 能直接读取的文件。
func (p *WorkspacePreparer) prepareRuntimeFiles(
	detection DetectionResult,
	workspace Workspace,
	instructions []Instruction,
	skills []Skill,
) error {
	if !workspace.Managed || workspace.Path == "" {
		if len(skills) > 0 {
			return status_error.BadRequest
		}
		return nil
	}
	if err := p.writeRuntimeInstructions(detection, workspace, instructions); err != nil {
		return err
	}
	if err := p.clearRuntimeSkillDirectories(detection, workspace.Path); err != nil {
		return err
	}
	if len(skills) == 0 {
		return nil
	}
	if err := validateRuntimeSkillSupport(detection); err != nil {
		return err
	}
	if err := validateRuntimeSkills(skills); err != nil {
		return err
	}

	root := runtimeSkillRoot(detection, workspace.Path)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return status_error.SkillUnavailable
	}
	for _, skill := range skills {
		target := runtimeSkillPath(root, skill.Name)
		if err := linkOrCopyRuntimeSkill(skill.Path, target); err != nil {
			return status_error.SkillUnavailable
		}
	}
	return nil
}

// runtimeSkillRoot 使用运行时注册信息声明的项目级 Skill 目录。
func runtimeSkillRoot(
	detection DetectionResult,
	workspacePath string,
) string {
	return filepath.Join(
		workspacePath,
		detection.Runtime.SkillDirectoryName,
		"skills",
	)
}

func (p *WorkspacePreparer) writeRuntimeInstructions(
	detection DetectionResult,
	workspace Workspace,
	instructions []Instruction,
) error {
	workspacePath := workspace.Path
	fileName := detection.Runtime.InstructionFileName
	if !safeWorkspaceEntryName(fileName) {
		return status_error.SkillUnavailable
	}
	var content strings.Builder
	content.WriteString("# NeuxBot Chat Workspace Constraints\n\n")
	appendWorkspaceFileRouting(&content, workspace.ProjectDirectories)
	for _, instruction := range instructions {
		name := strings.TrimSpace(instruction.Name)
		if name == "" {
			return status_error.SkillUnavailable
		}
		if isProfileInstruction(name) {
			content.WriteString(instruction.Content)
			content.WriteString("\n\n")
			continue
		}
		fmt.Fprintf(&content, "## %s\n\n%s\n\n", name, instruction.Content)
	}
	if err := writeWorkspaceTextFile(workspacePath, fileName, content.String()); err != nil {
		return err
	}
	for _, summary := range p.runtimes {
		knownFile := summary.InstructionFileName
		if knownFile == fileName || !safeWorkspaceEntryName(knownFile) {
			continue
		}
		if err := os.Remove(filepath.Join(workspacePath, knownFile)); err != nil && !os.IsNotExist(err) {
			return status_error.SkillUnavailable
		}
	}
	if err := os.Remove(filepath.Join(workspacePath, constants.TeamInstructionFileName)); err != nil && !os.IsNotExist(err) {
		return status_error.SkillUnavailable
	}
	return nil
}

// isProfileInstruction 判断指令是否为无需文件名标题的伙伴档案。
func isProfileInstruction(name string) bool {
	switch name {
	case constants.ProfileDocumentIdentityFileName,
		constants.ProfileDocumentPersonaFileName,
		constants.ProfileDocumentPlaybookFileName:
		return true
	default:
		return false
	}
}

// writeWorkspaceTextFile 原子写入工作区根目录下的文本文件。
func writeWorkspaceTextFile(workspacePath string, fileName string, content string) error {
	if !safeWorkspaceEntryName(fileName) {
		return status_error.SkillUnavailable
	}
	staging, err := os.CreateTemp(workspacePath, "runtime-instructions-")
	if err != nil {
		return status_error.SkillUnavailable
	}
	stagingPath := staging.Name()
	defer os.Remove(stagingPath)
	if _, err := staging.WriteString(content); err != nil {
		_ = staging.Close()
		return status_error.SkillUnavailable
	}
	if err := staging.Chmod(0o600); err != nil {
		_ = staging.Close()
		return status_error.SkillUnavailable
	}
	if err := staging.Close(); err != nil {
		return status_error.SkillUnavailable
	}
	if err := os.Rename(stagingPath, filepath.Join(workspacePath, fileName)); err != nil {
		return status_error.SkillUnavailable
	}
	return nil
}

// appendWorkspaceFileRouting 写入 Codex 和 Claude 共用的工作区文件归属规则。
func appendWorkspaceFileRouting(
	content *strings.Builder,
	directories []WorkspaceProjectDirectory,
) {
	content.WriteString("## Workspace File Routing\n\n")
	content.WriteString("The current working directory is a NeuxBot-managed Chat workspace, not a single project.\n\n")
	if len(directories) > 0 {
		content.WriteString("Available project directories:\n\n")
		for _, directory := range directories {
			if directory.Default {
				fmt.Fprintf(content, "- `%s` (default project directory)\n", directory.Path)
				continue
			}
			fmt.Fprintf(content, "- `%s`\n", directory.Path)
		}
		content.WriteString("\n")
	}
	content.WriteString("File routing rules:\n\n")
	content.WriteString("- When the user specifies a project or path, use the corresponding project directory.\n")
	content.WriteString("- When modifying an existing file, keep it in its owning project and use a path under that project directory.\n")
	content.WriteString("- New or modified files that clearly belong to a project must be written under that project's directory.\n")
	content.WriteString("- For cross-project work, create or modify files separately in each corresponding project directory.\n")
	fmt.Fprintf(
		content,
		"- Files confirmed to be temporary or unrelated to any project must be created under `%s/%s/`.\n",
		constants.ExecutionWorkspaceAttachmentsDirectory,
		constants.ExecutionWorkspaceGeneratedFilesDirectory,
	)
	fmt.Fprintf(
		content,
		"- Other paths under `%s/` contain user-provided files; do not overwrite them.\n",
		constants.ExecutionWorkspaceAttachmentsDirectory,
	)
	content.WriteString("- Do not create user deliverables in the NeuxBot Chat workspace root.\n\n")
}

func (p *WorkspacePreparer) clearRuntimeSkillDirectories(
	detection DetectionResult,
	workspacePath string,
) error {
	for _, path := range []string{
		filepath.Join(workspacePath, detection.Runtime.SkillDirectoryName, detection.Runtime.BaseRuntimeID),
		runtimeSkillRoot(detection, workspacePath),
	} {
		if err := os.RemoveAll(path); err != nil {
			return status_error.SkillUnavailable
		}
	}
	current := detection.Runtime.SkillDirectoryName
	if !safeWorkspaceEntryName(current) {
		return status_error.SkillUnavailable
	}
	for _, summary := range p.runtimes {
		name := summary.SkillDirectoryName
		if !safeWorkspaceEntryName(name) {
			continue
		}
		path := filepath.Join(workspacePath, name)
		if name == current {
			path = filepath.Join(path, detection.Runtime.BaseRuntimeID)
		}
		if err := os.RemoveAll(path); err != nil {
			return status_error.SkillUnavailable
		}
	}
	return nil
}

func validateRuntimeSkillSupport(detection DetectionResult) error {
	if !detection.Capabilities.Has(CapabilitySkillDirectory) {
		return status_error.SkillUnavailable
	}
	return nil
}

func validateRuntimeSkills(skills []Skill) error {
	seenNames := make(map[string]struct{}, len(skills))
	for _, skill := range skills {
		if !safeWorkspaceEntryName(skill.Name) || !filepath.IsAbs(skill.Path) {
			return status_error.SkillUnavailable
		}
		if _, exists := seenNames[skill.Name]; exists {
			return status_error.SkillUnavailable
		}
		seenNames[skill.Name] = struct{}{}
		info, err := os.Stat(filepath.Join(skill.Path, "SKILL.md"))
		if err != nil {
			return status_error.SkillUnavailable
		}
		if !info.Mode().IsRegular() {
			return status_error.SkillUnavailable
		}
	}
	return nil
}

// linkOrCopyRuntimeSkill 默认链接当前 Skill，操作系统不支持链接时复制完整目录。
func linkOrCopyRuntimeSkill(source string, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	if err := os.Symlink(source, target); err == nil {
		return nil
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	return managedskills.CopyDirectory(source, target)
}

func runtimeSkillPath(root string, name string) string {
	return filepath.Join(root, name)
}

func safeWorkspaceEntryName(value string) bool {
	return value != "" && filepath.Base(value) == value && value != "." &&
		value != ".." && !strings.ContainsAny(value, "/\\\x00\r\n")
}

// PluginEnvironmentRequest 分开提供已准备的模型身份、原生环境事实及插件交付资源。
type PluginEnvironmentRequest struct {
	// Target 是账号与软件能力准备后的私有进程启动事实。
	Target RuntimeTarget
	// Source 是智能体环境保存的原生配置源，不受模型账号投射影响。
	Source RuntimeTarget
	// Detection 是已有探测确认的基础智能体和真实程序。
	Detection DetectionResult
	// Workspace 是聊天拥有的受管工作区。
	Workspace string
	// DataDirectory 是插件不可变内容和原生派生目录所属的数据根目录。
	DataDirectory string
	// Native 是已按插件租约冻结的原生包。
	Native []plugins.NativeTarget
	// Snapshots 是待补充原生交付身份的固定插件事实。
	Snapshots []plugins.Snapshot
	// Environment 是插件交付所需的私有环境变量。
	Environment map[string]string
	// AllowExternal 决定是否允许当前原生环境的外部插件。
	AllowExternal bool
}

// PluginEnvironmentResult 返回原生交付完成的启动事实和待接纳的作用域标记。
type PluginEnvironmentResult struct {
	// Target 是模型身份、原生插件和管理命令路径合并后的启动事实。
	Target RuntimeTarget
	// Snapshots 是含实际原生插件身份的交付事实。
	Snapshots []plugins.Snapshot
	// CommitScope 只在业务接纳后保存本次原生能力摘要。
	CommitScope func() error
	// ScopeChanged 表示原生配置相对上次接纳已经变化。
	ScopeChanged bool
}

// PreparePluginEnvironment 拥有厂商配置根、原生插件交付及管理命令环境的装配规则。
func PreparePluginEnvironment(input PluginEnvironmentRequest) (PluginEnvironmentResult, error) {
	result := PluginEnvironmentResult{Target: input.Target, Snapshots: slices.Clone(input.Snapshots)}
	result.Target.Environment = maps.Clone(input.Target.Environment)
	if result.Target.Environment == nil {
		result.Target.Environment = map[string]string{}
	}
	cliID := input.Detection.Runtime.BaseRuntimeID
	if cliID == "" {
		cliID = input.Detection.Runtime.ID
	}
	nativeCommands := ""
	if cliID == constants.RuntimeIDCodex || cliID == constants.RuntimeIDClaude {
		if cliID == constants.RuntimeIDCodex {
			if err := plugins.PrepareCodexWorkspace(input.Workspace, input.Native); err != nil {
				return result, err
			}
		}
		variable, defaultDirectory := "CODEX_HOME", ".codex"
		if cliID == constants.RuntimeIDClaude {
			variable, defaultDirectory = "CLAUDE_CONFIG_DIR", ".claude"
		}
		sharedHome := input.Source.ConfigHome
		if sharedHome == "" {
			sharedHome = os.Getenv(variable)
		}
		if sharedHome == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return result, status_error.PluginStorageError
			}
			sharedHome = filepath.Join(home, defaultDirectory)
		}
		sharedHome, err := filepath.Abs(sharedHome)
		if err != nil {
			return result, status_error.PluginStorageError
		}
		binary := input.Detection.ResolvedBinary
		if binary == "" {
			binary = input.Source.Binary
		}
		if binary == "" {
			binary = cliID
		}
		executable, err := exec.LookPath(binary)
		if err == nil {
			executable, err = filepath.Abs(executable)
		}
		if err != nil {
			return result, status_error.RuntimeUnavailable
		}
		accountHome := ""
		secretKeys := []string{}
		if result.Target.AccountID != "" {
			accountHome = result.Target.Environment[variable]
			for key := range result.Target.Environment {
				if key != "PATH" {
					secretKeys = append(secretKeys, key)
				}
			}
			secretKeys = append(secretKeys, "OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_ACCESS_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL")
		}
		for key := range input.Environment {
			if key != "PATH" {
				secretKeys = append(secretKeys, key)
			}
		}
		native, err := plugins.PrepareNativeEnvironment(filepath.Join(input.DataDirectory, "plugins", "native-homes"), plugins.NativeEnvironmentSpec{
			RuntimeID: cliID, AccountHome: accountHome, SharedPluginHome: sharedHome, Executable: executable,
			PrefixArgs: input.Source.PrefixArgs, Targets: input.Native, AllowExternal: input.AllowExternal,
			ModelEnvironmentKeys: secretKeys, StateDirectory: filepath.Join(input.Workspace, constants.RuntimeStateDirectoryName, constants.PluginNativeScopeDirectory), Workspace: input.Workspace,
		})
		if err != nil {
			return result, err
		}
		result.Target.Environment[variable] = native.Home
		maps.Copy(result.Target.Environment, native.Environment)
		result.Target.ConfigHome = native.Home
		result.Target.CodexPluginConfiguration = native.CodexConfiguration
		result.Target.NativePluginScope = native.Fingerprint
		result.Target.ClaudeSettingsPath = native.ClaudeSettingsPath
		result.CommitScope, result.ScopeChanged = native.CommitScope, native.ScopeChanged
		if native.Marketplace != "" {
			if err := plugins.PrepareCodexWorkspace(input.Workspace, input.Native, native.Marketplace); err != nil {
				return result, err
			}
			for index := range result.Snapshots {
				if result.Snapshots[index].Delivery != constants.PluginDeliveryNative {
					continue
				}
				for _, target := range input.Native {
					if target.ID == result.Snapshots[index].ID {
						result.Snapshots[index].NativePluginID = target.Name + "@" + native.Marketplace
					}
				}
			}
		}
		nativeCommands = native.CommandDirectory
		if cliID == constants.RuntimeIDClaude {
			result.Target.NativePluginDirectories = slices.Clone(result.Target.NativePluginDirectories)
			for _, target := range input.Native {
				result.Target.NativePluginDirectories = append(result.Target.NativePluginDirectories, target.Root)
			}
		}
	}
	for key, value := range input.Environment {
		if key == "PATH" {
			prependPluginPath(result.Target.Environment, value)
			continue
		}
		result.Target.Environment[key] = value
	}
	if nativeCommands != "" {
		prependPluginPath(result.Target.Environment, nativeCommands)
	}
	return result, nil
}

// prependPluginPath 保留已有账号和软件能力入口，再按交付顺序添加插件命令。
func prependPluginPath(environment map[string]string, directory string) {
	base := environment["PATH"]
	if base == "" {
		base = os.Getenv("PATH")
	}
	environment["PATH"] = directory + string(os.PathListSeparator) + base
}

// resourcePreparation 保存一次冷路径的独立任务装配选项，不成为会话兼容事实。
type resourcePreparation struct {
	// taskResources 在 Prompt 返回时撤销当前任务授权。
	taskResources TaskResources
	// taskLease 保护任务物化的文件。
	taskLease ResourceLease
	// releaseWorkspace 在原生及领域资源退出后移除临时目录。
	releaseWorkspace func(context.Context) error
	// contributions 是独立工具任务的固定贡献。
	contributions native.Contributions
	// unattended 禁用原生请求用户输入能力。
	unattended bool
	// nativeInstructions 是已由厂商适配确认的系统指令。
	nativeInstructions []string
	// acquisitionError 保留资源取得的部分失败，阻止继续启动。
	acquisitionError error
}

// sessionResources 是同一 Session 内实际资源与逐步释放进度的唯一所有者。
type sessionResources struct {
	// domainClosed 标记领域释放已经成功，不改写回读快照中的释放端口。
	domainClosed bool
	// snapshot 是业务层回读的固定具名快照。
	snapshot SessionResources
	// bindings 是租约保护的技能目录和连接秘密。
	bindings []native.ResourceBinding
	// launch 是本次启动认证租约。
	launch *native.LaunchLease
	// credential 是仅在启动边界消费的冻结环境。
	credential native.Credential
	// connectionConfig 是本次消费者的原生连接参数。
	connectionConfig native.ConnectionConfig
	// taskLease 保护独立任务文件直到原生退出。
	taskLease ResourceLease
	// taskResources 撤销调用授权，不释放仍被进程使用的文件。
	taskResources TaskResources
	// releaseWorkspace 在所有依赖释放后移除临时目录。
	releaseWorkspace func(context.Context) error
	// contributions 保存独立工具任务的固定贡献。
	contributions native.Contributions
}

func (r *sessionResources) Close(ctx context.Context) error {
	if r.launch != nil {
		if err := r.launch.Close(ctx); err != nil {
			return err
		}
		r.launch = nil
	}
	if r.taskLease != nil {
		if err := r.taskLease.Close(ctx); err != nil {
			return err
		}
		r.taskLease = nil
	}
	if !r.domainClosed && r.snapshot.Close != nil {
		if err := r.snapshot.Close(ctx); err != nil {
			return err
		}
		r.domainClosed = true
	}
	if r.releaseWorkspace != nil {
		if err := r.releaseWorkspace(ctx); err != nil {
			return err
		}
		r.releaseWorkspace = nil
	}
	return nil
}
func (r *sessionResources) prepare(ctx context.Context, plan SessionPlan, options resourcePreparation, config ManagerConfig) error {
	if config.PrepareWorkspace != nil {
		if err := config.PrepareWorkspace(plan); err != nil {
			return err
		}
	}
	facts, err := config.Launch(r.snapshot.Target, plan.Detection)
	if err != nil {
		return err
	}
	descriptor, ok := native.Lookup(facts.BaseRuntimeID)
	if !ok {
		return status_error.RuntimeUnavailable
	}
	stateRoot := filepath.Dir(plan.Workspace.Path)
	if options.unattended {
		stateRoot = plan.Workspace.Path
	}
	nativeConfig := hostNativeConfig(r.snapshot.Target, facts, plan.Workspace.Path, stateRoot, plan.Policy.Execution.PermissionLevel)
	if options.unattended {
		nativeConfig.DefaultModeRequestUserInput = false
	}
	nativeConfig, err = native.ConfigureTaskInstructions(nativeConfig, options.nativeInstructions)
	if err != nil {
		return err
	}
	r.launch, err = native.PrepareLaunch(ctx, native.LaunchConfig{Native: nativeConfig, UseNativeAuth: !r.snapshot.Target.Isolated, Refresh: r.snapshot.Target.RefreshCredentials})
	if err != nil {
		return err
	}
	r.credential = r.launch.Credential()
	r.bindings = nativeResourceBindings(r.snapshot)
	if r.taskLease != nil {
		r.bindings = append(r.bindings, r.taskLease.Resources()...)
	}
	materialized := make([]string, 0, len(options.nativeInstructions))
	for index := range options.nativeInstructions {
		materialized = append(materialized, fmt.Sprintf("system-%d", index+1))
	}
	r.connectionConfig = native.ConnectionConfig{ID: plan.Target.RuntimeID, NativeID: facts.BaseRuntimeID, Factory: func(factoryContext context.Context, request native.OpenRequest) (acp.Agent, error) {
		return r.launch.New(factoryContext, request.Credential.Environment)
	}, CompletionPolicy: descriptor.Completion, ModeCandidates: chatModeCandidates(facts.BaseRuntimeID, plan.Policy.Execution), SingleModeFallback: descriptor.Modes.SingleModeFallback, MaterializedInstructions: materialized}
	if plan.Workspace.Managed && config.PrepareWorkspace != nil {
		r.connectionConfig.PromptContext = func(native.OpenRequest) (string, error) { return "", nil }
	}
	return nil
}

// nativeModelID 只判断账号身份，厂商模型命名空间由唯一原生注册表转换。
func nativeModelID(base, account, model string) string {
	return native.ModelID(base, account != "" && account != constants.AiAccountSystemID, model)
}

// hostNativeConfig 投影已冻结的产品工作区、权限和插件事实。
func hostNativeConfig(target RuntimeTarget, facts LaunchFacts, workspace, stateRoot string, permission PermissionLevel) native.AgentConfig {
	return native.AgentConfig{RuntimeID: facts.BaseRuntimeID, Executable: facts.Executable, PrefixArgs: facts.PrefixArgs, Environment: facts.Environment, WorkingDirectory: workspace, StateRootDirectory: stateRoot, PermissionMode: string(permission), Logger: AdapterLogger(context.Background()), DefaultModeRequestUserInput: constants.RuntimeCodexDefaultModeRequestUserInput, CodexConfiguration: target.CodexPluginConfiguration, ClaudeSettingsPath: target.ClaudeSettingsPath, NativePluginDirectories: target.NativePluginDirectories}
}
func nativeResourceBindings(snapshot SessionResources) []native.ResourceBinding {
	bindings := make([]native.ResourceBinding, 0, len(snapshot.Skills)+len(snapshot.Connectors))
	for _, skill := range snapshot.Skills {
		bindings = append(bindings, native.ResourceBinding{ID: "skill:" + skill.Name, Path: skill.Path})
	}
	for _, connector := range snapshot.Connectors {
		id := connector.ID
		if id == "" {
			id = connectorName(connector.ID, connector.Name, connector.RuntimeKey)
		}
		transport := native.MCPTransport(connector.Transport)
		if connector.Transport == ConnectorTransportStreamableHTTP {
			transport = native.MCPHTTP
		}
		bindings = append(bindings, native.ResourceBinding{ID: "mcp:" + id, MCP: &native.MCPConnection{Transport: transport, Command: connector.Command, Arguments: slices.Clone(connector.Arguments), Environment: maps.Clone(connector.Environment), URL: connector.URL, Headers: maps.Clone(connector.Headers)}})
	}
	return bindings
}
func appendContributions(a, b native.Contributions) native.Contributions {
	a.Instructions = append(a.Instructions, b.Instructions...)
	a.Skills = append(a.Skills, b.Skills...)
	a.MCP = append(a.MCP, b.MCP...)
	return a
}
