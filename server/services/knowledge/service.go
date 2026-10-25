// Package knowledge 拥有知识任务与独立模型服务装配，普通聊天只作为明确请求入口。
package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/httpapi"
	"github.com/JieWaZi/neux-knowledge/rules"
	"sync"
	"time"
)

// Config 连接独立知识核心与模型服务；聊天端口仅用于用户主动的普通知识对话。
type Config struct {
	// ResolveModel 验证并冻结独立模型选择，不接纳聊天或执行。
	ResolveModel func(context.Context, models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error)
	// Generator 为冻结选择创建纯文本模型服务，不拥有知识任务调度。
	Generator func(context.Context, models.LibraryExecutionDefaults) (core.Generator, error)

	// FreezeEmbedder 按库冻结独立嵌入配置，不能继承记忆或其他库凭据。
	FreezeEmbedder func(context.Context, string, string) (core.DocumentEmbedder, error)
	// Engine 是唯一知识内容和任务权威。
	Engine *core.Engine
	// References 保存本机连接、冻结模型选择和跨库请求回执。
	References controllers.KnowledgeController
	// Connections 只读取已配置远端及受管认证，不保存远端正文。
	Connections controllers.KnowledgeConnectionController
	// Projects 查询真实项目和目录来源。
	Projects controllers.ProjectController
	// Chats 仅核对普通聊天发起的检索与明确整理请求，不参与知识后台执行。
	Chats chat.Service
	// RemoteGuard 由独立服务器策略从持久身份恢复撤权闸门；缺省拒绝远端作业发布。
	RemoteGuard func(context.Context, json.RawMessage) (func(), error)
	// AutomaticRun 只按服务端运行身份读取原冻结配置。
	AutomaticRun func(context.Context, string) (models.AutomaticRun, error)
	// InvocationGuard 在每次实际知识提交前复核原任务调用和当前任务范围。
	InvocationGuard func(context.Context, string) (func(), error)
	// IntegrationGuard 从统一集成授权源恢复直接调用或自动任务的原授权。
	IntegrationGuard func(context.Context, json.RawMessage) (func(), error)
	// IntegrationPublicationGuard 在一次授权锁内核对全部 API Key 和原自动任务。
	IntegrationPublicationGuard func(context.Context, []json.RawMessage, string) (func(), error)
	// Events 复用进程内提交事件。
	Events *events.Hub
}

// Service 拥有知识任务后台等待，关闭页面不影响已接纳工作。
type Service struct {
	// closing 在同一准入锁内阻止关闭后的驱动、timer与终态恢复者。
	closing bool
	// attempts 保存每次尝试唯一的模型调用 Context 与取消句柄。
	attempts map[string]*organizeAttempt

	// config 是固定装配依赖。
	config Config
	// ctx 是Core根生命周期。
	ctx context.Context
	// cancel 结束宿主等待，不撤销核心已接纳任务。
	cancel context.CancelFunc
	// policy 串行化本机关系撤销与实际知识发布。
	policy sync.RWMutex
	// mutex 串行保护进程内作业占用。
	mutex sync.Mutex
	// running 保存本进程正在驱动的实际尝试，避免同尝试重复及吞掉下一次重试。
	running map[string]int
	// restart 保存用户显式重试后等待旧驱动退出的原任务。
	restart map[string]models.KnowledgeTaskConfiguration
	// workers 等待已启动后台任务退出。
	workers sync.WaitGroup
}

// New 只装配，不启动goroutine。
func New(config Config) *Service {
	return &Service{config: config, running: map[string]int{}, restart: map[string]models.KnowledgeTaskConfiguration{}}
}

// Start 先恢复跨库回执引用；只恢复已接纳任务的检查点，不重建任务。
func (s *Service) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	links, err := s.config.References.ListPendingOperations(ctx)
	if err != nil {
		return err
	}
	for _, link := range links {
		receiptID := link.RequestID
		if link.Operation == constants.KnowledgeRetryAuthorityOperation {
			receiptID = link.ReceiptRequestID
		}
		receipt, err := s.config.Engine.GetReceipt(ctx, receiptID)
		if err != nil {
			var e *core.Error
			if errors.As(err, &e) && e.Code == core.ErrNotFound {
				continue
			}
			return bridge.MapError(err)
		}
		if err := s.config.References.CompleteOperation(ctx, link.RequestID, receipt.LibraryID, receipt.ResourceID, receipt.JobID); err != nil {
			return err
		}

	}
	return s.recoverJobs(ctx)
}

