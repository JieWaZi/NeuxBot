package catalog

import (
	runtimediagnostic "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/diagnostic"

	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp/native"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/environment/launch"
	acp "github.com/JieWaZi/acp-go/sdk"
	"github.com/google/uuid"
	"sync"
	"time"
)

const acpManagementCloseTimeout = 5 * time.Second

// acpManager 是目录服务使用的无模型输入管理端口。
type acpManager interface {
	DetectOptions(context.Context, acpOptionsRequest) (acpOptionCatalog, error)
	ForkSession(context.Context, preparedRuntime, ForkRequest) (ForkResult, error)
	Close(context.Context) error
	ReleaseResources(context.Context, func(context.Context) error) error
}

// acpOptionsRequest 提供账号和已冻结的启动事实。
type acpOptionsRequest struct {
	ReleaseResources   func(context.Context) error     // ReleaseResources 在管理使用者确认退出后释放宿主探测依赖。
	UseNativeAuth      bool                            // UseNativeAuth 明确使用系统账号的原生认证文件。
	RefreshCredentials runtimeconfig.CredentialRefresh // RefreshCredentials 指向账号刷新权威。
	Model              string                          // Model 是需要实际确认的原生模型。
	RuntimeID          string                          // RuntimeID 是产品选择的基础或本地配置身份。
	Prepared           preparedRuntime                 // Prepared 是已有安装和环境事实。
	WorkingDirectory   string                          // WorkingDirectory 是宿主拥有的临时工作目录。
}

// acpOptionCatalog 是原始协议目录经过产品规则解释后的结果。
type acpOptionCatalog struct {
	AgentConnected   bool                            // AgentConnected 表示标准握手成功。
	SessionReady     bool                            // SessionReady 表示临时会话创建成功。
	Version          string                          // Version 是原生CLI版本。
	Capabilities     runtimeconfig.CapabilitySet     // Capabilities 是本次握手确认的产品能力。
	Models           []runtimeconfig.ModelOption     // Models 是可选择的模型目录。
	ReasoningOptions []runtimeconfig.ReasoningOption // ReasoningOptions 是当前模型的强度目录。
	DefaultModel     string                          // DefaultModel 是最初或显式选择的模型。
	DefaultReasoning string                          // DefaultReasoning 是该模型当前强度。
	PermissionLevels []runtimeconfig.PermissionLevel // PermissionLevels 是产品权限映射。
	Modes            []acp.SessionMode               // Modes 是原始模式目录。
}

// acpBackend 只装配短生命周期管理操作，不实现协议或厂商进程。
type acpBackend struct {
	mutex     sync.Mutex                                                                        // mutex 保护关闭门禁、测试工厂和仍持有依赖的管理所有者。
	bindings  map[string]acpRuntimeBinding                                                      // bindings 是冻结的产品目录解释规则。
	openAgent func(context.Context, *launch.PreparedAgent, acpAdapterConfig) (acp.Agent, error) // openAgent 在统一启动所有者上构造原生消费者。
	closing   bool                                                                              // closing 阻止应用关闭后继续接纳。
	owned     map[*managementOwner]struct{}                                                     // owned 保留未确认关闭的原生使用者。
	pending   int                                                                               // pending 统计尚未返回技术所有者的准备操作。
	changed   chan struct{}                                                                     // changed 在所有者或准备计数变化时唤醒关闭。

}

// managementOwner 保证原生使用者退出后才释放刷新入口与账号配置。
type managementOwner struct {
	// releaseResources 保存尚未交给统一启动所有者的宿主账号和目录。
	releaseResources func(context.Context) error
	// resources 是原生使用者退出后才可关闭的统一启动所有者。
	resources *launch.PreparedAgent
	// manager 是原生与协议连接的唯一技术所有者。
	manager *acpdriver.Management
	// closeGate 串行化单次关闭，允许并发关闭等待被取消。
	closeGate chan struct{}
}

