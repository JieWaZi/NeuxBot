// Package global 负责 NeuxBot Core 的进程级配置与组件装配。
package global

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/services/plugininstallation"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinwaker"
	integrationapi "github.com/JieWaZi/NeuxBot/server/internal/integration"
	knowledgebridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/memory"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/feishu"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/github"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	channelservice "github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	imservice "github.com/JieWaZi/NeuxBot/server/services/im"
	knowledgeservice "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/JieWaZi/NeuxBot/server/services/notification"
	"github.com/JieWaZi/NeuxBot/server/services/runtimeenvironment"
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

var (
	// KnowledgeService 复用页面、聊天与自动入口的真实知识维护。
	KnowledgeService *knowledgeservice.Service
	// KnowledgeRemoteMountControllerManager 只保存用户明确选择的远端库入口。
	KnowledgeRemoteMountControllerManager controllers.KnowledgeRemoteMountController
	// KnowledgeEngine 拥有专属知识库和受管原件生命周期。
	KnowledgeEngine *core.Engine
	// KnowledgeControllerManager 仅持久化本机知识接入引用。
	KnowledgeControllerManager controllers.KnowledgeController
	// AgentCapabilities 只接纳当前执行授权，不接受桌面全局管理令牌。
	AgentCapabilities *agentcapability.Registry
	// EventHub 是当前 Core 生命周期内所有安全事件的窄路由器。
	EventHub *events.Hub
	// NotificationControllerManager 查询原事实与版本化通知定位。
	NotificationControllerManager controllers.NotificationController
	// NotificationService 聚合原待办与待查看事实并维护带版本摘要。
	NotificationService *notification.Service
	// IMControllerManager 持久保存平台连接、聊天授权和投递。
	IMControllerManager controllers.IMController
	// IMService 接纳平台输入并管理独立交付生命周期。
	IMService *imservice.Service
	// WorkflowControllerManager 保存源码版本、请求回执和持久运行事实。
	WorkflowControllerManager controllers.WorkflowController
	// WorkflowCreationControllerManager 保存创作会话和批注。
	WorkflowCreationControllerManager controllers.WorkflowCreationController
	// WorkflowRunControllerManager 保存运行、帧、组、效果与人工请求。
	WorkflowRunControllerManager controllers.WorkflowRunController
	// WorkflowState 统一 API 与 Runner 的命令锁、租约和状态提交边界。
	WorkflowState *workflowstate.Coordinator
	// WorkflowService 提供多入口复用的源码、运行与执行能力。
	WorkflowService *workflow.Service
	// AutomaticTaskControllerManager 持久保存自动任务及唯一运行机会。
	AutomaticTaskControllerManager controllers.AutomaticTaskController
	// AttentionNoticeControllerManager 持久保存应用内工作提醒的关闭记录。
	AttentionNoticeControllerManager controllers.AttentionNoticeController
	// ScheduleService 管理定时机会及独立本机任务 API 的生命周期。
	ScheduleService *schedule.Service
	// AIAccountControllerManager 保存账号及加密凭据。
	AIAccountControllerManager controllers.AIAccountController
	// AIAccountService 管理账号使用和登录生命周期。
	AIAccountService *aiaccount.Service
	// Config 保存当前进程解析后的配置。
	Config = NewConfiguration()
	// DB 保存业务数据库的 GORM 连接。
	DB *gorm.DB
	// WakerControllerManager 提供全局伙伴持久化接口。
	WakerControllerManager controllers.WakerController
	// RolePresetControllerManager 提供全局角色预设持久化接口。
	RolePresetControllerManager controllers.RolePresetController
	// ProfileDocumentControllerManager 提供全局档案文档持久化接口。
	ProfileDocumentControllerManager controllers.ProfileDocumentController
	// TransactionControllerManager 提供统一数据库事务边界。
	TransactionControllerManager controllers.TransactionController
	// ProjectSourceControllerManager 提供全局项目来源查询索引接口。
	ProjectSourceControllerManager controllers.ProjectSourceController
	// ProjectControllerManager 提供全局伙伴项目持久化接口。
	ProjectControllerManager controllers.ProjectController
	// WorkResultControllerManager 保存结果交付与阅读事实。
	WorkResultControllerManager controllers.WorkResultController
	// WorkBoardControllerManager 聚合原工作的窄查询。
	WorkBoardControllerManager controllers.WorkBoardController
	// ChatControllerManager 提供全局对话查询索引接口。
	ChatControllerManager controllers.ChatController
	// ChatDeletionIntentControllerManager 提供 Chat/Team 聚合删除的权威提交标记。
	ChatDeletionIntentControllerManager controllers.ChatDeletionIntentController
	// ChannelControllerManager 提供全局团队配置与编排索引持久化接口。
	ChannelControllerManager controllers.ChannelController
	// ChannelThreadControllerManager 提供全局团队对话快照和持久事件接口。
	ChannelThreadControllerManager controllers.ChannelThreadController
	// ChannelTaskControllerManager 提供全局团队任务板和投递队列接口。
	ChannelTaskControllerManager controllers.ChannelTaskController
	// PluginAuthorizations 是各提供者唯一的共享身份、授权等待与工作占用装配。
	PluginAuthorizations map[string]*authorization.Authorizer
	// PluginControllerManager 保存插件资源、版本、共享配置和伙伴安装。
	PluginControllerManager controllers.PluginController
	// SkillControllerManager 提供全局技能包、版本和伙伴关系持久化接口。
	SkillControllerManager controllers.SkillController
	// MCPConnectorControllerManager 提供共享连接器和伙伴关系持久化接口。
	MCPConnectorControllerManager controllers.MCPConnectorController
	// RuntimeSDK 提供 CLI Runtime 的探测、协议调用和进程治理。
	RuntimeSDK *runtimes.SDK
	// RuntimeEnvironmentControllerManager 提供设备级 Runtime 环境持久化接口。
	RuntimeEnvironmentControllerManager controllers.RuntimeEnvironmentController
	// RuntimeEnvironmentService 提供 Runtime 环境初始化和探测生命周期。
	RuntimeEnvironmentService runtimeenvironment.Service
	// ChatService 提供 Chat 的全部稳定业务能力。
	ChatService chat.Module
	// ExecutionService 提供所有产品入口共用的工作准备与执行能力。
	ExecutionService execution.Service
	// ChannelService 提供团队对话、任务调度和结果回传能力。
	ChannelService channelservice.Service
	// MemoryService 是长期记忆存储与受限命令服务。
	MemoryService *memory.Service
)

