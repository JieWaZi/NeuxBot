// Package terminalruntime 由 Core 持有 PTY 子进程，隔离操作者、附件和回收生命周期。
package terminalruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// Started 是稳定终端身份。
type Started struct {
	// SessionID 标识当前 Core 中的 PTY。
	SessionID string `json:"sessionId"`
	// Shell 是当前实际 Shell。
	Shell string `json:"shell"`
}

// Snapshot 是附着时的有界终端屏幕。
type Snapshot struct {
	// Started 保存稳定会话身份。
	Started
	// AttachmentID 是当前控制附件身份。
	AttachmentID string `json:"attachmentId"`
	// Screen 是 xterm 序列化的屏幕与有界历史。
	Screen string `json:"screen"`
	// Sequence 是屏幕包含的最后输出序号。
	Sequence uint64 `json:"sequence"`
	// State 区分运行与退出。
	State string `json:"state"`
	// ExitCode 是已退出进程的返回值。
	ExitCode *int `json:"exitCode,omitempty"`
}

// wireMessage 是与受管 Node worker 共用的 JSONL 帧。
type wireMessage struct {
	// ID 是请求关联标识。
	ID string `json:"id,omitempty"`
	// Method 是白名单操作。
	Method string `json:"method,omitempty"`
	// Args 是已校验参数。
	Args any `json:"args,omitempty"`
	// Result 是成功响应正文。
	Result json.RawMessage `json:"result,omitempty"`
	// Error 是 worker 操作失败说明。
	Error string `json:"error,omitempty"`
	// Event 是固定的异步事件类型。
	Event string `json:"event,omitempty"`
	// Payload 是事件正文。
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Attachment 独占一个会话的实时输出，慢消费者断开后按屏幕恢复。
type Attachment struct {
	// Context 在撤销、替换或溢出时结束。
	Context context.Context
	// Events 是最多 1 MiB 的有序输出队列。
	Events chan []byte
	// cancel 撤销该输出附件。
	cancel context.CancelFunc
	// bytes 是尚未交付的字节预算。
	bytes atomic.Int64
	// ID 是 worker 签发的控制附件身份。
	ID string
}

// Consumed 释放已经交付的事件预算。
func (a *Attachment) Consumed(size int) { a.bytes.Add(-int64(size)) }

// ownedSession 保存 Core 授权和附件，不复制终端屏幕。
type ownedSession struct {
	// subject 是已认证的管理主体。
	subject string
	// owner 是前端稳定内容身份。
	owner string
	// root 是创建时的冻结规范目录。
	root string
	// started 是 worker 签发的稳定身份。
	started Started
	// attachment 是当前唯一控制附件。
	attachment *Attachment
	// grace 是无人附着后的回收计时器。
	grace *time.Timer
	// stopLifetime 释放浏览器会话撤销监听。
	stopLifetime func() bool
}

// Supervisor 拥有唯一 worker 及请求分派，不创建监听端口。
type Supervisor struct {
	// operations 串行化创建、附着与关闭以保证幂等。
	operations sync.Mutex
	// mu 保护会话、回执和输出附件集合。
	mu sync.Mutex
	// writeMu 防止多个请求在 JSONL 中交错。
	writeMu sync.Mutex
	// command 是当前 Core 拥有的子进程。
	command *exec.Cmd
	// input 只用于向受管 worker 写入白名单指令。
	input io.WriteCloser
	// pending 按关联标识交付请求回执。
	pending map[string]chan wireMessage
	// sessions 按会话标识保存授权信息。
	sessions map[string]*ownedSession
	// owners 按操作者和内容身份索引幂等会话。
	owners map[string]string
	// sequence 生成进程内唯一请求标识。
	sequence atomic.Uint64
	// done 在 worker 结束时关闭。
	done chan struct{}
	// ready 在 worker 完成装配时关闭。
	ready chan struct{}
	// gracePeriod 是断线后允许恢复原屏幕的窗口。
	gracePeriod time.Duration
	// stop 保证 worker 仅回收一次。
	stop sync.Once
}

// workerDiagnostics 将 worker 的诊断与标准输出 JSONL 协议分离。
type workerDiagnostics struct{}

// Write 将诊断交给统一日志入口；管道复制与回收由 exec 管理。
func (workerDiagnostics) Write(data []byte) (int, error) {
	if message := strings.TrimSpace(string(data)); message != "" {
		logx.Errorf("terminal runtime worker diagnostic: message=%q", message)
	}
	return len(data), nil
}

// New 启动同版本 worker，并在握手成功后接纳终端。
func New(node, worker string) (*Supervisor, error) {
	if node == "" {
		node = "node"
	}
	command := exec.Command(node, worker)
	command.Stderr = workerDiagnostics{}
	if os.Getenv("ALLY_TERMINAL_ELECTRON_NODE") == "1" {
		command.Env = append(os.Environ(), "ELECTRON_RUN_AS_NODE=1")
	}
	configureGroup(command)
	input, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	s := &Supervisor{command: command, input: input, pending: make(map[string]chan wireMessage), sessions: make(map[string]*ownedSession), owners: make(map[string]string), done: make(chan struct{}), ready: make(chan struct{}), gracePeriod: 60 * time.Second}
	if err = command.Start(); err != nil {
		return nil, err
	}
	go s.read(output)
	select {
	case <-s.ready:
		return s, nil
	case <-s.done:
		return nil, errors.New("终端程序启动失败")
	case <-time.After(10 * time.Second):
		s.Close()
		return nil, errors.New("终端程序启动超时")
	}
}

func (s *Supervisor) read(output io.Reader) {
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		var frame wireMessage
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			break
		}
		if frame.Event == "ready" {
			var version struct {
				// ProtocolVersion 是 worker 握手协议版本。
				ProtocolVersion int `json:"protocolVersion"`
			}
			if json.Unmarshal(frame.Payload, &version) != nil || version.ProtocolVersion != 1 {
				break
			}
			select {
			case <-s.ready:
			default:
				close(s.ready)
			}
			continue
		}
		if frame.ID != "" {
			s.mu.Lock()
			receiver := s.pending[frame.ID]
			delete(s.pending, frame.ID)
			s.mu.Unlock()
			if receiver != nil {
				receiver <- frame
			}
			continue
		}
		s.emit(frame)
	}
	killGroup(s.command)
	_ = s.command.Wait()
	s.mu.Lock()
	for _, session := range s.sessions {
		if session.attachment != nil {
			session.attachment.cancel()
		}
		if session.grace != nil {
			session.grace.Stop()
		}
		if session.stopLifetime != nil {
			session.stopLifetime()
		}
	}
	s.mu.Unlock()
	close(s.done)
}

