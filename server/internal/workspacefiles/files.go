package workspacefiles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

const readLimit = 64 * 1024 * 1024
const writeLimit = 10 * 1024 * 1024

// Resource 描述完整文件及普通外部修改的版本。
type Resource struct {
	// RelativePath 是授权根下的相对路径。
	RelativePath string `json:"relativePath"`
	// Size 是文件字节数。
	Size int64 `json:"size"`
	// Version 是文件内容和修改时间的不可解释指纹。
	Version string `json:"version"`
	// ModifiedAt 是最后修改时间，单位毫秒。
	ModifiedAt int64 `json:"modifiedAt"`
	// MediaType 是扩展名对应的内容类型。
	MediaType string `json:"mediaType"`
}

// Entry 是一个目录条目。
type Entry struct {
	// Name 是条目的展示名称。
	Name string `json:"name"`
	// RelativePath 是所属授权根下的路径。
	RelativePath string `json:"relativePath"`
	// IsDirectory 表明条目是否为目录。
	IsDirectory bool `json:"isDirectory"`
	// Size 是文件字节数，目录省略。
	Size int64 `json:"size,omitempty"`
}

// FileMatch 是搜索返回的文件身份。
type FileMatch struct {
	// Name 是文件展示名称。
	Name string `json:"name"`
	// Path 是授权范围内的真实文件路径。
	Path string `json:"path"`
	// RelativePath 是请求根下的相对路径。
	RelativePath string `json:"relativePath"`
}

func fileError(operation string, err error) error {
	if os.IsNotExist(err) {
		return status_error.WorkspaceResourceNotFound
	}
	if os.IsPermission(err) {
		return status_error.WorkspacePathInvalid
	}
	logx.Errorf("workspace file operation failed: operation=%q error=%v", operation, err)
	return status_error.WorkspaceStorageFailed
}

// openFile 使用 os.Root 再次约束真实访问，避免检查后的目录软链接替换越界。
func openFile(root, target string) (*os.File, error) {
	handle, err := os.OpenRoot(root)
	if err != nil {
		return nil, fileError("open-root", err)
	}
	defer handle.Close()
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return nil, status_error.WorkspacePathInvalid
	}
	file, err := handle.Open(relative)
	if err != nil {
		return nil, fileError("open-file", err)
	}
	return file, nil
}

func readResource(root, target, relative string) (Resource, []byte, error) {
	file, err := openFile(root, target)
	if err != nil {
		return Resource{}, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Resource{}, nil, fileError("stat-file", err)
	}
	if !info.Mode().IsRegular() {
		return Resource{}, nil, status_error.WorkspacePathInvalid
	}
	if info.Size() > readLimit {
		return Resource{}, nil, status_error.WorkspaceResourceTooLarge
	}
	bytes, err := io.ReadAll(io.LimitReader(file, readLimit+1))
	if err != nil {
		return Resource{}, nil, fileError("read-file", err)
	}
	if len(bytes) > readLimit {
		return Resource{}, nil, status_error.WorkspaceResourceTooLarge
	}
	after, err := file.Stat()
	if err != nil {
		return Resource{}, nil, fileError("stat-read-file", err)
	}
	if info.Size() != after.Size() || info.ModTime() != after.ModTime() {
		return Resource{}, nil, status_error.WorkspaceResourceConflict
	}
	hash := sha256.New()
	hash.Write(bytes)
	hash.Write([]byte(strconv.FormatInt(info.ModTime().UnixNano(), 10)))
	media := mime.TypeByExtension(strings.ToLower(filepath.Ext(target)))
	if media == "" {
		media = "application/octet-stream"
	}
	return Resource{RelativePath: relative, Size: int64(len(bytes)), Version: hex.EncodeToString(hash.Sum(nil)), ModifiedAt: info.ModTime().UnixMilli(), MediaType: media}, bytes, nil
}

// Read 返回经过范围与大小限制的完整文件快照。
func (m *Manager) Read(subject, id, root, relative string) (Resource, []byte, error) {
	allowed, target, err := m.resolve(subject, id, root, relative, false)
	if err != nil {
		return Resource{}, nil, err
	}
	return readResource(allowed, target, relative)
}