// Configuration 保存 NeuxBot Core 的本地运行配置。
type Configuration struct {
	// Server 配置 ginx 的回环 HTTP 服务。
	Server conf.Server `yaml:"server"`
	// DB 配置 GORM 数据库连接和 AutoMigrate。
	DB conf.DB `yaml:"db"`
	// Security 配置 Renderer 访问 Core 的本地认证。
	Security SecurityConfiguration `yaml:"security"`
	// WorkflowPythonExecutable 是明确配置的 CPython 3.11+ 路径，空值表示未配置。
	WorkflowPythonExecutable string `yaml:"workflowPythonExecutable"`
	// DataDirectory 保存数据库、技能、独立账号数据及程序配置。
	DataDirectory string `yaml:"-"`
	// PluginResourcesDirectory 是构建交付的本地插件与固定程序目录。
	PluginResourcesDirectory string `yaml:"-"`
	// ManagedFilesDirectory 保存 Chat、Team、工作流和 IM 的受管文件，默认位于用户主目录。
	ManagedFilesDirectory string `yaml:"-"`
}

// SecurityConfiguration 保存本地进程访问控制配置。
type SecurityConfiguration struct {
	// LocalToken 是桌面壳与 Core 进程共享的随机 Bearer Token。
	LocalToken string `yaml:"local_token" env:"ALLY_LOCAL_TOKEN"`
}

// NewConfiguration 创建适用于本地桌面的默认配置。
func NewConfiguration() *Configuration {
	server := conf.NewOptions(
		conf.WithName(constants.ApplicationName),
		conf.WithHostPorts(constants.CoreDefaultHost, constants.CoreDefaultPort),
		conf.WithGraceExitTime(constants.CoreGraceExitSeconds),
	)
	return &Configuration{
		Server: *server,
		DB: conf.DB{
			Type:   conf.Sqlite,
			DBName: filepath.Join(".ally", "ally.db"),
		},
	}
}

