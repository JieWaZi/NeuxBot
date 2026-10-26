// Package execution 编排入口无关的工作准备与 Runtime 执行生命周期。
package execution

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/projectsource"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/plugininstallation"
)

// Service 是当前 Chat API 与未来 Channel、Schedule、IM 共用的执行接口。
type Service interface {
	// ForkChat 在统一执行环境内建立普通会话的原生分支。
	ForkChat(context.Context, chatservice.ForkInput) (string, error)
	// Open 绑定后台执行生命周期并开始接纳 WorkIntent。
	Open(ctx context.Context) error
	// Close 取消全部 Runtime 执行并等待持久化收尾。
	Close()
	// Start 校验并执行一个入口无关的工作意图。
	Start(ctx context.Context, intent WorkIntent) (AcceptedExecution, error)
	// ValidateConfiguration 校验 Runtime、模型、推理强度和权限组合可以用于下一次执行。
	ValidateConfiguration(ctx context.Context, config ExecutionConfiguration) error
	// ResolveConfiguration 解析并校验有效账号、模型和推理配置，供编排入口冻结后续执行。
	ResolveConfiguration(ctx context.Context, config ExecutionConfiguration) (ExecutionConfiguration, error)
	// Cancel 取消指定 Chat 当前活跃的执行。
	Cancel(chatID string) error
	// CancelAndWait 取消指定 Chat 当前活跃执行并等待 durable 终态。
	CancelAndWait(ctx context.Context, chatID string) error
	// CancelExecutionAndWait 只取消指定执行占用，旧任务不得影响同Chat后续输入。
	CancelExecutionAndWait(ctx context.Context, chatID, executionID string) error
	// ResolveApproval 处理 Chat 当前执行的一次未决审批。
	ResolveApproval(ctx context.Context, chatID string, approvalID string, optionID string) error
	// ResolveElicitation 处理 Chat 当前执行的一次未决结构化用户输入。
	ResolveElicitation(
		ctx context.Context,
		chatID string,
		elicitationID string,
		content map[string]any,
		declined bool,
	) error
}

// ExecutionConfiguration 是各入口保存执行默认值前复用的稳定校验输入。
type ExecutionConfiguration struct {
	// Resolved 表示默认选择已经解析；合法空值表示原生默认行为，不再重读用户默认配置。
	Resolved bool
	// AccountID 是冻结的受管账号；system 明确使用原生登录配置。
	AccountID string
	// RuntimeID 是需要使用的开放 Runtime 标识。
	RuntimeID string
	// Model 是可选模型标识；空值使用 Runtime 默认模型。
	Model string
	// Reasoning 是可选推理强度标识；空值使用 Runtime 默认值。
	Reasoning string
	// PermissionLevel 是跨 Runtime 的产品权限等级。
	PermissionLevel constants.PermissionLevel
	// RequiredConnectorTransport 是调用入口要求 Runtime 明确支持的 MCP 传输；空值表示无额外要求。
	RequiredConnectorTransport constants.RuntimeConnectorTransport
}

// ChatGroupPreparer 是原子建立共享工作范围与多个独立 Chat 的深模块端口，不是 HTTP 业务用例。
type ChatGroupPreparer interface {
	// PrepareChatGroup 为团队首发批量建立共享工作范围和一人一个成员 Chat。
	PrepareChatGroup(ctx context.Context, intent PrepareChatGroupIntent) (PreparedChatGroup, error)
}

// Core 组合入口共用执行用例与原子 Chat 组准备端口。
type Core interface {
	Service
	ChatGroupPreparer
}