// recoverJobs 按核心库分页读取每库唯一队首，完成历史不进入宿主恢复候选。
func (s *Service) recoverJobs(ctx context.Context) error {
	for offset := 0; ; offset += constants.KnowledgeLibraryScanBatchSize {
		libraries, total, err := s.config.Engine.ListLibraries(ctx, offset, constants.KnowledgeLibraryScanBatchSize)
		if err != nil {
			return bridge.MapError(err)
		}
		for _, library := range libraries {
			if err := s.recoverLibrary(ctx, library.ID, ""); err != nil {
				return err
			}
		}
		if int64(offset+len(libraries)) >= total || len(libraries) == 0 {
			return nil
		}
	}
}

// recoverLibrary 只从核心队首恢复原接纳身份和冻结配置；忽略刚退出但尚未结算的同一任务。
func (s *Service) recoverLibrary(ctx context.Context, library, previousJob string) error {
	if ctx.Err() != nil {
		return nil
	}
	job, err := s.config.Engine.GetActiveJob(ctx, library)
	if err != nil {
		mapped := bridge.MapError(err)
		// 并发永久删除使本页库失效时，历史引用不阻断其余库恢复。
		if errors.Is(mapped, status_error.KnowledgeNotFound) {
			return nil
		}
		return mapped
	}
	if job == nil || job.ID == previousJob {
		return nil
	}
	origin, err := s.config.References.GetOperation(ctx, job.RequestID)
	if errors.Is(err, status_error.KnowledgeNotFound) {
		// 核心可由其他公开入口接纳任务；没有宿主锚点的队首不属于本服务。
		return nil
	}
	if err != nil {
		return err
	}
	if origin.Operation != "job.accept" || origin.Backend != "local" || origin.ConnectionID != "local" || origin.KnowledgeBaseID != job.LibraryID || origin.ResourceID != job.ID || origin.ConfigurationID == "" {
		return status_error.KnowledgeInvalid
	}
	session, err := s.jobConfiguration(ctx, job, origin.ConfigurationID)
	if err != nil {
		return err
	}
	if !job.CancelRequested && session.RemoteSubject != "" {
		_, ready, err := s.remotePublicationGuard(session)
		if err != nil {
			return err
		}
		if !ready {
			return nil
		}
	}
	s.drive(job.ID, session)
	return nil
}

// Close 收敛当前等待，核心作业保留以便重启对账。
func (s *Service) Close() {
	s.mutex.Lock()
	s.closing = true
	if s.cancel != nil {
		s.cancel()
	}
	s.mutex.Unlock()
	s.workers.Wait()
}

// OrganizeCommand 是两个真实入口共用的明确整理请求。
type OrganizeCommand struct {
	// Rebuild 显式重建所选原件，省略时保留已处理来源的跳过语义与旧请求摘要。
	Rebuild bool `json:"rebuild,omitempty"`

	// ExpectedVersion 非零时保持调用者已读库版本直到原子接纳。
	ExpectedVersion int64 `json:"expectedVersion,omitempty"`
	// StructureVersion 非零时保持调用者已读目录版本直到原子接纳。
	StructureVersion int64 `json:"structureVersion,omitempty"`
	// RequestID 是用户请求或自动机会稳定身份。
	RequestID string
	// LibraryID 是明确目标库。
	LibraryID string
	// SourceVersionIDs 为空时只选择当前pending来源；非空精确限定。
	SourceVersionIDs []string
	// ExplicitSources 表示空集合也是已冻结的无新增，不重新选择pending。
	ExplicitSources bool
	// Configuration 是用户明确选择的独立模型配置。
	Configuration models.KnowledgeTaskConfiguration
	// Message 是实际用户维护要求，不从模型文本反推授权。
	Message string
}