// Prepare 校验回环边界、应用数据目录并生成缺省令牌。
func Prepare(configuration *Configuration) error {
	if configuration.Server.Host == "" {
		configuration.Server.Host = constants.CoreDefaultHost
	}
	if configuration.Server.Host != constants.CoreDefaultHost {
		return status_error.CoreConfigurationInvalid
	}
	if configuration.Server.Port == 0 {
		configuration.Server.Port = constants.CoreDefaultPort
	}
	if dataDir := strings.TrimSpace(os.Getenv("ALLY_DATA_DIR")); dataDir != "" {
		configuration.DB.Type = conf.Sqlite
		configuration.DB.Dsn = ""
		configuration.DB.DBName = filepath.Join(dataDir, "ally.db")
	}
	if configuration.DB.Type == "" {
		configuration.DB.Type = conf.Sqlite
	}
	if configuration.DB.Type == conf.Sqlite {
		if strings.TrimSpace(configuration.DB.DBName) == "" &&
			strings.TrimSpace(configuration.DB.Dsn) == "" {
			configuration.DB.DBName = filepath.Join(".ally", "ally.db")
		}
		databasePath := configuration.DB.DBName
		if strings.TrimSpace(configuration.DB.Dsn) != "" {
			databasePath = configuration.DB.Dsn
		}
		if databasePath != ":memory:" {
			absoluteDatabasePath, err := filepath.Abs(databasePath)
			if err != nil {
				return mapCoreError("resolve_database_path", status_error.CoreDataDirectoryError, err)
			}
			configuration.DataDirectory = filepath.Dir(absoluteDatabasePath)
			if err := os.MkdirAll(configuration.DataDirectory, 0o700); err != nil {
				return mapCoreError("create_data_directory", status_error.CoreDataDirectoryError, err)
			}
		} else if dataDir := strings.TrimSpace(os.Getenv("ALLY_DATA_DIR")); dataDir != "" {
			absoluteDataDirectory, err := filepath.Abs(dataDir)
			if err != nil {
				return mapCoreError("resolve_data_directory", status_error.CoreDataDirectoryError, err)
			}
			configuration.DataDirectory = absoluteDataDirectory
		} else {
			temporaryDataDirectory, err := os.MkdirTemp("", "ally-data-")
			if err != nil {
				return mapCoreError("create_temporary_data_directory", status_error.CoreDataDirectoryError, err)
			}
			configuration.DataDirectory = temporaryDataDirectory
		}
	}
	if configuration.PluginResourcesDirectory == "" {
		configuration.PluginResourcesDirectory = strings.TrimSpace(os.Getenv("ALLY_PLUGIN_RESOURCES_DIR"))
	}
	if configuration.PluginResourcesDirectory == "" {
		executable, err := os.Executable()
		if err != nil {
			return mapCoreError("resolve_plugin_resources", status_error.CoreDataDirectoryError, err)
		}
		configuration.PluginResourcesDirectory = filepath.Join(filepath.Dir(executable), "..", "plugins")
	}
	managedDirectory := strings.TrimSpace(os.Getenv("ALLY_MANAGED_FILES_DIR"))
	if managedDirectory == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return mapCoreError("resolve_user_home_directory", status_error.CoreDataDirectoryError, err)
		}
		managedDirectory = filepath.Join(home, constants.ManagedFilesDirectoryName)
	}
	absoluteManagedDirectory, err := filepath.Abs(managedDirectory)
	if err != nil {
		return mapCoreError("resolve_managed_files_directory", status_error.CoreDataDirectoryError, err)
	}
	configuration.ManagedFilesDirectory = absoluteManagedDirectory
	if err := os.MkdirAll(configuration.ManagedFilesDirectory, 0o700); err != nil {
		return mapCoreError("create_managed_files_directory", status_error.CoreDataDirectoryError, err)
	}
	if strings.TrimSpace(configuration.Security.LocalToken) == "" {
		token, err := randomToken()
		if err != nil {
			return err
		}
		configuration.Security.LocalToken = token
	}
	return nil
}

// Load 按依赖顺序装配业务库、独立记忆库、Runtime 与后台生命周期。
func Load(ctx context.Context, configuration *Configuration, handlerFactory schedule.LocalHandlerFactory) error {
	return runLoadSequence(
		func() {
			if err := Close(); err != nil {
				logx.Errorf("rollback NeuxBot Core initialization failed: error=%v", err)
			}
		},
		func() error { return loadDB(ctx, configuration) },
		func() error { return cleanupSkillArtifacts(ctx, configuration) },
		func() error { return loadBuiltinSkills(ctx, configuration) },
		func() error { return loadMemory(ctx, configuration) },
		func() error { return loadKnowledge(ctx, configuration) },
		func() error { return loadRuntime(ctx) },
		func() error { return loadChat(ctx, configuration, handlerFactory) },
		func() error { return loadNotifications(ctx) },
		func() error { return loadIntegrationAPI(ctx) },
		func() error {
			hub := EventHub
			closePluginCatalog = catalog.Start(ctx, PluginCatalogSource(), filepath.Join(configuration.DataDirectory, "plugins", "catalog"), func() {
				_ = events.Emit(hub, constants.PluginEventTopic, "plugins.changed", events.Resource{Kind: "global"}, nil)
			})
			return nil
		},
	)
}

// runLoadSequence 依次执行初始化步骤，并在首个失败后统一回滚已装配资源。
func runLoadSequence(rollback func(), steps ...func() error) error {
	for _, step := range steps {
		if err := step(); err != nil {
			rollback()
			return err
		}
	}
	return nil
}

