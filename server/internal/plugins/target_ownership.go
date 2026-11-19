package plugins

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	storagefs "github.com/JieWaZi/NeuxBot/server/internal/storage"
)

const (
	targetOwnersDirectory   = ".owners"
	targetReceiptVersion    = 1
	targetReceiptMaxBytes   = 2048
	targetStageMarkerPrefix = ".neuxbot-target-stage-"
	targetStageMarkerLength = 26
)

// TargetRequest 固定原包、插件身份和需要生成的原生目标。
type TargetRequest struct {
	// PluginID 是逻辑插件身份。
	PluginID string
	// SourceRoot 是已选择的不可变插件树。
	SourceRoot string
	// SourceDigest 是可选的、调用者已核验的源树摘要。
	SourceDigest string
	// Package 是待物化的插件组件清单。
	Package Package
	// Runtime 是目标所属的原生运行时。
	Runtime string
}

// TargetCleanup 汇总当前已核验且未使用的派生目标文件。
type TargetCleanup struct {
	// Bytes 是可清理或实际删除的普通文件逻辑大小。
	Bytes int64
	// Files 是可清理或实际删除的普通文件数量。
	Files int
	// Skipped 是受活动使用或未知归属保护的候选目录数量。
	Skipped int
}

type targetReceipt struct {
	// Version 是受管派生目标归属记录的格式。
	Version int `json:"version"`
	// Stage 标识本应用创建而尚未发布的临时目录。
	Stage bool `json:"stage,omitempty"`
	// Marker 是生成时写入暂存本体的随机归属标记。
	Marker string `json:"marker,omitempty"`
	// PluginID 是参与正式目标路径身份的逻辑插件。
	PluginID string `json:"pluginId,omitempty"`
	// SourceDigest 是生成目标时原树的摘要。
	SourceDigest string `json:"sourceDigest,omitempty"`
	// Runtime 是生成目标的运行时。
	Runtime string `json:"runtime,omitempty"`
	// Digest 是已发布目标完整内容的摘要。
	Digest string `json:"digest,omitempty"`
}

type targetCacheUsage struct {
	// preparation 在生成与登记租约期间阻止回收同一根。
	preparation sync.RWMutex
	// mutex 保护已发布物理目录的引用计数。
	mutex sync.Mutex
	// active 保存仍可被原生或准备流程读取的精确派生目录。
	active map[string]int
}

// 同一进程内指向相同真实根的存储对象共享生成和使用保护。
var targetCacheUsages sync.Map

type targetCacheOwner struct {
	// root 锚定受管派生目录，只通过固定子项名称回收。
	root *os.Root
	// identity 是打开的受管根身份，删除前校验未被换绑。
	identity fs.FileInfo
	// receipts 锚定归属记录目录，记录不进入原生内容摘要。
	receipts *os.Root
	// usage 与同一真实受管根的生成、交付共享使用保护。
	usage *targetCacheUsage
}

// PrepareTarget 生成受管派生目标，返回必须随实际读取者释放的租约。
func (s *ContentStore) PrepareTarget(ctx context.Context, request TargetRequest) (NativeTarget, func(), error) {
	if err := ctx.Err(); err != nil {
		return NativeTarget{}, nil, status_error.OperationCanceled
	}
	owner, err := s.openTargetCache(true)
	if err != nil {
		return NativeTarget{}, nil, err
	}
	defer owner.close()
	owner.usage.preparation.RLock()
	defer owner.usage.preparation.RUnlock()
	target, err := prepareNativeTarget(ctx, owner.root.Name(), request.PluginID, request.Package, request.SourceRoot, request.Runtime, request.SourceDigest, owner)
	if err != nil {
		return NativeTarget{}, nil, err
	}
	// Codex 可以直接消费原包；原有内容租约拥有该路径。
	if !targetOwnerChild(owner.root.Name(), target.Root) {
		return target, func() {}, nil
	}
	name := filepath.Base(target.Root)
	if err := owner.writeReceipt(name, targetReceipt{
		Version: targetReceiptVersion, PluginID: target.ID, SourceDigest: target.SourceDigest,
		Runtime: target.Runtime, Digest: target.Digest,
	}); err != nil {
		return NativeTarget{}, nil, err
	}
	usage := owner.usage
	usage.mutex.Lock()
	usage.active[name]++
	usage.mutex.Unlock()
	return target, sync.OnceFunc(func() {
		usage.mutex.Lock()
		defer usage.mutex.Unlock()
		usage.active[name]--
		if usage.active[name] == 0 {
			delete(usage.active, name)
		}
	}), nil
}

