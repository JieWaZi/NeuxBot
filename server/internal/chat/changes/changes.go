// Package changes 保存授权目录在一次执行前后的独立、不可变文件快照。
// 私有 Git tree 的设计参考 DeepSeek Harness workspace-changes（ce7073d），
// Go 的安全遍历、非 Git 范围、耐久记录与有界差异为本项目适配。
package changes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gofrs/flock"
	"github.com/shrewx/ginx/pkg/statuserror"
	"github.com/sirupsen/logrus"
)

// Limits 集中定义采集、存储和展示的技术预算；零值使用规格默认值。
type Limits struct {
	// Timeout 是基线阶段或结束侧采集与差异收尾合计的最长时间。
	Timeout time.Duration
	// MaxEntries 是一次阶段最多枚举的目录条目数。
	MaxEntries int
	// MaxCaptureBytes 是一次阶段最多读取的文件字节数。
	MaxCaptureBytes int64
	// MaxChatBytes 是同一归属目录最多保存的去重后对象字节数。
	MaxChatBytes int64
	// MaxTextBytes 是可保存并比较的单侧文本上限。
	MaxTextBytes int64
	// MaxFiles 是单条记录可展示的文件数。
	MaxFiles int
	// MaxOutputBytes 是 Git 输出和单份差异记录的字节上限。
	MaxOutputBytes int
	// DiffTimeout 是单文件差异算法的时间预算。
	DiffTimeout time.Duration
	// MaxDiffLines 是单份差异最多返回的行数。
	MaxDiffLines int
}

// Options 配置采集范围内的附加排除规则和技术预算。
type Options struct {
	// Limits 是技术预算；调用方可以收紧默认上限。
	Limits Limits
	// ExcludePaths 是必须排除的绝对路径，通常来自既有受管秘密目录规则。
	ExcludePaths []string
	// IgnorePatterns 是按 gitignore 语法应用于每个根的用户排除规则。
	IgnorePatterns []string
	// GitExecutable 指定已解析的 Git 程序；空值从 PATH 查找。
	GitExecutable string
}

// Root 保存接纳时冻结的真实授权根，不把父仓库当作额外授权。
type Root struct {
	// ID 是规范路径的稳定摘要。
	ID string `json:"id"`
	// Path 是规范绝对目录路径。
	Path string `json:"path"`
	// Kind 表示原目录是否处于 Git 仓库中。
	Kind string `json:"kind"`
	// RepositoryRoot 仅描述已有仓库，不扩大遍历范围。
	RepositoryRoot string `json:"repositoryRoot,omitempty"`
	// Boundaries 是未覆盖的嵌套仓库、忽略文件和排除目录。
	Boundaries []Coverage `json:"boundaries,omitempty"`
}

// Coverage 说明一个相对路径及其未覆盖原因。
type Coverage struct {
	// Path 是范围内的相对路径；点表示整个根。
	Path string `json:"path"`
	// Reason 是稳定的技术原因。
	Reason string `json:"reason"`
}

// File 是同一对快照中的一项净变化；未知行数保持空值。
type File struct {
	// RootID 指向冻结根。
	RootID string `json:"rootId"`
	// Path 是结束侧路径，删除时为原路径。
	Path string `json:"path"`
	// AbsolutePath 是冻结根下的完整文件身份。
	AbsolutePath string `json:"absolutePath"`
	// OldPath 仅在识别到重命名时保存开始侧路径。
	OldPath string `json:"oldPath,omitempty"`
	// Status 区分 added、modified、deleted 与 renamed。
	Status string `json:"status"`
	// Content 区分 text、binary、oversized、unreadable。
	Content string `json:"content"`
	// Added 是同一差异计划中的新增行数。
	Added *int `json:"added,omitempty"`
	// Deleted 是同一差异计划中的删除行数。
	Deleted *int `json:"deleted,omitempty"`
	// Coarse 表示算法因预算采用整段替换。
	Coarse bool `json:"coarse,omitempty"`
}