// loadDB 初始化业务数据库、Repository，并写入内置连接器目录。
func loadDB(ctx context.Context, configuration *Configuration) error {
	// 强制迁移数据库
	configuration.DB.Migrate = true

	database, err := dbhelper.NewDB(configuration.DB)
	if err != nil {
		return mapCoreError("initialize_application_database", status_error.CoreDatabaseInitializationFailed, err)
	}

	EventHub = events.New()
	AgentCapabilities = agentcapability.New(builtinskills.Guide)
	DB = controllers.WithEventPublisher(database.DB, EventHub)
	KnowledgeControllerManager = controllers.NewKnowledgeController(DB)
	IMControllerManager = controllers.NewIMController(DB)
	WakerControllerManager = controllers.NewWakerController(DB)
	RolePresetControllerManager = controllers.NewRolePresetController(DB)
	ProfileDocumentControllerManager = controllers.NewProfileDocumentController(DB)
	TransactionControllerManager = controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(DB),
	)
	loadIntegrationAuthorization()
	ProjectSourceControllerManager = controllers.NewProjectSourceController(DB)
	ProjectControllerManager = controllers.NewProjectController(DB)
	WorkResultControllerManager = controllers.NewWorkResultController(DB)
	WorkBoardControllerManager = controllers.NewWorkBoardController(DB)
	NotificationControllerManager = controllers.NewNotificationController(DB)
	if err := WorkResultControllerManager.EnableTracking(ctx, time.Now().UnixMilli()); err != nil {
		return err
	}
	ChatControllerManager = controllers.NewChatController(DB)
	ChatDeletionIntentControllerManager = controllers.NewChatDeletionIntentController(DB)
	ChannelControllerManager = controllers.NewChannelController(DB)
	ChannelThreadControllerManager = controllers.NewChannelThreadController(DB)
	ChannelTaskControllerManager = controllers.NewChannelTaskController(DB)
	PluginControllerManager = controllers.NewPluginController(DB, func() {
		if PluginContents != nil {
			if err := PluginContents.Collect(); err != nil {
				logx.Warnf("collect plugin content after commit failed: %v", err)
			}
		}
	})
	PluginContents = plugins.NewContentStore(configuration.DataDirectory, func() ([]string, error) { return PluginControllerManager.ContentReferences(context.Background()) })
	providerPath := func(provider, version, binary string) string {
		if runtime.GOOS == "windows" {
			binary += ".exe"
		}
		return filepath.Join(configuration.PluginResourcesDirectory, "providers", provider, version, runtime.GOOS+"-"+runtime.GOARCH, binary)
	}
	changed := func(provider string) func(authorization.Session) {
		return func(authorization.Session) {
			if EventHub != nil {
				_ = events.Emit(EventHub, constants.PluginEventTopic, "plugins.changed", events.Resource{Kind: "global"}, map[string]string{"provider": provider})
			}
		}
	}
	PluginAuthorizations = map[string]*authorization.Authorizer{
		constants.PluginProviderFeishu: feishu.New(feishu.Config{Binary: providerPath(constants.PluginProviderFeishu, constants.FeishuCLIVersion, "lark-cli"), ProfileDirectory: filepath.Join(configuration.DataDirectory, "plugins", "providers", constants.PluginProviderFeishu), Changed: changed(constants.PluginProviderFeishu)}),
		constants.PluginProviderGitHub: github.New(github.Config{Binary: providerPath(constants.PluginProviderGitHub, constants.GitHubCLIVersion, "gh"), ProfileDirectory: filepath.Join(configuration.DataDirectory, "plugins", "providers", constants.PluginProviderGitHub), Changed: changed(constants.PluginProviderGitHub)}),
	}
	for _, auth := range PluginAuthorizations {
		auth.Open(ctx)
	}
	PluginBridgeEnvironment = os.Environ()
	PluginBridges = commandbridge.NewRegistry(ctx, func(id string) {
		if EventHub != nil {
			_ = events.Emit(EventHub, constants.PluginEventTopic, "plugins.changed", events.Resource{Kind: "plugin", ID: id}, nil)
		}
	})

	SkillControllerManager = controllers.NewSkillController(DB)
	MCPConnectorControllerManager = controllers.NewMCPConnectorController(DB)
	RuntimeEnvironmentControllerManager = controllers.NewRuntimeEnvironmentController(DB)
	AIAccountControllerManager = controllers.NewAIAccountController(DB)
	AutomaticTaskControllerManager = IntegrationService.CoordinateTasks(controllers.NewAutomaticTaskController(DB))
	AttentionNoticeControllerManager = controllers.NewAttentionNoticeController(DB)
	WorkflowControllerManager = controllers.NewWorkflowController(DB)
	WorkflowRunControllerManager = controllers.NewWorkflowRunController(DB)
	WorkflowCreationControllerManager = controllers.NewWorkflowCreationController(DB)
	AIAccountService = aiaccount.New(AIAccountControllerManager, configuration.DataDirectory)
	AIAccountService.SetEventPublisher(EventHub)
	AIAccountService.Start(ctx)

	rolePresetCatalog := builtinrolepreset.ControllerCatalog{
		Presets:   RolePresetControllerManager,
		Documents: ProfileDocumentControllerManager,
	}
	if err := builtinrolepreset.EnsureCatalog(ctx, rolePresetCatalog); err != nil {
		return mapCoreError("ensure_builtin_role_preset_catalog", status_error.CoreDatabaseInitializationFailed, err)
	}
	wakerCatalog := builtinwaker.ControllerCatalog{
		Wakers: WakerControllerManager,
	}
	if err := builtinwaker.EnsureDefaults(ctx, wakerCatalog); err != nil {
		return mapCoreError("ensure_builtin_wakers", status_error.CoreDatabaseInitializationFailed, err)
	}
	return nil
}

