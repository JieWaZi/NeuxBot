package workspacefiles

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"os"
	"path/filepath"
	"strings"
)

func validName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00")
}

// checkDirectoryMutation 防止通过修改父目录间接删除或移动账号配置。
func checkDirectoryMutation(target string) error {
	visited := 0
	err := filepath.WalkDir(target, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		visited++
		if visited > 10000 {
			return status_error.WorkspaceResourceTooLarge
		}
		if accountPath(name) {
			return status_error.WorkspacePathInvalid
		}
		return nil
	})
	if err == status_error.WorkspacePathInvalid || err == status_error.WorkspaceResourceTooLarge {
		return err
	}
	if err != nil {
		return fileError("check-directory-mutation", err)
	}
	return nil
}

// RenameDirectory 重命名目录，禁止修改授权根与覆盖已有目标。
func (m *Manager) RenameDirectory(subject, id, root, relative, name string) (string, error) {
	if !validName(name) {
		return "", status_error.WorkspacePathInvalid
	}
	m.mutations.Lock()
	defer m.mutations.Unlock()
	allowed, target, err := m.resolve(subject, id, root, relative, false)
	if err != nil {
		return "", err
	}
	if target == allowed {
		return "", status_error.WorkspacePathInvalid
	}
	if err := checkDirectoryMutation(target); err != nil {
		return "", err
	}
	h, err := os.OpenRoot(allowed)
	if err != nil {
		return "", fileError("open-directory-root", err)
	}
	defer h.Close()
	rel, _ := filepath.Rel(allowed, target)
	info, err := h.Stat(rel)
	if err != nil {
		return "", fileError("stat-directory", err)
	}
	if !info.IsDir() {
		return "", status_error.WorkspacePathInvalid
	}
	next := filepath.Join(filepath.Dir(rel), name)
	if _, err = h.Lstat(next); err == nil {
		return "", status_error.WorkspaceResourceConflict
	} else if !os.IsNotExist(err) {
		return "", fileError("stat-directory-target", err)
	}
	if err = h.Rename(rel, next); err != nil {
		return "", fileError("rename-directory", err)
	}
	return filepath.ToSlash(filepath.Join(filepath.Dir(relative), name)), nil
}

// DeleteDirectory 删除范围内目录树，os.Root 防止遍历软链接逃离范围。
func (m *Manager) DeleteDirectory(subject, id, root, relative string) error {
	m.mutations.Lock()
	defer m.mutations.Unlock()
	allowed, target, err := m.resolve(subject, id, root, relative, false)
	if err != nil {
		return err
	}
	if target == allowed {
		return status_error.WorkspacePathInvalid
	}
	if err := checkDirectoryMutation(target); err != nil {
		return err
	}
	h, err := os.OpenRoot(allowed)
	if err != nil {
		return fileError("open-directory-root", err)
	}
	defer h.Close()
	rel, _ := filepath.Rel(allowed, target)
	info, err := h.Stat(rel)
	if err != nil {
		return fileError("stat-directory", err)
	}
	if !info.IsDir() {
		return status_error.WorkspacePathInvalid
	}
	if err = h.RemoveAll(rel); err != nil {
		return fileError("delete-directory", err)
	}
	return nil
}

// Import 以不覆盖已有文件的方式导入一个有界上传。
func (m *Manager) Import(subject, id, root, directory, name string, bytes []byte) (string, error) {
	if !validName(name) {
		return "", status_error.WorkspacePathInvalid
	}
	if len(bytes) > readLimit {
		return "", status_error.WorkspaceResourceTooLarge
	}
	m.mutations.Lock()
	defer m.mutations.Unlock()
	relative := filepath.ToSlash(filepath.Join(directory, name))
	allowed, target, err := m.resolve(subject, id, root, relative, true)
	if err != nil {
		return "", err
	}
	h, err := os.OpenRoot(allowed)
	if err != nil {
		return "", fileError("open-import-root", err)
	}
	defer h.Close()
	rel, _ := filepath.Rel(allowed, target)
	f, err := h.OpenFile(rel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(err) {
		return "", status_error.WorkspaceResourceConflict
	}
	if err != nil {
		return "", fileError("create-import", err)
	}
	_, err = f.Write(bytes)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		h.Remove(rel)
		return "", fileError("write-import", err)
	}
	return relative, nil
}

// Resolution 是工作区文件引用的解析结果。
type Resolution struct {
	// Status 表示解析状态。
	Status string `json:"status"`
	// CanonicalPath 是成功解析的真实路径。
	CanonicalPath string `json:"canonicalPath,omitempty"`
	// BaseDirectory 是范围内解析基准目录。
	BaseDirectory string `json:"baseDirectory,omitempty"`
	// WorkspaceRoot 是当前受管根目录。
	WorkspaceRoot string `json:"workspaceRoot,omitempty"`
	// SourceRoots 是签发时冻结的项目目录。
	SourceRoots []string `json:"sourceRoots,omitempty"`
}

// ResolveReference 只使用句柄中已有的授权目录解析引用，不接受客户端扩大范围。
func (m *Manager) ResolveReference(subject, id, input, base string) (Resolution, error) {
	scope, err := m.scope(subject, id)
	if err != nil {
		return Resolution{}, err
	}
	input = strings.TrimSpace(input)
	if input == "~" || strings.HasPrefix(input, "~/") {
		if home, e := os.UserHomeDir(); e == nil {
			if input == "~" {
				input = home
			} else {
				input = filepath.Join(home, strings.TrimPrefix(input, "~/"))
			}
		}
	}
	if strings.ContainsRune(input, 0) {
		return Resolution{Status: "unavailable"}, nil
	}
	allowed := func(p string) bool {
		if accountPath(p) {
			return false
		}
		for _, r := range scope.roots {
			if within(r, p) {
				return true
			}
		}
		return false
	}
	if base == "" {
		base = scope.Root
	} else {
		base, err = canonicalDirectory(base)
		if err != nil {
			return Resolution{Status: "unavailable"}, nil
		}
		if !allowed(base) {
			return Resolution{Status: "outside_scope"}, nil
		}
	}
	candidates := []string{}
	try := func(p string) {
		actual, e := filepath.EvalSymlinks(p)
		if e != nil {
			return
		}
		for _, c := range candidates {
			if c == actual {
				return
			}
		}
		candidates = append(candidates, actual)
	}
	if filepath.IsAbs(input) {
		try(input)
	} else {
		try(filepath.Join(base, input))
		if len(candidates) == 0 {
			for _, r := range scope.roots {
				try(filepath.Join(r, input))
			}
		}
	}
	if len(candidates) == 0 {
		return Resolution{Status: "missing"}, nil
	}
	if len(candidates) > 1 {
		return Resolution{Status: "ambiguous"}, nil
	}
	target := candidates[0]
	if !allowed(target) {
		return Resolution{Status: "outside_scope"}, nil
	}
	info, err := os.Stat(target)
	if err != nil {
		return Resolution{Status: "unavailable"}, nil
	}
	state := "resolved"
	if info.IsDir() {
		state = "directory"
	} else if !info.Mode().IsRegular() {
		return Resolution{Status: "unavailable"}, nil
	}
	return Resolution{Status: state, CanonicalPath: target, BaseDirectory: base, WorkspaceRoot: scope.Root, SourceRoots: append([]string{}, scope.roots[1:]...)}, nil
}
