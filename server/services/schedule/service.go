package schedule

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	integrationapi "github.com/JieWaZi/NeuxBot/server/internal/integration"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/schedulecalc"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	channelservice "github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

// Executions 是自动入口共用的标准执行和保存前校验端口。
type Executions interface {
	// Start 以可靠接纳模式启动一个标准 Chat。
	Start(context.Context, execution.WorkIntent) (execution.AcceptedExecution, error)
	// ValidateConfiguration 校验运行时和权限组合。
	ValidateConfiguration(context.Context, execution.ExecutionConfiguration) error
}

// Chats 只读取首次执行权威状态及原子删除直接关联的 Chat。
type Chats interface {
	// GetChatSummary 读取 Worker 实际工具授权和输入等待状态。
	GetChatSummary(context.Context, string) (chat.ChatSummary, error)
	// ReadInitialExecution 读取首次执行及当前审批和输入事实，不投影消息正文。
	ReadInitialExecution(context.Context, string) (chat.InitialExecutionState, error)
	// DeleteChatsAtomically 在同一事务中提交 Chat 及任务聚合的删除。
	DeleteChatsAtomically(context.Context, []string, func(context.Context) error) error
}

// Teams 只负责把冻结机会交给既有团队首发用例。
type Teams interface {
	// SubmitMessage 以稳定首次发送身份建立团队会话。
	SubmitMessage(context.Context, channelservice.SubmitMessageInput) (channelservice.SubmitMessageResult, error)
}

// Workflows 复用工作流接纳和权威状态，不在调度服务内运行流程。
type Workflows interface {
	// UploadFile 保存工作流目标的任务级上传原件。
	UploadFile(context.Context, string, string, string, io.Reader) (workflowfiles.Upload, error)
	// OutputFiles 读取固定版本声明的真实最终文件。
	OutputFiles(context.Context, models.WorkflowRun) ([]workflow.OutputFile, error)
	// OpenOutputFile 打开本次运行已声明的文件产物。
	OpenOutputFile(context.Context, string, string, string) (*os.File, string, error)
	// ValidateTargetConfiguration 校验目标与执行资源，允许本次输入尚未提供。
	ValidateTargetConfiguration(context.Context, string, string, models.WorkflowExecutionDefaults) (models.WorkflowDefinition, models.WorkflowRevision, error)
	// ReadTarget 读取说明所需的工作流与不可变输入契约。
	ReadTarget(context.Context, string, string) (models.WorkflowDefinition, models.WorkflowRevision, error)
	// StartRun 以机会的稳定身份接纳工作流。
	StartRun(context.Context, workflow.StartRunCommand) (models.WorkflowRun, error)
}

// Config 提供自动任务需要的稳定依赖，时钟可用于确定性验收。
type Config struct {
	// AuthorizationWrite 将任务授权变更与知识提交置于统一协调锁内，回调只含数据库写入。
	AuthorizationWrite func(context.Context, func(context.Context) error) error
	// BindKnowledge 为已有自动任务冻结明确的知识维护授权，不另设调度器。
	BindKnowledge func(context.Context, execution.WorkIntent, models.KnowledgeMaintenanceAuthorization) (execution.WorkIntent, error)
	// WorkflowRuns 读取工作流真实运行、操作许可和节点状态。
	WorkflowRuns controllers.WorkflowRunController
	// APIHandler 由 API 层提供本机 HTTP 适配器，服务只拥有监听生命周期。
	APIHandler LocalHandlerFactory
	// APIListener 注入发布服务的唯一监听所有者，省略时保留独立测试和旧宿主行为。
	APIListener *integrationapi.Listener
	// Workflows 接纳冻结版本的工作流并读取真实状态。
	Workflows Workflows
	// Teams 复用团队首发、成员快照和任务编排。
	Teams Teams
	// Channels 读取团队负责人和成员默认执行配置。
	Channels controllers.ChannelController
	// Threads 查询团队首发幂等身份、成员会话和活跃工作。
	Threads controllers.ChannelThreadController
	// TeamTasks 读取团队运行仍待处理或失败的成员任务。
	TeamTasks controllers.ChannelTaskController
	// Tasks 持久保存配置、运行槽和唯一机会。
	Tasks controllers.AutomaticTaskController
	// Transactions 提供机会领取及聚合删除的事务边界。
	Transactions controllers.TransactionController
	// Wakers 验证任务归属仍存在。
	Wakers controllers.WakerController
	// Projects 验证伙伴项目及已授权真实目录。
	Projects controllers.ProjectController
	// Executions 接纳标准执行并验证执行配置。
	Executions Executions
	// Chats 读取权威运行状态及执行聚合删除。
	Chats Chats
	// DataDirectory 保存任务 API 监听偏好。
	DataDirectory string
	// ManagedFilesDirectory 保存工作流上传文件，独立于任务 API 监听配置。
	ManagedFilesDirectory string
	// Now 返回当前时间，未提供时使用系统时钟。
	Now func() time.Time
}

