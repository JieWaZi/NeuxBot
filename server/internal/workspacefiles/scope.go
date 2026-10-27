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
	// ID 是重启后失效的不透明范围标识。
	ID string `json:"id"`
	// Root 是聊天受管工作区的真实目录。
	Root string `json:"root"`
	// subject 是签发时的认证主体。
	subject string
	// roots 是聊天接纳时授权目录的真实路径快照。
	roots []string
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

// Open 为已验证的 Chat 冻结目录或配对用户明确选择的草稿目录签发主体专属范围。
func (m *Manager) Open(subject, workspace string, additional []string) (Scope, error) {
	if subject == "" {
		return Scope{}, status_error.WorkspaceScopeInvalid
	}
	root, err := canonicalDirectory(workspace)
	if err != nil {
		return Scope{}, err
	}
	roots := []string{root}
	for _, source := range additional {
		actual, err := canonicalDirectory(source)
		if err != nil {
			continue
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
	scope := Scope{ID: uuid.NewString(), Root: root, subject: subject, roots: roots, expires: now.Add(12 * time.Hour)}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, existing := range m.scopes {
		if !now.Before(existing.expires) {
			delete(m.scopes, id)
		}
	}
	for _, existing := range m.scopes {
		if existing.subject == subject && existing.Root == root && strings.Join(existing.roots, "\x00") == strings.Join(roots, "\x00") {
			return existing, nil
		}
	}
	// 句柄有界；达到上限要求客户端释放或重建范围，不能无限累积。
	if len(m.scopes) >= 1024 {
		return Scope{}, status_error.WorkspaceScopeInvalid
	}
	m.scopes[scope.ID] = scope
	return scope, nil
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

// resolve 校验逻辑路径和真实路径；只有冻结范围中的项目入口可以跨越受管根。
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
	if accountPath(actual) {
		return "", "", status_error.WorkspacePathInvalid
	}
	for _, allowed := range scope.roots {
		if within(allowed, actual) {
			return allowed, actual, nil
		}
	}
	return "", "", status_error.WorkspacePathInvalid
}