// Organize 接纳并后台驱动明确范围，普通上传不调用它。
func (s *Service) Organize(ctx context.Context, in OrganizeCommand) (*core.Receipt, models.KnowledgeTaskConfiguration, error) {
	if in.Rebuild && len(in.SourceVersionIDs) == 0 {
		return nil, in.Configuration, status_error.KnowledgeInvalid
	}
	if in.RequestID == "" {
		return nil, in.Configuration, status_error.KnowledgeInvalid
	}
	if in.Configuration.ID == "" {
		in.Configuration.ID = in.RequestID
	}
	in.Configuration.KnowledgeBaseID = in.LibraryID
	in.Configuration.Backend = "local"
	in.Configuration.ConnectionID = "local"
	in.Configuration.Message = in.Message
	// 摘要固定用户意图，不能因后续pending来源或Runtime默认变化改写同键请求。
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	link := models.KnowledgeOperationLink{RequestID: in.RequestID, Operation: "job.accept", Digest: hex.EncodeToString(sum[:]), Backend: "local", ConnectionID: "local", KnowledgeBaseID: in.LibraryID, ConfigurationID: in.Configuration.ID}
	if err := s.config.References.BeginOperation(ctx, link); err != nil {
		return nil, in.Configuration, err
	}
	if old, err := s.config.Engine.GetReceipt(ctx, in.RequestID); err == nil {
		session, err := s.config.References.GetTaskConfiguration(ctx, in.Configuration.ID)
		if err != nil {
			return nil, in.Configuration, err
		}
		if err := s.config.References.CompleteOperation(ctx, in.RequestID, old.LibraryID, old.ResourceID, old.JobID); err != nil {
			return nil, session, err
		}
		if old.ResourceID != "" {
			s.drive(old.ResourceID, session)
		}
		return old, session, nil
	} else {
		var e *core.Error
		if !errors.As(err, &e) || e.Code != core.ErrNotFound {
			return nil, in.Configuration, bridge.MapError(err)
		}
	}
	lib, err := s.config.Engine.GetLibrary(ctx, in.LibraryID)
	if err != nil {
		return nil, in.Configuration, bridge.MapError(err)
	}
	if (in.ExpectedVersion != 0 && lib.Version != in.ExpectedVersion) || (in.StructureVersion != 0 && lib.StructureVersion != in.StructureVersion) {
		return nil, in.Configuration, status_error.KnowledgeConflict
	}
	// 核心接纳暂未完成也沿用已落盘选择，不能重新解析后来变化的库或 Runtime 默认值。
	frozenConfiguration := false
	if _, err := s.config.References.GetTaskConfiguration(ctx, in.Configuration.ID); err == nil {
		in.Configuration, err = s.config.References.FreezeTaskConfiguration(ctx, in.Configuration)
		if err != nil {
			return nil, in.Configuration, err
		}
		frozenConfiguration = true
	} else if !errors.Is(err, status_error.KnowledgeNotFound) {
		return nil, in.Configuration, err
	}
	// 用户未覆盖配置时使用同一宿主持久偏好；已接纳回执的重放已在上方恢复旧配置。
	if !frozenConfiguration && in.Configuration.RuntimeID == "" && in.Configuration.AccountID == "" && in.Configuration.Model == "" && in.Configuration.Reasoning == "" {
		defaults, e := s.config.References.LibraryExecutionDefaults(ctx, in.LibraryID)
		if e != nil {
			return nil, in.Configuration, e
		}
		in.Configuration.RuntimeID = defaults.RuntimeID
		in.Configuration.AccountID = defaults.AccountID
		in.Configuration.Model = defaults.Model
		in.Configuration.Reasoning = defaults.Reasoning
	}
	if err := s.validateLibrary(lib); err != nil {
		return nil, in.Configuration, err
	}
	if !frozenConfiguration {
		if s.config.ResolveModel == nil {
			return nil, in.Configuration, status_error.KnowledgeUnsupported
		}
		selected, err := s.config.ResolveModel(ctx, models.LibraryExecutionDefaults{RuntimeID: in.Configuration.RuntimeID, AccountID: in.Configuration.AccountID, Model: in.Configuration.Model, Reasoning: in.Configuration.Reasoning})
		if err != nil {
			return nil, in.Configuration, err
		}
		in.Configuration.RuntimeID, in.Configuration.AccountID, in.Configuration.Model, in.Configuration.Reasoning = selected.RuntimeID, selected.AccountID, selected.Model, selected.Reasoning
	}
	if len(in.SourceVersionIDs) == 0 && !in.ExplicitSources {
		in.SourceVersionIDs, err = s.pendingSourceHeads(ctx, in.LibraryID)
		if err != nil {
			return nil, in.Configuration, err
		}
	}

	wikiLite := lib.Kind == "general" && lib.IndexingStrategy.WikiEnabled
	bundle, err := rules.LoadOrganize(lib.Kind, lib.WikiLanguage, wikiLite)
	if err != nil {
		return nil, in.Configuration, status_error.KnowledgeUnsupported
	}
	embeddingFingerprint := ""
	if lib.IndexingStrategy.VectorEnabled {
		if s.config.FreezeEmbedder == nil {
			return nil, in.Configuration, status_error.KnowledgeUnsupported
		}
		frozen, e := s.config.FreezeEmbedder(ctx, lib.ID, lib.EmbeddingModelID)
		if e != nil {
			return nil, in.Configuration, e
		}
		embeddingFingerprint = frozen.Fingerprint
	}
	snapshot := core.PermissionSnapshot{PolicyVersion: lib.PolicyVersion, Scope: []string{}, Operations: []string{"organize"}}
	if in.Configuration.RemoteSubject != "" {
		var recovery httpapi.RecoveryIdentity
		if err := json.Unmarshal(in.Configuration.RemoteAuthorization, &recovery); err != nil {
			return nil, in.Configuration, status_error.KnowledgeForbidden
		}
		snapshot = recovery.Permission
	}
	in.Configuration, err = s.config.References.FreezeTaskConfiguration(ctx, in.Configuration)
	if err != nil {
		return nil, in.Configuration, err
	}
	if in.Configuration.RemoteSubject != "" {
		ctx = core.WithActivityActor(ctx, core.ActivityActor{UserID: in.Configuration.RemoteSubject, Trigger: "api"})
	}
	release, guardErr := s.publishGuard(in.Configuration)(ctx)
	if guardErr != nil {
		return nil, in.Configuration, guardErr
	}

	configDigest := core.OrganizeConfigDigest(lib, bundle.SHA256)
	receipt, err := s.config.Engine.AcceptJob(ctx, core.AcceptJobInput{Rebuild: in.Rebuild, RequestID: in.RequestID, LibraryID: in.LibraryID, ExpectedVersion: lib.Version, StructureVersion: lib.StructureVersion, SourceVersionIDs: in.SourceVersionIDs, Permission: snapshot, RulesVersion: bundle.Version, ConfigDigest: configDigest, EmbeddingFingerprint: embeddingFingerprint})
	release()
	if err != nil {
		// 同键竞争者可能已接纳，导致本次默认范围投影为无新增；宿主已校验原始输入摘要，只恢复原回执。
		confirmed, readErr := s.config.Engine.GetReceipt(ctx, in.RequestID)
		if readErr != nil {
			return nil, in.Configuration, bridge.MapError(err)
		}
		receipt = confirmed
	}
	if err := s.config.References.CompleteOperation(ctx, in.RequestID, receipt.LibraryID, receipt.ResourceID, receipt.JobID); err != nil {
		return nil, in.Configuration, err
	}
	if receipt.ResourceID != "" {
		s.emitTaskChanged(receipt.JobID, receipt.LibraryID)
		s.drive(receipt.ResourceID, in.Configuration)
	}
	return receipt, in.Configuration, nil
}

