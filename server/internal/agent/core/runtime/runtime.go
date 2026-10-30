// Package runtime 拥有调用、热会话与租约的唯一技术生命周期。
package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/internal/plan"
	"log/slog"
	"strconv"
	"sync"
	"time"
)

// Config 是显式依赖与工程保护值。
type Config struct {
	Limits         c.Limits      // Limits 是默认调用预算。
	CleanupTimeout time.Duration // CleanupTimeout 是一次清理的总期限。
	Logger         *slog.Logger  // Logger 接收宿主提供的阶段诊断，不承载业务事件或输入正文。
}

// Runtime 在明确 Start 后接纳调用，构造不启动后台工作。
type Runtime struct {
	mu            sync.Mutex             // mu 保护接纳、取消和会话所有权。
	config        Config                 // config 是已冻结的模块配置。
	id            string                 // id 是本引擎随机身份。
	counter       uint64                 // counter 在引擎中单调分配调用与会话身份。
	active        map[string]*invocation // active 保存清理完成前的活动调用。
	sessions      map[string]*session    // sessions 保存本引擎的热会话与准备占位。
	started       bool                   // started 表示已开放接纳。
	closing       bool                   // closing 表示已永久停止接纳。
	closeAttempt  *cleanupAttempt        // closeAttempt 是当前或最近一次显式关闭尝试。
	closeRunning  bool                   // closeRunning 表示关闭尝试正在独占闲置资源。
	closeComplete bool                   // closeComplete 表示所有会话与驱动已确认释放。
	work          sync.WaitGroup         // work 等待活动调用和已开始的显式会话清理。
}

type invocation struct {
	ref             c.InvocationRef         // ref 是有效调用及会话修订。
	ctx             context.Context         // ctx 是本轮生命周期。
	cancel          context.CancelCauseFunc // cancel 保留第一个有效取消根因。
	session         *session                // session 是本次唯一占用的会话。
	finished        bool                    // finished 是取消与完成的线性化边界。
	stopPreparation func() bool             // stopPreparation 解除取得阶段的Context转发，已接纳回合由Run接管取消。
	bound           bool                    // bound 表示冷资源已完成交接；交接前取消只撤销取得资源的 Context。
	bindFault       error                   // bindFault 保留资源或实际配置交接失败。
	claimed         bool                    // claimed 防止准备句柄被提交、放弃或关闭重复消费。
}

type cleanupAttempt struct {
	done chan struct{} // done 在本次关闭尝试完成时关闭。
	err  error         // err 是本次关闭尝试的准确结果，发布后不再改变。
}

// New 校验并冻结配置，不创建 goroutine 或原生资源。
func New(cfg Config) (*Runtime, error) {
	if cfg.CleanupTimeout < 0 {
		return nil, fault(c.FaultInvalidSpec, "configure", "negative lifecycle budget", nil)
	}
	if cfg.CleanupTimeout == 0 {
		cfg.CleanupTimeout = 5 * time.Second
	}
	var err error
	cfg.Limits, err = plan.NormalizeLimits(cfg.Limits, plan.DefaultLimits())
	if err != nil {
		return nil, err
	}
	e := &Runtime{config: cfg, active: make(map[string]*invocation), sessions: make(map[string]*session)}
	var identity [16]byte
	if _, err = rand.Read(identity[:]); err != nil {
		return nil, fault(c.FaultPreparation, "configure", "runtime identity unavailable", err)
	}
	e.id = hex.EncodeToString(identity[:])
	return e, nil
}

// Start 显式打开接纳，无探测、预热或后台监听器。
func (e *Runtime) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fault(c.FaultCancelled, "start", "start cancelled", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closing {
		return fault(c.FaultClosed, "start", "runtime is closed", nil)
	}
	e.started = true
	return nil
}

func (e *Runtime) nextID(kind string) string {
	e.counter++
	return e.id + "/" + kind + "/" + strconv.FormatUint(e.counter, 10)
}

func (e *Runtime) executeCall(call *invocation, frozen plan.Spec, options c.RunOptions) c.Result {
	sess := call.session
	result := c.Result{Invocation: call.ref, Status: c.StatusFailed, StopReason: c.StopFailed, Output: c.Output{Status: c.OutputUnavailable, Reliability: c.ReliabilityUnverified}}
	sink := newSink(call, frozen, options, e.config.CleanupTimeout, e.config.Logger)
	raw := sess.driver.Prompt(call.ctx, c.TurnRequest{Invocation: call.ref, Input: frozen.Input, Limits: frozen.Limits, UserInputState: options.UserInputState}, sink)
	delivery, effects, streamFault := sink.finish()
	if !effects.PromptAccepted {
		logStage(e.config.Logger, call, "input_acceptance", time.Since(sink.promptStarted), false, false)
	}
	result.Status = raw.Status
	result.StopReason = raw.StopReason
	result.Fault = raw.Fault
	result.Usage = raw.Usage
	result.Configuration = raw.Configuration
	result.NativeSession = raw.NativeSession
	result.DeliveryFault = delivery
	result.Effects = effects
	if streamFault != nil && result.Fault == nil {
		result.Fault = streamFault
	}
	if result.Status == "" || result.StopReason == "" {
		result.Status = c.StatusFailed
		result.StopReason = c.StopFailed
		if result.Fault == nil {
			result.Fault = fault(c.FaultProtocol, "prompt", "driver returned no execution terminal state", nil)
		}
	}
	if result.Status == c.StatusCompleted && !result.Effects.PromptAccepted && result.Fault == nil {
		result.Status = c.StatusFailed
		result.StopReason = c.StopFailed
		result.Fault = fault(c.FaultProtocol, "prompt", "driver completed without prompt acceptance", nil)
	}
	e.settle(call, &result)
	result.Output = sink.collector.Finish(result.StopReason)
	keep := false
	e.mu.Lock()
	sess.nativeHandle = raw.NativeSession.Handle
	keep = !sess.closeAfterRun && sess.caps.HotResume && !e.closing && !sess.closing && result.Status == c.StatusCompleted && result.Fault == nil && result.DeliveryFault == nil
	if keep {
		sess.call = nil
	} else {
		sess.closing = true
	}
	e.mu.Unlock()
	if !keep {
		result.CleanupFault = e.releaseInvocationSession(call)
	}
	return result
}