// service 是 Execution Core 的唯一实现。
type service struct {
	// events 复用已提交 Chat 事件的应用内路由。
	events *events.Hub
	// agent 是统一执行能力的授权注册表。
	agent *agentcapability.Registry
	// agentEndpoint 是应用明确提供的本机执行协议地址。
	agentEndpoint string
	// agentBinary 是随应用发布的 CLI 真实路径。
	agentBinary string
	// agentBindings 提供伙伴当前普通业务能力的领域绑定。
	agentBindings func(models.Chat, string, []string, string) []agentcapability.Binding
	// validateIntent 在任何首发、续发或重试接纳前校验领域持久约束。
	validateIntent func(context.Context, WorkIntent) error
	// agentSessions 保存同一原生会话进程独占的连接文件位置。
	agentSessions *agentSessionFiles
	// acquireTeamChatInput 将团队成员 Chat 的直接输入纳入团队聚合接纳边界。
	acquireTeamChatInput func(context.Context, string, bool) (context.Context, func(), error)
	// accounts 为执行冻结账号并生成隔离配置。
	accounts *aiaccount.Service
	// runtime 执行一次结构化 Agent 调用并交付归一化事件。
	runtime runtimeRunner
	// journal 读写 Chat 权威事实、窄投影和 committed SSE。
	journal chatservice.Journal
	// dormantChats 批量建立尚无消息和执行的独立 Chat。
	dormantChats dormantChatCreator
	// wakerController 读取并验证伙伴。
	wakerController controllers.WakerController
	// profileController 读取执行上下文所需的伙伴文档。
	profileController controllers.ProfileDocumentController
	// projectController 解析新 Chat 选择的项目和冻结目录范围。
	projectController controllers.ProjectController
	// skillController 读取伙伴当前启用技能。
	skillController controllers.SkillController
	// connectorController 读取伙伴当前启用并已解密的连接器。
	connectorController controllers.MCPConnectorController
	// pluginInstallations 复用声明驱动身份与安装预检。
	pluginInstallations *plugininstallation.Service
	// pluginContents 保留进行中执行使用的内容目录。
	pluginContents *plugins.ContentStore
	// pluginController 读取插件安装的原子资源快照。
	pluginController controllers.PluginController
	// pluginLauncher 是受管 Core 可执行文件的绝对位置。
	pluginLauncher string
	// runtimeEnvironments 读取已经启用且探测可用的 Runtime 条目。
	runtimeEnvironments runtimeEnvironmentReader
	// skillDataDirectory 是技能版本文件的根目录。
	skillDataDirectory string
	// storage 统一确定普通 Chat 与团队成员 Chat 的日志和工作区地址。
	storage *chatstorage.Layout
	// executions 维护单 Chat 唯一执行约束和进程生命周期。
	executions *executionRegistry
	// createRequests 封住同一首次创建请求进入权威事实前的并发窗口。
	createRequests *createRequestRegistry
	// approvals 连接 Runtime 审批回调、Chat 权威事实和用户响应。
	approvals *approvalCoordinator
	// elicitations 连接 Runtime 结构化输入回调、Chat 权威事实和用户响应。
	elicitations *elicitationCoordinator
}

var _ Service = (*service)(nil)

// Config 声明 Execution Core 所需的稳定外部依赖。
type Config struct {
	// Events 提供已提交状态的有界观察。
	Events *events.Hub
	// Agent 是当前 Core 独占的短期执行能力注册表。
	Agent *agentcapability.Registry
	// AgentEndpoint 是与桌面管理认证分离的本机地址。
	AgentEndpoint string
	// AgentBinary 是应用提供的 CLI 绝对路径。
	AgentBinary string
	// AgentBindings 按冻结伙伴和目录范围签发普通能力。
	AgentBindings func(models.Chat, string, []string, string) []agentcapability.Binding
	// ValidateIntent 校验入口无关的领域接纳约束；为空时不增加额外限制。
	ValidateIntent func(context.Context, WorkIntent) error
	// AcquireTeamChatInput 保护团队成员直接输入，普通 Chat 通过不持锁。
	AcquireTeamChatInput func(context.Context, string, bool) (context.Context, func(), error)
	// Accounts 管理受管账号的使用租约和配置投射。
	Accounts *aiaccount.Service
	// Runtime 执行一次完整 Agent 调用并提供工作区投影声明。
	Runtime runtimeRunner
	// Journal 读写 Chat 权威事实和可重建投影。
	Journal chatservice.Journal
	// DormantChats 批量建立团队首发所需的独立空闲 Chat。
	DormantChats dormantChatCreator
	// Wakers 读取并验证伙伴。
	Wakers controllers.WakerController
	// Profiles 读取执行上下文所需的伙伴文档。
	Profiles controllers.ProfileDocumentController
	// Projects 读取新 Chat 选择的项目及其有序目录。
	Projects controllers.ProjectController
	// Skills 读取伙伴当前启用技能。
	Skills controllers.SkillController
	// Connectors 读取伙伴当前启用并已解密的连接器。
	Connectors controllers.MCPConnectorController
	// PluginInstallations 提供共享声明驱动身份。
	PluginInstallations *plugininstallation.Service
	// PluginContents 保护受管内容使用与延迟回收。
	PluginContents *plugins.ContentStore
	// Plugins 读取插件资源和安装。
	Plugins controllers.PluginController
	// PluginLauncher 提供保持 MCP 工作目录的 Core 程序。
	PluginLauncher string
	// RuntimeEnvironments 读取执行准备使用的 Runtime 探测快照。
	RuntimeEnvironments runtimeEnvironmentReader
	// SkillDataDirectory 是 NeuxBot 受管技能版本所在的应用数据目录。
	SkillDataDirectory string
	// StorageDirectory 是 Chat 与 Team 聚合目录共同使用的应用数据根目录。
	StorageDirectory string
}

// runtimeEnvironmentReader 是 Execution Core 读取执行环境快照的最小端口。
type runtimeEnvironmentReader interface {
	// Get 返回指定 Runtime 的持久化环境条目。
	Get(ctx context.Context, runtimeID string) (models.RuntimeEnvironment, error)
}

// dormantChatCreator 是 Execution Core 建立团队成员空闲 Chat 的最小端口。
type dormantChatCreator interface {
	// CreateDormantChats 批量建立只有 chat.created 首事件的独立 Chat。
	CreateDormantChats(ctx context.Context, chats []chatservice.DormantChat) ([]models.Chat, error)
	// DeleteProvisionalTeam 回滚尚未 ready 的 Team staging、日志和查询投影。
	DeleteProvisionalTeam(
		ctx context.Context,
		threadID string,
		provisioningID string,
		chatIDs []string,
	) error
}

