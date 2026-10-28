package runtimehost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sync"
	"time"

	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp"
	agentruntime "github.com/JieWaZi/NeuxBot/agent-runtime/runtime"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// ChatConfig 注入普通聊天所需的启动事实、目录资源与独立原生分叉能力。
type ChatConfig struct {
	// PrepareWorkspace 只物化受管原生指令与技能，不提交输入或创建进程。
	PrepareWorkspace func(runtimes.RunRequest) error
	// Launch 从已有探测与账号快照生成启动事实，不执行模型请求。
	Launch func(runtimes.RuntimeTarget, runtimes.DetectionResult) (runtimes.LaunchFacts, error)
	// Runtimes 返回当前已注册的CLI目录声明。
	Runtimes func() []runtimes.RuntimeSummary
	// Fork 是已有独立原生分叉操作，不参与普通聊天Run。
	Fork func(context.Context, runtimes.ForkSessionRequest) (runtimes.ForkSessionResult, error)
	// Driver 允许测试替换公开原生协议边界。
	Driver func(Environment, acpdriver.Factory) (c.Driver, error)
}

// ChatHost 将普通聊天的技术调用接入独立模块；Chat仍拥有消息日志与业务执行。
// 同一工作区的输入由Execution的单Chat占用串行化，mutex只保护跨Chat集合与退出。
type ChatHost struct {
	// config 保存应用注入的静态端口。
	config ChatConfig
	// mutex 保护会话所有者集合和关闭接纳。
	mutex sync.Mutex
	// sessions 按受管工作区保存会话配置事实及仍持有原生消费者的所有者。
	sessions map[string]*chatSession
	// closing 表示宿主不再接纳新的技术调用。
	closing bool
}

// chatSession 只关联实际原生会话与其独立模块所有者。
type chatSession struct {
	// runtime 拥有原生会话、认证和资源释放；关闭后为空，仅保留已知配置事实。
	runtime *agentruntime.Runtime
	// signature 是当前静态配置与授权范围的无秘密摘要。
	signature string
	// ref 是模块产生的热会话标识，不替代原生句柄。
	ref string
	// nativeID 是最近一次真实回执确认的原生句柄。
	nativeID string
}

// NewChat 创建不启动进程与后台线程的普通聊天宿主。
func NewChat(config ChatConfig) *ChatHost {
	return &ChatHost{config: config, sessions: make(map[string]*chatSession)}
}

// Runtimes 返回宿主已经注册的目录声明。
func (h *ChatHost) Runtimes() []runtimes.RuntimeSummary {
	if h.config.Runtimes == nil {
		return nil
	}
	return h.config.Runtimes()
}

// ForkSession 只调用独立原生分叉端口，不提交模型输入。
func (h *ChatHost) ForkSession(ctx context.Context, r runtimes.ForkSessionRequest) (runtimes.ForkSessionResult, error) {
	if h.config.Fork == nil {
		return runtimes.ForkSessionResult{}, status_error.RuntimePolicyUnsupported
	}
	return h.config.Fork(ctx, r)
}

// Run 执行不含额外宿主租约的普通聊天回合。
func (h *ChatHost) Run(ctx context.Context, r runtimes.RunRequest, sink runtimes.EventSink) runtimes.Result {
	return h.RunWithLease(ctx, r, sink, nil)
}

// RunSummaryWithLease 为已有原生会话生成隐藏交接，先释放热所有者再用只读配置冷恢复。
// 这项操作只恢复原生上下文，不重放历史；不支持恢复时由Execution采用原有近期历史回退。
func (h *ChatHost) RunSummaryWithLease(ctx context.Context, r runtimes.RunRequest, sink runtimes.EventSink, release func(context.Context) error) runtimes.Result {
	if err := h.CloseWorkspace(ctx, r.Workspace.Path); err != nil {
		if release != nil {
			_ = release(context.WithoutCancel(ctx))
		}
		return chatFailed(r, err)
	}
	r.Target.CloseAfterRun = true
	return h.runWithLease(ctx, r, sink, release, true)
}

// RunWithLease 接管已冻结的账号与插件租约，直到真实原生消费者退出才允许释放。
// 热会话沿用其原有租约，本轮未使用的新快照在返回时释放。
func (h *ChatHost) RunWithLease(ctx context.Context, r runtimes.RunRequest, sink runtimes.EventSink, release func(context.Context) error) runtimes.Result {
	return h.runWithLease(ctx, r, sink, release, false)
}

