package chat

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Service 集中声明 Chat 对外提供的全部业务能力。
//
// 调用方只依赖这些稳定用例和公开结果，不需要理解 JSONL、SQLite 查询索引、
// Runtime 重试、Session 恢复或事件适配实现。
type Service interface {
	// ReadExecutionOutcome 读取指定执行最后一次持久调用的终止和失败事实。
	ReadExecutionOutcome(context.Context, string, string) (ExecutionOutcome, error)
	// ReadExecution 按稳定身份读取任意已接纳回合的权威事实。
	ReadExecution(context.Context, string, string) (models.Execution, error)
	// ForkTargets 返回普通会话各个结束回合的真实分叉可用性。
	ForkTargets(ctx context.Context, chatID string) ([]ForkTarget, error)
	// ForkChat 事务化复制继承历史，并在原生分叉完成后公开子会话。
	ForkChat(ctx context.Context, input ForkInput, fork NativeFork) (string, error)
	// Start 完成权威日志恢复并开始接受新的 Chat 执行。
	Start(ctx context.Context) error
	// Close 取消全部执行并等待内部资源退出。
	Close()
	// RecoveryHealth 返回最近一次启动恢复的健康快照。
	RecoveryHealth() RecoveryHealth
	// CreateDormantChats 批量建立只有 chat.created 首事件的独立 Chat。
	CreateDormantChats(ctx context.Context, chats []DormantChat) ([]models.Chat, error)
	// AppendParticipantMessages 把外部参与者消息幂等写入已有 Chat，不启动 Runtime。
	AppendParticipantMessages(ctx context.Context, chatID string, messages []ParticipantMessageInput) error

	// GetChatSummary 返回打开 Chat 所需的有界摘要。
	GetChatSummary(ctx context.Context, chatID string) (ChatSummary, error)
	// ListChats 返回一位伙伴的最近 Chat。
	ListChats(ctx context.Context, input ListChatsInput) (ChatList, error)
	// CountMessagesByWakerGroupedByDay 从每 Chat 索引统计伙伴消息活动。
	CountMessagesByWakerGroupedByDay(ctx context.Context, wakerID string, startMs int64, endMs int64, loc *time.Location) (map[string]int64, error)
	// CountUserMessages 从权威日志索引统计真实用户输入，排除成员报告和系统协调消息。
	CountUserMessages(ctx context.Context, chatID string) (int64, error)
	// ReadMessageHistory 返回指定创建游标之前的消息窗口。
	ReadMessageHistory(ctx context.Context, chatID string, beforeSequence int64, limit int) (MessageHistoryPage, error)
	// GetMessageFileChange 从权威 JSONL 返回指定消息和文件的执行级 Diff。
	GetMessageFileChange(ctx context.Context, chatID string, messageID string, path string) (messageview.MessageFileChangeDiff, error)
	// ListEvents 返回持久化游标之后的安全公开事件。
	ListEvents(ctx context.Context, chatID string, after int64, limit int) ([]PublicEvent, error)
	// SubscribeEvents 衔接持久化历史与后续实时公开事件。
	SubscribeEvents(ctx context.Context, chatID string, after int64) (Subscription, error)

	// RenameChat 记录新的 Chat 标题。
	RenameChat(ctx context.Context, chatID string, title string) error
	// DeleteChat 删除权威日志、查询索引和归属可验证的受管目录。
	DeleteChat(ctx context.Context, chatID string) error
	// DeleteChats 在统一预检全部目标后批量删除权威日志、查询索引和受管目录。
	DeleteChats(ctx context.Context, chatIDs []string) error
	// DeleteChatsAtomically 让上层聚合事实与 Chat 索引在同一事务提交后再最终清理 staging。
	DeleteChatsAtomically(ctx context.Context, chatIDs []string, finalize func(context.Context) error) error
	// DeleteProvisionalTeam 回滚未 ready 的 Team 聚合和可能只完成一部分的成员 Chat。
	DeleteProvisionalTeam(ctx context.Context, threadID string, provisioningID string, chatIDs []string) error
	// DeleteWakerChats 从权威日志枚举并删除一位伙伴的全部独立 Chat，保留团队历史快照。
	DeleteWakerChats(ctx context.Context, wakerID string) error
}

// Module 是组合根同时交给查询入口和 Execution Core 的完整 Chat 模块。
type Module interface {
	// PrepareParticipantResult 在事务外定位正式直达的真实成员消息。
	PrepareParticipantResult(context.Context, string, string) (*models.WorkResultReceipt, error)
	// ReadInitialExecution 返回供入口查询的首次执行及当前交互事实，不投影消息正文。
	ReadInitialExecution(context.Context, string) (InitialExecutionState, error)
	// Service 提供 Chat 查询、恢复与元数据用例。
	Service
	// Journal 只交给 Execution Core 读写权威事实。
	Journal
}