// validateLibrary 校验整理目标仍为有效通用库，模型服务不依赖项目或伙伴。
func (s *Service) validateLibrary(lib *core.Library) error {
	if lib.Archived || lib.Kind != "general" {
		return status_error.KnowledgeForbidden
	}
	return nil
}

func (s *Service) validateTarget(ctx context.Context, waker, project string, lib *core.Library) error {
	if lib.Archived || lib.Kind != "general" {
		return status_error.KnowledgeForbidden
	}
	if project != "" {
		p, err := s.config.Projects.Get(ctx, project)
		if err != nil {
			return err
		}
		if p.WakerID != waker {
			return status_error.KnowledgeForbidden
		}
	}
	return nil
}

// drive 只把核心作业交给同步模型端口，页面和普通聊天不拥有其生命周期。
func (s *Service) drive(jobID string, session models.KnowledgeTaskConfiguration) {
	s.mutex.Lock()
	if s.closing || s.ctx == nil || s.ctx.Err() != nil {
		s.mutex.Unlock()
		return
	}
	job, err := s.config.Engine.GetJob(s.ctx, jobID)
	if err != nil {
		s.mutex.Unlock()
		logx.Errorf("knowledge task lookup failed: job_id=%s error=%v", jobID, err)
		return
	}
	// 首次尝试已由所有接纳入口固定；仅重试可能附加授权，必须在驱动处按当前尝试恢复。
	if job.Attempt > 1 {
		origin, readErr := s.config.References.GetOperation(s.ctx, job.RequestID)
		err = readErr
		if err == nil {
			session, err = s.jobConfiguration(s.ctx, job, origin.ConfigurationID)
		}
	}
	if err != nil {
		s.mutex.Unlock()
		logx.Errorf("knowledge task authority lookup failed: job_id=%s error=%v", jobID, err)
		return
	}
	if running := s.running[jobID]; running != 0 {
		if running != job.Attempt {
			s.restart[jobID] = session
		}
		s.mutex.Unlock()
		return
	}
	s.running[jobID] = job.Attempt
	s.workers.Add(1)
	s.mutex.Unlock()
	go func() {
		defer s.workers.Done()
		defer s.emitTaskChanged(jobID, session.KnowledgeBaseID)
		defer func() {
			s.mutex.Lock()
			delete(s.running, jobID)
			next, restart := s.restart[jobID]
			delete(s.restart, jobID)
			s.mutex.Unlock()
			if restart {
				s.drive(jobID, next)
			} else if err := s.recoverLibrary(s.ctx, job.LibraryID, jobID); err != nil {
				logx.Errorf("knowledge queue recovery failed: library_id=%s error=%v", job.LibraryID, err)
			}
		}()
		attempt, err := s.attemptContext(job, session)
		if err != nil {
			s.recordTaskFailure(job, err)
			return
		}
		defer s.finishAttempt(jobID, attempt)
		if job.CancelRequested {
			_, err = s.config.Engine.CancelOrganize(attempt.ctx, core.JobActionInput{RequestID: "knowledge-cancel-recovery/" + job.ID, JobID: job.ID, ExpectedVersion: job.Version})
			if err != nil {
				s.recordTaskFailure(job, err)
			}
			return
		}
		if s.config.Generator == nil {
			s.recordTaskFailure(job, status_error.KnowledgeUnsupported)
			return
		}
		generator, err := s.config.Generator(attempt.ctx, models.LibraryExecutionDefaults{RuntimeID: session.RuntimeID, AccountID: session.AccountID, Model: session.Model, Reasoning: session.Reasoning})
		if err != nil {
			s.recordTaskFailure(job, err)
			return
		}
		embedder := core.DocumentEmbedder{}
		if job.LibrarySnapshot != nil && (job.LibrarySnapshot.IndexingStrategy.VectorEnabled || job.LibrarySnapshot.IndexingStrategy.WikiEnabled && job.LibrarySnapshot.EmbeddingModelID != "") {
			if s.config.FreezeEmbedder == nil {
				err = status_error.KnowledgeUnsupported
			} else {
				embedder, err = s.config.FreezeEmbedder(attempt.ctx, job.LibraryID, job.LibrarySnapshot.EmbeddingModelID)
			}
			if err != nil && !job.LibrarySnapshot.IndexingStrategy.VectorEnabled {
				// Lite 的目录相关性排序可回退到已有目录；真正启用向量索引时仍须拒绝失败。
				logx.Warnf("knowledge taxonomy embedding unavailable: job_id=%s error=%v", jobID, err)
				embedder, err = core.DocumentEmbedder{}, nil
			}
		}
		if err == nil {
			var release func()
			release, err = s.publishAttemptGuard(session, jobID, job.Attempt)(attempt.ctx)
			if err == nil {
				release()
			}
		}
		if err == nil {
			_ = s.config.References.SetHostDiagnostic(attempt.ctx, job.RequestID, "active", "", "")
			_, err = s.config.Engine.RunOrganize(attempt.ctx, core.OrganizeInput{JobID: jobID, Instruction: session.Message, Embed: embedder.Embed, Reader: bridge.TextDocumentReader{}, Generator: generator, PublishGuard: s.publishAttemptGuard(session, jobID, job.Attempt), PrepareDocument: func(ctx context.Context, source string, artifact core.ReadArtifact) (core.ReadArtifact, error) {
				// 核心在外部嵌入结束后取得同一授权闸门，避免撤权期间的迟到索引提交。
				indexEmbedder := embedder
				indexEmbedder.PublishGuard = s.publishAttemptGuard(session, jobID, job.Attempt)
				return s.config.Engine.PrepareIndexedDocument(ctx, jobID, source, artifact, indexEmbedder)
			}, OnProgress: func(_ context.Context) { s.emitTaskChanged(jobID, session.KnowledgeBaseID) }, OnPublished: func(_ context.Context, _ *core.Receipt) { s.emitTaskChanged(jobID, session.KnowledgeBaseID) }})
		}
		if err != nil && s.ctx.Err() == nil {
			s.recordTaskFailure(job, err)
		} else if err == nil {
			_ = s.config.References.SetHostDiagnostic(context.Background(), job.RequestID, "idle", "", "")
		}
	}()
}

