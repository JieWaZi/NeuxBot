package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

const maximumArchiveBytes = 100 * 1024 * 1024

// ArtifactCleanup 汇总已核验的插件公开资料和完整归档。
type ArtifactCleanup struct {
	// Bytes 是预览可清理或本次已删除的逻辑字节数。
	Bytes int64
	// Files 是预览可清理或本次已删除的文件数。
	Files int
	// Skipped 是因为存在活动下载、读取租约或文件已变化而保留的候选数。
	Skipped int
}

type artifactDirectory struct {
	// name 是受管分发根内的固定目录。
	name string
	// suffix 是原分发器提交的不可变文件后缀。
	suffix string
	// limit 是本类分发对象允许的最大文件字节数。
	limit int64
	// locks 与现有下载、资料读取及安装使用同一路径的操作锁。
	locks *sync.Map
}

var cleanupDirectories = [...]artifactDirectory{
	{name: "details", suffix: ".json", limit: maxDetailsBytes, locks: &detailsLocks},
	{name: "archives", suffix: ".zip", limit: maximumArchiveBytes, locks: &archiveLocks},
}

// PreviewCachedArtifacts 在已登记的 Core 分发根内测量当前可回收的验证对象。
func PreviewCachedArtifacts(ctx context.Context, source string) (ArtifactCleanup, error) {
	index, err := liveArtifactOwner(source)
	if err != nil {
		return ArtifactCleanup{}, err
	}
	return inspectArtifacts(ctx, index, nil, nil)
}

// ClearCachedArtifacts 暂停当前预取，删除未被下载或读取租约保护的验证对象。
func ClearCachedArtifacts(ctx context.Context, source string) (ArtifactCleanup, error) {
	index, err := liveArtifactOwner(source)
	if err != nil {
		return ArtifactCleanup{}, err
	}
	index.mu.Lock()
	if index.archivePaused {
		index.mu.Unlock()
		return ArtifactCleanup{}, status_error.Conflict
	}
	index.archiveEpoch++
	index.archivePaused = true
	index.archivePending = false
	flight := index.archiveFlight
	if flight != nil {
		index.archiveCancel()
	}
	index.mu.Unlock()
	defer func() {
		index.mu.Lock()
		index.archivePaused = false
		index.mu.Unlock()
	}()

	if flight != nil {
		select {
		case <-flight.done:
		case <-ctx.Done():
			return ArtifactCleanup{}, status_error.OperationCanceled
		}
	}
	return inspectArtifacts(ctx, index, nil, func(remove func() error) error { return remove() })
}

// PruneUnreferencedArtifacts 回收当前已核验轻索引不再引用的空闲分发对象。
func PruneUnreferencedArtifacts(ctx context.Context, source string) error {
	index, err := liveArtifactOwner(source)
	if err != nil {
		return err
	}
	return pruneUnreferencedArtifacts(ctx, index)
}

func liveArtifactOwner(source string) (*remoteIndex, error) {
	value, exists := remoteIndexes.Load(source)
	if !exists {
		return nil, status_error.StorageRootInvalid
	}
	index := value.(*remoteIndex)
	if !index.assets || index.ctx.Err() != nil {
		return nil, status_error.StorageRootInvalid
	}
	return index, nil
}

func pruneUnreferencedArtifacts(ctx context.Context, index *remoteIndex) error {
	index.mu.RLock()
	snapshot, revision, paused := index.snapshot.Content, index.snapshotRevision, index.archivePaused
	index.mu.RUnlock()
	if paused || len(snapshot) == 0 {
		return nil
	}
	// 当前轻索引只拥有自己的引用；如果多个来源复用同一根，则保守保留。
	shared := false
	remoteIndexes.Range(func(_, value any) bool {
		other := value.(*remoteIndex)
		shared = other != index && other.ctx.Err() == nil && filepath.Clean(other.directory) == filepath.Clean(index.directory)
		return !shared
	})
	if shared {
		return nil
	}
	entries, err := parseIndex(snapshot)
	if err != nil {
		return err
	}
	protected := make(map[string]bool, len(entries)*2)
	for _, entry := range entries {
		protected[entry.DetailsSHA256+".json"] = true
		protected[entry.ArchiveSHA256+".zip"] = true
	}
	_, err = inspectArtifacts(ctx, index, protected, func(remove func() error) error {
		index.mu.RLock()
		defer index.mu.RUnlock()
		if index.snapshotRevision != revision || index.archivePaused {
			return fs.ErrNotExist
		}
		return remove()
	})
	return err
}

func inspectArtifacts(
	ctx context.Context,
	index *remoteIndex,
	protected map[string]bool,
	commit func(remove func() error) error,
) (ArtifactCleanup, error) {
	var result ArtifactCleanup
	if ctx.Err() != nil {
		return result, status_error.OperationCanceled
	}
	probe, cancel := context.WithTimeout(ctx, constants.StorageScanMaximumSeconds*time.Second)
	defer cancel()
	stop := context.AfterFunc(index.ctx, cancel)
	defer stop()

	info, err := os.Lstat(index.directory)
	if errors.Is(err, fs.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, artifactCleanupError("stat_root", "", err)
	}
	if !info.IsDir() {
		return result, status_error.StorageRootInvalid
	}
	root, err := os.OpenRoot(index.directory)
	if err != nil {
		return result, artifactCleanupError("open_root", "", err)
	}
	defer root.Close()
	same, err := root.Lstat(".")
	if err != nil || !os.SameFile(info, same) {
		return result, status_error.StorageRootInvalid
	}

	remaining := constants.StorageScanMaximumEntries
	for _, spec := range cleanupDirectories {
		if err := inspectArtifactDirectory(probe, root, info, spec, protected, commit, &result, &remaining); err != nil {
			if ctx.Err() != nil {
				return result, status_error.OperationCanceled
			}
			return result, err
		}
	}
	return result, nil
}