// PrepareAllTargets 为安装准备按原生成次序核验全部目标。
func (s *ContentStore) PrepareAllTargets(ctx context.Context, request TargetRequest) error {
	digest := request.SourceDigest
	if digest == "" {
		var err error
		digest, err = ContentDigest(request.SourceRoot)
		if err != nil {
			return err
		}
	}
	request.SourceDigest = digest
	for _, target := range targetRuntimes {
		request.Runtime = target
		_, release, err := s.PrepareTarget(ctx, request)
		if err != nil {
			return err
		}
		release()
	}
	return nil
}

// PreviewCachedTargets 统计当前可清理的派生目标和已失去所有者的暂存。
func (s *ContentStore) PreviewCachedTargets(ctx context.Context) (TargetCleanup, error) {
	return s.inspectCachedTargets(ctx, false)
}

// ClearCachedTargets 清理空闲、归属可验证的派生目标及暂存。
func (s *ContentStore) ClearCachedTargets(ctx context.Context) (TargetCleanup, error) {
	return s.inspectCachedTargets(ctx, true)
}

func (s *ContentStore) openTargetCache(create bool) (*targetCacheOwner, error) {
	canonical, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return nil, storageFailure(err)
	}
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return nil, storageFailure(err)
	}
	defer root.Close()
	pluginsRoot, err := openTargetSubdirectory(root, "plugins", create)
	if err != nil {
		return nil, err
	}
	defer pluginsRoot.Close()
	cache, err := openTargetSubdirectory(pluginsRoot, "targets", create)
	if err != nil {
		return nil, err
	}
	identity, err := cache.Lstat(".")
	if err != nil {
		cache.Close()
		return nil, storageFailure(err)
	}
	receipts, err := openTargetSubdirectory(cache, targetOwnersDirectory, create)
	if err != nil && (create || !errors.Is(err, fs.ErrNotExist)) {
		cache.Close()
		return nil, err
	}
	key := cache.Name()
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	value, _ := targetCacheUsages.LoadOrStore(key, &targetCacheUsage{active: map[string]int{}})
	return &targetCacheOwner{root: cache, identity: identity, receipts: receipts, usage: value.(*targetCacheUsage)}, nil
}

func openTargetSubdirectory(parent *os.Root, name string, create bool) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if create && errors.Is(err, fs.ErrNotExist) {
		if err = parent.Mkdir(name, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, storageFailure(err)
		}
		info, err = parent.Lstat(name)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fs.ErrNotExist
	}
	if err != nil {
		return nil, storageFailure(err)
	}
	if !info.IsDir() {
		return nil, status_error.PluginContentInvalid
	}
	directory, err := parent.OpenRoot(name)
	if err != nil {
		return nil, storageFailure(err)
	}
	actual, err := directory.Lstat(".")
	if err != nil || !os.SameFile(info, actual) {
		directory.Close()
		return nil, status_error.PluginContentInvalid
	}
	return directory, nil
}

func (owner *targetCacheOwner) close() {
	if owner.receipts != nil {
		owner.receipts.Close()
	}
	owner.root.Close()
}

