package catalog

import (
	"context"
	"errors"
	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp"
	"github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp/native"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimeauth"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
	acp "github.com/JieWaZi/acp-go/sdk"
	"github.com/google/uuid"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const acpManagementCloseTimeout = 5 * time.Second

// acpManager 是目录服务使用的无模型输入管理端口。
type acpManager interface {
	DetectOptions(context.Context, acpOptionsRequest) (acpOptionCatalog, error)
	ForkSession(context.Context, preparedRuntime, runtimecontract.ForkSessionRequest) (runtimecontract.ForkSessionResult, error)
	Close(context.Context) error
}

// acpOptionsRequest 提供账号和已冻结的启动事实。
type acpOptionsRequest struct {
	ReleaseResources   func()                            // ReleaseResources 在管理使用者确认退出后释放宿主探测依赖。
	RefreshCredentials runtimecontract.CredentialRefresh // RefreshCredentials 指向账号刷新权威。
	Model              string                            // Model 是需要实际确认的原生模型。
	RuntimeID          string                            // RuntimeID 是产品选择的基础或本地配置身份。
	Prepared           preparedRuntime                   // Prepared 是已有安装和环境事实。
	WorkingDirectory   string                            // WorkingDirectory 是宿主拥有的临时工作目录。
}

// acpOptionCatalog 是原始协议目录经过产品规则解释后的结果。
type acpOptionCatalog struct {
	AgentConnected   bool                              // AgentConnected 表示标准握手成功。
	SessionReady     bool                              // SessionReady 表示临时会话创建成功。
	Version          string                            // Version 是原生CLI版本。
	Capabilities     runtimecontract.CapabilitySet     // Capabilities 是本次握手确认的产品能力。
	Models           []runtimecontract.ModelOption     // Models 是可选择的模型目录。
	ReasoningOptions []runtimecontract.ReasoningOption // ReasoningOptions 是当前模型的强度目录。
	DefaultModel     string                            // DefaultModel 是最初或显式选择的模型。
	DefaultReasoning string                            // DefaultReasoning 是该模型当前强度。
	PermissionLevels []runtimecontract.PermissionLevel // PermissionLevels 是产品权限映射。
	Modes            []acp.SessionMode                 // Modes 是原始模式目录。
}

// acpBackend 只装配短生命周期管理操作，不实现协议或厂商进程。
type acpBackend struct {
	mutex    sync.Mutex                    // mutex 保护关闭门禁、测试工厂和仍持有依赖的管理所有者。
	bindings map[string]acpRuntimeBinding  // bindings 是冻结的产品目录解释规则。
	closing  bool                          // closing 阻止应用关闭后继续接纳。
	owned    map[*managementOwner]struct{} // owned 保留未确认关闭的原生使用者。
	pending  int                           // pending 统计尚未返回技术所有者的准备操作。
	changed  chan struct{}                 // changed 在所有者或准备计数变化时唤醒关闭。

}

// managementOwner 保证原生使用者退出后才释放刷新入口与账号配置。
type managementOwner struct {
	releaseResources func()                          // releaseResources 保存仍被原生使用者占用的账号与工作目录。
	manager          *acpdriver.Management           // manager 是原生与协议连接的唯一技术所有者。
	broker           *runtimeauth.CodexRefreshBroker // broker 是此原生进程的凭据刷新入口。
	sourceAuth       string                          // sourceAuth 是原生退出后需要对账的系统凭据文件。
}

func newACPBackend() *acpBackend {
	acpAdapterLogger(context.Background())
	return &acpBackend{bindings: defaultACPRuntimeBindings(), owned: make(map[*managementOwner]struct{}), changed: make(chan struct{})}
}
func (backend *acpBackend) binding(id string) (acpRuntimeBinding, error) {
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	binding, ok := backend.bindings[id]
	if backend.closing || !ok || binding.newAgent == nil {
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
	if owner.manager != nil {
		if err := owner.manager.Close(ctx); err != nil {
			return err
		}
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	if owner.sourceAuth != "" {
		unlock, err := runtimeauth.CodexFileCredentialGate(owner.sourceAuth).Lock(ctx)
		if err != nil {
			return err
		}
		_, err = runtimeauth.ReconcileCodexFileAuth(owner.sourceAuth)
		unlock()
		if err != nil {
			return err
		}
		owner.sourceAuth = ""
	}
	if owner.broker != nil {
		if err := owner.broker.Close(ctx); err != nil {
			return err
		}
		owner.broker = nil
	}
	if owner.releaseResources != nil {
		owner.releaseResources()
		owner.releaseResources = nil
	}
	delete(backend.owned, owner)
	close(backend.changed)
	backend.changed = make(chan struct{})
	return nil
}
func (backend *acpBackend) open(ctx context.Context, config acpAdapterConfig, binding acpRuntimeBinding, releaseResources func()) (*managementOwner, error) {
	owner := &managementOwner{releaseResources: releaseResources}
	backend.mutex.Lock()
	if backend.closing {
		backend.mutex.Unlock()
		return nil, status_error.RuntimeUnavailable
	}
	backend.pending++
	backend.mutex.Unlock()
	config.resources = owner
	defer func() {
		backend.mutex.Lock()
		backend.pending--
		backend.owned[owner] = struct{}{}
		close(backend.changed)
		backend.changed = make(chan struct{})
		backend.mutex.Unlock()
	}()

	var err error
	owner.manager, err = acpdriver.OpenManagement(ctx, func(ctx context.Context) (acp.Agent, error) { return binding.newAgent(ctx, config) }, acpdriver.ManagementConfig{CleanupTimeout: acpManagementCloseTimeout})
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
		acpAdapterLogger(ctx).Error("close ACP management resources failed", "error", err)
	}
	return err
}
func (backend *acpBackend) DetectOptions(ctx context.Context, request acpOptionsRequest) (catalog acpOptionCatalog, err error) {
	release := request.ReleaseResources
	defer func() {
		if release != nil {
			release()
		}
	}()
	binding, err := backend.binding(request.Prepared.definition.baseRuntimeID)
	if err != nil {
		return catalog, err
	}
	config := acpAdapterConfigForManagement(request.Prepared, runtimecontract.RunRequest{Target: runtimecontract.RuntimeTarget{RuntimeID: request.RuntimeID, RefreshCredentials: request.RefreshCredentials}, Workspace: runtimecontract.Workspace{Path: request.WorkingDirectory}}, binding)
	config.DetectionID = uuid.NewString()
	ctx = acpAgentLogContext(ctx, config, binding)
	defer func() {
		if err != nil && !errors.Is(ctx.Err(), context.Canceled) {
			acpAdapterLogger(ctx).Warn("ACP option detection failed", "error", runtimecontract.FailureDisplayMessage(err))
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
	if binding.baseRuntimeID == runtimecontract.RuntimeIDWorkBuddy {
		skipModels = []string{constants.RuntimeWorkBuddyFastModelID, constants.RuntimeWorkBuddyBalancedModelID, constants.RuntimeWorkBuddyDeepModelID}
	}
	raw, err := owner.manager.Probe(ctx, acpdriver.ProbeRequest{Workspace: c.WorkspaceScope{Root: request.WorkingDirectory}, Model: request.Model, ModelConfigID: binding.modelConfigID, InspectModelOptions: !binding.nativeProcess || binding.probeModelOptions, SkipModelOptions: skipModels, AllowEmptyModelCatalog: binding.nativeProcess})
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

// newNativeAgent 只投影产品已确定的参数，全部原生构造由native.New统一拥有。
func newNativeAgent(ctx context.Context, config acpAdapterConfig) (acp.Agent, error) {
	if config.BaseRuntimeID == runtimecontract.RuntimeIDCodex && config.resources != nil {
		owner := config.resources
		refresh := config.RefreshCredentials
		if refresh == nil {
			var err error
			owner.sourceAuth, err = runtimeauth.CodexAuthPathForEnvironment(config.Environment)
			if err != nil {
				return nil, err
			}
			refresh, err = runtimeauth.CodexFileRefresh(owner.sourceAuth, nil)
			if err != nil {
				return nil, err
			}
		}
		if refresh != nil {
			var err error
			owner.broker, err = runtimeauth.NewCodexRefreshBroker(refresh)
			if err != nil {
				return nil, err
			}
			environment, err := buildEnvironment(config.Environment, map[string]string{runtimeauth.CodexRefreshURLVariable: owner.broker.URL()})
			if err != nil {
				return nil, err
			}
			config.Environment = environment.list()
		}
	}
	args := cloneStrings(config.PrefixArgs)
	switch config.BaseRuntimeID {
	case runtimecontract.RuntimeIDCodex:
		keys := make([]string, 0, len(config.CodexPluginConfiguration))
		for key := range config.CodexPluginConfiguration {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			args = append(args, "-c", key+"="+config.CodexPluginConfiguration[key])
		}
	case runtimecontract.RuntimeIDClaude:
		if config.ClaudeSettingsPath != "" {
			args = append(args, "--settings", config.ClaudeSettingsPath)
		}
		for _, directory := range config.NativePluginDirectories {
			args = append(args, "--plugin-dir", directory)
		}
	}
	return native.New(ctx, native.Config{RuntimeID: config.BaseRuntimeID, Executable: config.Executable, PrefixArgs: args, Environment: config.Environment, WorkingDirectory: config.WorkingDirectory, StateDirectory: config.StateDirectory, StateDirectoryForWorkspace: func(cwd string) string { return filepath.Join(filepath.Dir(cwd), "cursor-state") }, PermissionMode: config.PermissionMode, Logger: acpAdapterLogger(ctx), DefaultModeRequestUserInput: constants.RuntimeCodexDefaultModeRequestUserInput})
}