func newACPBackend() *acpBackend {
	AdapterLogger(context.Background())
	return &acpBackend{openAgent: func(ctx context.Context, prepared *launch.PreparedAgent, _ acpAdapterConfig) (acp.Agent, error) {
		return prepared.New(ctx, nil)
	}, bindings: defaultACPRuntimeBindings(), owned: make(map[*managementOwner]struct{}), changed: make(chan struct{})}
}
func (backend *acpBackend) binding(id string) (acpRuntimeBinding, error) {
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	binding, ok := backend.bindings[id]
	if backend.closing || !ok {
		return acpRuntimeBinding{}, status_error.RuntimeUnavailable
	}
	return binding, nil
}
func (backend *acpBackend) Close(ctx context.Context) error {
	backend.mutex.Lock()
	backend.closing = true
	backend.mutex.Unlock()
	for {
		backend.mutex.Lock()
		owners := make([]*managementOwner, 0, len(backend.owned))
		for owner := range backend.owned {
			owners = append(owners, owner)
		}
		backend.mutex.Unlock()
		var result error
		for _, owner := range owners {
			result = errors.Join(result, backend.closeOwner(ctx, owner))
		}
		if result != nil {
			return result
		}
		backend.mutex.Lock()
		if backend.pending == 0 && len(backend.owned) == 0 {
			backend.mutex.Unlock()
			return nil
		}
		changed := backend.changed
		backend.mutex.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return errors.Join(status_error.RuntimeShutdownFailed, ctx.Err())
		}
	}
}
func (backend *acpBackend) closeOwner(ctx context.Context, owner *managementOwner) error {
	select {
	case owner.closeGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-owner.closeGate }()
	if owner.manager != nil {
		if err := owner.manager.Close(ctx); err != nil {
			return err
		}
		owner.manager = nil
	}
	if owner.resources != nil {
		if err := owner.resources.Close(ctx); err != nil {
			return err
		}
		owner.resources = nil
	}
	if owner.releaseResources != nil {
		if err := owner.releaseResources(ctx); err != nil {
			return err
		}
		owner.releaseResources = nil
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	delete(backend.owned, owner)
	close(backend.changed)
	backend.changed = make(chan struct{})
	return nil
}

// ReleaseResources 保留尚未启动消费者也未能成功释放的租约，应用关闭时可重试。
func (backend *acpBackend) ReleaseResources(ctx context.Context, release func(context.Context) error) error {
	if release == nil {
		return nil
	}
	owner := &managementOwner{releaseResources: release, closeGate: make(chan struct{}, 1)}
	backend.mutex.Lock()
	backend.owned[owner] = struct{}{}
	backend.mutex.Unlock()
	return backend.closeOwner(ctx, owner)
}
func (backend *acpBackend) open(ctx context.Context, config acpAdapterConfig, binding acpRuntimeBinding, releaseResources func(context.Context) error) (*managementOwner, error) {
	owner := &managementOwner{releaseResources: releaseResources, closeGate: make(chan struct{}, 1)}
	backend.mutex.Lock()
	if backend.closing {
		backend.mutex.Unlock()
		return nil, status_error.RuntimeUnavailable
	}
	backend.pending++
	backend.mutex.Unlock()
	defer func() {
		backend.mutex.Lock()
		backend.pending--
		backend.owned[owner] = struct{}{}
		close(backend.changed)
		backend.changed = make(chan struct{})
		backend.mutex.Unlock()
	}()

	var err error
	owner.resources, err = launch.Prepare(ctx, launch.Config{Native: native.Config{RuntimeID: config.BaseRuntimeID, Executable: config.Executable, PrefixArgs: config.PrefixArgs, Environment: config.Environment, WorkingDirectory: config.WorkingDirectory, StateRootDirectory: config.StateRootDirectory, PermissionMode: config.PermissionMode, Logger: AdapterLogger(ctx), CodexConfiguration: config.CodexPluginConfiguration, ClaudeSettingsPath: config.ClaudeSettingsPath, NativePluginDirectories: config.NativePluginDirectories, DefaultModeRequestUserInput: constants.RuntimeCodexDefaultModeRequestUserInput}, UseNativeAuth: config.UseNativeAuth, Refresh: config.RefreshCredentials, Release: releaseResources})
	if owner.resources != nil {
		owner.releaseResources = nil
	}
	if err != nil {
		return owner, err
	}
	owner.manager, err = acpdriver.OpenManagement(ctx, func(ctx context.Context) (acp.Agent, error) { return backend.openAgent(ctx, owner.resources, config) }, acpdriver.ManagementConfig{CleanupTimeout: acpManagementCloseTimeout})
	return owner, err
}
func (backend *acpBackend) finish(ctx context.Context, owner *managementOwner) error {
	if owner == nil {
		return nil
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), acpManagementCloseTimeout)
	defer cancel()
	err := backend.closeOwner(cleanup, owner)
	if err != nil {
		AdapterLogger(ctx).Error("close ACP management resources failed", "error", err)
	}
	return err
}
func (backend *acpBackend) DetectOptions(ctx context.Context, request acpOptionsRequest) (catalog acpOptionCatalog, err error) {
	release := request.ReleaseResources
	defer func() {
		if release != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), acpManagementCloseTimeout)
			defer cancel()
			if closeErr := backend.ReleaseResources(cleanup, release); err == nil {
				err = closeErr
			}
		}
	}()
	binding, err := backend.binding(request.Prepared.definition.baseRuntimeID)
	if err != nil {
		return catalog, err
	}
	config := acpAdapterConfigForManagement(request.Prepared, runtimeconfig.RuntimeTarget{RuntimeID: request.RuntimeID, RefreshCredentials: request.RefreshCredentials}, c.WorkspaceScope{Root: request.WorkingDirectory}, false, constants.PermissionLevelDefault, binding)
	config.DetectionID = uuid.NewString()
	config.UseNativeAuth = request.UseNativeAuth
	config.StateRootDirectory = request.WorkingDirectory
	ctx = acpAgentLogContext(ctx, config, binding)
	defer func() {
		if err != nil && !errors.Is(ctx.Err(), context.Canceled) {
			AdapterLogger(ctx).Warn("ACP option detection failed", "error", runtimediagnostic.FailureDisplayMessage(err))
		}
	}()
	owner, err := backend.open(ctx, config, binding, release)
	if owner != nil {
		release = nil
	}
	defer func() {
		if closeErr := backend.finish(ctx, owner); err == nil {
			err = closeErr
		}
	}()
	if err != nil {
		return catalog, err
	}
	var skipModels []string
	if binding.descriptor.ID == runtimeconfig.RuntimeIDWorkBuddy {
		skipModels = []string{constants.RuntimeWorkBuddyFastModelID, constants.RuntimeWorkBuddyBalancedModelID, constants.RuntimeWorkBuddyDeepModelID}
	}
	raw, err := owner.manager.Probe(ctx, acpdriver.ProbeRequest{Workspace: c.WorkspaceScope{Root: request.WorkingDirectory}, Model: request.Model, ModelConfigID: binding.descriptor.ModelConfigID, InspectModelOptions: binding.descriptor.InspectModelOptions, SkipModelOptions: skipModels, AllowEmptyModelCatalog: binding.descriptor.AllowEmptyModelCatalog})
	catalog = acpInitializeCatalog(binding, raw.Initialize)
	catalog.SessionReady = raw.SessionReady
	if raw.Modes != nil {
		catalog.Modes = append([]acp.SessionMode(nil), raw.Modes.AvailableModes...)
	}
	if !raw.SessionReady {
		return catalog, err
	}
	decoder := catalogDecoder{binding: binding, models: map[string][]acp.SessionConfigOption{}}
	for _, model := range raw.Models {
		if model.ConfigOptions != nil {
			decoder.models[model.Model] = model.ConfigOptions
		}
	}
	options, decodeErr := decoder.detectSessionOptions(ctx, "", raw.ConfigOptions, catalog.Modes)
	catalog.Models, catalog.ReasoningOptions = options.Models, options.ReasoningOptions
	catalog.DefaultModel, catalog.DefaultReasoning = options.DefaultModel, options.DefaultReasoning
	catalog.PermissionLevels = options.PermissionLevels
	if err == nil {
		err = decodeErr
	}
	return catalog, err
}