// runtimeRunner 是 Execution Core 对 Runtime 深模块的最小消费接口。
type runtimeRunner interface {
	// ForkSession 使用冻结运行配置创建独立原生会话。
	ForkSession(context.Context, runtimes.ForkSessionRequest) (runtimes.ForkSessionResult, error)
	// Runtimes 返回工作区投影所需的 Runtime 摘要。
	Runtimes() []runtimes.RuntimeSummary
	// Run 阻塞执行一次调用，并按发生顺序交付统一事件。
	Run(context.Context, runtimes.RunRequest, runtimes.EventSink) runtimes.Result
}

// New 校验配置并创建尚未绑定进程生命周期的 Execution Core。
func New(config Config) (Core, error) {
	if config.Journal == nil {
		return nil, status_error.BadRequest
	}
	if config.Runtime == nil {
		return nil, status_error.BadRequest
	}
	if config.Wakers == nil {
		return nil, status_error.BadRequest
	}
	if config.Profiles == nil {
		return nil, status_error.BadRequest
	}
	if config.Projects == nil {
		return nil, status_error.BadRequest
	}
	if config.RuntimeEnvironments == nil {
		return nil, status_error.BadRequest
	}
	storageDirectory := strings.TrimSpace(config.StorageDirectory)
	if storageDirectory == "" {
		return nil, status_error.BadRequest
	}
	storage, err := chatstorage.NewLayout(storageDirectory)
	if err != nil {
		return nil, err
	}
	return &service{
		agent:                config.Agent,
		events:               config.Events,
		agentEndpoint:        config.AgentEndpoint,
		agentBinary:          config.AgentBinary,
		agentBindings:        config.AgentBindings,
		validateIntent:       config.ValidateIntent,
		agentSessions:        &agentSessionFiles{slots: map[string]agentSessionSlot{}},
		accounts:             config.Accounts,
		acquireTeamChatInput: config.AcquireTeamChatInput,
		runtime:              config.Runtime,
		journal:              config.Journal,
		dormantChats:         config.DormantChats,
		wakerController:      config.Wakers,
		profileController:    config.Profiles,
		projectController:    config.Projects,
		skillController:      config.Skills,
		pluginContents:       config.PluginContents,
		pluginInstallations:  config.PluginInstallations,
		pluginController:     config.Plugins, pluginLauncher: config.PluginLauncher,
		connectorController: config.Connectors,
		runtimeEnvironments: config.RuntimeEnvironments,
		skillDataDirectory:  strings.TrimSpace(config.SkillDataDirectory),
		storage:             storage,
		executions:          newExecutionRegistry(),
		createRequests:      newCreateRequestRegistry(),
		approvals:           newApprovalCoordinator(config.Journal),
		elicitations:        newElicitationCoordinator(config.Journal, config.Events),
	}, nil
}

// Open 绑定后台执行生命周期并开始接纳 WorkIntent。
func (m *service) Open(ctx context.Context) error {
	if m == nil || m.journal == nil {
		return status_error.ChatServiceNotStarted
	}
	m.executions.start(ctx)
	return nil
}

// Close 取消全部 Runtime 执行并等待持久化收尾。
func (m *service) Close() {
	if m != nil && m.executions != nil {
		m.executions.close()
	}
}

// Cancel 取消指定 Chat 当前活跃的执行。
func (m *service) Cancel(chatID string) error {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return status_error.BadRequest
	}
	m.executions.cancel(chatID)
	return nil
}

// CancelAndWait 取消指定 Chat 当前活跃执行并等待 durable 终态。
func (m *service) CancelAndWait(ctx context.Context, chatID string) error {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return status_error.BadRequest
	}
	return m.executions.cancelAndWait(ctx, chatID)
}

// CancelExecutionAndWait 原子匹配聊天与执行身份后取消，等待该次执行的持久收尾。
func (m *service) CancelExecutionAndWait(ctx context.Context, chatID, executionID string) error {
	chatID = strings.TrimSpace(chatID)
	executionID = strings.TrimSpace(executionID)
	if chatID == "" || executionID == "" {
		return status_error.BadRequest
	}
	return m.executions.cancelExecutionAndWait(ctx, chatID, executionID)
}

func resolveWorkingDirectory(value string) (models.ProjectSource, error) {
	return projectsource.Resolve(value)
}

func defaultTitle(text string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	runes := []rune(normalized)
	if len(runes) <= constants.DefaultChatTitleMaxRunes {
		return normalized
	}
	return string(runes[:constants.DefaultChatTitleMaxRunes]) + "..."
}

func validateMessage(text string) error {
	if strings.TrimSpace(text) == "" || !utf8.ValidString(text) || len(text) > 1024*1024 {
		return status_error.BadRequest
	}
	return nil
}

func marshalData(value interface{}) json.RawMessage {
	data, err := eventlog.MarshalData(value)
	if err != nil {
		panic(err)
	}
	return data
}

func nowMillis() int64 {
	return time.Now().UnixMilli()
}
