package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// SourceState 描述目录缓存与本次同步状态，不把同步失败伪装为空库。
type SourceState struct {
	// State 是available/cached/stale/local。
	State string `json:"state"`
	// FetchedAt 是最后成功同步的毫秒时间。
	FetchedAt int64 `json:"fetchedAt"`
	// ErrorCode 是同步失败的稳定错误。
	ErrorCode string `json:"errorCode,omitempty"`
}

// storedIndex 将完整目录与 HTTP 校验信息一起原子保存，避免元数据和正文串版本。
type storedIndex struct {
	// 上次核验成功的完整目录
	Content json.RawMessage `json:"content"`
	// 上游内容实体标识
	ETag string `json:"etag,omitempty"`
	// 上游内容修改时间
	LastModified string `json:"lastModified,omitempty"`
	// 上次核验成功时间
	FetchedAt int64 `json:"fetchedAt"`
}

// remoteIndex 在一次 Core 生命周期内共享一个来源的更新和读取快照。
type remoteIndex struct {
	// assets 表示当前 Core 所有者负责后台归档预取。
	assets bool
	// archiveFlight 合并当前归档预取，不占用轻索引请求的完成边界。
	archiveFlight *indexRefresh
	// archivePending 记录预取期间发布的新索引，当前批次结束后补齐。
	archivePending bool
	// archiveWorkers 让 Core 退出等待全部归档工作。
	archiveWorkers sync.WaitGroup
	// transfers 限制归档预取与交互下载共享的并发传输。
	transfers chan struct{}
	// directory 保存来源目录、公开资料与完整归档。
	directory string
	// flight 合并同来源当前在途的条件请求。
	flight *indexRefresh
	// recoveryGate 串行合并分发恢复，允许调用方取消等待。
	recoveryGate chan struct{}
	// recoveryAt 限制同来源失败恢复的频率，稍后重试仍可重新核验。
	recoveryAt time.Time
	// recoveryEntry 绑定最近失败的实际归档，目录变更后不会复用旧错误。
	recoveryEntry Entry
	// recoveryError 在短冷却期间复用同一分发故障，防止并发请求重复长等待。
	recoveryError error
	// recoveryDocumentation 区分资料故障和完整安装包故障，不能相互冒充。
	recoveryDocumentation bool
	// 快照与状态读写保护
	mu sync.RWMutex
	// 当前已核验的持久快照
	snapshot storedIndex
	// 对外同步状态
	state SourceState
	// 无缓存时的同步错误
	err error
	// 当前来源的固定目录地址
	source string
	// 持久缓存位置
	filename string
	// 启动同步的取消边界
	ctx context.Context
	// 每次启动只同步一次
	once sync.Once
	// 同步退出及通知完成信号
	done chan struct{}
	// 同步完成后唤醒既有资源查询
	changed func()
}

// indexRefresh 只保存一次在途核验的完成信号和最终结果。
type indexRefresh struct {
	// done 在结果发布后关闭。
	done chan struct{}
	// err 是本次核验错误，不被下一次核验覆盖。
	err error
}

var remoteIndexes sync.Map

// Start 注册已有轻索引并后台更新，归档独立预取，关闭时取消并等待全部工作。
func Start(ctx context.Context, source, directory string, changed func()) func() {
	ctx, cancel := context.WithCancel(ctx)
	index := newRemoteIndex(ctx, source, cacheFilename(directory, source), changed)
	index.assets = true
	remoteIndexes.Store(source, index)
	index.startRefresh()
	return func() {
		cancel()
		<-index.done
		index.mu.RLock()
		flight := index.flight
		index.mu.RUnlock()
		if flight != nil {
			<-flight.done
		}
		index.archiveWorkers.Wait()
	}
}

func cacheFilename(directory, source string) string {
	sum := sha256.Sum256([]byte(source))
	return filepath.Join(directory, "catalog", hex.EncodeToString(sum[:])+".json")
}

func indexCache(source string) string {
	if value, ok := remoteIndexes.Load(source); ok {
		return value.(*remoteIndex).filename
	}
	return cacheFilename(filepath.Join(os.TempDir(), "neuxbot-plugin-catalog"), source)
}

