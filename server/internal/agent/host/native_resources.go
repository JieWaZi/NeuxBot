package runtimehost

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp/native"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/environment/catalog"
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/environment/launch"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// nativeModelID 只判断产品账号身份，模型命名空间交由原生适配器转换。
func nativeModelID(base, account, model string) string {
	return native.ModelID(base, account != "" && account != constants.AiAccountSystemID, model)
}

// hostNativeConfig 只投影产品已冻结的工作区、权限与插件事实，原生参数由唯一工厂生成。
func hostNativeConfig(target runtimeconfig.RuntimeTarget, facts runtimeconfig.LaunchFacts, workspace, stateRoot string, permission runtimeconfig.PermissionLevel) native.Config {
	config := native.Config{
		RuntimeID: facts.BaseRuntimeID, Executable: facts.Executable,
		PrefixArgs: facts.PrefixArgs, Environment: facts.Environment,
		WorkingDirectory: workspace, StateRootDirectory: stateRoot, PermissionMode: string(permission), Logger: catalog.AdapterLogger(context.Background()),
		DefaultModeRequestUserInput: constants.RuntimeCodexDefaultModeRequestUserInput,
		CodexConfiguration:          target.CodexPluginConfiguration, ClaudeSettingsPath: target.ClaudeSettingsPath,
		NativePluginDirectories: target.NativePluginDirectories,
	}
	return config
}

// nativePreparationConfig 为聊天和独立任务提供同一个原生装配入口。
type nativePreparationConfig struct {
	// launch 读取已冻结的启动事实。
	launch func(runtimeconfig.RuntimeTarget, runtimeconfig.DetectionResult) (runtimeconfig.LaunchFacts, error)
	// driver 是原生协议测试替身入口。
	driver func(Environment, acpdriver.Factory) (c.Driver, error)
	// prepareWorkspace 物化受管聊天文件。
	prepareWorkspace func(runtimecontract.SessionPlan) error
	// taskResources 注入任务授权环境与调用撤销端点。
	taskResources TaskResources
	// taskLease 是已经取得且需交给内核的任务文件租约。
	taskLease c.ResourceLease
	// releaseWorkspace 在所有原生依赖释放后移除临时目录。
	releaseWorkspace func(context.Context) error
	// contributions 是独立工具任务声明的固定技术资源。
	contributions c.Contributions
	// unattended 关闭独立任务的原生请求用户输入能力。
	unattended bool
	// nativeInstructions 是已由原生适配器确认需要安装的系统指令。
	nativeInstructions []string
	// acquisitionError 保留部分资源获取失败，禁止继续装配原生消费者。
	acquisitionError error
}

// nativeResources 把实际业务资源和统一启动资源作为一个租约交给内核。
type nativeResources struct {
	snapshot         runtimecontract.SessionResources // snapshot 是当前原生消费者固定的具名业务事实。
	bindings         []c.ResourceBinding              // bindings 是实际技能和连接器资源。
	prepared         *launch.PreparedAgent            // prepared 是统一启动与认证生命周期。
	taskLease        c.ResourceLease                  // taskLease 保护独立任务物化的资源目录。
	releaseWorkspace func(context.Context) error      // releaseWorkspace 在领域资源归还后清理临时目录。
}

// Resources 交付当前租约保护的固定技能和连接器绑定。
func (r *nativeResources) Resources() []c.ResourceBinding { return r.bindings }

// nativeResourceBindings 只在实际资源交接时展开租约保护的目录与 MCP 秘密。
func nativeResourceBindings(snapshot runtimecontract.SessionResources) []c.ResourceBinding {
	bindings := make([]c.ResourceBinding, 0, len(snapshot.Skills)+len(snapshot.Connectors))
	for _, skill := range snapshot.Skills {
		bindings = append(bindings, c.ResourceBinding{ID: "skill:" + skill.Name, Path: skill.Path})
	}
	for _, connector := range snapshot.Connectors {
		id := connector.ID
		if id == "" {
			id = connectorName(connector.ID, connector.Name, connector.RuntimeKey)
		}
		transport := c.MCPTransport(connector.Transport)
		if connector.Transport == runtimeconfig.ConnectorTransportStreamableHTTP {
			transport = c.MCPHTTP
		}
		bindings = append(bindings, c.ResourceBinding{ID: "mcp:" + id, MCP: &c.MCPConnection{
			Transport: transport, Command: connector.Command, Arguments: append([]string(nil), connector.Arguments...),
			Environment: connector.Environment, URL: connector.URL, Headers: connector.Headers,
		}})
	}
	return bindings
}

// Close 依次关闭认证与领域资源，成功步骤不参与后续重试。
func (r *nativeResources) Close(ctx context.Context) error {
	if r.prepared != nil {
		if err := r.prepared.Close(ctx); err != nil {
			return err
		}
		r.prepared = nil
	}
	if r.taskLease != nil {
		if err := r.taskLease.Close(ctx); err != nil {
			return err
		}
		r.taskLease = nil
	}
	if r.snapshot.Close != nil {
		if err := r.snapshot.Close(ctx); err != nil {
			return err
		}
		r.snapshot.Close = nil
	}
	if r.releaseWorkspace != nil {
		if err := r.releaseWorkspace(ctx); err != nil {
			return err
		}
		r.releaseWorkspace = nil
	}
	return nil
}

