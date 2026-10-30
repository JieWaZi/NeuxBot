package acp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// AgentFactory 创建由管理连接独占的公开 Agent，宿主负责冻结启动参数。
type AgentFactory func(context.Context) (sdk.Agent, error)

// ManagementConfig 配置独立管理连接的资源释放期限。
type ManagementConfig struct {
	CleanupTimeout time.Duration // CleanupTimeout 是单次清理的最大等待期限，零值使用默认值。
}

// Management 拥有独立短连接；调用者顺序使用 Probe 或 Fork，并在失败后仍负责 Close。
type Management struct {
	session        *session               // session 复用执行连接的公开协议和可重试原生资源所有权。
	initialized    sdk.InitializeResponse // initialized 保留握手已经确认的原始事实。
	cleanupTimeout time.Duration          // cleanupTimeout 限定临时句柄及原生资源的单次关闭等待。
}

// ProbeRequest 声明无 Prompt 探测的工作区和模型选择。
type ProbeRequest struct {
	Workspace              contract.WorkspaceScope // Workspace 是本次临时会话的真实工作范围。
	Model                  string                  // Model 非空时必须经 setter 回执确认，不能只相信初始目录。
	ModelConfigID          sdk.SessionConfigId     // ModelConfigID 只在原生目录缺少标准模型类别时提供配置标识。
	InspectModelOptions    bool                    // InspectModelOptions 允许切换尚无专属元数据的模型补读配置。
	SkipModelOptions       []string                // SkipModelOptions 按原生模型标识跳过 setter 补读，保留当前配置、已有元数据和原始模型目录。
	AllowEmptyModelCatalog bool                    // AllowEmptyModelCatalog 允许没有模型目录的 Agent 返回当前会话事实。
}

// ProbeResult 保留原始协议事实，后续失败不抹掉已经确认的初始化和会话状态。
type ProbeResult struct {
	Initialize    sdk.InitializeResponse    // Initialize 是原生握手响应。
	SessionReady  bool                      // SessionReady 表示已获得真实临时会话身份。
	ConfigOptions []sdk.SessionConfigOption // ConfigOptions 是初始或目标模型经确认后的配置。
	Modes         *sdk.SessionModeState     // Modes 是临时会话声明的模式状态。
	Models        []ModelConfiguration      // Models 按原生模型目录顺序保存专属配置。
}

// ModelConfiguration 保留模型专属原始配置，nil 目录表示未确认，空目录表示确认无配置。
type ModelConfiguration struct {
	Model         string                    // Model 是原生模型标识。
	ConfigOptions []sdk.SessionConfigOption // ConfigOptions 来自模型元数据、当前会话或 setter 回执。
	Modes         *sdk.SessionModeState     // Modes 是读取配置时的会话模式状态。
}

// ForkRequest 声明原生分叉位置和宿主已有的耐久对账参数。
type ForkRequest struct {
	SourceSessionID  string                  // SourceSessionID 是来源原生句柄。
	Position         string                  // Position 是原生返回的不透明回合位置，空值表示当前末尾。
	SourceWorkspace  string                  // SourceWorkspace 是来源原生会话绑定的目录。
	ReceiptDirectory string                  // ReceiptDirectory 是宿主持有的私有持久操作目录。
	ReconcileOnly    bool                    // ReconcileOnly 只对账已派发结果，不允许再次创建分支。
	Workspace        contract.WorkspaceScope // Workspace 是目标原生会话的工作范围。
	MCPServers       []sdk.McpServer         // MCPServers 是宿主准备的原始连接器参数。
	CloseForkSession bool                    // CloseForkSession 在声明关闭能力时释放新句柄的管理连接占用。
}

// ForkResult 保留实际生成的原生身份和经握手确认的分叉精度。
type ForkResult struct {
	SessionID string // SessionID 是新分支用于后续恢复的原生句柄。
	Mode      string // Mode 是适配器实际声明的分叉精度。
}