// List 返回有界且排序稳定的目录内容，隐藏未获准的链接和账号配置。
func (m *Manager) List(subject, id, root, relative string) ([]Entry, error) {
	allowed, target, err := m.resolve(subject, id, root, relative, false)
	if err != nil {
		return nil, err
	}
	file, err := openFile(allowed, target)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries, err := file.ReadDir(10001)
	if err != nil && err != io.EOF {
		return nil, fileError("read-directory", err)
	}
	if len(entries) > 10000 {
		return nil, status_error.WorkspaceResourceTooLarge
	}
	result := make([]Entry, 0, len(entries))
	for _, item := range entries {
		child := filepath.ToSlash(filepath.Join(relative, item.Name()))
		_, actual, err := m.resolve(subject, id, root, child, false)
		if err != nil {
			continue
		}
		info, err := os.Stat(actual)
		if err != nil {
			continue
		}
		result = append(result, Entry{Name: item.Name(), RelativePath: child, IsDirectory: info.IsDir(), Size: info.Size()})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].IsDirectory != result[j].IsDirectory {
			return result[i].IsDirectory
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// Search 只遍历冻结范围内的有界目录，并响应调用取消。
func (m *Manager) Search(ctx context.Context, subject, id, root, query, scopePath string, limit int) ([]FileMatch, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	result := make([]FileMatch, 0)
	pending := []string{scopePath}
	visited := 0
	seen := map[string]bool{}
	for len(pending) > 0 && len(result) < limit && visited < 10000 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dir := pending[0]
		pending = pending[1:]
		_, actual, err := m.resolve(subject, id, root, dir, false)
		if err != nil {
			if visited == 0 {
				return nil, err
			}
			continue
		}
		if seen[actual] {
			continue
		}
		seen[actual] = true
		visited++
		entries, err := m.List(subject, id, root, dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDirectory {
				if entry.Name != ".git" && entry.Name != "node_modules" {
					pending = append(pending, entry.RelativePath)
				}
				continue
			}
			if !strings.Contains(strings.ToLower(entry.RelativePath), strings.ToLower(query)) {
				continue
			}
			_, actual, err := m.resolve(subject, id, root, entry.RelativePath, false)
			if err != nil {
				continue
			}
			result = append(result, FileMatch{Name: entry.Name, Path: actual, RelativePath: entry.RelativePath})
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

// Write 只更新存在的普通文件，先比较版本再以同目录临时文件原子替换。
func (m *Manager) Write(subject, id, root, relative, content, expected string) (Resource, error) {
	if len(content) > writeLimit {
		return Resource{}, status_error.WorkspaceResourceTooLarge
	}
	m.mutations.Lock()
	defer m.mutations.Unlock()
	allowed, target, err := m.resolve(subject, id, root, relative, false)
	if err != nil {
		return Resource{}, err
	}
	current, _, err := readResource(allowed, target, relative)
	if err != nil {
		return Resource{}, err
	}
	if expected == "" || current.Version != expected {
		return Resource{}, status_error.WorkspaceResourceConflict
	}
	handle, err := os.OpenRoot(allowed)
	if err != nil {
		return Resource{}, fileError("open-write-root", err)
	}
	defer handle.Close()
	rel, _ := filepath.Rel(allowed, target)
	temporary := filepath.Join(filepath.Dir(rel), ".ally-write-"+strconv.FormatInt(int64(os.Getpid()), 10)+"-"+current.Version[:12])
	file, err := handle.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Resource{}, fileError("create-write", err)
	}
	defer handle.Remove(temporary)
	if _, err = file.WriteString(content); err != nil {
		file.Close()
		return Resource{}, fileError("write-file", err)
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return Resource{}, fileError("sync-file", err)
	}
	if err = file.Close(); err != nil {
		return Resource{}, fileError("close-write", err)
	}
	latest, _, err := readResource(allowed, target, relative)
	if err != nil {
		return Resource{}, err
	}
	if latest.Version != expected {
		return Resource{}, status_error.WorkspaceResourceConflict
	}
	info, err := handle.Stat(rel)
	if err != nil {
		return Resource{}, fileError("stat-write", err)
	}
	if err = handle.Chmod(temporary, info.Mode().Perm()); err != nil {
		return Resource{}, fileError("chmod-write", err)
	}
	if err = handle.Rename(temporary, rel); err != nil {
		return Resource{}, fileError("replace-file", err)
	}
	written, _, err := readResource(allowed, target, relative)
	return written, err
}

// Rename 重命名文件，拒绝覆盖和过期的编辑版本。
func (m *Manager) Rename(subject, id, root, relative, name, expected string) (Resource, error) {
	if name == "" || name == "." || strings.ContainsAny(name, "/\\\x00") || name == ".." {
		return Resource{}, status_error.WorkspacePathInvalid
	}
	m.mutations.Lock()
	defer m.mutations.Unlock()
	allowed, target, err := m.resolve(subject, id, root, relative, false)
	if err != nil {
		return Resource{}, err
	}
	current, _, err := readResource(allowed, target, relative)
	if err != nil {
		return Resource{}, err
	}
	if expected == "" || current.Version != expected {
		return Resource{}, status_error.WorkspaceResourceConflict
	}
	next := filepath.ToSlash(filepath.Join(filepath.Dir(relative), name))
	nextAllowed, nextTarget, err := m.resolve(subject, id, root, next, true)
	if err != nil {
		return Resource{}, err
	}
	if nextAllowed != allowed {
		return Resource{}, status_error.WorkspacePathInvalid
	}
	handle, err := os.OpenRoot(allowed)
	if err != nil {
		return Resource{}, fileError("open-rename-root", err)
	}
	defer handle.Close()
	rel, _ := filepath.Rel(allowed, target)
	nextRel, _ := filepath.Rel(allowed, nextTarget)
	if _, err = handle.Lstat(nextRel); err == nil {
		return Resource{}, status_error.WorkspaceResourceConflict
	} else if !os.IsNotExist(err) {
		return Resource{}, fileError("stat-rename", err)
	}
	if err = handle.Rename(rel, nextRel); err != nil {
		return Resource{}, fileError("rename-file", err)
	}
	renamed, _, err := readResource(allowed, nextTarget, next)
	return renamed, err
}

// Delete 仅删除授权范围内的普通文件，目录删除由独立交互负责。
func (m *Manager) Delete(subject, id, root, relative, expected string) error {
	m.mutations.Lock()
	defer m.mutations.Unlock()
	allowed, target, err := m.resolve(subject, id, root, relative, false)
	if err != nil {
		return err
	}
	current, _, err := readResource(allowed, target, relative)
	if err != nil {
		return err
	}
	if expected == "" || current.Version != expected {
		return status_error.WorkspaceResourceConflict
	}
	handle, err := os.OpenRoot(allowed)
	if err != nil {
		return fileError("open-delete-root", err)
	}
	defer handle.Close()
	rel, _ := filepath.Rel(allowed, target)
	info, err := handle.Stat(rel)
	if err != nil {
		return fileError("stat-delete", err)
	}
	if !info.Mode().IsRegular() {
		return status_error.WorkspacePathInvalid
	}
	if err = handle.Remove(rel); err != nil {
		return fileError("delete-file", err)
	}
	return nil
}