// Summary 是可在内容闭包落盘后引用的耐久成果。
type Summary struct {
	// ID 是执行身份的安全摘要，也是记录目录名。
	ID string `json:"id"`
	// ExecutionID 关联调用方的执行事实。
	ExecutionID string `json:"executionId"`
	// Version 在 pending 到终态时递增。
	Version int `json:"version"`
	// State 为 pending、ready、partial、unavailable 或 incomplete。
	State string `json:"state"`
	// Reason 是采集失败或覆盖不足的稳定技术原因。
	Reason string `json:"reason,omitempty"`
	// Roots 保存冻结授权范围及实际覆盖。
	Roots []Root `json:"roots"`
	// Files 是有界且排序稳定的可展示净变化。
	Files []File `json:"files"`
	// TotalFiles 是完整枚举时的已知净变化总数。
	TotalFiles int `json:"totalFiles"`
	// Truncated 表示展示列表未包含所有已知文件。
	Truncated bool `json:"truncated,omitempty"`
	// Complete 表示枚举与读取覆盖完整，不能以展示数量推断。
	Complete bool `json:"complete"`
}

// Capture 唯一拥有一次文件采集；它不保存模型或 Execution 的运行状态。
type Capture struct {
	// mu 串行化同一记录的收尾和关闭。
	mu sync.Mutex
	// owned 是调用方已授权的 Chat 归属目录。
	owned string
	// options 保存本轮已冻结预算与排除规则。
	options Options
	// record 保存已经完成写入的耐久事实。
	record record
	// rootInfo 固定本进程开始侧的目录身份，防止原路径被替换后静默改绑。
	rootInfo map[string]fs.FileInfo
	// lease 在两个采集阶段之间保护活动记录，恢复只能接管进程退出后的句柄。
	lease *flock.Flock
	// closed 防止对同一记录重复收尾。
	closed bool
	// afterFrozen 表示结束侧已耐久保存，后续差异计算不再读取当前工作树。
	afterFrozen bool
	// finalizationDeadline 让结束侧冻结和差异计算共用原有的一次收尾预算。
	finalizationDeadline time.Time
	// discarded 表示未发布记录已删除，重试只能回收孤立对象，不能再次删除同名新记录。
	discarded bool
}

type record struct {
	// Summary 是对调用方开放的轻量事实。
	Summary Summary `json:"summary"`
	// Before 是执行前已完成的范围快照。
	Before []snapshot `json:"before,omitempty"`
	// After 是执行后已完成的范围快照。
	After []snapshot `json:"after,omitempty"`
	// Limits 固定同一条记录的比较规则。
	Limits Limits `json:"limits"`
}

type snapshot struct {
	// RootID 关联冻结根。
	RootID string `json:"rootId"`
	// Tree 是应用私有对象库中的 Git tree。
	Tree string `json:"tree,omitempty"`
	// Entries 保存可比较的对象及读取状态。
	Entries map[string]entry `json:"entries"`
	// Missing 保存本阶段不能完整枚举或读取的前缀。
	Missing []Coverage `json:"missing,omitempty"`
	// Boundaries 保存按范围策略明确排除的前缀。
	Boundaries []Coverage `json:"boundaries,omitempty"`
}

type entry struct {
	// Blob 是私有 Git blob 的寻址键。
	Blob string `json:"blob,omitempty"`
	// Mode 是普通文件、可执行文件或链接对象的 Git mode。
	Mode uint32 `json:"mode"`
	// Size 是原文件的字节数。
	Size int64 `json:"size"`
	// Content 保存 text、binary、oversized 或 unreadable 状态。
	Content string `json:"content"`
	// Fingerprint 为过大文件保存有界完整读取后的内容摘要。
	Fingerprint string `json:"fingerprint,omitempty"`
}