func newRemoteIndex(ctx context.Context, source, filename string, changed func()) *remoteIndex {
	index := &remoteIndex{directory: filepath.Dir(filepath.Dir(filename)), ctx: ctx, source: source, filename: filename, recoveryGate: make(chan struct{}, 1), done: make(chan struct{}), changed: changed, state: SourceState{State: "unavailable"}, transfers: make(chan struct{}, 2)}
	if data, err := os.ReadFile(filename); err == nil && len(data) <= 5*1024*1024 {
		// 仅接受当前持久快照结构，不为旧裸目录增加兼容读取。
		if json.Unmarshal(data, &index.snapshot) != nil || len(index.snapshot.Content) == 0 {
			index.snapshot = storedIndex{}
		}
		if _, err := parseIndex(index.snapshot.Content); err == nil {
			index.state = SourceState{State: "cached", FetchedAt: index.snapshot.FetchedAt}
		} else {
			index.snapshot = storedIndex{}
		}
	}
	if len(index.snapshot.Content) == 0 && !httpAddress(source) {
		if raw, err := os.ReadFile(filepath.Join(source, "catalog.json")); err == nil {
			if _, err := parseIndex(raw); err == nil {
				index.snapshot.Content = raw
				index.state = SourceState{State: "local"}
			}
		}
	}
	return index
}

func remoteSource(source string) *remoteIndex {
	if value, ok := remoteIndexes.Load(source); ok {
		return value.(*remoteIndex)
	}
	index := newRemoteIndex(context.Background(), source, indexCache(source), nil)
	actual, _ := remoteIndexes.LoadOrStore(source, index)
	return actual.(*remoteIndex)
}

// State 返回当前来源的同步状态，读取本身不会访问远端。
func State(source string) SourceState {
	if value, ok := remoteIndexes.Load(source); ok {
		index := value.(*remoteIndex)
		index.mu.RLock()
		defer index.mu.RUnlock()
		return index.state
	}
	return SourceState{State: "local"}
}

// Snapshot 返回当前可读的轻量目录与同批来源状态，不启动远程同步或下载归档。
func Snapshot(source string) ([]Entry, SourceState) {
	_, registered := remoteIndexes.Load(source)
	if !httpAddress(source) && !registered {
		entries, err := List(source)
		if err != nil {
			return nil, SourceState{State: "unavailable", ErrorCode: sourceErrorCode(err)}
		}
		return entries, SourceState{State: "local"}
	}
	index := remoteSource(source)
	index.mu.RLock()
	data, state := index.snapshot.Content, index.state
	index.mu.RUnlock()
	if len(data) == 0 {
		return nil, state
	}
	entries, err := parseIndex(data)
	if err != nil {
		return nil, SourceState{State: "unavailable", ErrorCode: sourceErrorCode(err)}
	}
	for i := range entries {
		entries[i].SourceKey = SourceKey(source, entries[i].Key)
		entries[i].cacheRoot = index.directory
		entries[i].transfers = index.transfers
	}
	return entries, state
}

func sourceErrorCode(err error) string {
	if coded, ok := err.(interface{ Key() string }); ok {
		return coded.Key()
	}
	return status_error.PluginStorageError.Key()
}

// Refresh 显式核验当前目录；并发请求共享同一次同步，失败继续保留旧快照。
func Refresh(ctx context.Context, source string) error {
	if value, ok := remoteIndexes.Load(source); ok {
		return value.(*remoteIndex).refreshCurrent(ctx)
	}
	if !httpAddress(source) {
		_, err := List(source)
		return err
	}
	return remoteSource(source).refreshCurrent(ctx)
}

// RefreshInBackground 接纳 Core 持有的轻索引更新，归档预取使用独立完成边界。
func RefreshInBackground(source string) error {
	value, exists := remoteIndexes.Load(source)
	if !exists || !value.(*remoteIndex).assets {
		// 只由Start登记的Core所有者接纳完整后台下载，临时只读索引不能创建无取消边界的任务。
		return status_error.PluginDistributionUnavailable
	}
	_, err := value.(*remoteIndex).refreshInBackground()
	return err
}

// beginRefresh 在返回前预约唯一在途身份，避免后台协程尚未运行时重复接纳。
func (index *remoteIndex) beginRefresh() (*indexRefresh, bool, error) {
	index.mu.Lock()
	defer index.mu.Unlock()
	if index.ctx.Err() != nil {
		return nil, false, status_error.PluginDistributionUnavailable
	}
	if index.flight != nil {
		return index.flight, false, nil
	}
	flight := &indexRefresh{done: make(chan struct{})}
	index.flight = flight
	return flight, true, nil
}