func (s *Supervisor) emit(frame wireMessage) {
	if frame.Event != "terminal-output" && frame.Event != "terminal-exit" {
		return
	}
	var identity struct {
		// SessionID 指向事件所属的受管会话。
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(frame.Payload, &identity) != nil {
		return
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[identity.SessionID]
	if session == nil || session.attachment == nil {
		return
	}
	a := session.attachment
	if a.bytes.Add(int64(len(data))) > 1<<20 {
		a.cancel()
		return
	}
	select {
	case a.Events <- data:
	default:
		a.cancel()
	}
}

func (s *Supervisor) call(ctx context.Context, method string, args any, result any) error {
	id := fmt.Sprint(s.sequence.Add(1))
	ch := make(chan wireMessage, 1)
	s.mu.Lock()
	s.pending[id] = ch
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.pending, id); s.mu.Unlock() }()
	data, err := json.Marshal(wireMessage{ID: id, Method: method, Args: args})
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	_, err = s.input.Write(append(data, '\n'))
	s.writeMu.Unlock()
	if err != nil {
		return err
	}
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case frame := <-ch:
		if frame.Error != "" {
			return errors.New(frame.Error)
		}
		if result != nil {
			return json.Unmarshal(frame.Result, result)
		}
		return nil
	case <-s.done:
		return errors.New("终端程序已退出")
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		s.forceStop()
		return errors.New("终端程序响应超时")
	}
}

// Start 创建或恢复同一主体和内容的终端，不允许更换已冻结目录。
func (s *Supervisor) Start(ctx, lifetime context.Context, subject, owner, root string, create bool) (Started, error) {
	s.operations.Lock()
	defer s.operations.Unlock()
	key := subject + "\x00" + owner
	if !s.Available() {
		return Started{}, errors.New("终端程序不可用")
	}
	s.mu.Lock()
	existing := s.sessions[s.owners[key]]
	s.mu.Unlock()
	if existing != nil {
		if existing.root != root {
			return Started{}, errors.New("终端工作目录已变化")
		}
		return existing.started, nil
	}
	var result Started
	// 创建使用 Core 所有权；浏览器请求取消不能产生无法追踪的孤儿 PTY。
	if err := s.call(context.Background(), "start", map[string]any{"root": root, "ownerKey": key, "createWhenMissing": create}, &result); err != nil {
		return Started{}, err
	}
	session := &ownedSession{subject: subject, owner: owner, root: root, started: result}
	s.mu.Lock()
	s.sessions[result.SessionID] = session
	s.owners[key] = result.SessionID
	session.grace = time.AfterFunc(s.gracePeriod, func() { _ = s.closeOwner(subject, owner, true) })
	s.mu.Unlock()
	stop := context.AfterFunc(lifetime, func() { _ = s.CloseOwner(subject, owner) })
	s.mu.Lock()
	session.stopLifetime = stop
	s.mu.Unlock()
	return result, nil
}