// loadBuiltinSkills 安装或刷新内置技能，并为系统默认伙伴补齐所属角色的技能关系。
func loadBuiltinSkills(ctx context.Context, configuration *Configuration) error {
	installer := builtinskills.Installer{
		DataDirectory: configuration.DataDirectory,
		Skills:        SkillControllerManager,
		Transactions:  TransactionControllerManager,
	}
	if err := installer.EnsureCatalog(ctx); err != nil {
		return err
	}
	for _, definition := range builtinrolepreset.DefinitionsWithDefaultWaker() {
		if err := builtinskills.EnableForWaker(
			ctx,
			SkillControllerManager,
			definition.DefaultWaker.ID,
			definition.DefaultSkills,
		); err != nil {
			return err
		}
	}
	return nil
}

// cleanupSkillArtifacts 清理技能导入残留和已无引用的版本目录。
func cleanupSkillArtifacts(ctx context.Context, configuration *Configuration) error {
	if err := managedskills.CleanupTemporaryArtifacts(configuration.DataDirectory); err != nil {
		logx.Errorf("cleanup stale skill temporary artifacts failed: error=%v", err)
	}
	versionPaths, err := SkillControllerManager.ListVersionContentPaths(ctx)
	if err != nil {
		return mapCoreError("list_skill_version_paths", status_error.CoreDatabaseInitializationFailed, err)
	}
	if err := managedskills.CleanupUnreferencedVersions(configuration.DataDirectory, versionPaths); err != nil {
		logx.Errorf("cleanup unreferenced skill versions failed: error=%v", err)
	}
	return nil
}

// loadMemory 打开独立记忆库并清理孤儿范围。
func loadMemory(ctx context.Context, configuration *Configuration) error {
	service, err := memory.NewService(memory.ServiceConfig{
		DBPath: filepath.Join(configuration.DataDirectory, "memory.db"),
	})
	if err != nil {
		return mapCoreError("initialize_memory_service", status_error.CoreMemoryInitializationFailed, err)
	}
	MemoryService = service
	if err := MemoryService.CleanupOrphanScopes(
		ctx,
		memory.NewScopeExistsResolver(WakerControllerManager, ProjectControllerManager),
	); err != nil {
		MemoryService.Close()
		MemoryService = nil
		return mapCoreError("cleanup_memory_orphan_scopes", status_error.CoreMemoryInitializationFailed, err)
	}

	return nil
}

// loadRuntime 装配 Runtime SDK 并启动设备级环境探测。
func loadRuntime(ctx context.Context) error {
	sdk, err := runtimes.New(runtimes.Config{})
	if err != nil {
		return mapCoreError("initialize_runtime_sdk", status_error.CoreRuntimeInitializationFailed, err)
	}
	RuntimeSDK = sdk
	RuntimeEnvironmentService, err = runtimeenvironment.New(runtimeenvironment.Config{
		Accounts:                  AIAccountService,
		AccountWorkspaceDirectory: filepath.Join(Config.DataDirectory, "account-workspaces"),
		RuntimeSDK:                RuntimeSDK,
		Environments:              RuntimeEnvironmentControllerManager,
		Transactions:              TransactionControllerManager,
		Events:                    EventHub,
	})
	if err != nil {
		return mapCoreError("initialize_runtime_environment_service", status_error.CoreRuntimeInitializationFailed, err)
	}
	if err := RuntimeEnvironmentService.Start(ctx); err != nil {
		return mapCoreError("start_runtime_environment_service", status_error.CoreRuntimeInitializationFailed, err)
	}
	return nil
}