// service 是 Service 的唯一实现，维护权威日志顺序、查询投影与事件订阅。
type service struct {
	// storage 统一确定普通 Chat 与团队成员 Chat 的日志和工作区地址。
	storage *chatstorage.Layout
	// deletions 管理整聚合 staging、数据库提交后的清理和崩溃恢复。
	deletions *chatstorage.DeletionCoordinator
	// deletionIntents 持久化 prepared/committed 删除提交事实。
	deletionIntents controllers.ChatDeletionIntentController
	// logStore 持久化每个对话的权威追加日志。
	logStore *eventlog.Store
	// eventCommitter 固化 JSONL 提交后的发布和消息位置索引顺序。
	eventCommitter *eventCommitter
	// projectController 解析首次发送选择的项目和冻结目录范围。
	projectController controllers.ProjectController
	// queryIndexes 集中封装全部可从 JSONL 重建的 SQLite 查询索引。
	queryIndexes *queryIndexStore
	// subscriptions 维护持久化事件和 transient 快照的进程内订阅者。
	subscriptions *subscriptionHub
	// recovery 负责启动时以 JSONL 为权威校准全部 SQLite 查询索引。
	recovery *recoveryCoordinator
}

var _ Service = (*service)(nil)
var _ Module = (*service)(nil)

// Config 声明构造 Chat Service 所需的稳定外部依赖。
type Config struct {
	// Results 保存用户阅读事实与可重放交付引用，不能在索引修复中删除。
	Results controllers.WorkResultController
	// StorageDirectory 是 Chat 与 Team 聚合目录共同使用的应用数据根目录。
	StorageDirectory string
	// Projects 读取 Chat 列表最近使用项目的查询投影。
	Projects controllers.ProjectController
	// QueryIndexes 提供全部可从 JSONL 重建的 SQLite 查询索引。
	QueryIndexes QueryIndexDependencies
}

// QueryIndexDependencies 汇集具有“可落后、可重建”语义的 Repository Controller。
type QueryIndexDependencies struct {
	// Chats 持久化 Chat 主查询索引。
	Chats controllers.ChatController
	// RuntimeSessions 持久化 Runtime 原生会话续接查询索引。
	RuntimeSessions controllers.RuntimeSessionController
	// Transactions 提供跨查询索引替换的数据库事务边界。
	Transactions controllers.TransactionController
	// DeletionIntents 持久化文件 staging 与数据库事务之间的提交标记。
	DeletionIntents controllers.ChatDeletionIntentController
}

// New 校验显式配置并创建尚未绑定进程生命周期的 Chat Service。
func New(config Config) (Module, error) {
	if config.Projects == nil ||
		config.QueryIndexes.Chats == nil ||
		config.QueryIndexes.RuntimeSessions == nil ||
		config.QueryIndexes.Transactions == nil ||
		config.QueryIndexes.DeletionIntents == nil {
		return nil, status_error.BadRequest
	}
	return newService(config)
}

// newService 创建具体实现，供包内契约测试检查内部不变量。
func newService(config Config) (*service, error) {
	storageDirectory := strings.TrimSpace(config.StorageDirectory)
	if storageDirectory == "" {
		return nil, status_error.ChatLogError
	}
	storage, err := chatstorage.NewLayout(storageDirectory)
	if err != nil {
		return nil, err
	}
	logStore, err := eventlog.NewStore(storageDirectory)
	if err != nil {
		return nil, err
	}
	deletions, err := chatstorage.NewDeletionCoordinator(storage)
	if err != nil {
		return nil, err
	}
	// 订阅中心和查询索引只在 Service 内部共享，对调用方保持单一 Interface。
	subscriptions := newSubscriptionHub()
	queryIndexes := newQueryIndexStore(
		config.QueryIndexes.Chats,
		config.QueryIndexes.RuntimeSessions,
		config.QueryIndexes.Transactions,
	)
	queryIndexes.results = config.Results
	queryIndexes.logStore = logStore
	implementation := &service{
		storage:           storage,
		deletions:         deletions,
		deletionIntents:   config.QueryIndexes.DeletionIntents,
		logStore:          logStore,
		projectController: config.Projects,
		queryIndexes:      queryIndexes,
		eventCommitter: newEventCommitter(
			logStore,
			subscriptions,
		),
		subscriptions: subscriptions,
	}
	implementation.recovery = newRecoveryCoordinator(logStore, queryIndexes)
	return implementation, nil
}

// Start 先完成权威日志恢复，再允许 Execution Core 接纳新工作。
func (m *service) Start(ctx context.Context) error {
	if err := m.deletions.Resume(func(intentID string, chatIDs []string) (bool, error) {
		intent, exists, err := m.deletionIntents.Get(ctx, intentID)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, nil
		}
		if !slices.Equal(intent.ChatIDs, chatIDs) {
			return false, status_error.ChatLogCorrupted
		}
		return intent.State == "committed", nil
	}, func(intentID string) error {
		return m.deletionIntents.Delete(ctx, intentID)
	}); err != nil {
		return err
	}
	orphanedIntents, err := m.deletionIntents.List(ctx)
	if err != nil {
		return err
	}
	for _, intent := range orphanedIntents {
		if err := m.deletionIntents.Delete(ctx, intent.ID); err != nil {
			return err
		}
	}
	if err := m.logStore.RefreshCatalog(); err != nil {
		return err
	}
	if err := m.recoverForks(ctx); err != nil {
		return err
	}
	if err := m.logStore.RefreshCatalog(); err != nil {
		return err
	}
	return m.recovery.reconcileAll(ctx)
}

// Close 关闭全部进程内事件订阅。
func (m *service) Close() {
	m.subscriptions.closeAll()
}

func marshalData(value interface{}) json.RawMessage {
	data, err := eventlog.MarshalData(value)
	if err != nil {
		panic(err)
	}
	return data
}
