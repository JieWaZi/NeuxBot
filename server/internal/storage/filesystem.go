// Package storage 提供受管文件的有界只读计量。
package storage

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

const (
	directoryBatchSize = 128
	maxProblemCount    = 128
	maxProblemPath     = 256
)

// Roots 保存程序状态与受管业务文件各自的配置根。
type Roots struct {
	// Data 是 Core 程序数据根。
	Data string
	// Managed 是受管业务文件根。
	Managed string
	// Database 是可选的程序 SQLite 文件路径，相对路径按照 Core 工作目录解析。
	Database string
}

// Limits 限制一次测量的工作量，零值采用产品上限。
type Limits struct {
	// MaxEntries 限制两根共用的目录项检查总数。
	MaxEntries int
	// MaxDepth 限制从配置根递归进入子目录的层数。
	MaxDepth int
	// Timeout 限制整个测量使用的时间。
	Timeout time.Duration
}

// FileEntry 是已计量且在本次扫描中唯一归属的常规文件。
type FileEntry struct {
	// identity 标识本次扫描观测到的文件实例。
	identity fileIdentity
	// ModifiedAt 是文件测量时的修改时间。
	ModifiedAt time.Time
	// Path 是文件在扫描时的真实布局绝对路径。
	Path string
	// OwnerRoot 是该文件所属的规范配置根。
	OwnerRoot string
	// Bytes 是常规文件的逻辑字节数。
	Bytes int64
	// Category 是由布局确定的计量分类。
	Category constants.StorageCategory
	// ChatID 是会话私有文件的归属身份。
	ChatID string
	// WakerID 是位于伙伴受管空间的文件归属身份。
	WakerID string
	// Folder 是已识别的会话私有子目录，用于清理预览归组。
	Folder string
}

// Problem 记录导致统计不完整的有界诊断。
type Problem struct {
	// Operation 是失败的扫描操作。
	Operation string
	// Reason 是扫描不完整的具体原因。
	Reason string
	// OwnerRoot 是发生问题的规范配置根。
	OwnerRoot string
	// Path 是裁剪后的根内相对位置。
	Path string
	// Code 是稳定的失败身份。
	Code status_error.StatusError
}

// Disk 是至少一个受管根实际所在的磁盘卷。
type Disk struct {
	// ID 是该平台的实际卷身份。
	ID string
	// Roots 是属于同一卷的各个规范配置根。
	Roots []string
	// TotalBytes 是该卷的容量。
	TotalBytes uint64
	// AvailableBytes 是该卷对当前调用者可用的容量。
	AvailableBytes uint64
}

// Measurement 包含全量扫描预算内可证实的文件小计。
type Measurement struct {
	// Roots 是输入配置解析后的规范路径。
	Roots Roots
	// Bytes 是全局去重后的常规文件逻辑字节数。
	Bytes int64
	// Files 是全局去重后的常规文件数。
	Files int
	// Complete 表示所有统计范围和磁盘容量都已测量。
	Complete bool
	// Entries 是用于按类和实体聚合的常规文件记录。
	Entries []FileEntry
	// FileAliases 保存实体下的其他硬链接路径，用于逻辑明细且不计入磁盘总量。
	FileAliases []FileEntry
	// Problems 是导致统计不完整的有界原因列表。
	Problems []Problem
	// Disks 是去重后的实际卷容量。
	Disks []Disk
}

type rootScope uint8

const (
	dataRoot rootScope = 1 << iota
	managedRoot
)

type scanRoot struct {
	// path 是完成规范化的配置根。
	path string
	// volumePath 指向用于测量卷的最近已存在目录。
	volumePath string
	// info 是规范配置根解析时观测的文件身份，缺失根为 nil。
	info fs.FileInfo
	// scope 合并了指向同一目录的布局归属。
	scope rootScope
	// databaseName 是程序数据根内实际使用的 SQLite 文件名。
	databaseName string
	// pluginAlias 表示同根时大小写不同的两个插件前缀被文件系统映射到同一目录。
	pluginAlias bool
}

type fileIdentity struct {
	// volume 是文件系统提供的卷编号。
	volume uint64
	// inode 是该文件在卷内的身份。
	inode uint64
}