// loadChat 先恢复 Chat 权威事实，再装配入口无关的 Execution Core。
func loadChat(ctx context.Context, configuration *Configuration, handlerFactory schedule.LocalHandlerFactory) error {
	chatModule, err := chat.New(chat.Config{
		StorageDirectory: configuration.ManagedFilesDirectory,
		Projects:         ProjectControllerManager,
		Results:          WorkResultControllerManager,
		QueryIndexes: chat.QueryIndexDependencies{
			Chats:           ChatControllerManager,
			RuntimeSessions: controllers.NewRuntimeSessionController(DB),
			Transactions:    TransactionControllerManager,
			DeletionIntents: ChatDeletionIntentControllerManager,
		},
	})
	if err != nil {
		return mapCoreError("initialize_chat_service", status_error.CoreChatInitializationFailed, err)
	}
	ChatService = chatModule
	if err := ChatService.Start(ctx); err != nil {
		return mapCoreError("start_chat_service", status_error.CoreChatInitializationFailed, err)
	}
	executionCore, err := execution.New(execution.Config{
		ValidateIntent: func(ctx context.Context, intent execution.WorkIntent) error {
			if KnowledgeService == nil {
				return nil
			}
			return KnowledgeService.ValidateWorkIntent(ctx, intent)
		},
		AcquireTeamChatInput: func(ctx context.Context, chatID string, userInput bool) (context.Context, func(), error) {
			if ChannelService == nil {
				return ctx, func() {}, nil
			}
			return ChannelService.AcquireChatInput(ctx, chatID, userInput)
		},
		Agent: AgentCapabilities, Events: EventHub,
		AgentEndpoint: "http://127.0.0.1:" + strconv.Itoa(configuration.Server.Port) + constants.AgentCLIPath,
		AgentBinary:   agentBinaryPath(),
		AgentBindings: func(chat models.Chat, executionID string, roots []string, automaticRunID string) []agentcapability.Binding {
			bindings := MemoryService.AgentBindings(memory.Context{RunID: executionID, ChatID: chat.ID, PartnerID: chat.WakerID, ProjectID: chat.ProjectID})
			if KnowledgeService != nil {
				knowledgeChat := chat
				knowledgeChat.ActiveExecutionID = executionID
				if automaticRunID != "" {
					bindings = append(bindings, KnowledgeService.AutomaticBindings(context.Background(), knowledgeChat, automaticRunID)...)
				} else {
					bindings = append(bindings, KnowledgeService.AgentBindings(context.Background(), knowledgeChat)...)
				}
			}
			if IMService != nil {
				bindings = append(bindings, IMService.AgentBindings(chat, executionID, roots, EventHub)...)
			}
			return bindings
		},
		Accounts:            AIAccountService,
		Runtime:             RuntimeSDK,
		Journal:             chatModule,
		DormantChats:        chatModule,
		Wakers:              WakerControllerManager,
		Profiles:            ProfileDocumentControllerManager,
		Projects:            ProjectControllerManager,
		Skills:              SkillControllerManager,
		PluginInstallations: plugininstallation.New(plugininstallation.Config{Contents: PluginContents, DataDirectory: configuration.DataDirectory, Authorizers: PluginAuthorizations, Bridges: PluginBridges, BridgeEnvironment: PluginBridgeEnvironment}),
		PluginContents:      PluginContents, Plugins: PluginControllerManager, PluginAuthorizers: PluginAuthorizations, PluginLauncher: coreExecutablePath(),
		Connectors:          MCPConnectorControllerManager,
		RuntimeEnvironments: RuntimeEnvironmentControllerManager,
		SkillDataDirectory:  configuration.DataDirectory,
		StorageDirectory:    configuration.ManagedFilesDirectory,
	})
	if err != nil {
		return mapCoreError("initialize_execution_service", status_error.CoreChatInitializationFailed, err)
	}
	ExecutionService = executionCore
	libraryEmbeddings := knowledgebridge.LibraryEmbeddings{Engine: KnowledgeEngine, References: KnowledgeControllerManager}
	KnowledgeService = knowledgeservice.New(knowledgeservice.Config{AutomaticRun: AutomaticTaskControllerManager.GetRun, InvocationGuard: IntegrationService.AcquireInvocationGuard, IntegrationGuard: IntegrationService.RestoreKnowledgeGuard, IntegrationPublicationGuard: IntegrationService.RestoreKnowledgeGuards, ResolveModel: resolveKnowledgeModel, Generator: newKnowledgeModelGenerator, FreezeEmbedder: libraryEmbeddings.Freeze, Connections: KnowledgeConnectionControllerManager, Engine: KnowledgeEngine, References: KnowledgeControllerManager, Projects: ProjectControllerManager, Chats: ChatService, Events: EventHub})
	if err := KnowledgeService.Start(ctx); err != nil {
		return err
	}
	if err := ExecutionService.Open(ctx); err != nil {
		return mapCoreError("open_execution_service", status_error.CoreChatInitializationFailed, err)
	}
	channelModule, err := channelservice.New(channelservice.Config{
		AutomaticRun:        AutomaticTaskControllerManager.GetRun,
		Commands:            controllers.NewAgentCommandController(DB),
		PrepareDirectResult: ChatService.PrepareParticipantResult,
		Results:             WorkResultControllerManager,
		BeforeUserInput: func(ctx context.Context, threadID string) error {
			if ScheduleService == nil {
				return nil
			}
			return ScheduleService.FreezeTeamRun(ctx, threadID)
		},
		Channels:     ChannelControllerManager,
		Threads:      ChannelThreadControllerManager,
		Tasks:        ChannelTaskControllerManager,
		Transactions: TransactionControllerManager,
		Wakers:       WakerControllerManager,
		Chats:        ChatService,
		Executions:   ExecutionService,
		ChatGroups:   executionCore,
	})
	if err != nil {
		return mapCoreError(
			"initialize_channel_service",
			status_error.CoreChannelInitializationFailed,
			err,
		)
	}
	ChannelService = channelModule
	if err := ChannelService.Start(ctx); err != nil {
		return mapCoreError("start_channel_service", status_error.CoreChannelInitializationFailed, err)
	}
	IMService = imservice.New(imservice.Config{Events: EventHub, Store: IMControllerManager, Transactions: TransactionControllerManager, Executions: ExecutionService, Chats: ChatService, Teams: ChannelService, Channels: ChannelControllerManager, Threads: ChannelThreadControllerManager, TeamTasks: ChannelTaskControllerManager, DataDirectory: configuration.ManagedFilesDirectory})
	WorkflowState = workflowstate.New(WorkflowControllerManager, WorkflowRunControllerManager, TransactionControllerManager)
	WorkflowService = workflow.New(workflow.Config{
		Commands: controllers.NewAgentCommandController(DB), Events: EventHub,
		State:            WorkflowState,
		Resources:        workflowResources,
		IMCall:           IMService.WorkflowCall,
		IMValidate:       IMService.ValidateWorkflowTarget,
		Store:            WorkflowControllerManager,
		Runs:             WorkflowRunControllerManager,
		Transactions:     TransactionControllerManager,
		Worker:           workflow.CoreWorkerAdapter{Executions: ExecutionService, Chats: chatModule},
		CreationSessions: WorkflowCreationControllerManager,
		Consumers:        controllers.NewWorkflowConsumerController(DB),
		Executions:       ExecutionService,
		Chats:            chatModule,
		Wakers:           WakerControllerManager,
		PythonExecutable: configuration.WorkflowPythonExecutable,
		Connectors:       MCPConnectorControllerManager,
		DataDirectory:    configuration.ManagedFilesDirectory,
		Projects:         ProjectControllerManager,
	})
	scheduleConfig := schedule.Config{AuthorizationWrite: IntegrationService.WithAuthorizationWrite,
		BindKnowledge: KnowledgeService.BindIntent,
		Workflows:     WorkflowService,
		WorkflowRuns:  WorkflowRunControllerManager,
		APIHandler:    handlerFactory,
		Tasks:         AutomaticTaskControllerManager, Transactions: TransactionControllerManager,
		Wakers: WakerControllerManager, Projects: ProjectControllerManager,
		Executions: ExecutionService, Chats: chatModule, DataDirectory: configuration.DataDirectory,
		ManagedFilesDirectory: configuration.ManagedFilesDirectory,
		Teams:                 ChannelService, Channels: ChannelControllerManager, Threads: ChannelThreadControllerManager, TeamTasks: ChannelTaskControllerManager,
	}
	IntegrationAPIListener = integrationapi.NewListener(configuration.DataDirectory, func(address string) http.Handler {
		return handlerFactory(ScheduleService, scheduleConfig, address)
	})
	scheduleConfig.APIListener = IntegrationAPIListener
	ScheduleService = schedule.New(scheduleConfig)
	if err := WorkflowService.Start(ctx); err != nil {
		return err
	}
	if err := ScheduleService.Start(ctx); err != nil {
		return mapCoreError("start_schedule_service", status_error.AutomaticTaskServiceInvalid, err)
	}
	if err := IMService.Start(ctx); err != nil {
		return err
	}
	return nil
}