// Attach 返回有界恢复屏幕并替换同一会话的旧控制附件。
func (s *Supervisor) Attach(ctx context.Context, subject, id string) (Snapshot, *Attachment, error) {
	s.operations.Lock()
	defer s.operations.Unlock()
	s.mu.Lock()
	session := s.sessions[id]
	if session == nil || session.subject != subject {
		s.mu.Unlock()
		return Snapshot{}, nil, errors.New("终端会话不可用")
	}
	if session.grace != nil {
		session.grace.Stop()
	}
	if session.attachment != nil {
		session.attachment.cancel()
	}
	life, cancel := context.WithCancel(ctx)
	a := &Attachment{Context: life, cancel: cancel, Events: make(chan []byte, 64)}
	session.attachment = a
	s.mu.Unlock()
	var snapshot Snapshot
	if err := s.call(ctx, "attach", map[string]string{"sessionId": id}, &snapshot); err != nil {
		cancel()
		s.scheduleDetach(session, a, id)
		return Snapshot{}, nil, err
	}
	a.ID = snapshot.AttachmentID
	return snapshot, a, nil
}

func (s *Supervisor) scheduleDetach(session *ownedSession, a *Attachment, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session.attachment != a {
		return
	}
	session.attachment = nil
	a.cancel()
	session.grace = time.AfterFunc(s.gracePeriod, func() { _ = s.closeOwner(session.subject, session.owner, true) })
}

// Detach 释放控制附件并保留六十秒刷新恢复窗口。
func (s *Supervisor) Detach(subject, id string, a *Attachment) {
	s.operations.Lock()
	defer s.operations.Unlock()
	s.mu.Lock()
	session := s.sessions[id]
	valid := session != nil && session.subject == subject && session.attachment == a
	s.mu.Unlock()
	if !valid {
		return
	}
	_ = s.call(context.Background(), "detach", map[string]string{"sessionId": id, "attachmentId": a.ID}, nil)
	s.scheduleDetach(session, a, id)
}

// Control 仅转发当前主体及附件拥有的输入与尺寸操作。
func (s *Supervisor) Control(ctx context.Context, subject, id string, a *Attachment, method string, data string, cols, rows int) error {
	s.mu.Lock()
	session := s.sessions[id]
	valid := session != nil && session.subject == subject && session.attachment == a
	s.mu.Unlock()
	if !valid {
		return errors.New("终端控制已失效")
	}
	args := map[string]any{"sessionId": id, "attachmentId": a.ID}
	switch method {
	case "write":
		if len(data) > 64<<10 {
			return errors.New("终端输入过大")
		}
		args["data"] = data
	case "resize":
		if cols < 2 || cols > 500 || rows < 1 || rows > 300 {
			return errors.New("终端尺寸无效")
		}
		args["size"] = map[string]int{"cols": cols, "rows": rows}
	default:
		return errors.New("终端操作无效")
	}
	return s.call(ctx, method, args, nil)
}

// CloseOwner 明确关闭内容或认证生命周期拥有的交互终端。
func (s *Supervisor) CloseOwner(subject, owner string) error {
	return s.closeOwner(subject, owner, false)
}

func (s *Supervisor) closeOwner(subject, owner string, detachedOnly bool) error {
	s.operations.Lock()
	defer s.operations.Unlock()
	key := subject + "\x00" + owner
	s.mu.Lock()
	id := s.owners[key]
	session := s.sessions[id]
	if session == nil || detachedOnly && session.attachment != nil {
		s.mu.Unlock()
		return nil
	}
	delete(s.owners, key)
	delete(s.sessions, id)
	if session.attachment != nil {
		session.attachment.cancel()
	}
	if session.grace != nil {
		session.grace.Stop()
	}
	if session.stopLifetime != nil {
		session.stopLifetime()
	}
	s.mu.Unlock()
	return s.call(context.Background(), "close", map[string]string{"ownerKey": key}, nil)
}

func (s *Supervisor) forceStop() { s.stop.Do(func() { _ = s.input.Close(); killGroup(s.command) }) }

// Close 有界释放 worker 和其持有的全部 PTY。
func (s *Supervisor) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.call(ctx, "dispose", map[string]any{}, nil)
	_ = s.input.Close()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		s.forceStop()
		select {
		case <-s.done:
		case <-time.After(3 * time.Second):
		}
	}
}

// Available 表达当前 worker 的真实可用性。
func (s *Supervisor) Available() bool {
	if s == nil {
		return false
	}
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}