func normalizedLimits(v Limits) Limits {
	if v.Timeout <= 0 || v.Timeout > 30*time.Second {
		v.Timeout = 30 * time.Second
	}
	if v.MaxEntries <= 0 || v.MaxEntries > 50000 {
		v.MaxEntries = 50000
	}
	if v.MaxCaptureBytes <= 0 || v.MaxCaptureBytes > 256<<20 {
		v.MaxCaptureBytes = 256 << 20
	}
	if v.MaxChatBytes <= 0 || v.MaxChatBytes > 1<<30 {
		v.MaxChatBytes = 1 << 30
	}
	if v.MaxTextBytes <= 0 || v.MaxTextBytes > 2<<20 {
		v.MaxTextBytes = 2 << 20
	}
	if v.MaxFiles <= 0 || v.MaxFiles > 500 {
		v.MaxFiles = 500
	}
	if v.MaxOutputBytes <= 0 || v.MaxOutputBytes > 8<<20 {
		v.MaxOutputBytes = 8 << 20
	}
	if v.DiffTimeout <= 0 || v.DiffTimeout > 100*time.Millisecond {
		v.DiffTimeout = 100 * time.Millisecond
	}
	if v.MaxDiffLines <= 0 || v.MaxDiffLines > 5000 {
		v.MaxDiffLines = 5000
	}
	return v
}

func identity(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:16])
}
func base(owned string) string { return filepath.Join(owned, "changes") }
func recordPath(owned, id string) string {
	return filepath.Join(base(owned), "records", id, "record.json")
}
func validID(id string) bool { data, err := hex.DecodeString(id); return err == nil && len(data) == 16 }