// prepareNativeResources 统一投影实际资源、准备认证并装配驱动；不打开进程或提交输入。
func prepareNativeResources(ctx context.Context, declared runtimecontract.SessionPlan, snapshot runtimecontract.SessionResources, config nativePreparationConfig) (c.SessionResources, error) {
	owned := &nativeResources{snapshot: snapshot, taskLease: config.taskLease, releaseWorkspace: config.releaseWorkspace}
	actual := c.SessionResources{Resources: owned, CloseAfterRun: snapshot.Target.CloseAfterRun}
	if ctx == nil {
		return actual, status_error.BadRequest
	}
	if err := ctx.Err(); err != nil {
		return actual, err
	}
	if config.acquisitionError != nil {
		return actual, config.acquisitionError
	}
	if snapshot.Session != nil {
		session := c.SessionDirective{Mode: c.SessionMode(snapshot.Session.Mode)}
		actual.Session = &session
	}
	plan := declared
	plan.Target, plan.Skills, plan.Connectors = runtimecontract.DeclareTarget(snapshot.Target), snapshot.Skills, runtimecontract.DeclareConnectors(snapshot.Connectors)
	plan.PluginIDs = nil
	for _, plugin := range snapshot.PluginResources {
		plan.PluginIDs = append(plan.PluginIDs, plugin.ID)
	}
	technical, err := technicalSessionPlan(plan)
	owned.bindings = nativeResourceBindings(snapshot)
	if err != nil {
		return actual, err
	}
	technical.Contributions.Instructions = append(technical.Contributions.Instructions, config.contributions.Instructions...)
	technical.Contributions.Skills = append(technical.Contributions.Skills, config.contributions.Skills...)
	technical.Contributions.MCP = append(technical.Contributions.MCP, config.contributions.MCP...)
	if config.taskLease != nil {
		owned.bindings = append(owned.bindings, config.taskLease.Resources()...)
	}
	actual.ActualPlan = &technical
	if config.prepareWorkspace != nil {
		if err = config.prepareWorkspace(plan); err != nil {
			return actual, err
		}
	}
	facts, err := config.launch(snapshot.Target, plan.Detection)
	if err != nil {
		return actual, err
	}
	descriptor, ok := native.Lookup(facts.BaseRuntimeID)
	if !ok {
		return actual, status_error.RuntimeUnavailable
	}
	stateRoot := filepath.Dir(plan.Workspace.Path)
	if config.unattended {
		stateRoot = plan.Workspace.Path
	}
	nativeConfig := hostNativeConfig(snapshot.Target, facts, plan.Workspace.Path, stateRoot, plan.Policy.Execution.PermissionLevel)
	if config.unattended {
		nativeConfig.DefaultModeRequestUserInput = false
	}
	nativeConfig, err = native.ConfigureTaskInstructions(nativeConfig, config.nativeInstructions)
	if err != nil {
		return actual, err
	}
	prepared, err := launch.Prepare(ctx, launch.Config{Native: nativeConfig, UseNativeAuth: !snapshot.Target.Isolated, Refresh: snapshot.Target.RefreshCredentials})
	owned.prepared = prepared
	if err != nil {
		return actual, err
	}
	actual.Credential = prepared.Credential()
	factory := acpdriver.Factory(func(factoryContext context.Context, request c.OpenRequest) (acp.Agent, error) {
		return prepared.New(factoryContext, request.Credential.Environment)
	})
	environment := Environment{BaseRuntimeID: facts.BaseRuntimeID, Target: snapshot.Target, Detection: plan.Detection}
	if config.driver != nil {
		actual.Driver, err = config.driver(environment, factory)
	} else {
		materialized := make([]string, 0, len(config.nativeInstructions))
		for index := range config.nativeInstructions {
			materialized = append(materialized, fmt.Sprintf("system-%d", index+1))
		}
		driverConfig := acpdriver.Config{ID: plan.Target.RuntimeID, NativeID: facts.BaseRuntimeID, MaterializedInstructions: materialized, Factory: factory, CompletionPolicy: descriptor.Completion, ModeCandidates: chatModeCandidates(facts.BaseRuntimeID, plan.Policy.Execution), SingleModeFallback: descriptor.Modes.SingleModeFallback}
		if plan.Workspace.Managed && config.prepareWorkspace != nil {
			driverConfig.PromptContext = func(c.OpenRequest) (string, error) { return "", nil }
		}
		actual.Driver, err = acpdriver.New(driverConfig)
	}
	if config.taskResources != nil && actual.Driver != nil {
		actual.Driver = &taskDriver{Driver: actual.Driver, resources: config.taskResources}
	}
	return actual, err
}