// Service 在 Core 生命周期内协调任务接纳、计划机会与独立本机监听器。
type Service struct {
	// config 保存构造时固定的依赖。
	config Config
	// gate 串行保护配置、接纳及删除边界，不覆盖 Runtime 的实际执行。
	gate sync.Mutex
	// life 保护后台上下文、监听器及关闭等待的登记。
	life sync.Mutex
	// ctx 是显式 Start 后生效的 Core 子上下文。
	ctx context.Context
	// cancel 取消后台扫描和所有正在接纳的请求。
	cancel context.CancelFunc
	// workers 等待扫描和本机监听器完全退出。
	workers sync.WaitGroup
	// suspended 表示桌面已报告休眠，期间不接纳定时机会。
	suspended bool
	// lastTick 是前一次连续运行的扫描时间，用于检测时钟跳变。
	lastTick time.Time
	// apiOwner 是状态与地址配置的唯一发布服务。
	apiOwner *integrationapi.Listener
	// ownsAPI 区分独立宿主和由 Core 全局管理的共享监听。
	ownsAPI bool
	// rates 保存每个任务最近一分钟已接纳 API 请求的时间。
	rates map[string][]time.Time
	// sealedWakers 封闭伙伴聚合删除期间的新任务及新接纳。
	sealedWakers map[string]bool
}

// New 构造尚未启动的自动任务服务。
func New(config Config) *Service {
	if config.Now == nil {
		config.Now = time.Now
	}
	s := &Service{config: config, rates: map[string][]time.Time{}, sealedWakers: map[string]bool{}, apiOwner: config.APIListener}
	if s.apiOwner == nil {
		s.ownsAPI = true
		s.apiOwner = integrationapi.NewListener(config.DataDirectory, func(address string) http.Handler { return config.APIHandler(s, config, address) })
	}
	return s
}

// APIStatus 继续使用原任务状态合同，实际状态归集成监听所有者。
type APIStatus = integrationapi.ListenerStatus

// LocalHandlerFactory 把 API 层的真实 HTTP 入口注入后台监听器。
type LocalHandlerFactory func(*Service, Config, string) http.Handler

// mutationScope 只在一次同步调用范围内证明当前服务的协调锁归属。
type mutationScope struct {
	// owner 指向本次持锁的调度组件，禁止跨服务复用保护。
	owner *Service
	// active 在释放前为真，释放后的 Context 不能再次绕过加锁。
	active atomic.Bool
}

type mutationScopeKey struct{}

// AcquireMutation 在完整 API 用例期间封闭配置、接纳和删除的竞争。
// 返回的 Context 只供本次同步调用及其事务使用，release 可以重复调用。
func (s *Service) AcquireMutation(ctx context.Context) (context.Context, func()) {
	if scope, ok := ctx.Value(mutationScopeKey{}).(*mutationScope); ok && scope.owner == s && scope.active.Load() {
		return ctx, func() {}
	}
	s.gate.Lock()
	scope := &mutationScope{owner: s}
	scope.active.Store(true)
	return context.WithValue(ctx, mutationScopeKey{}, scope), func() {
		if scope.active.CompareAndSwap(true, false) {
			s.gate.Unlock()
		}
	}
}