// Begin 在 Agent 取得执行权限之前保存基线；采集不可用也返回带明确状态的句柄。
// ownedDir 必须是原 Chat 归属目录；公开读取时调用方仍须验证原 Chat 的访问权。
func Begin(ctx context.Context, ownedDir, executionID string, roots []string, options Options) (*Capture, error) {
	if executionID == "" || !filepath.IsAbs(ownedDir) {
		return nil, status_error.WorkspacePathInvalid
	}
	options.Limits = normalizedLimits(options.Limits)
	options.ExcludePaths = append([]string(nil), options.ExcludePaths...)
	options.IgnorePatterns = append([]string(nil), options.IgnorePatterns...)
	for i, path := range options.ExcludePaths {
		if !filepath.IsAbs(path) {
			return nil, status_error.WorkspacePathInvalid
		}
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			options.ExcludePaths[i] = resolved
		} else {
			options.ExcludePaths[i] = filepath.Clean(path)
		}
	}
	owned, err := prepareOwned(ownedDir)
	if err != nil {
		return nil, storageError("prepare", err)
	}
	frozen, err := freezeRoots(roots)
	scopeUnavailable := err != nil
	if scopeUnavailable {
		frozen = []Root{}
		seen := map[string]bool{}
		for _, path := range roots {
			if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
				return nil, status_error.WorkspaceScopeInvalid
			}
			path = filepath.Clean(path)
			if canonical, err := filepath.EvalSymlinks(path); err == nil {
				path = canonical
			}
			if seen[path] {
				continue
			}
			seen[path] = true
			frozen = append(frozen, Root{ID: identity(path), Path: path, Kind: "unavailable", Boundaries: []Coverage{{Path: ".", Reason: "root_unavailable"}}})
		}
	}
	rootBytes := 0
	for _, root := range frozen {
		encoded, _ := json.Marshal(root)
		rootBytes += len(encoded)
		if rootBytes > options.Limits.MaxOutputBytes || len(frozen) > options.Limits.MaxEntries {
			return nil, status_error.WorkspaceResourceTooLarge
		}
	}
	c := &Capture{owned: owned, options: options, record: record{Summary: Summary{ID: identity(executionID), ExecutionID: executionID, Version: 1, State: "pending", Roots: frozen, Files: []File{}}, Limits: options.Limits}}
	c.rootInfo = make(map[string]fs.FileInfo, len(frozen))
	for _, root := range frozen {
		if scopeUnavailable {
			break
		}
		info, err := os.Stat(root.Path)
		if err != nil {
			scopeUnavailable = true
			break
		}
		c.rootInfo[root.ID] = info
	}
	phase, cancel := context.WithTimeout(ctx, options.Limits.Timeout)
	defer cancel()
	err = c.withLock(phase, func() error {
		if _, statErr := os.Stat(recordPath(c.owned, c.record.Summary.ID)); statErr == nil {
			return status_error.WorkspaceResourceConflict
		} else if !os.IsNotExist(statErr) {
			return storageError("inspect_record", statErr)
		}
		if err := os.MkdirAll(filepath.Dir(recordPath(c.owned, c.record.Summary.ID)), 0700); err != nil {
			return storageError("create_record", err)
		}
		if err := syncDir(filepath.Join(base(c.owned), "records")); err != nil {
			return storageError("sync_record_directory", err)
		}
		c.lease = flock.New(filepath.Join(filepath.Dir(recordPath(c.owned, c.record.Summary.ID)), "capture.lock"))
		available, err := c.lease.TryLock()
		if err != nil {
			return storageError("capture_lease", err)
		}
		if !available {
			return status_error.WorkspaceResourceConflict
		}
		// 在任何基线 I/O 之前提交 pending，恢复时不依赖进程内句柄。
		if err := saveRecord(c.owned, &c.record); err != nil {
			return err
		}
		if scopeUnavailable {
			c.record.Summary.State, c.record.Summary.Reason = "unavailable", "scope_unavailable"
			return saveRecord(c.owned, &c.record)
		}
		git, reason := resolveGit(phase, options.GitExecutable, options.Limits)
		if reason != "" {
			c.record.Summary.State, c.record.Summary.Reason = "unavailable", reason
			return saveRecord(c.owned, &c.record)
		}
		c.options.GitExecutable = git
		if err := ensureRepository(phase, c.owned, git, options.Limits); err != nil {
			c.record.Summary.State, c.record.Summary.Reason = "unavailable", "git_unavailable"
			return saveRecord(c.owned, &c.record)
		}
		before, err := c.collect(phase, "before")
		if err != nil {
			c.record.Summary.State, c.record.Summary.Reason = "unavailable", captureReason(phase, "baseline_failed")
		} else {
			c.record.Before = before
		}
		return saveRecord(c.owned, &c.record)
	})
	if err != nil {
		c.releaseLease()
		return nil, err
	}
	return c, nil
}

// Summary 返回当前已耐久保存事实的独立副本。
func (c *Capture) Summary() Summary {
	c.mu.Lock()
	defer c.mu.Unlock()
	return copySummary(c.record.Summary)
}

func copySummary(summary Summary) Summary {
	data, _ := json.Marshal(summary)
	var v Summary
	_ = json.Unmarshal(data, &v)
	return v
}

// Freeze 保存结束侧实际落盘内容；返回后可以允许下一轮修改同一工作树。
// 调用方应提供独立于模型取消的有界 Context，差异计算仍由 Finish 完成。
func (c *Capture) Freeze(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return status_error.WorkspaceResourceConflict
	}
	return c.freeze(ctx)
}

func (c *Capture) freeze(ctx context.Context) error {
	if c.afterFrozen || c.record.Summary.State != "pending" {
		return nil
	}
	if c.finalizationDeadline.IsZero() {
		c.finalizationDeadline = time.Now().Add(c.options.Limits.Timeout)
	}
	phase, cancel := context.WithDeadline(ctx, c.finalizationDeadline)
	defer cancel()
	err := c.withLock(phase, func() error {
		after, err := c.collect(phase, "after")
		if err != nil {
			c.record.Summary.State, c.record.Summary.Reason = "unavailable", captureReason(phase, "finalization_failed")
			c.record.Summary.Version++
		} else {
			c.record.After = after
		}
		return saveRecord(c.owned, &c.record)
	})
	if err != nil {
		return err
	}
	c.afterFrozen = true
	return nil
}