func (s *Service) emitTaskChanged(job, library string) {
	if s.config.Events != nil {
		_ = events.Emit(s.config.Events, "knowledge", "knowledge.changed", events.Resource{Kind: "knowledge_library", ID: library, ParentID: "local"}, map[string]any{"ref": bridge.LocalRef(library), "jobId": job})
	}
}

// recordTaskFailure 将不可恢复故障结算为核心失败，退出进程与资料删除沿各自生命周期收束。
func (s *Service) recordTaskFailure(job *core.Job, cause error) {
	if s.ctx != nil && s.ctx.Err() != nil {
		return
	}
	code, diagnostic := hostDiagnostic(cause)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	current, err := s.config.Engine.GetJob(ctx, job.ID)
	if err == nil && current.Attempt == job.Attempt && !current.CancelRequested && (current.State == "queued" || current.State == "processing") {
		_, err = s.config.Engine.FailOrganize(ctx, job.ID, job.Attempt, diagnostic)
	}
	hostState := "idle"
	if err != nil {
		hostState = "blocked"
		logx.Errorf("settle knowledge task failure failed: job_id=%s error=%v", job.ID, err)
	}
	if saveErr := s.config.References.SetHostDiagnostic(ctx, job.RequestID, hostState, code, diagnostic); saveErr != nil {
		logx.Errorf("knowledge task diagnostic failed: job_id=%s error=%v", job.ID, saveErr)
	}
	logx.Errorf("knowledge task stopped: job_id=%s error=%v", job.ID, cause)
}