// OpenManagement 只初始化公开协议，不创建会话或发送 Prompt；取得 Agent 后始终返回可关闭所有者。
func OpenManagement(ctx context.Context, factory AgentFactory, config ManagementConfig) (*Management, error) {
	if ctx == nil || factory == nil || config.CleanupTimeout < 0 {
		return nil, fault(contract.FaultInvalidSpec, "management", "ACP management requires a context, factory and nonnegative cleanup timeout", nil)
	}
	if config.CleanupTimeout == 0 {
		config.CleanupTimeout = defaultCleanupTimeout
	}
	if err := ctx.Err(); err != nil {
		return nil, fault(contract.FaultCancelled, "management", "ACP management preparation cancelled", err)
	}
	agent, err := factory(ctx)
	if agent == nil {
		return nil, fault(contract.FaultPreparation, "management", "ACP agent creation failed", err)
	}
	s := &session{native: agent, done: make(chan struct{}), closeGate: make(chan struct{}, 1)}
	m := &Management{session: s, cleanupTimeout: config.CleanupTimeout}
	partial := func(cause error) (*Management, error) { s.closed = true; close(s.done); return m, cause }
	if err != nil {
		return partial(fault(contract.FaultPreparation, "management", "ACP agent creation failed", err))
	}
	if err = ctx.Err(); err != nil {
		return partial(fault(contract.FaultCancelled, "management", "ACP management preparation cancelled", err))
	}
	if err = s.connect(); err != nil {
		s.closed = true
		return m, err
	}
	abort := context.AfterFunc(ctx, s.stop)
	defer abort()
	m.initialized, err = s.initialize(ctx)
	if err != nil {
		s.stop()
		return m, err
	}
	if err = ctx.Err(); err != nil {
		s.stop()
		return m, fault(contract.FaultCancelled, "management", "ACP management preparation cancelled", err)
	}
	return m, nil
}

// InitializeResponse 返回已完成握手的浅拷贝，不创建会话或发送请求；调用者不得修改其中共享的指针、切片和元数据。
func (m *Management) InitializeResponse() sdk.InitializeResponse {
	return m.initialized
}

// Close 确认协议与原生资源均停止；失败保留对象供调用者重试后再释放其依赖。
func (m *Management) Close(ctx context.Context) error {
	closeCtx, cancel := context.WithTimeout(ctx, m.cleanupTimeout)
	defer cancel()
	return m.session.Close(closeCtx)
}

// Probe 创建并关闭独占临时会话，读取原始目录和可选模型专属参数，全程不发送 Prompt。
func (m *Management) Probe(ctx context.Context, request ProbeRequest) (result ProbeResult, err error) {
	result.Initialize = m.initialized
	if err = m.ready(ctx, request.Workspace); err != nil {
		return result, err
	}
	directories := nativeDirectories(request.Workspace, m.initialized.AgentCapabilities.SessionCapabilities)
	response, err := m.session.client.NewSession(ctx, sdk.NewSessionRequest{Cwd: request.Workspace.Root, AdditionalDirectories: directories, McpServers: []sdk.McpServer{}})
	if err != nil {
		return result, err
	}
	if response.SessionId == "" {
		return result, fault(contract.FaultProtocol, "probe", "ACP session identity is missing", nil)
	}
	result.SessionReady = true
	result.ConfigOptions = response.ConfigOptions
	result.Modes = response.Modes
	defer func() {
		if closeErr := m.closeSession(ctx, response.SessionId); err == nil {
			err = closeErr
		}
	}()
	modelConfig := modelOption(result.ConfigOptions, request.ModelConfigID)
	configID := request.ModelConfigID
	if modelConfig != nil {
		configID = modelConfig.Id
	}
	if request.Model != "" {
		if configID == "" {
			return result, fault(contract.FaultProtocol, "probe", "ACP model configuration identity is missing", nil)
		}
		updated, selectErr := m.selectModel(ctx, response.SessionId, configID, request.Model)
		if selectErr != nil {
			return result, selectErr
		}
		result.ConfigOptions = updated
		modelConfig = modelOption(result.ConfigOptions, configID)
	}
	if modelConfig == nil {
		if request.AllowEmptyModelCatalog {
			return result, nil
		}
		return result, fault(contract.FaultPreparation, "probe", "ACP model catalog is unavailable", nil)
	}
	seen := make(map[string]bool)
	for _, option := range modelValues(modelConfig.Options) {
		model := strings.TrimSpace(string(option.Value))
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		configuration := ModelConfiguration{Model: model, Modes: result.Modes}
		configs, known := acpmeta.ModelConfigOptions(option.Meta)
		if known {
			configuration.ConfigOptions = configs
		} else if option.Value == modelConfig.CurrentValue {
			configuration.ConfigOptions = result.ConfigOptions
		} else if request.InspectModelOptions && !slices.Contains(request.SkipModelOptions, model) {
			configuration.ConfigOptions, err = m.selectModel(ctx, response.SessionId, configID, model)
			if err != nil {
				return result, err
			}
		}
		result.Models = append(result.Models, configuration)
	}
	if len(result.Models) == 0 && !request.AllowEmptyModelCatalog {
		return result, fault(contract.FaultPreparation, "probe", "ACP model catalog is empty", nil)
	}
	return result, nil
}