// Finish 从已冻结的两侧快照计算差异；未显式 Freeze 的调用方仍在此保存结束侧。
func (c *Capture) Finish(ctx context.Context) (*Summary, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, status_error.WorkspaceResourceConflict
	}
	if err := c.freeze(ctx); err != nil {
		return nil, err
	}
	if c.record.Summary.State == "pending" {
		phase, cancel := context.WithDeadline(ctx, c.finalizationDeadline)
		defer cancel()
		if err := c.withLock(phase, func() error {
			if err := c.compare(phase); err != nil {
				c.record.Summary.State, c.record.Summary.Reason = "unavailable", captureReason(phase, "comparison_failed")
				c.record.Summary.Complete = false
				c.record.Summary.Files = []File{}
			}
			c.record.Summary.Version++
			return saveRecord(c.owned, &c.record)
		}); err != nil {
			return nil, err
		}
	}
	c.closed = true
	c.releaseLease()
	v := copySummary(c.record.Summary)
	return &v, nil
}

// Close 放弃未完成的采集，不以关闭时文件冒充执行结束快照。
func (c *Capture) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.releaseLease()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.record.Summary.State != "pending" {
		return nil
	}
	c.record.Summary.State, c.record.Summary.Reason = "incomplete", "capture_closed"
	c.record.Summary.Version++
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.withLock(ctx, func() error { return saveRecord(c.owned, &c.record) })
}

func (c *Capture) releaseLease() {
	if c.lease != nil {
		c.lease.Unlock()
		c.lease.Close()
		c.lease = nil
	}
}

func (c *Capture) withLock(ctx context.Context, run func() error) error {
	lock := flock.New(filepath.Join(base(c.owned), "write.lock"))
	defer lock.Close()
	ok, err := lock.TryLockContext(ctx, 10*time.Millisecond)
	if err != nil || !ok {
		if ctx.Err() != nil {
			return status_error.OperationCanceled
		}
		return storageError("lock", err)
	}
	defer lock.Unlock()
	return run()
}

func captureReason(ctx context.Context, fallback string) string {
	if ctx.Err() != nil {
		return "capture_interrupted"
	}
	return fallback
}

func storageError(operation string, err error) error {
	if _, ok := err.(statuserror.CommonError); ok {
		return err
	}
	logrus.WithError(err).WithField("operation", "chat_changes_"+operation).Warn("file snapshot storage failed")
	return status_error.WorkspaceStorageFailed
}

func prepareOwned(owned string) (string, error) {
	if !filepath.IsAbs(owned) {
		return "", status_error.WorkspacePathInvalid
	}
	missing := []string{}
	for current := filepath.Clean(owned); ; current = filepath.Dir(current) {
		if _, err := os.Stat(current); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}
		missing = append(missing, current)
		if filepath.Dir(current) == current {
			break
		}
	}
	if err := os.MkdirAll(owned, 0700); err != nil {
		return "", err
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := syncDir(filepath.Dir(missing[i])); err != nil {
			return "", err
		}
	}
	canonical, err := filepath.EvalSymlinks(owned)
	if err != nil {
		return "", err
	}
	for _, path := range []string{base(canonical), filepath.Join(base(canonical), "records")} {
		if err := os.Mkdir(path, 0700); err != nil {
			if !os.IsExist(err) {
				return "", err
			}
		} else if err := syncDir(filepath.Dir(path)); err != nil {
			return "", err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", status_error.WorkspaceScopeInvalid
		}
	}
	return canonical, nil
}