// Resume 从原任务和原模型选择恢复，不扩大来源快照。
func (s *Service) Resume(ctx context.Context, job string) error { return s.resume(ctx, job, false) }

func (s *Service) resume(ctx context.Context, job string, restart bool) error {
	j, err := s.config.Engine.GetJob(ctx, job)
	if err != nil {
		return bridge.MapError(err)
	}
	origin, err := s.config.References.GetOperation(ctx, j.RequestID)
	if err != nil {
		return err
	}
	session, err := s.jobConfiguration(ctx, j, origin.ConfigurationID)
	if err != nil {
		return err
	}
	release, err := s.publishGuard(session)(ctx)
	if err != nil {
		return err
	}
	release()
	s.mutex.Lock()
	if runningAttempt := s.running[job]; runningAttempt != 0 {
		if restart && j.Attempt != runningAttempt {
			s.restart[job] = session
		}
		s.mutex.Unlock()
		return nil
	}
	s.mutex.Unlock()
	s.drive(job, session)
	return nil
}

// currentResources 仅发现未归档通用库，不使用项目关系扩大知识范围。
func (s *Service) currentResources(ctx context.Context) ([]bridge.ResourceRef, error) {
	refs := []bridge.ResourceRef{}
	for offset := 0; s.config.Engine != nil; offset += 100 {
		rows, total, err := s.config.Engine.ListLibraries(ctx, offset, 100)
		if err != nil {
			return nil, bridge.MapError(err)
		}
		for _, lib := range rows {
			if lib.Kind == "general" && !lib.Archived {
				refs = append(refs, bridge.LocalRef(lib.ID))
			}
		}
		if int64(offset+100) >= total {
			break
		}
	}

	return refs, nil
}

// AgentBindings 冻结普通执行可见库；整理通过CLI选择当前消息范围并逐次校验。
func (s *Service) AgentBindings(ctx context.Context, chat models.Chat) []agentcapability.Binding {
	maintenance, err := s.maintenanceChatLink(ctx, chat.ID)
	if err != nil {
		return deniedKnowledgeReads(err)
	}
	if maintenance != nil {
		bindings, err := s.MaintenanceBindings(ctx, chat, bridge.ResourceRef{Backend: maintenance.Backend, ConnectionID: maintenance.ConnectionID, KnowledgeBaseID: maintenance.KnowledgeBaseID}, maintenance.ResourceID)
		if err != nil {
			return deniedKnowledgeReads(err)
		}
		return bindings
	}
	provider, err := s.freezeReadProvider(ctx)
	if err != nil {
		return deniedKnowledgeReads(err)
	}
	current := func(ctx context.Context, _ string) ([]bridge.ResourceRef, error) {
		return s.agentReadResources(ctx, provider)
	}
	refs, err := current(ctx, chat.ProjectID)
	if err != nil {
		return deniedKnowledgeReads(err)
	}
	bindings := bridge.ProviderReadBindings(provider, bridge.FreezeScope(chat.ProjectID, refs), current)
	for i := range bindings {
		call := bindings[i].Call
		bindings[i].Call = func(ctx context.Context, request agentcapability.Request) (agentcapability.Result, error) {
			s.policy.RLock()
			defer s.policy.RUnlock()
			return call(ctx, request)
		}
	}
	if chat.ID != "" && chat.WakerID != "" && chat.ActiveExecutionID != "" {
		bindings = append(bindings, s.chatOrganizeBinding(ctx, chat, refs))
		bindings = append(bindings, s.wikiBindings(ctx, chat, refs, provider, "")...)
	}
	return bindings
}