type scanner struct {
	// ctx 包含一次扫描自己的时间预算和生命周期取消。
	ctx context.Context
	// limits 是已收紧至产品上限的实际扫描预算。
	limits Limits
	// result 保存本次扫描的已核验小计和完整性。
	result Measurement
	// roots 是去重后需要分别按各自布局遍历的受管根。
	roots []*scanRoot
	// files 记录已计量的常规文件身份。
	files map[fileIdentity]struct{}
	// directories 避免同一目录经多个布局或挂载别名重复遍历。
	directories map[fileIdentity]struct{}
	// systemSkillDirectories 缓存各受管工作目录的系统技能投影路径。
	systemSkillDirectories map[string]map[string]struct{}
	// systemSkillChatLayout 复用受管根的实际伙伴和团队执行布局。
	systemSkillChatLayout *chatstorage.Layout
	// visited 是两根已经检查的目录项总数。
	visited int
	// exhausted 标记两根共用的目录项预算耗尽。
	exhausted bool
}

// Measure 在一次共享预算内测量配置根、已识别文件归属和实际卷容量。
func Measure(ctx context.Context, roots Roots, limits Limits) (Measurement, error) {
	limits = Limits{
		MaxEntries: bounded(limits.MaxEntries, constants.StorageScanMaximumEntries),
		MaxDepth:   bounded(limits.MaxDepth, constants.StorageScanMaximumDepth),
		Timeout:    bounded(limits.Timeout, constants.StorageScanMaximumSeconds*time.Second),
	}
	scanCtx, cancel := context.WithTimeout(ctx, limits.Timeout)
	defer cancel()

	s := scanner{
		ctx: scanCtx, limits: limits, result: Measurement{Complete: true},
		files: make(map[fileIdentity]struct{}), directories: make(map[fileIdentity]struct{}),
	}
	if scanCtx.Err() == nil && (roots.Data == "" || roots.Managed == "") {
		s.problem("resolve_root", "invalid_root", nil, "", status_error.StorageRootInvalid)
		return s.result, status_error.StorageRootInvalid
	}
	s.result.Roots.Data = s.addRoot(roots.Data, dataRoot)
	s.result.Roots.Managed = s.addRoot(roots.Managed, managedRoot)
	s.setDatabase(roots.Database)
	for _, root := range s.roots {
		if scanCtx.Err() != nil {
			break
		}
		s.measureDisk(root)
	}
	for _, root := range s.roots {
		if scanCtx.Err() != nil || s.exhausted {
			break
		}
		if root.info != nil {
			s.walkRoot(root)
		}
	}
	if ctx.Err() != nil {
		s.problem("scan", "canceled", nil, "", status_error.OperationCanceled)
		return s.result, status_error.OperationCanceled
	}
	if scanCtx.Err() != nil {
		s.problem("scan", "timeout", nil, "", status_error.OperationTimeout)
	}
	return s.result, nil
}

func bounded[T ~int | ~int64](requested, maximum T) T {
	if requested <= 0 {
		return maximum
	}
	return min(requested, maximum)
}

func (s *scanner) addRoot(configured string, scope rootScope) string {
	if s.ctx.Err() != nil {
		return ""
	}
	resolved, err := resolveRoot(configured)
	if err != nil {
		root := &scanRoot{path: resolved.path, scope: scope}
		s.ioProblem("resolve_root", root, ".", err, status_error.StorageRootInvalid)
		return resolved.path
	}
	for _, existing := range s.roots {
		if resolved.path == existing.path || (resolved.info != nil && existing.info != nil && os.SameFile(resolved.info, existing.info)) {
			existing.scope |= scope
			return existing.path
		}
	}
	resolved.scope = scope
	s.roots = append(s.roots, resolved)
	return resolved.path
}

func (s *scanner) setDatabase(configured string) {
	if configured == "" || configured == ":memory:" || s.ctx.Err() != nil {
		return
	}
	path, err := filepath.Abs(configured)
	if err != nil {
		return
	}
	for _, root := range s.roots {
		if root.scope&dataRoot == 0 || root.info == nil {
			continue
		}
		parent, err := os.Stat(filepath.Dir(path))
		if err == nil && os.SameFile(root.info, parent) {
			root.databaseName = filepath.Base(path)
			s.result.Roots.Database = filepath.Join(root.path, root.databaseName)
		}
		return
	}
}