func saveRecord(owned string, r *record) error {
	if err := writeJSON(recordPath(owned, r.Summary.ID), r); err != nil {
		return storageError("save_record", err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	file, err := os.CreateTemp(filepath.Dir(path), "pending-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(0600); err == nil {
		err = json.NewEncoder(&jsonLimitWriter{target: file, remaining: 64 << 20}).Encode(value)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

type jsonLimitWriter struct {
	// target 是等待原子提交的私有暂存文件。
	target io.Writer
	// remaining 与读取上限一致，防止公布不可恢复的超大元数据。
	remaining int64
}

func (writer *jsonLimitWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > writer.remaining {
		return 0, io.ErrShortBuffer
	}
	n, err := writer.target.Write(data)
	writer.remaining -= int64(n)
	return n, err
}

func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func loadRecord(owned, id string) (*record, error) {
	if !validID(id) || !filepath.IsAbs(owned) {
		return nil, status_error.WorkspacePathInvalid
	}
	root, err := os.OpenRoot(owned)
	if err != nil {
		return nil, storageError("open_record_root", err)
	}
	defer root.Close()
	f, err := root.Open(filepath.Join("changes", "records", id, "record.json"))
	if os.IsNotExist(err) {
		return nil, status_error.ChatMessageFileChangeNotFound
	}
	if err != nil {
		return nil, storageError("read_record", err)
	}
	defer f.Close()
	var r record
	if err := json.NewDecoder(io.LimitReader(f, 64<<20)).Decode(&r); err != nil {
		return nil, storageError("decode_record", err)
	}
	if err := validateSummary(r.Summary, id); err != nil {
		return nil, err
	}
	return &r, nil
}

func validateSummary(summary Summary, id string) error {
	if summary.ID != id || identity(summary.ExecutionID) != id {
		return status_error.WorkspaceScopeInvalid
	}
	for _, rt := range summary.Roots {
		if !filepath.IsAbs(rt.Path) || rt.ID != identity(rt.Path) {
			return status_error.WorkspaceScopeInvalid
		}
	}
	for _, file := range summary.Files {
		valid := false
		for _, root := range summary.Roots {
			if root.ID == file.RootID && filepath.IsLocal(file.Path) && file.Path != "." && !strings.ContainsRune(file.Path, 0) && file.AbsolutePath == filepath.Join(root.Path, filepath.FromSlash(file.Path)) && (file.OldPath == "" || filepath.IsLocal(file.OldPath) && file.OldPath != "." && !strings.ContainsRune(file.OldPath, 0)) {
				valid = true
				break
			}
		}
		if !valid {
			return status_error.WorkspaceScopeInvalid
		}
	}
	return nil
}

// ReadSummary 读取已提交的不可变摘要，不读取当前工作树。
func ReadSummary(ownedDir, recordID string) (*Summary, error) {
	if !filepath.IsAbs(ownedDir) || !validID(recordID) {
		return nil, status_error.WorkspacePathInvalid
	}
	root, err := os.OpenRoot(ownedDir)
	if err != nil {
		return nil, storageError("open_summary_root", err)
	}
	defer root.Close()
	file, err := root.Open(filepath.Join("changes", "records", recordID, "record.json"))
	if os.IsNotExist(err) {
		return nil, status_error.ChatMessageFileChangeNotFound
	}
	if err != nil {
		return nil, storageError("read_summary", err)
	}
	defer file.Close()
	var result struct {
		// Summary 只解码公开摘要，跳过较大的内部前后对象清单。
		Summary Summary `json:"summary"`
	}
	if err := json.NewDecoder(io.LimitReader(file, 64<<20)).Decode(&result); err != nil {
		return nil, storageError("decode_summary", err)
	}
	if err := validateSummary(result.Summary, recordID); err != nil {
		return nil, err
	}
	return &result.Summary, nil
}

// WalkSummaries 逐条访问本 Chat 的耐久摘要，不累计列表，也不读取当前文件或 Git 对象。
// 宿主可在 Recover 后据执行身份窄读日志引用，避免为恢复文件成果重放全部工具正文。
func WalkSummaries(ownedDir string, visit func(Summary) error) error {
	if !filepath.IsAbs(ownedDir) {
		return status_error.WorkspacePathInvalid
	}
	root, err := os.OpenRoot(ownedDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return storageError("walk_summaries_root", err)
	}
	defer root.Close()
	directory, err := root.Open(filepath.Join("changes", "records"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return storageError("walk_summaries", err)
	}
	defer directory.Close()
	for {
		entries, readErr := directory.ReadDir(128)
		for _, entry := range entries {
			if !entry.IsDir() || !validID(entry.Name()) {
				continue
			}
			summary, err := ReadSummary(ownedDir, entry.Name())
			if err != nil {
				return err
			}
			if err := visit(*summary); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return storageError("walk_summaries", readErr)
		}
	}
}

// Recover 在宿主恢复阶段将遗留 pending 标为 incomplete；不会补造结束侧快照。
// 活动采集持有的逐记录租约会被跳过，其基线对象仍作为垃圾回收的保留根。
func Recover(ownedDir string) error {
	owned, err := prepareOwned(ownedDir)
	if err != nil {
		return storageError("recover_root", err)
	}
	c := &Capture{owned: owned}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return c.withLock(ctx, func() error {
		entries, err := os.ReadDir(filepath.Join(base(owned), "records"))
		if err != nil {
			return storageError("recover_list", err)
		}
		records := []*record{}
		for _, item := range entries {
			if ctx.Err() != nil {
				return status_error.OperationCanceled
			}
			if !item.IsDir() || !validID(item.Name()) {
				continue
			}
			r, err := loadRecord(owned, item.Name())
			if err == status_error.ChatMessageFileChangeNotFound {
				root, openErr := os.OpenRoot(owned)
				if openErr != nil {
					return storageError("recover_orphan_root", openErr)
				}
				removeErr := root.RemoveAll(filepath.Join("changes", "records", item.Name()))
				root.Close()
				if removeErr != nil {
					return storageError("recover_orphan_record", removeErr)
				}
				continue
			}
			if err != nil {
				return err
			}
			records = append(records, r)
			lease := flock.New(filepath.Join(filepath.Dir(recordPath(owned, item.Name())), "capture.lock"))
			available, leaseErr := lease.TryLock()
			if leaseErr != nil {
				lease.Close()
				return storageError("recover_lease", leaseErr)
			}
			if !available {
				lease.Close()
				continue
			}
			if err := cleanupScratch(owned, item.Name()); err != nil {
				lease.Unlock()
				lease.Close()
				return storageError("recover_scratch", err)
			}
			if r.Summary.State != "pending" {
				lease.Unlock()
				lease.Close()
				continue
			}
			r.Summary.State, r.Summary.Reason = "incomplete", "process_interrupted"
			r.Summary.Version++
			if err := saveRecord(owned, r); err != nil {
				lease.Unlock()
				lease.Close()
				return err
			}
			lease.Unlock()
			lease.Close()
		}
		return collectGarbage(ctx, owned, records)
	})
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func freezeRoots(paths []string) ([]Root, error) {
	var roots []Root
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return nil, status_error.WorkspaceScopeInvalid
		}
		path, err := filepath.EvalSymlinks(filepath.Clean(path))
		if err != nil {
			return nil, status_error.WorkspaceScopeInvalid
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return nil, status_error.WorkspaceScopeInvalid
		}
		roots = append(roots, Root{ID: identity(path), Path: path, Kind: "shadow"})
	}
	sort.Slice(roots, func(i, j int) bool {
		if len(roots[i].Path) != len(roots[j].Path) {
			return len(roots[i].Path) < len(roots[j].Path)
		}
		return roots[i].Path < roots[j].Path
	})
	result := []Root{}
	for _, rt := range roots {
		duplicate := false
		for _, parent := range result {
			if rt.Path == parent.Path || inside(parent.Path, rt.Path) && !hasRepositoryBoundary(parent.Path, rt.Path) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			rt.RepositoryRoot = locateRepository(rt.Path)
			if rt.RepositoryRoot != "" {
				rt.Kind = "git"
			}
			result = append(result, rt)
		}
	}
	return result, nil
}