// ForgetTask 释放任务聚合删除后已无归属的进程限流记录。
func (s *Service) ForgetTask(ctx context.Context, taskID string) {
	_, release := s.AcquireMutation(ctx)
	defer release()
	delete(s.rates, taskID)
}

// Start 先对账未完成接纳并聚合错过计划，再显式启动扫描和本机监听器。
func (s *Service) Start(ctx context.Context) error {
	if s.config.APIHandler == nil {
		return status_error.AutomaticTaskServiceInvalid
	}
	s.life.Lock()
	if s.ctx != nil {
		s.life.Unlock()
		return status_error.AutomaticTaskServiceInvalid
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.life.Unlock()
	s.gate.Lock()
	err := s.recover(s.ctx)
	if err == nil {
		err = s.advanceMissed(s.ctx, s.config.Now())
	}
	s.lastTick = s.config.Now()
	s.gate.Unlock()
	if err != nil {
		s.cancel()
		return err
	}
	if s.ownsAPI {
		if err := s.apiOwner.Start(s.ctx); err != nil {
			return err
		}
	}
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				if err := s.Tick(s.ctx); err != nil && s.ctx.Err() == nil {
					logx.Errorf("scan automatic task schedules failed: error_code=%s", util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key()))
				}
			}
		}
	}()
	return nil
}

// Close 先取消接纳和扫描，再关闭监听器并等待后台工作退出。
func (s *Service) Close() {
	s.life.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.life.Unlock()
	if s.ownsAPI {
		s.apiOwner.Close()
	}
	s.workers.Wait()
}

func (s *Service) recover(ctx context.Context) error {
	tasks, _, err := s.config.Tasks.List(ctx, controllers.AutomaticTaskFilter{})
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task.Deleting {
			if err := s.config.Tasks.SetDeleting(ctx, task.ID, false); err != nil {
				return err
			}
		}
		if task.ActiveRunID == "" {
			continue
		}
		run, err := s.config.Tasks.GetRun(ctx, task.ActiveRunID)
		if err != nil {
			return err
		}
		if err := s.ReconcileRun(ctx, &run); err != nil {
			return err
		}
		if run.Admission == constants.AutomaticRunAdmissionPending {
			if run.Source == constants.AutomaticRunSourceSchedule {
				if err := s.interrupt(ctx, &run); err != nil {
					return err
				}
			} else if err := s.config.Tasks.ReleaseRun(ctx, task.ID, run.ID); err != nil {
				return err
			}
		} else if err := s.refreshSlot(ctx, task); err != nil {
			return err
		}
	}
	return nil
}

// Tick 处理连续运行期间的到期机会；时钟或调度大幅跳变时只推进错过游标。
func (s *Service) Tick(ctx context.Context) error {
	ctx, release := s.AcquireMutation(ctx)
	defer release()
	if s.suspended || ctx.Err() != nil {
		return nil
	}
	now := s.config.Now()
	gap := now.Sub(s.lastTick)
	wallGap := time.Duration(now.UnixMilli()-s.lastTick.UnixMilli()) * time.Millisecond
	if !s.lastTick.IsZero() && (gap > time.Minute || wallGap-gap > 2*time.Second || wallGap < 0) {
		s.lastTick = now
		return s.advanceMissed(ctx, now)
	}
	s.lastTick = now
	tasks, err := s.config.Tasks.Due(ctx, now.UnixMilli())
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if s.sealedWakers[task.WakerID] {
			continue
		}
		if now.UnixMilli()-*task.NextRunAt > constants.AutomaticTaskScheduleGraceSeconds*1000 {
			if err := s.missTask(ctx, task, now); err != nil {
				return err
			}
			continue
		}
		rule, err := schedulecalc.Compile(task.Schedule)
		if err != nil {
			return err
		}
		planned := *task.NextRunAt
		next := timePointer(rule.Next(now))
		key := fmt.Sprintf("schedule:%d:%d", task.TriggerVersion, planned)
		_, err = s.Admit(ctx, AdmissionCommand{Task: task, Source: constants.AutomaticRunSourceSchedule, OccurrenceKey: key, RequestID: fmt.Sprint(planned), ScheduledAt: &planned, NextRunAt: next})
		if errors.Is(err, status_error.AutomaticTaskBusy) {
			if err := s.skipBusy(ctx, task.ID, planned, next, key); err != nil {
				return err
			}
		} else if err != nil {
			// 已登记的接纳失败推进了游标，后续计划继续运行，不重试这一机会。
			logx.Errorf("admit scheduled automatic task failed: task_id=%s error_code=%s", task.ID, util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key()))
		}
	}
	return nil
}