// WithPolicyWrite 串行化关系变更与正在提交的本地发布。
func (s *Service) WithPolicyWrite(run func() error) error {
	s.policy.Lock()
	defer s.policy.Unlock()
	return run()
}

// SetRemoteGuard 只由服务端组合根在启动恢复前装配，不能由请求修改。
func (s *Service) SetRemoteGuard(guard func(context.Context, json.RawMessage) (func(), error)) error {
	s.mutex.Lock()
	s.config.RemoteGuard = guard
	s.mutex.Unlock()
	if s.ctx == nil {
		return nil
	}
	return s.recoverJobs(s.ctx)
}

// remotePublicationGuard 统一按原授权来源选择闸门，并告知恢复者对应依赖是否已经装配。
func (s *Service) remotePublicationGuard(session models.KnowledgeTaskConfiguration) (func(context.Context, json.RawMessage) (func(), error), bool, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	var origin struct {
		// Origin 只区分受信恢复机制，不从正文获取授权。
		Origin string `json:"origin"`
		// AutomaticRunID 固定原自动运行，不能从请求正文恢复。
		AutomaticRunID string `json:"automaticRunId"`
	}
	if json.Unmarshal(session.RemoteAuthorization, &origin) != nil {
		return nil, false, status_error.KnowledgeForbidden
	}
	guard := s.config.RemoteGuard
	switch origin.Origin {
	case "":
	case "direct_api":
		guard = s.config.IntegrationGuard
	case "automatic_task":
		invocationGuard := s.config.InvocationGuard
		guard = func(ctx context.Context, _ json.RawMessage) (func(), error) {
			if invocationGuard == nil || origin.AutomaticRunID == "" {
				return nil, status_error.KnowledgeForbidden
			}
			return invocationGuard(ctx, origin.AutomaticRunID)
		}
		return guard, invocationGuard != nil, nil
	default:
		// 未知来源沿实际发布路径明确失败，不能冒用已装配的其他授权。
		return nil, true, nil
	}
	return guard, guard != nil, nil
}

// publishAttemptGuard 将真实提交同时绑定原尝试，旧模型准备不能驱动已重试的新尝试。
func (s *Service) publishAttemptGuard(session models.KnowledgeTaskConfiguration, jobID string, attempt int) func(context.Context) (func(), error) {
	return func(ctx context.Context) (func(), error) {
		release, err := s.publishGuard(session)(ctx)
		if err != nil {
			return nil, err
		}
		current, err := s.config.Engine.GetJob(ctx, jobID)
		if err != nil {
			release()
			return nil, bridge.MapError(err)
		}
		if current.Attempt != attempt {
			release()
			return nil, status_error.KnowledgeCancelled
		}
		return release, nil
	}
}

func (s *Service) publishGuard(session models.KnowledgeTaskConfiguration) func(context.Context) (func(), error) {
	return func(ctx context.Context) (func(), error) {
		remoteRelease := func() {}
		if len(session.PublicationAuthorizations) > 0 {
			var err error
			remoteRelease, err = s.retryPublicationGuard(ctx, session)
			if err != nil {
				return nil, err
			}
		} else if session.RemoteSubject != "" {
			guard, _, err := s.remotePublicationGuard(session)
			if err != nil {
				return nil, err
			}
			if guard == nil {
				return nil, status_error.KnowledgeUnsupported
			}
			remoteRelease, err = guard(ctx, session.RemoteAuthorization)
			if err != nil {
				return nil, err
			}
		}
		s.policy.RLock()
		lib, err := s.config.Engine.GetLibrary(ctx, session.KnowledgeBaseID)
		if err == nil {
			err = s.validateLibrary(lib)
		} else {
			err = bridge.MapError(err)
		}
		if err != nil {
			s.policy.RUnlock()
			remoteRelease()
			return nil, err
		}
		return func() { s.policy.RUnlock(); remoteRelease() }, nil
	}
}