func (owner *targetCacheOwner) writeReceipt(name string, receipt targetReceipt) error {
	if !receipt.validFor(name) {
		return status_error.PluginContentInvalid
	}
	if previous, ok := owner.readReceipt(name); ok && previous == receipt {
		return nil
	}
	data, err := json.Marshal(receipt)
	if err != nil || len(data) > targetReceiptMaxBytes {
		return status_error.PluginContentInvalid
	}
	temporary := "receipt-" + rand.Text()
	file, err := owner.receipts.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return storageFailure(err)
	}
	defer owner.receipts.Remove(temporary)
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return storageFailure(err)
	}
	if err := owner.receipts.Rename(temporary, name+".json"); err != nil {
		return storageFailure(err)
	}
	return nil
}

func (owner *targetCacheOwner) readReceipt(name string) (targetReceipt, bool) {
	var receipt targetReceipt
	if owner.receipts == nil {
		return receipt, false
	}
	location := name + ".json"
	info, err := owner.receipts.Lstat(location)
	if err != nil || !info.Mode().IsRegular() || info.Size() > targetReceiptMaxBytes {
		return receipt, false
	}
	file, err := owner.receipts.Open(location)
	if err != nil {
		return receipt, false
	}
	defer file.Close()
	current, err := file.Stat()
	if err != nil || !os.SameFile(info, current) {
		return receipt, false
	}
	data, err := io.ReadAll(io.LimitReader(file, targetReceiptMaxBytes+1))
	if err != nil || len(data) > targetReceiptMaxBytes || json.Unmarshal(data, &receipt) != nil || !receipt.validFor(name) {
		return targetReceipt{}, false
	}
	return receipt, true
}

func (receipt targetReceipt) validFor(name string) bool {
	if receipt.Version != targetReceiptVersion || !filepath.IsLocal(name) || filepath.Base(name) != name {
		return false
	}
	if receipt.Stage {
		token, matched := strings.CutPrefix(name, "native-")
		return matched && isDecimalTargetToken(token) && len(receipt.Marker) == targetStageMarkerLength &&
			strings.Trim(receipt.Marker, "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567") == "" &&
			receipt.PluginID == "" && receipt.SourceDigest == "" && receipt.Runtime == "" && receipt.Digest == ""
	}
	if receipt.Marker != "" || receipt.PluginID == "" || !registeredTarget(receipt.Runtime) || !validTargetDigest(receipt.SourceDigest) || !validTargetDigest(receipt.Digest) {
		return false
	}
	key := targetCacheKey(receipt.PluginID, receipt.SourceDigest, receipt.Runtime, receipt.Digest)
	token, matched := strings.CutPrefix(name, key+"-repair-")
	return name == key || (matched && isDecimalTargetToken(token))
}

func isDecimalTargetToken(token string) bool {
	return token != "" && strings.Trim(token, "0123456789") == ""
}

func looksLikeTargetCacheDirectory(name string) bool {
	if token, staging := strings.CutPrefix(name, "native-"); staging {
		return isDecimalTargetToken(token)
	}
	digest, token, repair := strings.Cut(name, "-repair-")
	return validTargetDigest(digest) && (!repair || isDecimalTargetToken(token))
}

func (owner *targetCacheOwner) beginStage(stage string) error {
	name := filepath.Base(stage)
	marker := rand.Text()[:targetStageMarkerLength]
	path := filepath.Join(name, targetStageMarkerPrefix+marker)
	file, err := owner.root.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return storageFailure(err)
	}
	_, writeErr := io.WriteString(file, marker)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return storageFailure(err)
	}
	return owner.writeReceipt(name, targetReceipt{Version: targetReceiptVersion, Stage: true, Marker: marker})
}

func (owner *targetCacheOwner) hasStageMarker(name, marker string) bool {
	path := filepath.Join(name, targetStageMarkerPrefix+marker)
	info, err := owner.root.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(marker)) {
		return false
	}
	file, err := owner.root.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(len(marker)+1)))
	return err == nil && string(data) == marker
}