func resolveRoot(configured string) (*scanRoot, error) {
	absolute, err := filepath.Abs(configured)
	result := &scanRoot{path: absolute}
	if err != nil {
		return result, err
	}
	// 未使用的根尚未创建时，从最近已存在的真实父目录恢复配置路径。
	for path := absolute; ; path = filepath.Dir(path) {
		resolved, resolveErr := filepath.EvalSymlinks(path)
		if resolveErr != nil {
			if !errors.Is(resolveErr, fs.ErrNotExist) || filepath.Dir(path) == path {
				return result, resolveErr
			}
			if _, statErr := os.Lstat(path); !errors.Is(statErr, fs.ErrNotExist) {
				return result, resolveErr
			}
			continue
		}
		info, err := os.Lstat(resolved)
		if err != nil {
			return result, err
		}
		if !info.IsDir() {
			return result, status_error.StorageRootInvalid
		}
		suffix, err := filepath.Rel(path, absolute)
		if err != nil {
			return result, err
		}
		result.path = filepath.Join(resolved, suffix)
		result.volumePath = resolved
		if suffix == "." {
			result.info = info
		}
		return result, nil
	}
}

func (s *scanner) measureDisk(root *scanRoot) {
	volume, err := measureVolume(root.volumePath)
	if err != nil {
		s.ioProblem("read_volume", root, ".", err, status_error.StorageVolumeUnavailable)
		return
	}
	for index := range s.result.Disks {
		if s.result.Disks[index].ID == volume.ID {
			s.result.Disks[index].Roots = append(s.result.Disks[index].Roots, root.path)
			return
		}
	}
	volume.Roots = []string{root.path}
	s.result.Disks = append(s.result.Disks, volume)
}

func (s *scanner) walkRoot(root *scanRoot) {
	opened, err := openConfiguredRoot(root)
	if err != nil {
		s.ioProblem("open_root", root, ".", err, status_error.StorageScanIncomplete)
		return
	}
	defer opened.Close()
	if root.scope == dataRoot|managedRoot {
		program, programErr := opened.Lstat("plugins")
		catalog, catalogErr := opened.Lstat("Plugins")
		root.pluginAlias = programErr == nil && catalogErr == nil &&
			program.IsDir() && catalog.IsDir() && os.SameFile(program, catalog)
	}
	s.walk(opened, root, root.info, ".", 0)
	if s.ctx.Err() == nil && !s.exhausted {
		s.checkDirectory(root, ".", root.info, os.Lstat, root.path)
	}
}

func (s *scanner) walk(opened *os.Root, owner *scanRoot, observed fs.FileInfo, relative string, depth int) {
	directory, err := opened.Open(".")
	if err != nil {
		s.ioProblem("open_directory", owner, relative, err, status_error.StorageScanIncomplete)
		return
	}
	defer directory.Close()
	actual, err := directory.Stat()
	if err != nil {
		s.ioProblem("stat_directory", owner, relative, err, status_error.StorageScanIncomplete)
		return
	}
	if !os.SameFile(observed, actual) {
		s.problem("open_directory", "changed", owner, relative, status_error.StorageScanIncomplete)
		return
	}
	identity, err := filesystemIdentity(directory, actual)
	if err != nil {
		s.ioProblem("identify_directory", owner, relative, err, status_error.StorageScanIncomplete)
		return
	}
	if _, seen := s.directories[identity]; seen {
		return
	}
	s.directories[identity] = struct{}{}

	for s.ctx.Err() == nil && !s.exhausted {
		remaining := min(directoryBatchSize, s.limits.MaxEntries-s.visited+1)
		entries, readErr := directory.ReadDir(remaining)
		for _, entry := range entries {
			if s.ctx.Err() != nil || s.exhausted {
				return
			}
			if s.visited == s.limits.MaxEntries {
				s.exhausted = true
				s.problem("read_directory", "entry_limit", owner, relative, status_error.StorageScanIncomplete)
				return
			}
			s.visited++
			s.visitEntry(opened, owner, relative, depth, entry.Name())
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			s.ioProblem("read_directory", owner, relative, readErr, status_error.StorageScanIncomplete)
			return
		}
	}
}