func (s *Service) skipBusy(ctx context.Context, taskID string, planned int64, next *int64, key string) error {
	task, err := s.config.Tasks.Get(ctx, taskID)
	if err != nil {
		return err
	}
	task.NextRunAt = next
	now := s.config.Now().UnixMilli()
	return s.config.Transactions.Transaction(ctx, func(tx context.Context) error {
		advanced, err := s.config.Tasks.AdvanceSchedule(tx, task, task.Revision)
		if err != nil {
			return err
		}
		if !advanced {
			return status_error.AutomaticTaskRevisionConflict
		}
		return s.config.Tasks.CreateRun(tx, models.AutomaticRun{ID: util.New(), TaskID: task.ID, WakerID: task.WakerID, OccurrenceKey: key, Source: constants.AutomaticRunSourceSchedule,
			Configuration: task.AutomaticTaskConfiguration, ConfigurationRevision: task.ConfigurationVersion, Admission: constants.AutomaticRunAdmissionSkipped,
			ErrorCode: status_error.AutomaticTaskBusy.Key(), ScheduledAt: &planned, CreatedAt: now, UpdatedAt: now})
	})
}

func (s *Service) advanceMissed(ctx context.Context, now time.Time) error {
	tasks, err := s.config.Tasks.Due(ctx, now.UnixMilli())
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if err := s.missTask(ctx, task, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) missTask(ctx context.Context, task models.AutomaticTask, now time.Time) error {
	rule, err := schedulecalc.Compile(task.Schedule)
	if err != nil {
		return err
	}
	count, last, next := rule.Missed(time.UnixMilli(*task.NextRunAt), now)
	task.MissedCount += count
	task.LastMissedAt = timePointer(last)
	task.NextRunAt = timePointer(next)
	_, err = s.config.Tasks.AdvanceSchedule(ctx, task, task.Revision)
	return err
}

// SetSuspended 接入桌面休眠和恢复，恢复时跳过所有过去的计划机会。
func (s *Service) SetSuspended(ctx context.Context, suspended bool) error {
	s.gate.Lock()
	defer s.gate.Unlock()
	s.suspended = suspended
	s.lastTick = s.config.Now()
	if !suspended {
		return s.advanceMissed(ctx, s.lastTick)
	}
	return nil
}

func (s *Service) begin(ctx context.Context) (context.Context, func(), error) {
	s.life.Lock()
	defer s.life.Unlock()
	if s.ctx == nil || s.ctx.Err() != nil {
		return nil, nil, status_error.OperationCanceled
	}
	linked, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	s.workers.Add(1)
	return linked, func() { stop(); cancel(); s.workers.Done() }, nil
}

// GetAPIStatus 从唯一发布服务读取实际监听事实。
func (s *Service) GetAPIStatus() APIStatus { return s.apiOwner.GetAPIStatus() }

// SetAPIAddress 由旧入口修改同一个集成服务，不再保存第二份配置。
func (s *Service) SetAPIAddress(ctx context.Context, ip string, port int) error {
	return s.apiOwner.SetAPIAddress(ctx, ip, port)
}

// SealWaker 为伙伴聚合删除持有接纳边界，返回必须释放的恢复函数。
func (s *Service) SealWaker(wakerID string) func() {
	s.gate.Lock()
	s.sealedWakers[wakerID] = true
	return func() {
		delete(s.sealedWakers, wakerID)
		s.gate.Unlock()
	}
}

// WithAuthorizationWrite 保持先接纳锁后授权锁的顺序；回调不能执行模型、网络或文件操作。
func (s *Service) WithAuthorizationWrite(ctx context.Context, change func(context.Context) error) error {
	if s.config.AuthorizationWrite == nil {
		return change(ctx)
	}
	return s.config.AuthorizationWrite(ctx, change)
}
