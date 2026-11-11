package changes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	configformat "github.com/go-git/go-git/v5/plumbing/format/config"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/sirupsen/logrus"
)

type boundedOutput struct {
	// buffer 保存没有被截断的输出前缀。
	buffer bytes.Buffer
	// limit 限制子进程输出的内存。
	limit int
	// exceeded 标记输出不完整，不能用于生成事实。
	exceeded bool
}

func (b *boundedOutput) Write(data []byte) (int, error) {
	if len(data) > b.limit-b.buffer.Len() {
		b.exceeded = true
		return 0, io.ErrShortBuffer
	}
	return b.buffer.Write(data)
}

func gitEnvironment() []string {
	values := []string{}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(key, "GIT_") || key == "LC_ALL" {
			continue
		}
		values = append(values, value)
	}
	return append(values, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_CONFIG_COUNT=0", "GIT_ATTR_NOSYSTEM=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_REPLACE_OBJECTS=1", "LC_ALL=C")
}

func runGit(ctx context.Context, executable, cwd, repository, indexPath string, limits Limits, args ...string) ([]byte, error) {
	return runGitInput(ctx, executable, cwd, repository, indexPath, limits, nil, args...)
}

func runGitInput(ctx context.Context, executable, cwd, repository, indexPath string, limits Limits, input io.Reader, args ...string) ([]byte, error) {
	argv := []string{"-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-c", "core.splitIndex=false", "-c", "core.untrackedCache=false", "-c", "core.attributesFile=" + os.DevNull, "-c", "core.autocrlf=false", "-c", "protocol.allow=never", "-c", "core.fsync=all", "-c", "core.fsyncMethod=fsync"}
	if repository != "" {
		argv = append(argv, "--git-dir="+repository)
	}
	command := exec.CommandContext(ctx, executable, append(argv, args...)...)
	command.Dir, command.Env, command.WaitDelay = cwd, gitEnvironment(), time.Second
	command.Stdin = input
	if indexPath != "" {
		command.Env = append(command.Env, "GIT_INDEX_FILE="+indexPath)
	}
	output, diagnostic := &boundedOutput{limit: limits.MaxOutputBytes}, &boundedOutput{limit: 16 << 10}
	command.Stdout, command.Stderr = output, diagnostic
	if err := command.Run(); err != nil {
		logrus.WithError(err).WithField("operation", "chat_changes_git").WithField("stderr", diagnostic.buffer.String()).Warn("private snapshot Git command failed")
		if ctx.Err() != nil {
			return nil, status_error.OperationCanceled
		}
		if output.exceeded {
			return nil, status_error.WorkspaceResourceTooLarge
		}
		return nil, status_error.WorkspaceGitFailed
	}
	if output.exceeded {
		return nil, status_error.WorkspaceResourceTooLarge
	}
	return output.buffer.Bytes(), nil
}

func resolveGit(ctx context.Context, selected string, limits Limits) (string, string) {
	if selected == "" {
		var err error
		selected, err = exec.LookPath("git")
		if err != nil {
			return "", "git_unavailable"
		}
	}
	resolved, err := filepath.Abs(selected)
	if err != nil {
		return "", "git_unavailable"
	}
	if _, err := runGit(ctx, resolved, os.TempDir(), "", "", limits, "--version"); err != nil {
		return "", captureReason(ctx, "git_unavailable")
	}
	return resolved, ""
}

func repositoryPath(owned string) string { return filepath.Join(base(owned), "repository.git") }
func objectStore(owned string) *filesystem.Storage {
	return filesystem.NewStorage(osfs.New(repositoryPath(owned)), cache.NewObjectLRU(8<<20))
}