func inspectArtifactDirectory(
	ctx context.Context,
	root *os.Root,
	rootInfo fs.FileInfo,
	spec artifactDirectory,
	protected map[string]bool,
	commit func(remove func() error) error,
	result *ArtifactCleanup,
	remaining *int,
) error {
	info, err := root.Lstat(spec.name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return artifactCleanupError("stat_directory", spec.name, err)
	}
	if !info.IsDir() {
		return status_error.StorageRootInvalid
	}
	directory, err := root.OpenRoot(spec.name)
	if err != nil {
		return artifactCleanupError("open_directory", spec.name, err)
	}
	defer directory.Close()
	listing, err := directory.Open(".")
	if err != nil {
		return artifactCleanupError("read_directory", spec.name, err)
	}
	defer listing.Close()
	actual, err := listing.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return status_error.StorageRootInvalid
	}
	for ctx.Err() == nil {
		entries, readErr := listing.ReadDir(min(128, *remaining+1))
		for _, entry := range entries {
			if ctx.Err() != nil || *remaining == 0 {
				return status_error.StorageScanIncomplete
			}
			*remaining = *remaining - 1
			name := entry.Name()
			digest, hasSuffix := strings.CutSuffix(name, spec.suffix)
			if !hasSuffix || !validArtifactDigest(digest) || protected[name] {
				continue
			}
			remove := func() error {
				currentRoot, err := os.Lstat(root.Name())
				if err != nil {
					return err
				}
				currentDir, err := root.Lstat(spec.name)
				if err != nil {
					return err
				}
				if !os.SameFile(rootInfo, currentRoot) || !os.SameFile(info, currentDir) {
					return fs.ErrInvalid
				}
				return directory.Remove(name)
			}
			if err := inspectArtifact(ctx, directory, spec, name, digest, commit, remove, result); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return artifactCleanupError("read_directory", spec.name, readErr)
		}
	}
	return status_error.StorageScanIncomplete
}

func inspectArtifact(
	ctx context.Context,
	directory *os.Root,
	spec artifactDirectory,
	name, digest string,
	commit func(remove func() error) error,
	remove func() error,
	result *ArtifactCleanup,
) error {
	state := artifactState(spec.locks, filepath.Join(directory.Name(), name))
	select {
	case state.gate <- struct{}{}:
		defer state.release()
	default:
		result.Skipped++
		return nil
	}
	if state.readers.Load() > 0 {
		result.Skipped++
		return nil
	}
	info, err := directory.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return artifactCleanupError("stat_artifact", spec.name, err)
	}
	if !info.Mode().IsRegular() || info.Size() > spec.limit {
		return nil
	}
	file, err := directory.Open(name)
	if err != nil {
		return artifactCleanupError("open_artifact", spec.name, err)
	}
	checked, err := file.Stat()
	if err != nil || !checked.Mode().IsRegular() || !os.SameFile(info, checked) {
		_ = file.Close()
		result.Skipped++
		return nil
	}
	hash := sha256.New()
	written, readErr := io.Copy(hash, io.LimitReader(artifactCleanupReader{ctx: ctx, reader: file}, info.Size()+1))
	_ = file.Close()
	if readErr != nil {
		if ctx.Err() != nil {
			return status_error.StorageScanIncomplete
		}
		return artifactCleanupError("hash_artifact", spec.name, readErr)
	}
	if written != info.Size() || hex.EncodeToString(hash.Sum(nil)) != digest {
		return nil
	}
	after, err := directory.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && (!os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()))) {
		result.Skipped++
		return nil
	}
	if err != nil {
		return artifactCleanupError("restat_artifact", spec.name, err)
	}
	if ctx.Err() != nil {
		return status_error.StorageScanIncomplete
	}
	if commit != nil {
		if err := commit(remove); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				result.Skipped++
				return nil
			}
			return artifactCleanupError("remove_artifact", spec.name, err)
		}
		state.failure = nil
	}
	result.Files++
	result.Bytes += written
	return nil
}

func validArtifactDigest(value string) bool {
	if len(value) != 2*sha256.Size {
		return false
	}
	for _, value := range value {
		if (value < '0' || value > '9') && (value < 'a' || value > 'f') {
			return false
		}
	}
	return true
}

func artifactCleanupError(operation, directory string, err error) error {
	logx.Warnf("plugin cached artifact maintenance failed: operation=%s directory=%s error=%v", operation, directory, err)
	return status_error.StorageCleanupFailed
}

type artifactCleanupReader struct {
	// ctx 在每块文件读取前检查 Core 生命周期及清理预算。
	ctx context.Context
	// reader 提供已验证为常规文件的读取流。
	reader io.Reader
}

func (reader artifactCleanupReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}