func (s *scanner) visitEntry(root *os.Root, owner *scanRoot, parent string, depth int, name string) {
	relative := filepath.Join(parent, name)
	info, err := root.Lstat(name)
	if err != nil {
		s.ioProblem("stat_entry", owner, relative, err, status_error.StorageScanIncomplete)
		return
	}
	switch {
	case info.IsDir():
		// 独立配置的嵌套根用自身布局遍历，只在外层计一次目录项。
		for _, other := range s.roots {
			if other != owner && other.info != nil && os.SameFile(info, other.info) {
				return
			}
		}
		if depth == s.limits.MaxDepth {
			s.problem("open_directory", "depth_limit", owner, relative, status_error.StorageScanIncomplete)
			return
		}
		child, err := root.OpenRoot(name)
		if err != nil {
			s.ioProblem("open_directory", owner, relative, err, status_error.StorageScanIncomplete)
			return
		}
		defer child.Close()
		s.walk(child, owner, info, relative, depth+1)
		if s.ctx.Err() == nil && !s.exhausted {
			s.checkDirectory(owner, relative, info, root.Lstat, name)
		}
	case info.Mode().IsRegular():
		s.addFile(root, owner, relative, name, info)
	}
}

func (s *scanner) checkDirectory(owner *scanRoot, relative string, observed fs.FileInfo, stat func(string) (fs.FileInfo, error), name string) {
	actual, err := stat(name)
	if err != nil {
		s.ioProblem("restat_directory", owner, relative, err, status_error.StorageScanIncomplete)
		return
	}
	if !os.SameFile(observed, actual) || !observed.ModTime().Equal(actual.ModTime()) {
		s.problem("restat_directory", "changed", owner, relative, status_error.StorageScanIncomplete)
	}
}

func (s *scanner) addFile(root *os.Root, owner *scanRoot, relative string, name string, info fs.FileInfo) {
	identity, err := regularFileIdentity(root, name, info)
	if err != nil {
		s.ioProblem("identify_file", owner, relative, err, status_error.StorageScanIncomplete)
		return
	}
	entry := classifyFile(owner, relative)
	entry.Path = filepath.Join(owner.path, relative)
	entry.OwnerRoot = owner.path
	entry.Bytes = info.Size()
	entry.ModifiedAt = info.ModTime()
	entry.identity = identity
	if entry.Category == constants.StorageCategoryWorkFiles && s.isSystemSkillFile(owner, root, relative, name, info) {
		entry.Category = constants.StorageCategoryPrograms
	}
	if _, seen := s.files[identity]; seen {
		if entry.WakerID != "" || entry.ChatID != "" {
			s.result.FileAliases = append(s.result.FileAliases, entry)
		}
		return
	}
	s.files[identity] = struct{}{}
	s.result.Bytes += entry.Bytes
	s.result.Files++
	s.result.Entries = append(s.result.Entries, entry)
}

func (s *scanner) ioProblem(operation string, root *scanRoot, relative string, err error, code status_error.StatusError) {
	reason := "io_error"
	switch {
	case errors.Is(err, fs.ErrPermission):
		reason = "permission_denied"
	case errors.Is(err, fs.ErrNotExist):
		reason = "disappeared"
	case errors.Is(err, fs.ErrInvalid):
		reason = "changed"
	case errors.Is(err, status_error.StorageRootInvalid):
		reason = "invalid_root"
	}
	if len(s.result.Problems) < maxProblemCount {
		logx.Warnf("storage measurement %s failed: root=%q relative=%q error=%v", operation, root.path, relative, err)
	}
	s.problem(operation, reason, root, relative, code)
}

func (s *scanner) problem(operation string, reason string, root *scanRoot, relative string, code status_error.StatusError) {
	s.result.Complete = false
	if len(s.result.Problems) == maxProblemCount {
		if reason != "canceled" && reason != "timeout" && reason != "entry_limit" {
			return
		}
		s.result.Problems = s.result.Problems[:maxProblemCount-1]
	}
	item := Problem{Operation: operation, Reason: reason, Code: code}
	if root != nil {
		item.OwnerRoot = root.path
	}
	if relative != "." {
		path := filepath.ToSlash(relative)
		item.Path = strings.ToValidUTF8(path[:min(len(path), maxProblemPath)], "")
	}
	s.result.Problems = append(s.result.Problems, item)
}