// Fork 验证实际声明的分叉精度，原样传递位置和收据；成功身份在关闭句柄失败时仍返回。
func (m *Management) Fork(ctx context.Context, request ForkRequest) (ForkResult, error) {
	if err := m.ready(ctx, request.Workspace); err != nil {
		return ForkResult{}, err
	}
	if request.SourceSessionID == "" {
		return ForkResult{}, fault(contract.FaultInvalidSpec, "fork", "ACP fork requires a source session", nil)
	}
	mode := acpmeta.ForkMode(m.initialized.Meta)
	capabilities := m.initialized.AgentCapabilities.SessionCapabilities
	if capabilities.Fork == nil || mode == acpmeta.ForkUnsupported || request.Position != "" && mode != acpmeta.ForkTurn {
		return ForkResult{}, fault(contract.FaultUnsupported, "fork", "ACP fork precision is not advertised", nil)
	}
	encoded, err := json.Marshal(request.MCPServers)
	if err != nil {
		return ForkResult{}, fault(contract.FaultInvalidSpec, "fork", "ACP MCP parameters cannot be encoded", err)
	}
	var servers []sdk.UnstableMcpServer
	if err = json.Unmarshal(encoded, &servers); err != nil {
		return ForkResult{}, fault(contract.FaultInvalidSpec, "fork", "ACP MCP parameters cannot be decoded", err)
	}
	meta := acpmeta.PositionMetadata(request.Position)
	if meta == nil {
		meta = map[string]any{}
	}
	meta["forkReceiptDirectory"] = request.ReceiptDirectory
	meta["sourceCwd"] = request.SourceWorkspace
	meta["reconcileOnly"] = request.ReconcileOnly
	directories := nativeDirectories(request.Workspace, capabilities)
	response, err := m.session.client.UnstableForkSession(ctx, sdk.UnstableForkSessionRequest{SessionId: sdk.SessionId(request.SourceSessionID), Cwd: request.Workspace.Root, AdditionalDirectories: directories, McpServers: servers, Meta: meta})
	if err != nil {
		return ForkResult{}, err
	}
	if response.SessionId == "" || string(response.SessionId) == request.SourceSessionID {
		return ForkResult{}, fault(contract.FaultProtocol, "fork", "ACP fork did not return an independent session", nil)
	}
	result := ForkResult{SessionID: string(response.SessionId), Mode: mode}
	if request.CloseForkSession && capabilities.Close != nil {
		return result, m.closeSession(ctx, response.SessionId)
	}
	return result, nil
}

func (m *Management) ready(ctx context.Context, workspace contract.WorkspaceScope) error {
	if ctx == nil {
		return fault(contract.FaultInvalidSpec, "management", "ACP management requires a context", nil)
	}
	if err := ctx.Err(); err != nil {
		return fault(contract.FaultCancelled, "management", "ACP management operation cancelled", err)
	}
	m.session.mu.Lock()
	closed := m.session.closed
	m.session.mu.Unlock()
	if closed {
		return fault(contract.FaultClosed, "management", "ACP management connection is closed", nil)
	}
	if !filepath.IsAbs(workspace.Root) {
		return fault(contract.FaultInvalidSpec, "workspace", "ACP working directory must be absolute", nil)
	}
	for _, directory := range workspace.Directories {
		if !filepath.IsAbs(directory) {
			return fault(contract.FaultInvalidSpec, "workspace", "ACP additional working directory must be absolute", nil)
		}
	}
	return nil
}

func (m *Management) closeSession(ctx context.Context, id sdk.SessionId) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.cleanupTimeout)
	defer cancel()
	_, err := m.session.client.CloseSession(cleanup, sdk.CloseSessionRequest{SessionId: id})
	return err
}

func (m *Management) selectModel(ctx context.Context, id sdk.SessionId, configID sdk.SessionConfigId, model string) ([]sdk.SessionConfigOption, error) {
	updated, err := m.session.client.SetSessionConfigOption(ctx, sdk.SetSessionConfigOptionRequest{ValueId: &sdk.SetSessionConfigOptionValueId{SessionId: id, ConfigId: configID, Value: sdk.SessionConfigValueId(model)}})
	if err != nil {
		return nil, err
	}
	selected := modelOption(updated.ConfigOptions, configID)
	if selected == nil || string(selected.CurrentValue) != model {
		return updated.ConfigOptions, fault(contract.FaultProtocol, "probe", "ACP model setter did not confirm requested value", nil)
	}
	return updated.ConfigOptions, nil
}

func modelOption(options []sdk.SessionConfigOption, id sdk.SessionConfigId) *sdk.SessionConfigOptionSelect {
	if selected := categoryOption(options, sdk.SessionConfigOptionCategoryModel); selected != nil {
		return selected
	}
	if id != "" {
		for _, option := range options {
			if option.Select != nil && option.Select.Id == id && option.Select.Category == nil {
				return option.Select
			}
		}
	}
	return nil
}

func modelValues(options sdk.SessionConfigSelectOptions) []sdk.SessionConfigSelectOption {
	if options.Ungrouped != nil {
		return *options.Ungrouped
	}
	var result []sdk.SessionConfigSelectOption
	if options.Grouped != nil {
		for _, group := range *options.Grouped {
			result = append(result, group.Options...)
		}
	}
	return result
}
