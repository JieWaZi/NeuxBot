// Package workspacefiles 管理冻结工作范围内的文件访问，不拥有聊天或项目业务状态。
package workspacefiles

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/google/uuid"
)

// Scope 是绑定认证主体的临时文件范围。
type Scope struct {
	// 范围标识
	ID string `json:"id"`
	// 工作目录
	Root string `json:"root"`
	// subject 是签发时的认证主体。
	subject string
	// roots 是聊天接纳时授权目录的真实路径快照。
	roots []string
	// chatID 区分共享 cwd 中不同 Chat 的私有状态。
	chatID string
	// chatRoot 仅供附件和隐藏记录操作使用，不是通用文件授权根。
	chatRoot string
	// expires 是范围句柄的失效时间。
	expires time.Time
}

// Manager 保存有界的临时范围，并串行化文件修改以检查预期版本。
type Manager struct {
	// mu 保护范围签发和查找。
	mu sync.Mutex
	// scopes 保存当前进程的范围句柄。
	scopes map[string]Scope
	// mutations 防止本服务的检查与写入交错。
	mutations sync.Mutex
	// watchMu 保护物理监听的引用计数与信号分发，不阻塞授权句柄查询。
	watchMu sync.Mutex
	// watches 按已核验的真实路径共享底层资源，不保存文件内容。
	watches map[physicalWatchKey]*physicalWatch
}

// New 创建不启动后台任务的工作区文件管理器。
func New() *Manager { return &Manager{scopes: make(map[string]Scope)} }

// Open 为配对用户明确选择的真实草稿目录签发主体专属范围。
func (m *Manager) Open(subject, workspace string, additional []string) (Scope, error) {
	return m.open(subject, workspace, additional, "", "")
}

// OpenChat 将真实执行范围和指定 Chat 的私有文件身份一起冻结。
func (m *Manager) OpenChat(subject, chatID, workspace, privateRoot string, additional []string) (Scope, error) {
	if chatID == "" || subject == "" {
		return Scope{}, status_error.WorkspaceScopeInvalid
	}
	info, err := os.Lstat(privateRoot)
	if err != nil {
		return Scope{}, fileError("stat-chat-root", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Scope{}, status_error.WorkspacePathInvalid
	}
	private, err := canonicalDirectory(privateRoot)
	if err != nil {
		return Scope{}, err
	}
	h, err := os.OpenRoot(private)
	if err != nil {
		return Scope{}, fileError("open-chat-root", err)
	}
	defer h.Close()
	if err = h.MkdirAll("attachments", 0700); err != nil {
		return Scope{}, fileError("create-chat-attachments", err)
	}
	info, err = h.Lstat("attachments")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Scope{}, status_error.WorkspacePathInvalid
	}
	return m.open(subject, workspace, additional, chatID, private)
}

func (m *Manager) open(subject, workspace string, additional []string, chatID, private string) (Scope, error) {
	if subject == "" {
		return Scope{}, status_error.WorkspaceScopeInvalid
	}
	root, err := canonicalDirectory(workspace)
	if err != nil {
		return Scope{}, err
	}
	roots := []string{root}
	sources := append([]string(nil), additional...)
	if private != "" {
		sources = append(sources, filepath.Join(private, "attachments"))
	}
	for _, source := range sources {
		actual, err := canonicalDirectory(source)
		if err != nil {
			continue
		}
		if private != "" && source == filepath.Join(private, "attachments") && actual != source {
			return Scope{}, status_error.WorkspacePathInvalid
		}
		exists := false
		for _, allowed := range roots {
			if actual == allowed {
				exists = true
				break
			}
		}
		if !exists {
			roots = append(roots, actual)
		}
	}
	now := time.Now()
	scope := Scope{ID: uuid.NewString(), Root: root, subject: subject, roots: roots, chatID: chatID, chatRoot: private, expires: now.Add(12 * time.Hour)}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, existing := range m.scopes {
		if !now.Before(existing.expires) {
			delete(m.scopes, id)
		}
	}
	for _, existing := range m.scopes {
		if existing.subject == subject && existing.Root == root && existing.chatID == chatID && existing.chatRoot == private && strings.Join(existing.roots, "\x00") == strings.Join(roots, "\x00") {
			return existing, nil
		}
	}
	if len(m.scopes) >= 1024 {
		return Scope{}, status_error.WorkspaceScopeInvalid
	}
	m.scopes[scope.ID] = scope
	return scope, nil
}