func ensureRepository(ctx context.Context, owned, git string, limits Limits) error {
	path := repositoryPath(owned)
	if info, err := os.Lstat(path); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return status_error.WorkspaceScopeInvalid
		}
		return nil
	} else if !os.IsNotExist(err) {
		return storageError("repository", err)
	}
	_, err := runGit(ctx, git, base(owned), "", "", limits, "init", "--bare", "--template=", path)
	if err != nil {
		return err
	}
	if err := syncRepositoryLayout(owned); err != nil {
		return storageError("sync_repository_layout", err)
	}
	return nil
}

func syncRepositoryLayout(owned string) error {
	root := repositoryPath(owned)
	for _, name := range []string{"HEAD", "config"} {
		file, err := os.Open(filepath.Join(root, name))
		if err != nil {
			return err
		}
		err = file.Sync()
		file.Close()
		if err != nil {
			return err
		}
	}
	for _, directory := range []string{filepath.Join(root, "objects", "pack"), filepath.Join(root, "objects"), filepath.Join(root, "refs"), root, base(owned)} {
		if err := syncDir(directory); err != nil {
			return err
		}
	}
	return nil
}

func locateRepository(path string) string {
	for current := path; ; current = filepath.Dir(current) {
		if info, err := os.Lstat(filepath.Join(current, ".git")); err == nil && (info.IsDir() || info.Mode().IsRegular()) {
			return current
		}
		if filepath.Dir(current) == current {
			return ""
		}
	}
}

func hasRepositoryBoundary(parent, child string) bool {
	for current := child; current != parent && inside(parent, current); current = filepath.Dir(current) {
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return true
		}
	}
	if repository := locateRepository(parent); repository != "" {
		handle, err := os.OpenRoot(repository)
		if err != nil {
			return true
		}
		defer handle.Close()
		data, err := readIgnore(handle, ".gitmodules", 1<<20)
		if err != nil {
			return true
		}
		modules := config.NewModules()
		if modules.Unmarshal(data) == nil {
			for _, module := range modules.Submodules {
				if filepath.IsLocal(module.Path) {
					boundary := filepath.Join(repository, filepath.FromSlash(module.Path))
					if inside(parent, boundary) && inside(boundary, child) {
						return true
					}
				}
			}
		}
	}
	return false
}

func gitMetadata(root string) string {
	path := filepath.Join(root, ".git")
	info, err := os.Lstat(path)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return path
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return ""
	}
	handle, err := os.OpenRoot(root)
	if err != nil {
		return ""
	}
	defer handle.Close()
	data, err := readIgnore(handle, ".git", 4096)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "gitdir: ") {
		return ""
	}
	target := strings.TrimSpace(strings.TrimPrefix(line, "gitdir: "))
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	return filepath.Clean(target)
}

func excludedPath(relative string) bool {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for i, part := range parts {
		if part == ".git" || part == "workspace-owner.json" {
			return true
		}
		if part == "runtime" && i+1 < len(parts) && parts[i+1] == "accounts" {
			return true
		}
	}
	return false
}

type collector struct {
	// capture 保存冻结配置和归属。
	capture *Capture
	// store 仅写入应用私有对象目录。
	store *filesystem.Storage
	// visited 跨根累计已枚举条目数。
	visited int
	// readBytes 跨根累计实际读取字节数。
	readBytes int64
	// pathBytes 限制清单路径本身占用的空间，避免超长路径绕过条目预算。
	pathBytes int
	// ignoreBytes 限制忽略文件本身的累计读取和解析成本。
	ignoreBytes int64
	// storedBytes 是同 Chat 已去重的逻辑对象字节数。
	storedBytes int64
	// stream 暂存有界的 fast-import blob 数据，不执行项目过滤器。
	stream *os.File
	// buffered 对当前导入批次按 blob 身份去重。
	buffered map[string]bool
	// existing 复用配额枚举得到的对象身份，避免逐文件重复访问对象目录。
	existing map[string]bool
	// exhausted 表示本阶段不能再增加覆盖。
	exhausted bool
}