// Close 按依赖逆序停止后台流程并关闭数据库连接。
func Close() error {
	closeIntegrationAPI()
	var result error
	KnowledgeConnectionControllerManager = nil
	KnowledgeRemoteMountControllerManager = nil
	if KnowledgeService != nil {
		KnowledgeService.Close()
		KnowledgeService = nil
	}
	if closePluginCatalog != nil {
		closePluginCatalog()
		closePluginCatalog = nil
	}
	if AgentCapabilities != nil {
		AgentCapabilities.Close()
		AgentCapabilities = nil
	}
	if NotificationService != nil {
		NotificationService.Close()
		NotificationService = nil
	}
	if EventHub != nil {
		EventHub.Close()
		EventHub = nil
	}
	if IMService != nil {
		IMService.Close()
		IMService = nil
	}
	if ScheduleService != nil {
		ScheduleService.Close()
		ScheduleService = nil
	}
	if WorkflowService != nil {
		WorkflowService.Close()
		WorkflowService = nil
		WorkflowState = nil
	}
	if ChannelService != nil {
		ChannelService.Close()
		ChannelService = nil
	}
	if ExecutionService != nil {
		ExecutionService.Close()
		ExecutionService = nil
	}
	for _, auth := range PluginAuthorizations {
		auth.Close()
	}
	PluginAuthorizations = nil
	if PluginBridges != nil {
		PluginBridges.Close()
		PluginBridges = nil
	}
	if ChatService != nil {
		ChatService.Close()
		ChatService = nil
	}
	if RuntimeEnvironmentService != nil {
		RuntimeEnvironmentService.Close()
		RuntimeEnvironmentService = nil
	}
	if AIAccountService != nil {
		AIAccountService.Close()
		AIAccountService = nil
	}
	if RuntimeSDK != nil {
		closeContext, cancelClose := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		if err := RuntimeSDK.Close(closeContext); err != nil {
			result = mapCoreError("close_runtime_sdk", status_error.CoreShutdownFailed, err)
		}
		cancelClose()
		RuntimeSDK = nil
	}
	if KnowledgeEngine != nil {
		if err := KnowledgeEngine.Close(); err != nil && result == nil {
			result = knowledgebridge.MapError(err)
		}
		KnowledgeEngine = nil
	}
	if MemoryService != nil {
		MemoryService.Close()
		MemoryService = nil
	}
	if DB == nil {
		return result
	}
	sqlDB, err := DB.DB()
	if err != nil {
		mapped := mapCoreError("resolve_database_connection", status_error.CoreShutdownFailed, err)
		if result == nil {
			result = mapped
		}
		DB = nil
		return result
	}
	if err := sqlDB.Close(); err != nil {
		mapped := mapCoreError("close_database_connection", status_error.CoreShutdownFailed, err)
		if result == nil {
			result = mapped
		}
	}
	DB = nil
	return result
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", mapCoreError("generate_local_token", status_error.CoreTokenGenerationFailed, err)
	}
	return hex.EncodeToString(bytes), nil
}