// allows 拒绝通用根覆盖 Chat 私有元数据，即使显式项目根是其祖先。
func (s Scope) allows(target string) bool {
	if accountPath(target) {
		return false
	}
	if s.chatRoot != "" && within(filepath.Dir(s.chatRoot), target) && !within(filepath.Join(s.chatRoot, "attachments"), target) {
		return false
	}
	for _, root := range s.roots {
		if within(root, target) {
			return true
		}
	}
	return false
}

// Close 释放当前主体的范围句柄。
func (m *Manager) Close(subject, id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if scope, ok := m.scopes[id]; ok && scope.subject == subject {
		delete(m.scopes, id)
	}
}

func (m *Manager) scope(subject, id string) (Scope, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	scope, ok := m.scopes[id]
	if !ok || subject == "" || scope.subject != subject || !time.Now().Before(scope.expires) {
		return Scope{}, status_error.WorkspaceScopeInvalid
	}
	return scope, nil
}

func canonicalDirectory(name string) (string, error) {
	if name == "" {
		return "", status_error.WorkspacePathInvalid
	}
	actual, err := filepath.EvalSymlinks(name)
	if err != nil {
		return "", fileError("resolve-root", err)
	}
	actual, err = filepath.Abs(actual)
	if err != nil {
		return "", fileError("absolute-root", err)
	}
	info, err := os.Stat(actual)
	if err != nil {
		return "", fileError("stat-root", err)
	}
	if !info.IsDir() {
		return "", status_error.WorkspacePathInvalid
	}
	return actual, nil
}

func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func safeRelative(value string) bool {
	if filepath.IsAbs(value) || strings.ContainsAny(value, "\x00\\") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

func accountPath(value string) bool {
	parts := strings.Split(filepath.ToSlash(value), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "runtime" && parts[i+1] == "accounts" {
			return true
		}
	}
	return false
}

// logicalAttachmentRoot 将未指定显式根的会话附件引用限定到当前 Chat。
func (s Scope) logicalAttachmentRoot(requestedRoot, relative string) string {
	if requestedRoot == "" && s.chatRoot != "" && strings.HasPrefix(relative, "attachments/") {
		return filepath.Join(s.chatRoot, "attachments")
	}
	return ""
}

// resolve 校验逻辑路径和真实路径；只允许冻结的真实目录和本 Chat 附件。
func (m *Manager) resolve(subject, id, requestedRoot, relative string, missing bool) (string, string, error) {
	scope, err := m.scope(subject, id)
	if err != nil {
		return "", "", err
	}
	if !safeRelative(relative) || accountPath(relative) {
		return "", "", status_error.WorkspacePathInvalid
	}
	root := scope.Root
	if requestedRoot != "" {
		actual, err := canonicalDirectory(requestedRoot)
		if err != nil {
			return "", "", err
		}
		allowed := false
		for _, item := range scope.roots {
			if actual == item {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", "", status_error.WorkspacePathInvalid
		}
		root = actual
	}
	target := filepath.Join(root, filepath.FromSlash(relative))
	attachmentRoot := scope.logicalAttachmentRoot(requestedRoot, relative)
	if attachmentRoot != "" {
		target = filepath.Join(scope.chatRoot, filepath.FromSlash(relative))
	}
	actual, err := filepath.EvalSymlinks(target)
	if os.IsNotExist(err) && missing {
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(target))
		if parentErr != nil {
			return "", "", fileError("resolve-parent", parentErr)
		}
		actual = filepath.Join(parent, filepath.Base(target))
		err = nil
	}
	if err != nil {
		return "", "", fileError("resolve-file", err)
	}
	if !scope.allows(actual) {
		return "", "", status_error.WorkspacePathInvalid
	}
	if attachmentRoot != "" {
		if !within(attachmentRoot, actual) {
			return "", "", status_error.WorkspacePathInvalid
		}
		return attachmentRoot, actual, nil
	}
	for _, allowed := range scope.roots {
		if within(allowed, actual) {
			return allowed, actual, nil
		}
	}
	return "", "", status_error.WorkspacePathInvalid
}