func (c *Capture) collect(ctx context.Context, phase string) ([]snapshot, error) {
	worker := &collector{capture: c, store: objectStore(c.owned), existing: map[string]bool{}}
	defer worker.discardStream()
	iterator, err := worker.store.IterEncodedObjects(plumbing.AnyObject)
	if err != nil {
		return nil, storageError("object_usage", err)
	}
	err = iterator.ForEach(func(value plumbing.EncodedObject) error {
		if ctx.Err() != nil {
			return status_error.OperationCanceled
		}
		worker.storedBytes += value.Size()
		worker.existing[value.Hash().String()] = true
		return nil
	})
	iterator.Close()
	if err != nil {
		return nil, storageError("object_usage", err)
	}
	result := make([]snapshot, 0, len(c.record.Summary.Roots))
	for _, root := range c.record.Summary.Roots {
		if ctx.Err() != nil {
			return nil, status_error.OperationCanceled
		}
		value, err := worker.collectRoot(ctx, root, phase)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func appendPatterns(patterns []gitignore.Pattern, data []byte, domain []string) []gitignore.Pattern {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, gitignore.ParsePattern(line, domain))
	}
	return patterns
}

func readIgnore(root *os.Root, name string, maximum int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, status_error.WorkspaceScopeInvalid
	}
	if info.Size() > maximum {
		return nil, status_error.WorkspaceResourceTooLarge
	}
	f, err := openRegular(root, name, info)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maximum+1))
	if int64(len(data)) > maximum {
		return nil, status_error.WorkspaceResourceTooLarge
	}
	return data, err
}

func (worker *collector) readPatterns(root *os.Root, path string) ([]byte, error) {
	data, err := readIgnore(root, path, int64(worker.capture.options.Limits.MaxOutputBytes)-worker.ignoreBytes)
	worker.ignoreBytes += int64(len(data))
	return data, err
}

func (worker *collector) initialPatterns(ctx context.Context, root Root) ([]gitignore.Pattern, string, map[string]string, error) {
	patterns := []gitignore.Pattern{}
	prefix := ""
	tracked := map[string]string{}
	if root.RepositoryRoot != "" {
		repo, err := os.OpenRoot(root.RepositoryRoot)
		if err != nil {
			return nil, "", nil, err
		}
		defer repo.Close()
		metadata := gitMetadata(root.RepositoryRoot)
		if metadata != "" {
			meta, err := os.OpenRoot(metadata)
			if err != nil {
				return nil, "", nil, err
			}
			common, readErr := readIgnore(meta, "commondir", 4096)
			if readErr != nil {
				meta.Close()
				return nil, "", nil, readErr
			}
			if len(common) > 0 {
				commonPath := strings.TrimSpace(string(common))
				if !filepath.IsAbs(commonPath) {
					commonPath = filepath.Join(metadata, commonPath)
				}
				meta.Close()
				meta, err = os.OpenRoot(commonPath)
				if err != nil {
					return nil, "", nil, err
				}
			}
			tracked, err = worker.trackedPaths(ctx, root, metadata, meta)
			if err != nil {
				meta.Close()
				return nil, "", nil, err
			}
			exclude, readErr := worker.readPatterns(meta, filepath.Join("info", "exclude"))
			meta.Close()
			if readErr != nil {
				return nil, "", nil, readErr
			}
			patterns = appendPatterns(patterns, exclude, nil)
		}
		if root.Path != root.RepositoryRoot {
			rel, _ := filepath.Rel(root.RepositoryRoot, root.Path)
			prefix = filepath.ToSlash(rel)
			current := "."
			domain := []string{}
			for _, part := range strings.Split(prefix, "/") {
				data, err := worker.readPatterns(repo, filepath.Join(current, ".gitignore"))
				if err != nil {
					return nil, "", nil, err
				}
				patterns = appendPatterns(patterns, data, append([]string(nil), domain...))
				current = filepath.Join(current, part)
				domain = append(domain, part)
			}
		}
	}
	for _, pattern := range worker.capture.options.IgnorePatterns {
		domain := []string{}
		if prefix != "" {
			domain = strings.Split(prefix, "/")
		}
		patterns = append(patterns, gitignore.ParsePattern(pattern, domain))
	}
	return patterns, prefix, tracked, nil
}