func (index *remoteIndex) refreshInBackground() (*indexRefresh, error) {
	flight, started, err := index.beginRefresh()
	if started {
		go index.runRefresh(index.ctx, flight)
	}
	return flight, err
}

func (index *remoteIndex) refreshCurrent(ctx context.Context) error {
	flight, started, err := index.beginRefresh()
	if err != nil {
		return err
	}
	if started {
		return index.runRefresh(ctx, flight)
	}
	select {
	case <-flight.done:
		return flight.err
	case <-ctx.Done():
		return status_error.PluginDistributionUnavailable
	}
}

func (index *remoteIndex) runRefresh(ctx context.Context, flight *indexRefresh) error {
	// 同步读取保留原调用方取消；后台启动与刷新只由Core生命周期取消。
	fetchCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(index.ctx, cancel)
	flight.err = index.fetch(fetchCtx)
	if index.assets && fetchCtx.Err() == nil {
		index.startArchivePrefetch()
	}
	stop()
	cancel()
	if index.changed != nil && index.ctx.Err() == nil {
		index.changed()
	}
	index.mu.Lock()
	close(flight.done)
	index.flight = nil
	index.mu.Unlock()
	return flight.err
}

func readIndex(source string) ([]byte, error) {
	_, registered := remoteIndexes.Load(source)
	if !httpAddress(source) && !registered {
		data, err := os.ReadFile(filepath.Join(source, "catalog.json"))
		if err != nil {
			logx.Warnf("read local plugin catalog failed: error=%v", err)
			return nil, status_error.PluginStorageError
		}
		return data, nil
	}
	index := remoteSource(source)
	index.mu.RLock()
	previous := index.snapshot.Content
	index.mu.RUnlock()
	if len(previous) != 0 {
		return previous, nil
	}
	// 首次无缓存的并发查询复用启动同步，既不伪造空目录，也不重复请求。
	if err := index.refresh(index.ctx); err != nil {
		return nil, err
	}
	index.mu.RLock()
	defer index.mu.RUnlock()
	return index.snapshot.Content, index.err
}

func (index *remoteIndex) startRefresh() {
	index.once.Do(func() {
		flight, _ := index.refreshInBackground()
		go func() {
			if flight != nil {
				<-flight.done
			}
			close(index.done)
		}()
	})
}

func (index *remoteIndex) refresh(ctx context.Context) error {
	index.startRefresh()
	select {
	case <-index.done:
		return nil
	case <-ctx.Done():
		return status_error.PluginDistributionUnavailable
	}
}

func (index *remoteIndex) fail(err error) error {
	index.mu.Lock()
	defer index.mu.Unlock()
	index.err = err
	if coded, ok := err.(interface{ Key() string }); ok {
		index.state.ErrorCode = coded.Key()
	}
	index.state.State = "unavailable"
	if len(index.snapshot.Content) != 0 {
		index.state.State = "stale"
		index.err = nil
	}
	return err
}