// logStage 只记录技术边界耗时，不把诊断混入规范事件或持久消息。
func logStage(logger *slog.Logger, call *invocation, stage string, elapsed time.Duration, success, reused bool) {
	if logger != nil {
		fields := []any{"stage", stage, "elapsed_ms", float64(elapsed) / float64(time.Millisecond), "success", success, "driver_id", call.session.declared.Profile.DriverID, "invocation_id", call.ref.ID, "session_id", call.ref.SessionID}
		if stage == "cli_initialization" {
			fields = append(fields, "reused", reused)
		}
		logger.InfoContext(call.ctx, "agent runtime stage", fields...)
	}
}

func (e *Runtime) settle(call *invocation, result *c.Result) {
	e.mu.Lock()
	defer e.mu.Unlock()
	call.finished = true
	if cause := context.Cause(call.ctx); cause != nil {
		first := asFault(cause, c.FaultCancelled, "execute", "invocation cancelled")
		result.Fault = first
		result.Status = c.StatusCancelled
		result.StopReason = c.StopCancelled
		if first.Code != c.FaultCancelled {
			result.Status = c.StatusFailed
			result.StopReason = c.StopFailed
		}
		if first.Code == c.FaultInteractionRequired {
			result.StopReason = c.StopInteractionRequired
		}
	}
	if result.Status != c.StatusCompleted && result.Status != c.StatusCancelled && result.Status != c.StatusFailed {
		result.Status = c.StatusFailed
		result.StopReason = c.StopFailed
		result.Fault = fault(c.FaultProtocol, "prompt", "invalid driver execution status", nil)
	}
	if (result.Effects.PromptAccepted || result.Effects.ToolActivity) && result.Status != c.StatusCompleted {
		result.Effects.OutcomeUnknown = true
	}
}

// Close 永久停止接纳并启动唯一清理；调用者期限不取消已经开始的释放。
func (e *Runtime) Close(ctx context.Context) error {
	e.mu.Lock()
	if !e.closing {
		e.closing = true
		for _, call := range e.active {
			if !call.finished {
				call.cancel(fault(c.FaultCancelled, "close", "runtime closed", nil))
				e.abandonPreparedLocked(call)
			}
		}
	}
	if e.closeComplete {
		e.mu.Unlock()
		return nil
	}
	if !e.closeRunning {
		e.closeRunning = true
		e.closeAttempt = &cleanupAttempt{done: make(chan struct{})}
		go e.shutdown(e.closeAttempt)
	}
	attempt := e.closeAttempt
	e.mu.Unlock()
	select {
	case <-attempt.done:
		return attempt.err
	case <-ctx.Done():
		return fault(c.FaultCleanup, "close", "runtime cleanup deadline exceeded", ctx.Err())
	}
}
func (e *Runtime) shutdown(attempt *cleanupAttempt) {
	e.work.Wait()
	e.mu.Lock()
	sessions := make([]*session, 0, len(e.sessions))
	for _, sess := range e.sessions {
		e.beginSessionCleanup(sess)
		sessions = append(sessions, sess)
	}
	e.mu.Unlock()
	// 所有闲置会话与 Driver 共用一次关闭预算，不按会话数乘以超时。
	ctx, cancel := context.WithTimeout(context.Background(), e.config.CleanupTimeout)
	defer cancel()
	var first error
	for _, sess := range sessions {
		cleanup := e.releaseSessionWith(ctx, sess)
		e.mu.Lock()
		e.finishSessionCleanup(sess, cleanup)
		e.mu.Unlock()
		if cleanup != nil && first == nil {
			first = cleanup
		}
	}
	e.mu.Lock()
	remaining := len(e.sessions)
	e.mu.Unlock()
	e.mu.Lock()
	e.closeComplete = remaining == 0
	e.closeRunning = false
	attempt.err = first
	close(attempt.done)
	e.mu.Unlock()
}

func failed(id string, f *c.Fault) c.Result {
	return c.Result{Invocation: c.InvocationRef{ID: id}, Status: c.StatusFailed, StopReason: c.StopFailed, Fault: f, Output: c.Output{Status: c.OutputUnavailable, Reliability: c.ReliabilityUnverified}}
}
func fault(code c.FaultCode, stage, message string, cause error) *c.Fault {
	return &c.Fault{Code: code, Stage: stage, Message: message, Cause: cause}
}
func asFault(err error, code c.FaultCode, stage, message string) *c.Fault {
	var technical *c.Fault
	if errors.As(err, &technical) {
		return technical
	}
	return fault(code, stage, message, err)
}