// runWithLease 共享原生准备与收尾；summary 只选择已有原生上下文，不改写普通续聊配置。
func (h *ChatHost) runWithLease(ctx context.Context, r runtimes.RunRequest, sink runtimes.EventSink, release func(context.Context) error, summary bool) runtimes.Result {
	resources := &chatResources{release: release}
	defer func() {
		if !resources.acquired {
			if err := resources.Close(context.Background()); err != nil {
				logx.Errorf("release unused chat resource failed: runtime_id=%s error=%v", r.Target.RuntimeID, err)
			}
		}
	}()
	failure := func(err error) runtimes.Result { return chatFailed(r, err) }
	if ctx == nil || sink == nil || h.config.Launch == nil {
		return failure(status_error.BadRequest)
	}
	spec, bindings, err := chatSpec(r)
	if err != nil {
		return failure(err)
	}
	resources.bindings = bindings
	signature := chatSignature(spec, r.Target)
	h.mutex.Lock()
	if h.closing {
		h.mutex.Unlock()
		return failure(status_error.RuntimeUnavailable)
	}
	owner := h.sessions[r.Workspace.Path]
	h.mutex.Unlock()
	storedSignature := signature
	if summary {
		storedSignature = ""
		if owner != nil && owner.nativeID == r.Session.ID {
			storedSignature = owner.signature
		}
	}
	// 配置变化明确新建会话，并只在新会话使用有界历史播种；原生恢复不重放历史。
	if !summary && owner != nil && (r.Session.Mode == runtimes.SessionModeNew || (owner.signature != "" && owner.signature != signature) || owner.nativeID != r.Session.ID) {
		if err := h.CloseWorkspace(ctx, r.Workspace.Path); err != nil {
			return failure(err)
		}
		owner = nil
		if r.Session.Mode == runtimes.SessionModeResume && r.Session.ID != "" {
			r.Session = runtimes.SessionDirective{Mode: runtimes.SessionModeNew}
		}
	}
	if owner != nil && owner.runtime == nil {
		owner = nil
	}
	if owner == nil {
		if h.config.PrepareWorkspace != nil {
			if err := h.config.PrepareWorkspace(r); err != nil {
				return failure(err)
			}
		}
		launch, err := h.config.Launch(r.Target, r.Detection)
		if err != nil {
			return failure(err)
		}
		env := Environment{BaseRuntimeID: launch.BaseRuntimeID, Target: r.Target, Detection: r.Detection}
		descriptor, ok := runtimeDescriptors[launch.BaseRuntimeID]
		if !ok {
			return failure(status_error.RuntimeUnavailable)
		}
		credentials := &chatCredentials{provider: &credentialProvider{input: TextRequest{RuntimeID: r.Target.RuntimeID, AccountID: r.Target.AccountID}, environment: env, workspace: filepath.Dir(r.Workspace.Path), target: r.Target, launch: launch}}
		resources.credentials = credentials
		nativeFactory := acpdriver.Factory(func(factoryContext context.Context, open c.OpenRequest) (acp.Agent, error) {
			return credentials.agent(factoryContext, open, r.Policy.Execution.PermissionLevel)
		})
		var driver c.Driver
		if h.config.Driver != nil {
			driver, err = h.config.Driver(env, nativeFactory)
		} else {
			config := acpdriver.Config{ID: r.Target.RuntimeID, Factory: nativeFactory, CompletionPolicy: descriptor.completion, ModeCandidates: chatModeCandidates(launch.BaseRuntimeID, r.Policy.Execution), SingleModeFallback: descriptor.singleModeFallback}
			if r.Workspace.Managed && h.config.PrepareWorkspace != nil {
				config.PromptContext = func(c.OpenRequest) (string, error) { return "", nil }
			}
			driver, err = acpdriver.New(config)
		}
		if err != nil {
			return failure(mapFault(err))
		}
		directories := append([]string{r.Workspace.Path}, r.Workspace.AdditionalWritableDirectories...)
		engine, err := agentruntime.New(agentruntime.Config{Drivers: []c.Driver{driver}, Credentials: credentials, Resources: resources, Grants: c.Grants{Directories: directories, AllowAllTools: true}})
		if err != nil {
			_ = driver.Close(context.WithoutCancel(ctx))
			return failure(mapFault(err))
		}
		if err := engine.Start(ctx); err != nil {
			_ = engine.Close(context.WithoutCancel(ctx))
			return failure(mapFault(err))
		}
		owner = &chatSession{runtime: engine, signature: storedSignature}
		h.mutex.Lock()
		if h.closing {
			h.mutex.Unlock()
			_ = engine.Close(context.WithoutCancel(ctx))
			return failure(status_error.RuntimeUnavailable)
		}
		h.sessions[r.Workspace.Path] = owner
		h.mutex.Unlock()
		if r.Session.Mode == runtimes.SessionModeResume {
			checkpoint, err := engine.NativeCheckpoint(spec, r.Session.ID)
			if err != nil {
				_ = h.CloseWorkspace(context.WithoutCancel(ctx), r.Workspace.Path)
				return failure(mapFault(err))
			}
			spec.Session = c.SessionDirective{Mode: c.SessionResume, Checkpoint: checkpoint}
		} else {
			spec.Session = c.SessionDirective{Mode: c.SessionNew, Seed: chatSeed(r.History)}
		}
	} else {
		spec.Session = c.SessionDirective{Mode: c.SessionResume, Ref: owner.ref}
	}
	bridge := &chatObserver{sink: sink}
	result := owner.runtime.Run(ctx, spec, c.RunOptions{Observer: bridge, UserInputState: r.UserInputState, InteractionHandler: chatInteraction{approval: r.ApprovalHandler, input: r.ElicitationHandler, readOnly: r.Policy.Execution.WorkspaceReadOnly}})
	if result.Usage.Known || result.Usage.ContextKnown {
		delivery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err := bridge.emit(delivery, runtimes.EventTypeUsage, chatUsage(result.Usage))
		cancel()
		if err != nil && result.DeliveryFault == nil {
			result.DeliveryFault = &c.Fault{Code: c.FaultDelivery, Stage: "usage", Message: "final usage delivery failed", Cause: err}
		}
	}
	output := chatResult(r, result, bridge)
	owner.ref = result.Invocation.SessionID
	owner.nativeID = output.Session.OutputID
	if r.Target.CloseAfterRun || result.Fault != nil || result.DeliveryFault != nil || result.CleanupFault != nil || owner.ref == "" || owner.nativeID == "" {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		if err := h.CloseWorkspace(cleanup, r.Workspace.Path); err != nil {
			logx.Errorf("close chat native session pending: runtime_id=%s invocation_id=%s error=%v", r.Target.RuntimeID, result.Invocation.ID, err)
		}
		cancel()
	}
	return output
}