// trackedPaths 只把有界 index/shared-index 数据交给私有 Git，不加载原仓库配置或 include。
func (worker *collector) trackedPaths(ctx context.Context, scope Root, metadata string, common *os.Root) (map[string]string, error) {
	tracked := map[string]string{}
	source, err := os.OpenRoot(metadata)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	limit := int64(worker.capture.options.Limits.MaxOutputBytes)
	indexData, err := readIgnore(source, "index", limit)
	if err != nil {
		return nil, err
	}
	if indexData == nil {
		return tracked, nil
	}
	limit -= int64(len(indexData))
	configData, err := readIgnore(common, "config", limit)
	if err != nil {
		return nil, err
	}
	limit -= int64(len(configData))
	format := "sha1"
	if len(configData) > 0 {
		parsed := configformat.New()
		if err := configformat.NewDecoder(bytes.NewReader(configData)).Decode(parsed); err != nil {
			return nil, err
		}
		if selected := parsed.Section("extensions").Option("objectformat"); selected != "" {
			if selected != "sha1" && selected != "sha256" {
				return nil, status_error.WorkspaceGitFailed
			}
			format = selected
		}
	}
	directory, err := os.MkdirTemp(filepath.Dir(recordPath(worker.capture.owned, worker.capture.record.Summary.ID)), "pending-source-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	if _, err := runGit(ctx, worker.capture.options.GitExecutable, base(worker.capture.owned), "", "", worker.capture.options.Limits, "init", "--bare", "--template=", "--object-format="+format, directory); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(directory, "index"), indexData, 0600); err != nil {
		return nil, err
	}
	listing, err := source.Open(".")
	if err != nil {
		return nil, err
	}
	defer listing.Close()
	enumerated := 0
	for {
		entries, readErr := listing.ReadDir(128)
		for _, entry := range entries {
			enumerated++
			if enumerated > worker.capture.options.Limits.MaxEntries {
				return nil, status_error.WorkspaceResourceTooLarge
			}
			if ctx.Err() != nil {
				return nil, status_error.OperationCanceled
			}
			name := entry.Name()
			if !strings.HasPrefix(name, "sharedindex.") {
				continue
			}
			suffix := strings.TrimPrefix(name, "sharedindex.")
			if len(suffix) != 40 && len(suffix) != 64 {
				continue
			}
			if _, err := hex.DecodeString(suffix); err != nil {
				continue
			}
			data, err := readIgnore(source, name, limit)
			if err != nil {
				return nil, err
			}
			limit -= int64(len(data))
			if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
				return nil, err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	args := []string{"ls-files", "--cached", "--stage", "-z"}
	if scope.Path != scope.RepositoryRoot {
		relative, _ := filepath.Rel(scope.RepositoryRoot, scope.Path)
		args = append(args, "--", ":(top,literal)"+filepath.ToSlash(relative))
	}
	data, err := runGit(ctx, worker.capture.options.GitExecutable, base(worker.capture.owned), directory, "", worker.capture.options.Limits, args...)
	if err != nil {
		return nil, err
	}
	for _, raw := range bytes.Split(data, []byte{0}) {
		header, name, ok := strings.Cut(string(raw), "\t")
		fields := strings.Fields(header)
		if ok && len(fields) == 3 {
			tracked[name] = fields[0]
			for parent := path.Dir(name); parent != "." && parent != "/"; parent = path.Dir(parent) {
				if tracked[parent] == "" {
					tracked[parent] = "directory"
				}
			}
		}
	}
	return tracked, nil
}

func (worker *collector) collectRoot(ctx context.Context, root Root, phase string) (snapshot, error) {
	result := snapshot{RootID: root.ID, Entries: map[string]entry{}}
	directory := root.Path
	if root.File {
		directory = filepath.Dir(root.Path)
	}
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil || canonical != directory {
		result.Missing = append(result.Missing, Coverage{Path: ".", Reason: "root_unavailable"})
		return result, nil
	}
	handle, err := os.OpenRoot(directory)
	if err != nil {
		result.Missing = append(result.Missing, Coverage{Path: ".", Reason: "root_unreadable"})
		return result, nil
	}
	defer handle.Close()
	currentInfo, err := handle.Stat(".")
	if err != nil || !os.SameFile(worker.capture.rootInfo[root.ID], currentInfo) {
		result.Missing = append(result.Missing, Coverage{Path: ".", Reason: "root_replaced"})
		return result, nil
	}
	var patterns []gitignore.Pattern
	var prefix string
	var tracked map[string]string
	if !root.File {
		patterns, prefix, tracked, err = worker.initialPatterns(ctx, root)
		if err != nil {
			result.Missing = append(result.Missing, Coverage{Path: ".", Reason: "ignore_rules_unreadable"})
			return result, nil
		}
	}
	if worker.exhausted {
		result.Missing = append(result.Missing, Coverage{Path: ".", Reason: "capture_budget"})
		return result, nil
	}
	visit := func(path string, item fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		encodedPath, _ := json.Marshal(path)
		worker.pathBytes += len(encodedPath)
		if worker.pathBytes > worker.capture.options.Limits.MaxOutputBytes {
			worker.exhausted = true
			result.Missing = append(result.Missing, Coverage{Path: ".", Reason: "manifest_budget"})
			return fs.SkipAll
		}
		if walkErr != nil {
			result.Missing = append(result.Missing, Coverage{Path: path, Reason: "unreadable"})
			return nil
		}
		full := root.absolutePath(path)
		if path != "." {
			reason := ""
			if excludedPath(path) || root.File && excludedPath(filepath.ToSlash(full)) || inside(base(worker.capture.owned), full) || workspace.IsInternalExecutionPath(worker.capture.options.ManagedSpace, full) {
				reason = "internal"
			}
			for _, excluded := range worker.capture.options.ExcludePaths {
				if filepath.IsAbs(excluded) && inside(filepath.Clean(excluded), full) {
					reason = "excluded"
					break
				}
			}
			if reason != "" {
				result.Boundaries = append(result.Boundaries, Coverage{Path: path, Reason: reason})
				if item.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if item.IsDir() {
				if _, err := handle.Lstat(filepath.Join(path, ".git")); err == nil {
					result.Boundaries = append(result.Boundaries, Coverage{Path: path, Reason: "nested_repository"})
					return fs.SkipDir
				}
			}
			matchPath := path
			if prefix != "" {
				matchPath = prefix + "/" + path
			}
			if tracked[matchPath] == "160000" {
				result.Boundaries = append(result.Boundaries, Coverage{Path: path, Reason: "submodule"})
				if item.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if tracked[matchPath] == "" && gitignore.NewMatcher(patterns).Match(strings.Split(matchPath, "/"), item.IsDir()) {
				result.Boundaries = append(result.Boundaries, Coverage{Path: path, Reason: "ignored"})
				if item.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}
		if item.IsDir() {
			data, err := worker.readPatterns(handle, filepath.Join(path, ".gitignore"))
			if err != nil {
				result.Missing = append(result.Missing, Coverage{Path: path, Reason: "ignore_rules_unreadable"})
				return fs.SkipDir
			}
			domain := []string{}
			if prefix != "" {
				domain = strings.Split(prefix, "/")
			}
			if path != "." {
				domain = append(domain, strings.Split(path, "/")...)
			}
			patterns = appendPatterns(patterns, data, domain)
			return nil
		}
		info, err := item.Info()
		if err != nil {
			result.Entries[path] = entry{Content: "unreadable"}
			result.Missing = append(result.Missing, Coverage{Path: path, Reason: "unreadable"})
			return nil
		}
		if root.File && !info.Mode().IsRegular() {
			result.Missing = append(result.Missing, Coverage{Path: path, Reason: "file_replaced"})
			return nil
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			result.Boundaries = append(result.Boundaries, Coverage{Path: path, Reason: "special_file"})
			return nil
		}
		value, err := worker.captureEntry(ctx, handle, path, info)
		if err != nil {
			return err
		}
		result.Entries[path] = value
		if value.Content == "unreadable" || value.Content == "oversized" && value.Fingerprint == "" {
			result.Missing = append(result.Missing, Coverage{Path: path, Reason: "content_unavailable"})
		}
		return nil
	}
	if root.File {
		// 精确引用只 Lstat 并打开单个文件，不枚举父目录，也不读取旁边的忽略文件。
		name := filepath.Base(root.Path)
		info, statErr := handle.Lstat(name)
		if os.IsNotExist(statErr) {
			// 删除是可比较的结束侧，不是范围不可用。
		} else if statErr != nil {
			err = visit(name, nil, statErr)
		} else if info.IsDir() {
			result.Missing = append(result.Missing, Coverage{Path: name, Reason: "file_replaced"})
		} else if worker.visited >= worker.capture.options.Limits.MaxEntries {
			err = status_error.WorkspaceResourceTooLarge
		} else {
			worker.visited++
			err = visit(name, fs.FileInfoToDirEntry(info), nil)
		}
	} else {
		err = worker.walkBounded(handle, visit)
	}
	if err == status_error.WorkspaceResourceTooLarge || err == fs.SkipAll {
		worker.exhausted = true
		result.Missing = append(result.Missing, Coverage{Path: ".", Reason: "entry_budget"})
		err = nil
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, status_error.OperationCanceled
		}
		return result, storageError("walk", err)
	}
	if err := worker.flush(ctx); err != nil {
		return result, err
	}
	// 新建私有 index 而不复制用户 index，可避免 split-index 和未合并条目泄漏。
	indexValue := &index.Index{Version: 2}
	paths := make([]string, 0, len(result.Entries))
	for path := range result.Entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		value := result.Entries[path]
		if value.Blob == "" {
			continue
		}
		e := indexValue.Add(path)
		e.Hash = plumbing.NewHash(value.Blob)
		e.Mode = filemode.FileMode(value.Mode)
	}
	indexPath := filepath.Join(filepath.Dir(recordPath(worker.capture.owned, worker.capture.record.Summary.ID)), root.ID+"-"+phase+".index")
	f, err := os.OpenFile(indexPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, storageError("create_index", err)
	}
	err = index.NewEncoder(f).Encode(indexValue)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return result, storageError("write_index", err)
	}
	defer os.Remove(indexPath)
	data, err := runGit(ctx, worker.capture.options.GitExecutable, base(worker.capture.owned), repositoryPath(worker.capture.owned), indexPath, worker.capture.options.Limits, "write-tree")
	if err != nil {
		return result, err
	}
	tree := strings.TrimSpace(string(data))
	if len(tree) != 40 {
		return result, status_error.WorkspaceGitFailed
	}
	result.Tree = tree
	if err := syncTree(ctx, worker.capture.owned, tree); err != nil {
		return result, err
	}
	if err := syncDir(filepath.Join(repositoryPath(worker.capture.owned), "objects")); err != nil {
		return result, storageError("sync_object_directory", err)
	}
	return result, nil
}

func syncTree(ctx context.Context, owned, hash string) error {
	if ctx.Err() != nil {
		return status_error.OperationCanceled
	}
	tree, err := object.GetTree(objectStore(owned), plumbing.NewHash(hash))
	if err != nil {
		return storageError("sync_tree", err)
	}
	for _, entry := range tree.Entries {
		if entry.Mode == filemode.Dir {
			if err := syncTree(ctx, owned, entry.Hash.String()); err != nil {
				return err
			}
		}
	}
	path := filepath.Join(repositoryPath(owned), "objects", hash[:2], hash[2:])
	file, err := os.Open(path)
	if err != nil {
		return storageError("sync_tree", err)
	}
	err = file.Sync()
	file.Close()
	if err != nil {
		return storageError("sync_tree", err)
	}
	if err := syncDir(filepath.Dir(path)); err != nil {
		return storageError("sync_tree_directory", err)
	}
	return nil
}

func (worker *collector) captureEntry(ctx context.Context, root *os.Root, path string, info fs.FileInfo) (entry, error) {
	limits := worker.capture.options.Limits
	value := entry{Mode: uint32(filemode.Regular), Size: info.Size(), Content: "text"}
	var data []byte
	if info.Mode()&os.ModeSymlink != 0 {
		link, err := root.Readlink(path)
		if err != nil {
			value.Content = "unreadable"
			return value, nil
		}
		data = []byte(link)
		value.Mode = uint32(filemode.Symlink)
		value.Size = int64(len(data))
	} else {
		if info.Mode().Perm()&0111 != 0 {
			value.Mode = uint32(filemode.Executable)
		}
		if worker.readBytes+info.Size() > limits.MaxCaptureBytes {
			value.Content = "oversized"
			return value, nil
		}
		f, err := openRegular(root, path, info)
		if err != nil {
			value.Content = "unreadable"
			return value, nil
		}
		defer f.Close()
		if info.Size() > limits.MaxTextBytes {
			hash := sha256.New()
			n, err := copyContext(ctx, hash, io.LimitReader(f, limits.MaxCaptureBytes-worker.readBytes+1))
			worker.readBytes += n
			if err != nil {
				return value, err
			}
			value.Content = "oversized"
			value.Size = n
			if worker.readBytes > limits.MaxCaptureBytes {
				return value, nil
			}
			value.Fingerprint = hex.EncodeToString(hash.Sum(nil))
			data = []byte("oversized\x00" + value.Fingerprint + fmt.Sprint(n))
		} else {
			data = make([]byte, min(info.Size()+1, limits.MaxTextBytes+1))
			var n int
			n, err = io.ReadFull(f, data)
			data = data[:n]
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				err = nil
			}
			worker.readBytes += int64(len(data))
			if err != nil {
				value.Content = "unreadable"
				return value, nil
			}
			if int64(len(data)) > limits.MaxTextBytes || worker.readBytes > limits.MaxCaptureBytes {
				value.Content = "oversized"
				return value, nil
			}
			value.Size = int64(len(data))
		}
		current, err := f.Stat()
		if err != nil || !os.SameFile(info, current) || info.Size() != current.Size() || !info.ModTime().Equal(current.ModTime()) {
			value.Content = "unreadable"
			value.Fingerprint = ""
			return value, nil
		}
	}
	if value.Content == "text" && (bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 || !utf8.Valid(data)) {
		value.Content = "binary"
	}
	hash := plumbing.ComputeHash(plumbing.BlobObject, data)
	name := hash.String()
	if !worker.existing[name] {
		if !worker.buffered[name] && worker.storedBytes+int64(len(data)) > limits.MaxChatBytes {
			value.Content = "unreadable"
			return value, nil
		}
		if !worker.buffered[name] {
			if worker.stream == nil {
				directory := filepath.Dir(recordPath(worker.capture.owned, worker.capture.record.Summary.ID))
				var err error
				worker.stream, err = os.CreateTemp(directory, "pending-objects-")
				if err != nil {
					return value, storageError("create_object_stream", err)
				}
				worker.buffered = map[string]bool{}
			}
			if _, err := fmt.Fprintf(worker.stream, "blob\ndata %d\n", len(data)); err != nil {
				return value, storageError("write_object_stream", err)
			}
			if _, err := worker.stream.Write(data); err != nil {
				return value, storageError("write_object_stream", err)
			}
			if _, err := worker.stream.Write([]byte{'\n'}); err != nil {
				return value, storageError("write_object_stream", err)
			}
			worker.buffered[name] = true
			worker.storedBytes += int64(len(data))
		}
	}
	value.Blob = name
	return value, nil
}

func (worker *collector) discardStream() {
	if worker.stream != nil {
		name := worker.stream.Name()
		worker.stream.Close()
		os.Remove(name)
		worker.stream = nil
	}
	worker.buffered = nil
}

func (worker *collector) flush(ctx context.Context) error {
	if worker.stream == nil {
		return nil
	}
	defer worker.discardStream()
	packDir := filepath.Join(repositoryPath(worker.capture.owned), "objects", "pack")
	before, err := os.ReadDir(packDir)
	if err != nil {
		return storageError("list_packs", err)
	}
	existing := map[string]bool{}
	for _, file := range before {
		existing[file.Name()] = true
	}
	if _, err := worker.stream.WriteString("done\n"); err != nil {
		return storageError("finish_object_stream", err)
	}
	if _, err := worker.stream.Seek(0, io.SeekStart); err != nil {
		return storageError("rewind_object_stream", err)
	}
	if _, err := runGitInput(ctx, worker.capture.options.GitExecutable, base(worker.capture.owned), repositoryPath(worker.capture.owned), "", worker.capture.options.Limits, worker.stream, "fast-import", "--quiet", "--done", "--max-pack-size=256m"); err != nil {
		return err
	}
	after, err := os.ReadDir(packDir)
	if err != nil {
		return storageError("list_packs", err)
	}
	for _, file := range after {
		if existing[file.Name()] || !file.Type().IsRegular() {
			continue
		}
		handle, err := os.Open(filepath.Join(packDir, file.Name()))
		if err != nil {
			return storageError("sync_pack", err)
		}
		err = handle.Sync()
		handle.Close()
		if err != nil {
			return storageError("sync_pack", err)
		}
	}
	if err := syncDir(packDir); err != nil {
		return storageError("sync_pack_directory", err)
	}
	for hash := range worker.buffered {
		worker.existing[hash] = true
	}
	worker.store = objectStore(worker.capture.owned)
	return nil
}

func copyContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buffer := make([]byte, 64<<10)
	var total int64
	for {
		if ctx.Err() != nil {
			return total, status_error.OperationCanceled
		}
		n, err := src.Read(buffer)
		if n > 0 {
			written, writeErr := dst.Write(buffer[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

// walkBounded 限制 ReadDir 本身读取的条目数，避免先装入整个巨型目录再截断。
func (worker *collector) walkBounded(root *os.Root, visit fs.WalkDirFunc) error {
	info, err := root.Lstat(".")
	if err != nil {
		return visit(".", nil, err)
	}
	if worker.visited >= worker.capture.options.Limits.MaxEntries {
		return status_error.WorkspaceResourceTooLarge
	}
	worker.visited++
	var walk func(string, fs.DirEntry) error
	walk = func(path string, item fs.DirEntry) error {
		if err := visit(path, item, nil); err != nil {
			if err == fs.SkipDir {
				return nil
			}
			return err
		}
		if !item.IsDir() {
			return nil
		}
		remaining := worker.capture.options.Limits.MaxEntries - worker.visited
		file, err := root.Open(path)
		if err != nil {
			return visit(path, item, err)
		}
		children, readErr := file.ReadDir(remaining + 1)
		file.Close()
		overflow := len(children) > remaining
		if overflow {
			children = children[:remaining]
		}
		worker.visited += len(children)
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		for _, child := range children {
			if err := walk(filepath.ToSlash(filepath.Join(path, child.Name())), child); err != nil {
				return err
			}
		}
		if overflow {
			return status_error.WorkspaceResourceTooLarge
		}
		if readErr != nil && readErr != io.EOF {
			return visit(path, item, readErr)
		}
		return nil
	}
	return walk(".", fs.FileInfoToDirEntry(info))
}