func (index *remoteIndex) fetch(ctx context.Context) error {
	fail := func(err error) error {
		if ctx.Err() != nil {
			return status_error.PluginDistributionUnavailable
		}
		return index.fail(err)
	}
	index.mu.RLock()
	next := index.snapshot
	index.mu.RUnlock()
	if !httpAddress(index.source) {
		raw, err := os.ReadFile(filepath.Join(index.source, "catalog.json"))
		if err != nil {
			logx.Warnf("read local plugin catalog failed: error=%v", err)
			return fail(status_error.PluginStorageError)
		}
		if _, err := parseIndex(raw); err != nil {
			return fail(err)
		}
		next.Content = raw
	} else {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, index.source, nil)
		if err != nil {
			return fail(status_error.PluginContentInvalid)
		}
		if next.ETag != "" {
			request.Header.Set("If-None-Match", next.ETag)
		}
		if next.LastModified != "" {
			request.Header.Set("If-Modified-Since", next.LastModified)
		}
		client := http.Client{Timeout: 15 * time.Second}
		started := time.Now()
		response, err := client.Do(request)
		if err != nil {
			logDistributionFailure("index_get", "", index.source, response, started, err)
			return fail(status_error.PluginDistributionUnavailable)
		}
		defer response.Body.Close()
		switch response.StatusCode {
		case http.StatusNotModified:
			if len(next.Content) == 0 {
				return fail(status_error.PluginContentInvalid)
			}
		case http.StatusOK:
			data, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
			if err != nil {
				logDistributionFailure("index_body", "", index.source, response, started, err)
				return fail(status_error.PluginDistributionUnavailable)
			}
			if _, err := parseIndex(data); err != nil {
				return fail(err)
			}
			next.Content = data
			next.ETag = response.Header.Get("ETag")
			next.LastModified = response.Header.Get("Last-Modified")
		default:
			logDistributionFailure("index_status", "", index.source, response, started, nil)
			return fail(status_error.PluginDistributionUnavailable)
		}
	}
	if ctx.Err() != nil {
		return fail(status_error.PluginDistributionUnavailable)
	}
	next.FetchedAt = time.Now().UnixMilli()
	if err := index.save(next); err != nil {
		logx.Warnf("persist plugin catalog failed: error=%v", err)
		return fail(status_error.PluginStorageError)
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	index.snapshot = next
	index.err = nil
	index.state = SourceState{State: "available", FetchedAt: next.FetchedAt}
	if !httpAddress(index.source) {
		index.state.State = "local"
	}
	return nil
}

func (index *remoteIndex) save(next storedIndex) error {
	if err := os.MkdirAll(filepath.Dir(index.filename), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(index.filename), "index-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	// 编码保持目录 JSON 的语义内容；HTTP 实体标识与同一份内容一并提交。
	var data bytes.Buffer
	if err := json.NewEncoder(&data).Encode(next); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data.Bytes()); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), index.filename)
}

// startArchivePrefetch 将可重建的归档缓存工作交给 Core，不延长索引请求。
func (index *remoteIndex) startArchivePrefetch() {
	index.mu.Lock()
	defer index.mu.Unlock()
	if index.ctx.Err() != nil {
		return
	}
	if index.archiveFlight != nil {
		index.archivePending = true
		return
	}
	flight := &indexRefresh{done: make(chan struct{})}
	index.archiveFlight = flight
	index.archiveWorkers.Add(1)
	go index.runArchivePrefetch(flight)
}

func (index *remoteIndex) runArchivePrefetch(flight *indexRefresh) {
	defer index.archiveWorkers.Done()
	for {
		if err := index.prefetchArchives(index.ctx); err != nil && index.ctx.Err() == nil {
			// 归档失败不抹去已经发布的目录；安装仍按同一摘要重试真实包。
			logx.Warnf("plugin archive prefetch failed: error_code=%s", sourceErrorCode(err))
		}
		index.mu.RLock()
		raw := index.snapshot.Content
		index.mu.RUnlock()
		if entries, err := parseIndex(raw); err == nil && index.ctx.Err() == nil {
			if err := pruneArtifacts(index.directory, entries, time.Now()); err != nil {
				logx.Warnf("prune plugin distribution cache failed: error_code=%s", sourceErrorCode(err))
			}
		}
		index.mu.Lock()
		if index.archivePending && index.ctx.Err() == nil {
			index.archivePending = false
			index.mu.Unlock()
			continue
		}
		index.archivePending = false
		index.archiveFlight = nil
		close(flight.done)
		index.mu.Unlock()
		return
	}
}

// prefetchArchives 只缓存已声明完整包，固定两路下载，不解析或执行其中程序。
func (index *remoteIndex) prefetchArchives(ctx context.Context) error {
	index.mu.RLock()
	raw := index.snapshot.Content
	index.mu.RUnlock()
	if len(raw) == 0 {
		return nil
	}
	entries, err := parseIndex(raw)
	if err != nil {
		return err
	}
	jobs := make(chan Entry)
	var workers sync.WaitGroup
	var mutex sync.Mutex
	var failure error
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for entry := range jobs {
				if ctx.Err() != nil {
					continue
				}
				entry.cacheRoot = index.directory
				entry.transfers = index.transfers
				if _, err := download(ctx, entry, true); err != nil {
					mutex.Lock()
					if failure == nil {
						failure = err
					}
					mutex.Unlock()
				}
			}
		}()
	}
	for _, entry := range entries {
		select {
		case jobs <- entry:
		case <-ctx.Done():
		}
	}
	close(jobs)
	workers.Wait()
	return failure
}