// CloseWorkspace 在重写共享认证或工作区文件前完成旧原生消费者的释放。
func (h *ChatHost) CloseWorkspace(ctx context.Context, workspace string) error {
	h.mutex.Lock()
	owner := h.sessions[workspace]
	h.mutex.Unlock()
	if owner == nil || owner.runtime == nil {
		return nil
	}
	if err := owner.runtime.Close(ctx); err != nil {
		logx.Errorf("close chat owner failed: error=%v", err)
		return status_error.RuntimeShutdownFailed
	}
	h.mutex.Lock()
	if h.sessions[workspace] == owner {
		owner.runtime = nil
		owner.ref = ""
		if owner.nativeID == "" {
			delete(h.sessions, workspace)
		}
	}
	h.mutex.Unlock()
	return nil
}

// Close 在Execution停止并排空回合后关闭剩余热会话；失败允许同一所有者重试。
func (h *ChatHost) Close(ctx context.Context) error {
	h.mutex.Lock()
	h.closing = true
	workspaces := make([]string, 0, len(h.sessions))
	for workspace := range h.sessions {
		workspaces = append(workspaces, workspace)
	}
	h.mutex.Unlock()
	for _, workspace := range workspaces {
		if err := h.CloseWorkspace(ctx, workspace); err != nil {
			return err
		}
		h.mutex.Lock()
		delete(h.sessions, workspace)
		h.mutex.Unlock()
	}
	return nil
}

// chatResources 保留本次真正被原生会话使用的资源及可重试释放顺序。
type chatResources struct {
	// bindings 是租约保护的技能路径与MCP连接事实。
	bindings []c.ResourceBinding
	// credentials 在回收账号投射前关闭原生刷新入口。
	credentials *chatCredentials
	// release 回收外部账号与插件；失败时保持租约所有权。
	release func(context.Context) error
	// acquired 表示模块已接管资源，宿主不得再自行释放。
	acquired bool
}

func (r *chatResources) Acquire(context.Context, c.ResourceRequest) (c.ResourceLease, error) {
	r.acquired = true
	return r, nil
}
func (r *chatResources) Resources() []c.ResourceBinding { return r.bindings }
func (r *chatResources) Close(ctx context.Context) error {
	if r.credentials != nil {
		if err := r.credentials.Close(ctx); err != nil {
			return err
		}
	}
	if r.release != nil {
		if err := r.release(ctx); err != nil {
			return err
		}
		r.release = nil
	}
	return nil
}

func chatSignature(spec c.ExecutionSpec, target runtimes.RuntimeTarget) string {
	spec.Input = c.Input{}
	spec.Session = c.SessionDirective{}
	spec.Limits = c.Limits{}
	data, _ := json.Marshal([]any{spec, target.Binary, target.PrefixArgs, target.ConfigHome, target.NativePluginDirectories, target.NativePluginScope, target.CodexPluginConfiguration, target.ClaudeSettingsPath})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func chatSeed(history []runtimes.HistoryMessage) []c.SeedMessage {
	seed := make([]c.SeedMessage, 0, len(history))
	for _, message := range history {
		references := make([]c.CodeReference, 0, len(message.CodeReferences))
		for _, reference := range message.CodeReferences {
			references = append(references, c.CodeReference{Path: reference.Path, StartLine: reference.StartLine, EndLine: reference.EndLine, Content: reference.Content})
		}
		seed = append(seed, c.SeedMessage{Role: message.Role, Text: message.Content, CodeReferences: references})
	}
	return seed
}

func chatFailed(r runtimes.RunRequest, err error) runtimes.Result {
	code := runtimes.ClassifyFailure(r.Detection.Runtime.BaseRuntimeID, err)
	return runtimes.Result{ExitCode: -1, Session: runtimes.SessionResult{Mode: r.Session.Mode, InputID: r.Session.ID, State: runtimes.SessionStateUnknown}, Failure: &runtimes.Failure{Code: code, Stage: constants.RuntimeRunFailureStagePrepare, Message: err.Error(), DisplayMessage: runtimes.FailureDisplayMessage(err), Transience: runtimes.FailureTransiencePermanent}}
}