// mapCoreError 保留下层稳定状态错误，并在首次转换外部错误时记录技术上下文。
func mapCoreError(operation string, fallback status_error.StatusError, err error) error {
	var stable interface {
		error
		Code() int64
	}
	if errors.As(err, &stable) {
		return err
	}
	logx.Errorf("core operation failed: operation=%s error=%v", operation, err)
	return fallback
}

// loadNotifications 在原领域恢复后启用提交监听及初始权威摘要。
func loadNotifications(ctx context.Context) error {
	NotificationService = notification.New(NotificationControllerManager, WorkResultControllerManager, TransactionControllerManager, EventHub)
	return NotificationService.Start(ctx)
}

// agentBinaryPath 只接受应用装配路径，不接受模型命令行提供的服务地址或令牌。
func agentBinaryPath() string {
	if configured := os.Getenv(constants.AgentCLIBinaryEnvironment); configured != "" {
		return configured
	}
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	filename := "allycli"
	if runtime.GOOS == "windows" {
		filename += ".exe"
	}
	return filepath.Join(filepath.Dir(executable), filename)
}

// coreExecutablePath 只使用当前受管 Core 的真实可执行文件。
func coreExecutablePath() string {
	value, err := os.Executable()
	if err != nil {
		return ""
	}
	return value
}

// PluginBridges 管理声明驱动的唯一插件身份。
var PluginBridges *commandbridge.Registry

// PluginBridgeEnvironment 只包含固定受信程序资产。
var PluginBridgeEnvironment []string

// PluginContents 保存当前 Core 的短期内容使用计数。
var PluginContents *plugins.ContentStore

// loadKnowledge 打开独立知识数据库，不启动模型或第二套执行宿主。
func loadKnowledge(ctx context.Context, configuration *Configuration) error {
	KnowledgeConnectionControllerManager = controllers.NewKnowledgeConnectionController(DB)
	KnowledgeRemoteMountControllerManager = controllers.NewKnowledgeRemoteMountController(DB)
	if KnowledgeControllerManager != nil {
		if err := KnowledgeControllerManager.RecoverRemoteSending(ctx); err != nil {
			return err
		}
	}
	var err error
	KnowledgeEngine, err = knowledgebridge.Open(ctx, configuration.DataDirectory, configuration.ManagedFilesDirectory)
	return err
}

// closePluginCatalog 取消并等待启动时的目录更新，先于事件通道关闭。
var closePluginCatalog func()

// PluginCatalogSource 返回HTTP分发入口；显式本地目录用于镜像与测试。
func PluginCatalogSource() string {
	if value := strings.TrimSpace(os.Getenv("ALLY_PLUGIN_CATALOG_URL")); value != "" {
		return value
	}
	return constants.PluginCatalogHTTPSource
}