func targetOwnerChild(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != "." && filepath.IsLocal(relative) && filepath.Base(relative) == relative
}

func (s *ContentStore) inspectCachedTargets(ctx context.Context, clear bool) (TargetCleanup, error) {
	var result TargetCleanup
	if ctx.Err() != nil {
		return result, status_error.OperationCanceled
	}
	scan, cancel := context.WithTimeout(ctx, constants.StorageScanMaximumSeconds*time.Second)
	defer cancel()
	owner, err := s.openTargetCache(false)
	if errors.Is(err, fs.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer owner.close()
	records, err := owner.root.Open(".")
	if err != nil {
		return result, storageFailure(err)
	}
	defer records.Close()
	visited := 0
	for scan.Err() == nil {
		batch, readErr := records.ReadDir(min(128, constants.StorageScanMaximumEntries-visited+1))
		for _, record := range batch {
			if scan.Err() != nil {
				break
			}
			if visited == constants.StorageScanMaximumEntries {
				return result, status_error.StorageScanIncomplete
			}
			visited++
			name := record.Name()
			if !looksLikeTargetCacheDirectory(name) {
				continue
			}
			if err := owner.inspectTarget(scan, name, clear, &result); err != nil {
				if scan.Err() != nil && ctx.Err() == nil {
					return result, status_error.StorageScanIncomplete
				}
				return result, err
			}
		}
		if errors.Is(readErr, io.EOF) && scan.Err() == nil {
			return result, nil
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return result, storageFailure(readErr)
		}
	}
	if ctx.Err() != nil {
		return result, status_error.OperationCanceled
	}
	return result, status_error.StorageScanIncomplete
}

func (owner *targetCacheOwner) inspectTarget(ctx context.Context, name string, clear bool, result *TargetCleanup) error {
	if !owner.usage.preparation.TryLock() {
		result.Skipped++
		return nil
	}
	defer owner.usage.preparation.Unlock()
	owner.usage.mutex.Lock()
	active := owner.usage.active[name] != 0
	owner.usage.mutex.Unlock()
	if active {
		result.Skipped++
		return nil
	}
	receipt, verified := owner.readReceipt(name)
	if !verified || (receipt.Stage && !owner.hasStageMarker(name, receipt.Marker)) {
		result.Skipped++
		return nil
	}
	info, err := owner.root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return storageFailure(err)
	}
	if !info.IsDir() {
		result.Skipped++
		return nil
	}
	path := filepath.Join(owner.root.Name(), name)
	known, err := storagefs.Measure(ctx, storagefs.Roots{Data: path, Managed: path}, storagefs.Limits{})
	if err != nil {
		return err
	}
	if !known.Complete || (!receipt.Stage && !verifiedTargetContent(path, receipt.Digest, known.Entries)) {
		result.Skipped++
		return nil
	}
	if ctx.Err() != nil {
		return status_error.OperationCanceled
	}
	rootInfo, rootErr := os.Lstat(owner.root.Name())
	current, err := owner.root.Lstat(name)
	if rootErr != nil || err != nil || !os.SameFile(owner.identity, rootInfo) ||
		!os.SameFile(info, current) || !info.ModTime().Equal(current.ModTime()) {
		result.Skipped++
		return nil
	}
	if clear {
		if err := owner.root.RemoveAll(name); err != nil {
			return storageFailure(err)
		}
	}
	result.Bytes += known.Bytes
	result.Files += known.Files
	if clear {
		if err := owner.receipts.Remove(name + ".json"); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return storageFailure(err)
		}
	}
	return nil
}

func verifiedTargetContent(path, digest string, entries []storagefs.FileEntry) bool {
	for _, entry := range entries {
		relative, err := filepath.Rel(path, entry.Path)
		if err != nil || strings.HasPrefix(filepath.ToSlash(relative), ".git/") {
			return false
		}
	}
	return VerifyContent(path, digest) == nil
}