// Retry 复核当前授权后重试原冻结任务，不读取其他pending来源。
func (s *Service) Retry(ctx context.Context, request, job string, version int64) (*core.Receipt, error) {
	return s.retry(ctx, request, job, version, nil)
}

func (s *Service) retry(ctx context.Context, request, job string, version int64, extra json.RawMessage) (*core.Receipt, error) {
	j, err := s.config.Engine.GetJob(ctx, job)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	link, err := s.config.References.GetOperation(ctx, j.RequestID)
	if err != nil {
		return nil, err
	}
	session, err := s.jobConfiguration(ctx, j, link.ConfigurationID)
	if err != nil {
		return nil, err
	}
	if prior, e := s.config.Engine.GetReceipt(ctx, request); e == nil {
		release, guardErr := s.publishGuard(session)(ctx)
		if guardErr != nil {
			return nil, guardErr
		}
		prior, e = s.config.Engine.RetryJob(ctx, core.JobActionInput{RequestID: request, JobID: job, ExpectedVersion: version})
		release()
		if e != nil {
			return nil, bridge.MapError(e)
		}
		if e = s.resume(ctx, job, true); e != nil {
			return nil, e
		}
		return prior, nil
	} else if !errors.Is(bridge.MapError(e), status_error.KnowledgeNotFound) {
		return nil, bridge.MapError(e)
	}
	if len(extra) > 0 {
		session.PublicationAuthorizations = append(session.PublicationAuthorizations, extra)
		session.ID = request + "/configuration"
	}
	release, err := s.publishGuard(session)(ctx)
	if err != nil {
		return nil, err
	}
	if len(session.PublicationAuthorizations) > 0 {
		session.ID = request + "/configuration"
		frozen, e := s.config.References.FreezeTaskConfiguration(ctx, session)
		if e != nil {
			release()
			return nil, e
		}
		session = frozen
		digest, _ := json.Marshal(struct {
			Job           string // 原核心任务身份。
			Version       int64  // 已读核心版本。
			Configuration string // 本次冻结授权配置。
		}{job, version, session.ID})
		sum := sha256.Sum256(digest)
		e = s.config.References.BeginOperation(ctx, models.KnowledgeOperationLink{RequestID: request + "/authority", ReceiptRequestID: request, Operation: constants.KnowledgeRetryAuthorityOperation, Digest: hex.EncodeToString(sum[:]), Backend: "local", ConnectionID: "local", KnowledgeBaseID: j.LibraryID, ExpectedResourceID: job, TargetAttempt: j.Attempt + 1, TargetVersion: version, ConfigurationID: session.ID})
		if e != nil {
			release()
			return nil, e
		}
	}
	receipt, err := s.config.Engine.RetryJob(ctx, core.JobActionInput{RequestID: request, JobID: job, ExpectedVersion: version})
	release()
	if err != nil {
		return nil, bridge.MapError(err)
	}
	s.emitTaskChanged(j.ID, j.LibraryID)
	if err := s.resume(ctx, job, true); err != nil {
		return nil, err
	}
	return receipt, nil
}

// ResolveModel 验证知识模型偏好，不接纳任何后台生成或聊天执行。
func (s *Service) ResolveModel(ctx context.Context, selected models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
	if s.config.ResolveModel == nil {
		return selected, status_error.KnowledgeUnsupported
	}
	return s.config.ResolveModel(ctx, selected)
}

// SearchSourceChunks 复用原件读取管线，查询模型仍使用目标库的固定嵌入配置。
func (s *Service) SearchSourceChunks(ctx context.Context, libraryID, query string, limit int) ([]bridge.DocumentReadHit, string, error) {
	return (bridge.LocalReadProvider{Engine: s.config.Engine, FreezeEmbedder: s.config.FreezeEmbedder, Limit: limit}).SearchChunks(ctx, bridge.LocalRef(libraryID), query)
}

// ExecutionCondition 只核对目标库的服务端执行配置，不触发生成或接纳作业。
func (s *Service) ExecutionCondition(ctx context.Context, libraryID string) string {
	if s == nil || s.config.References == nil || s.config.ResolveModel == nil || s.config.Generator == nil {
		return "execution_unavailable"
	}
	defaults, err := s.config.References.LibraryExecutionDefaults(ctx, libraryID)
	if err != nil {
		return "execution_not_configured"
	}
	if _, err = s.ResolveModel(ctx, defaults); err != nil {
		return "execution_not_configured"
	}
	return "ready"
}
