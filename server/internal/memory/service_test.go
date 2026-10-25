package memory

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// fakeEmbedder 是测试用的确定性嵌入替身，按显式映射或首字符 one-hot 返回向量。
type fakeEmbedder struct {
	// mutex 保护以下可变字段。
	mutex sync.Mutex
	// vectorsByText 是按文本显式指定的向量映射。
	vectorsByText map[string][]float32
	// dimensions 是未命中映射时 one-hot 向量的维度。
	dimensions int
	// calls 是累计嵌入调用次数。
	calls int
	// fail 表示调用是否返回错误。
	fail bool
}

// newFakeEmbedder 创建给定维度的确定性嵌入替身。
func newFakeEmbedder(dimensions int) *fakeEmbedder {
	return &fakeEmbedder{
		vectorsByText: make(map[string][]float32),
		dimensions:    dimensions,
	}
}

// Embed 返回单条文本的确定性向量。
func (f *fakeEmbedder) Embed(
	_ context.Context,
	text string,
	_ constants.MemoryEmbeddingAction,
) ([]float32, error) {
	vectors, err := f.EmbedBatch(nil, []string{text}, constants.MemoryEmbeddingActionAdd)
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

// EmbedBatch 返回多条文本的确定性向量。
func (f *fakeEmbedder) EmbedBatch(
	_ context.Context,
	texts []string,
	_ constants.MemoryEmbeddingAction,
) ([][]float32, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if f.fail {
		return nil, status_error.MemoryEmbeddingNotConfigured
	}
	f.calls++
	vectors := make([][]float32, 0, len(texts))
	for _, text := range texts {
		if vector, exists := f.vectorsByText[text]; exists {
			vectors = append(vectors, vector)
			continue
		}
		vector := make([]float32, f.dimensions)
		runes := []rune(text)
		if len(runes) > 0 {
			vector[int(runes[0])%f.dimensions] = 1
		} else {
			vector[0] = 1
		}
		vectors = append(vectors, vector)
	}
	return vectors, nil
}

// semanticConfig 是语义模式测试使用的完整配置。
var semanticConfig = EmbeddingConfig{BaseURL: "http://fake.local", APIKey: "k", Model: "model-a"}

// newTestService 创建使用临时数据库与嵌入替身的 Memory Service。
func newTestService(t *testing.T, config EmbeddingConfig, embedder Embedder) *Service {
	t.Helper()
	service, err := NewService(ServiceConfig{
		DBPath: filepath.Join(t.TempDir(), "memory.db"),
		ConfigSource: func(context.Context) (EmbeddingConfig, error) {
			return config, nil
		},
		EmbedderFactory: func(EmbeddingConfig) Embedder { return embedder },
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	t.Cleanup(service.Close)
	return service
}

// newContext 构造指定伙伴与项目的记忆上下文。
func newContext(partnerID string, projectID string) Context {
	return Context{RunID: "run-1", ChatID: "conv-1", PartnerID: partnerID, ProjectID: projectID}
}

// mustWrite 写入一条记忆并断言成功。
func mustWrite(t *testing.T, service *Service, mc Context, scope constants.MemoryScope, content string) WriteResult {
	t.Helper()
	result, err := service.Write(context.Background(), mc, scope, content)
	if err != nil {
		t.Fatalf("write %q: %v", content, err)
	}
	return result
}

// mustSearch 检索并断言成功。
func mustSearch(
	t *testing.T,
	service *Service,
	mc Context,
	query string,
	scope constants.MemoryScope,
) SearchResult {
	t.Helper()
	result, err := service.Search(context.Background(), mc, query, scope, 0)
	if err != nil {
		t.Fatalf("search %q: %v", query, err)
	}
	return result
}

// resultIDs 收集检索结果的记忆标识集合。
func resultIDs(result SearchResult) map[string]bool {
	ids := make(map[string]bool, len(result.Results))
	for _, item := range result.Results {
		ids[item.Memory.ID] = true
	}
	return ids
}

func TestWriteKeywordMode(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	mc := newContext("partner-a", "project-x")

	written := mustWrite(t, service, mc, constants.MemoryScopePartner, "用户喜欢深色模式")
	if written.Duplicate {
		t.Fatalf("first write should not be duplicate")
	}
	if written.Memory.Scope != constants.MemoryScopePartner || written.Memory.ScopeID != "partner-a" {
		t.Fatalf("unexpected scope: %v %v", written.Memory.Scope, written.Memory.ScopeID)
	}

	result := mustSearch(t, service, mc, "深色模式", constants.MemoryScopeAll)
	if result.Mode != constants.MemoryRetrievalModeKeyword {
		t.Fatalf("expected keyword mode, got %v", result.Mode)
	}
	if !resultIDs(result)[written.Memory.ID] {
		t.Fatalf("expected written memory in results")
	}
	// 短于 trigram 最小长度的查询走 LIKE 兜底。
	short := mustSearch(t, service, mc, "深色", constants.MemoryScopePartner)
	if !resultIDs(short)[written.Memory.ID] {
		t.Fatalf("expected LIKE fallback to find memory")
	}
}

func TestWriteExactDedup(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	mc := newContext("partner-a", "project-x")

	first := mustWrite(t, service, mc, constants.MemoryScopePartner, "同一条记忆")
	second := mustWrite(t, service, mc, constants.MemoryScopePartner, "同一条记忆")
	if !second.Duplicate || second.Memory.ID != first.Memory.ID {
		t.Fatalf("expected exact dedup to return existing memory")
	}
	// 不同范围写相同内容不去重。
	other := mustWrite(t, service, mc, constants.MemoryScopeProject, "同一条记忆")
	if other.Duplicate || other.Memory.ID == first.Memory.ID {
		t.Fatalf("expected separate memory in project scope")
	}
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.MemoryCount != 2 {
		t.Fatalf("expected 2 memories, got %d", status.MemoryCount)
	}
}

func TestFirstEmbeddingConfigurationKeepsKeywordSearchUntilReindex(t *testing.T) {
	config := EmbeddingConfig{}
	embedder := newFakeEmbedder(3)
	service, err := NewService(ServiceConfig{
		DBPath: filepath.Join(t.TempDir(), "memory.db"),
		ConfigSource: func(context.Context) (EmbeddingConfig, error) {
			return config, nil
		},
		EmbedderFactory: func(EmbeddingConfig) Embedder { return embedder },
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	t.Cleanup(service.Close)
	memoryContext := newContext("partner-a", "")
	written := mustWrite(t, service, memoryContext, constants.MemoryScopePartner, "用户喜欢深色模式")

	config = semanticConfig
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("status after configuration: %v", err)
	}
	if status.Mode != constants.MemoryRetrievalModeKeyword || !status.NeedsReindex {
		t.Fatalf("incomplete index must stay in keyword mode: %+v", status)
	}
	result := mustSearch(t, service, memoryContext, "深色模式", constants.MemoryScopePartner)
	if result.Mode != constants.MemoryRetrievalModeKeyword || !resultIDs(result)[written.Memory.ID] {
		t.Fatalf("existing memory must remain searchable before reindex: %+v", result)
	}

	if _, err := service.Reindex(context.Background()); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	status, err = service.Status(context.Background())
	if err != nil || status.Mode != constants.MemoryRetrievalModeSemantic || status.NeedsReindex {
		t.Fatalf("reindex must enable semantic mode: status=%+v err=%v", status, err)
	}
}

func TestEmptyReindexAllowsFirstSemanticWrite(t *testing.T) {
	embedder := newFakeEmbedder(4)
	service := newTestService(t, semanticConfig, embedder)
	result, err := service.Reindex(context.Background())
	if err != nil {
		t.Fatalf("empty reindex: %v", err)
	}
	if result.VectorCount != 0 || result.Dimensions != 0 {
		t.Fatalf("unexpected empty reindex result: %+v", result)
	}

	memoryContext := newContext("partner-a", "")
	written := mustWrite(t, service, memoryContext, constants.MemoryScopePartner, "首次语义记忆")
	resultPage, err := service.ListMemories(context.Background(), ListFilter{Scope: constants.MemoryScopePartner, ScopeID: "partner-a"})
	if err != nil || len(resultPage.Items) != 1 || resultPage.Items[0].ID != written.Memory.ID {
		t.Fatalf("first write after empty reindex: page=%+v err=%v", resultPage, err)
	}
	status, err := service.Status(context.Background())
	if err != nil || status.VectorCount != 1 || status.Dimensions != 4 {
		t.Fatalf("semantic index after first write: status=%+v err=%v", status, err)
	}
}

func TestTimelineRetainsForgottenMemoryScope(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	memoryContext := newContext("partner-a", "")
	written := mustWrite(t, service, memoryContext, constants.MemoryScopePartner, "需要保留到时间线")
	if err := service.Forget(context.Background(), memoryContext, written.Memory.ID); err != nil {
		t.Fatalf("forget: %v", err)
	}

	timeline, err := service.ListTimeline(context.Background(), ListFilter{Scope: constants.MemoryScopePartner, ScopeID: "partner-a"})
	if err != nil {
		t.Fatalf("list timeline: %v", err)
	}
	if timeline.Total != 2 || len(timeline.Items) != 2 {
		t.Fatalf("timeline must retain add and delete events: %+v", timeline)
	}
	if timeline.Items[0].Event != constants.MemoryEventDelete || timeline.Items[0].OldMemory != "需要保留到时间线" {
		t.Fatalf("unexpected latest timeline event: %+v", timeline.Items[0])
	}
}

func TestManagementListFiltersTreatWildcardCharactersLiterally(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	memoryContext := newContext("partner-a", "")
	mustWrite(t, service, memoryContext, constants.MemoryScopePartner, "项目完成度 50%_stable")
	mustWrite(t, service, memoryContext, constants.MemoryScopePartner, "另一条普通记忆")

	page, err := service.ListMemories(context.Background(), ListFilter{Scope: constants.MemoryScopePartner, ScopeID: "partner-a", Query: "50%_"})
	if err != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("literal wildcard memory filter: page=%+v err=%v", page, err)
	}
	timeline, err := service.ListTimeline(context.Background(), ListFilter{Scope: constants.MemoryScopePartner, ScopeID: "partner-a", Query: "50%_"})
	if err != nil || timeline.Total != 1 || len(timeline.Items) != 1 {
		t.Fatalf("literal wildcard timeline filter: page=%+v err=%v", timeline, err)
	}
}

func TestCleanupOrphanScopesRemovesCurrentDataAndTimeline(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	partnerA := newContext("partner-a", "")
	partnerB := newContext("partner-b", "")
	mustWrite(t, service, partnerA, constants.MemoryScopePartner, "应被清理的记忆")
	mustWrite(t, service, partnerB, constants.MemoryScopePartner, "应保留的记忆")

	err := service.CleanupOrphanScopes(context.Background(), func(_ context.Context, _ constants.MemoryScope, scopeID string) (bool, error) {
		return scopeID == "partner-b", nil
	})
	if err != nil {
		t.Fatalf("cleanup orphan scopes: %v", err)
	}
	orphanPage, err := service.ListTimeline(context.Background(), ListFilter{Scope: constants.MemoryScopePartner, ScopeID: "partner-a"})
	if err != nil || orphanPage.Total != 0 {
		t.Fatalf("orphan timeline must be removed: page=%+v err=%v", orphanPage, err)
	}
	retainedPage, err := service.ListMemories(context.Background(), ListFilter{Scope: constants.MemoryScopePartner, ScopeID: "partner-b"})
	if err != nil || retainedPage.Total != 1 {
		t.Fatalf("existing owner data must remain: page=%+v err=%v", retainedPage, err)
	}
}

func TestEmbeddingSettingsKeepCredentialEncryptedAndSupportClear(t *testing.T) {
	service, err := NewService(ServiceConfig{DBPath: filepath.Join(t.TempDir(), "memory.db")})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	t.Cleanup(service.Close)
	settings, err := service.UpdateEmbeddingSettings(context.Background(), EmbeddingSettingsUpdate{BaseURL: "https://api.example.com/v1", Model: "embed-a", APIKey: "secret-key"})
	if err != nil || !settings.APIKeyConfigured {
		t.Fatalf("save settings: settings=%+v err=%v", settings, err)
	}
	ciphertext, exists, err := service.store.getMeta(metaKeyEmbeddingAPIKey)
	if err != nil || !exists || ciphertext == "secret-key" || strings.Contains(ciphertext, "secret-key") {
		t.Fatalf("credential must be encrypted: exists=%v value=%q err=%v", exists, ciphertext, err)
	}
	settings, err = service.UpdateEmbeddingSettings(context.Background(), EmbeddingSettingsUpdate{BaseURL: "", Model: "", ClearAPIKey: true})
	if err != nil || settings.APIKeyConfigured || settings.BaseURL != "" || settings.Model != "" {
		t.Fatalf("clear settings: settings=%+v err=%v", settings, err)
	}
}

func TestEmbeddingSettingsRejectInvalidBaseURL(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	for _, baseURL := range []string{
		"ftp://example.com/v1",
		"https://",
		"https://example.com/v1?tenant=one",
	} {
		if _, err := service.UpdateEmbeddingSettings(context.Background(), EmbeddingSettingsUpdate{BaseURL: baseURL}); err == nil || !errors.Is(err, status_error.BadRequest) {
			t.Fatalf("invalid base URL must be rejected: value=%q err=%v", baseURL, err)
		}
	}
}

func TestScopeIsolation(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	partnerA := newContext("partner-a", "project-x")
	partnerB := newContext("partner-b", "project-x")
	projectY := newContext("partner-a", "project-y")

	own := mustWrite(t, service, partnerA, constants.MemoryScopePartner, "伙伴偏好使用中文交流")
	shared := mustWrite(t, service, partnerA, constants.MemoryScopeProject, "项目约定使用 Go 构建")

	// Partner 隔离：B 检索不到 A 的伙伴记忆。
	resultB := mustSearch(t, service, partnerB, "中文交流", constants.MemoryScopeAll)
	if resultIDs(resultB)[own.Memory.ID] {
		t.Fatalf("partner memory must be isolated per partner")
	}
	// Project 共享：同一项目标识下的共享记忆对双方可见。
	resultShared := mustSearch(t, service, partnerB, "项目约定", constants.MemoryScopeAll)
	if !resultIDs(resultShared)[shared.Memory.ID] {
		t.Fatalf("project memory must be visible within the same project")
	}
	// Partner 跨项目：A 在项目 Y 中仍可检索自己的伙伴记忆。
	resultY := mustSearch(t, service, projectY, "中文交流", constants.MemoryScopePartner)
	if !resultIDs(resultY)[own.Memory.ID] {
		t.Fatalf("partner memory must follow the partner across projects")
	}
	// Project 隔离：项目 X 的共享记忆在项目 Y 中不可见。
	resultYAll := mustSearch(t, service, projectY, "项目约定", constants.MemoryScopeAll)
	if resultIDs(resultYAll)[shared.Memory.ID] {
		t.Fatalf("project memory must not leak into another project")
	}
	// scope=all 合并两个分支：OR 查询同时命中伙伴与项目记忆。
	resultAll := mustSearch(t, service, partnerA, "中文交流 项目约定", constants.MemoryScopeAll)
	ids := resultIDs(resultAll)
	if !ids[own.Memory.ID] || !ids[shared.Memory.ID] {
		t.Fatalf("scope=all must merge partner and project legs")
	}
}

func TestNoProjectChat(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	mc := newContext("partner-a", "")

	if _, err := service.Write(context.Background(), mc, constants.MemoryScopeProject, "项目记忆"); err == nil {
		t.Fatalf("project write without project must fail")
	} else if !errors.Is(err, status_error.MemoryProjectNotBound) {
		t.Fatalf("expected no-project error, got %v", err)
	}
	if _, err := service.Search(context.Background(), mc, "项目", constants.MemoryScopeProject, 0); err == nil {
		t.Fatalf("project search without project must fail")
	}
	// scope=all 在无项目时只检索伙伴分支。
	written := mustWrite(t, service, mc, constants.MemoryScopePartner, "伙伴记忆")
	result := mustSearch(t, service, mc, "伙伴记忆", constants.MemoryScopeAll)
	if !resultIDs(result)[written.Memory.ID] {
		t.Fatalf("scope=all without project must still cover partner leg")
	}
}

func TestUpdateAndForget(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	mc := newContext("partner-a", "project-x")

	written := mustWrite(t, service, mc, constants.MemoryScopePartner, "旧的项目约定内容")
	if err := service.Update(context.Background(), mc, written.Memory.ID, "新的项目约定内容"); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated := mustSearch(t, service, mc, "新的项目约定", constants.MemoryScopePartner)
	if !resultIDs(updated)[written.Memory.ID] {
		t.Fatalf("updated memory must be searchable by new content")
	}
	stale := mustSearch(t, service, mc, "旧的项目约定", constants.MemoryScopePartner)
	if resultIDs(stale)[written.Memory.ID] {
		t.Fatalf("old content must not be searchable after update")
	}

	history, err := service.listHistoryForTest(written.Memory.ID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 || history[0].Event != constants.MemoryEventAdd ||
		history[1].Event != constants.MemoryEventUpdate ||
		history[1].OldMemory != "旧的项目约定内容" || history[1].NewMemory != "新的项目约定内容" {
		t.Fatalf("unexpected history: %+v", history)
	}

	if err := service.Forget(context.Background(), mc, written.Memory.ID); err != nil {
		t.Fatalf("forget: %v", err)
	}
	forgotten := mustSearch(t, service, mc, "新的项目约定", constants.MemoryScopePartner)
	if resultIDs(forgotten)[written.Memory.ID] {
		t.Fatalf("forgotten memory must not be searchable")
	}
	history, err = service.listHistoryForTest(written.Memory.ID)
	if err != nil {
		t.Fatalf("history after forget: %v", err)
	}
	last := history[len(history)-1]
	if len(history) != 3 || last.Event != constants.MemoryEventDelete || !last.IsDeleted ||
		last.OldMemory != "新的项目约定内容" || last.NewMemory != "" {
		t.Fatalf("unexpected history after forget: %+v", last)
	}
}

func TestUpdateSameContentFollowsMemoryContractHistorySemantics(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	memoryContext := newContext("partner-a", "")
	written := mustWrite(t, service, memoryContext, constants.MemoryScopePartner, "保持不变的内容")

	if err := service.Update(context.Background(), memoryContext, written.Memory.ID, "保持不变的内容"); err != nil {
		t.Fatalf("same-content update: %v", err)
	}
	history, err := service.listHistoryForTest(written.Memory.ID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 || history[1].Event != constants.MemoryEventUpdate ||
		history[1].OldMemory != "保持不变的内容" || history[1].NewMemory != "保持不变的内容" {
		t.Fatalf("same-content update must append UPDATE history: %+v", history)
	}
}

func TestCrossScopeWriteAccess(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	partnerA := newContext("partner-a", "project-x")
	partnerB := newContext("partner-b", "project-x")

	written := mustWrite(t, service, partnerA, constants.MemoryScopePartner, "伙伴 A 的私有记忆")
	// 越权读取其他范围的记忆标识，update/forget 统一返回 not found。
	if err := service.Update(context.Background(), partnerB, written.Memory.ID, "篡改"); err == nil {
		t.Fatalf("cross-scope update must fail")
	} else if !errors.Is(err, status_error.MemoryNotFound) {
		t.Fatalf("expected not-found for cross-scope update, got %v", err)
	}
	if err := service.Forget(context.Background(), partnerB, written.Memory.ID); err == nil {
		t.Fatalf("cross-scope forget must fail")
	} else if !errors.Is(err, status_error.MemoryNotFound) {
		t.Fatalf("expected not-found for cross-scope forget, got %v", err)
	}
}

func TestSemanticModeAndScoreContract(t *testing.T) {
	embedder := newFakeEmbedder(4)
	embedder.vectorsByText["用户喜欢深色模式"] = []float32{1, 0, 0, 0}
	embedder.vectorsByText["项目使用 Go 构建"] = []float32{0, 1, 0, 0}
	embedder.vectorsByText["深色"] = []float32{1, 0, 0, 0}
	embedder.vectorsByText["完全无关的查询词"] = []float32{0, 0, 1, 0}
	service := newTestService(t, semanticConfig, embedder)
	mc := newContext("partner-a", "project-x")

	dark := mustWrite(t, service, mc, constants.MemoryScopePartner, "用户喜欢深色模式")
	mustWrite(t, service, mc, constants.MemoryScopePartner, "项目使用 Go 构建")

	result := mustSearch(t, service, mc, "深色", constants.MemoryScopePartner)
	if result.Mode != constants.MemoryRetrievalModeSemantic {
		t.Fatalf("expected semantic mode, got %v", result.Mode)
	}
	if len(result.Results) != 1 || result.Results[0].Memory.ID != dark.Memory.ID {
		t.Fatalf("expected only the matching memory, got %+v", result.Results)
	}
	// 完全一致向量的 cosine distance 为 0，score 必须为 1。
	if result.Results[0].Score != 1.0 {
		t.Fatalf("expected score 1.0 for identical vector, got %f", result.Results[0].Score)
	}
	// 正交向量 score 为 0，被 threshold=0.1 门控排除。
	unrelated := mustSearch(t, service, mc, "完全无关的查询词", constants.MemoryScopePartner)
	if len(unrelated.Results) != 0 {
		t.Fatalf("orthogonal results must be gated by threshold, got %+v", unrelated.Results)
	}
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Configured || status.Mode != constants.MemoryRetrievalModeSemantic ||
		status.Model != "model-a" || status.Dimensions != 4 || status.VectorCount != 2 {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestEmbeddingConflictAndReindex(t *testing.T) {
	embedder := newFakeEmbedder(4)
	service := newTestService(t, semanticConfig, embedder)
	mc := newContext("partner-a", "project-x")
	mustWrite(t, service, mc, constants.MemoryScopePartner, "切换模型前写入的记忆")

	// 切换模型后进入冲突状态：写入不再写向量，检索降级关键词。
	conflictConfig := EmbeddingConfig{BaseURL: "http://fake.local", APIKey: "k", Model: "model-b"}
	service.configSource = func(context.Context) (EmbeddingConfig, error) { return conflictConfig, nil }
	later := mustWrite(t, service, mc, constants.MemoryScopePartner, "切换模型后写入的记忆")
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Conflict || status.Mode != constants.MemoryRetrievalModeKeyword {
		t.Fatalf("expected conflict keyword mode, got %+v", status)
	}
	if status.VectorCount != 1 {
		t.Fatalf("conflict write must not add vectors, got %d", status.VectorCount)
	}
	result := mustSearch(t, service, mc, "切换模型后", constants.MemoryScopePartner)
	if result.Mode != constants.MemoryRetrievalModeKeyword || !result.Conflict {
		t.Fatalf("expected keyword fallback with conflict flag, got %+v", result.Mode)
	}
	if !resultIDs(result)[later.Memory.ID] {
		t.Fatalf("keyword fallback must find memories written during conflict")
	}

	// 用新模型重建索引后恢复语义模式，且既有记忆全部重新嵌入。
	resultReindex, err := service.Reindex(context.Background())
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if resultReindex.VectorCount != 2 || resultReindex.Model != "model-b" {
		t.Fatalf("unexpected reindex result: %+v", resultReindex)
	}
	status, err = service.Status(context.Background())
	if err != nil {
		t.Fatalf("status after reindex: %v", err)
	}
	if status.Conflict || status.Mode != constants.MemoryRetrievalModeSemantic ||
		status.Model != "model-b" || status.VectorCount != 2 {
		t.Fatalf("expected healed semantic mode, got %+v", status)
	}
}

func TestReindexRequiresConfig(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	if _, err := service.Reindex(context.Background()); err == nil {
		t.Fatalf("reindex without embedding config must fail")
	} else if !errors.Is(err, status_error.MemoryEmbeddingNotConfigured) {
		t.Fatalf("expected embedding-unavailable error, got %v", err)
	}
}

func TestEmbeddingFailureLeavesNoResidue(t *testing.T) {
	embedder := newFakeEmbedder(4)
	service := newTestService(t, semanticConfig, embedder)
	mc := newContext("partner-a", "project-x")

	embedder.mutex.Lock()
	embedder.fail = true
	embedder.mutex.Unlock()
	if _, err := service.Write(context.Background(), mc, constants.MemoryScopePartner, "写入失败的记忆"); err == nil {
		t.Fatalf("write with failing embedder must fail")
	}
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.MemoryCount != 0 || status.VectorCount != 0 {
		t.Fatalf("failed write must leave no residue, got %+v", status)
	}
}

func TestDeleteScope(t *testing.T) {
	// 语义模式下构造全部相同的向量，保证检索命中，专注于级联删除断言。
	embedder := newFakeEmbedder(4)
	embedder.vectorsByText["伙伴级联记忆"] = []float32{1, 0, 0, 0}
	embedder.vectorsByText["项目级联记忆"] = []float32{1, 0, 0, 0}
	embedder.vectorsByText["级联记忆"] = []float32{1, 0, 0, 0}
	service := newTestService(t, semanticConfig, embedder)
	mc := newContext("partner-a", "project-x")
	mustWrite(t, service, mc, constants.MemoryScopePartner, "伙伴级联记忆")
	mustWrite(t, service, mc, constants.MemoryScopeProject, "项目级联记忆")

	if err := service.DeleteScope(context.Background(), constants.MemoryScopePartner, "partner-a"); err != nil {
		t.Fatalf("delete partner scope: %v", err)
	}
	result := mustSearch(t, service, mc, "级联记忆", constants.MemoryScopeAll)
	ids := resultIDs(result)
	if len(ids) != 1 {
		t.Fatalf("expected only project memory to survive, got %d", len(ids))
	}
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.MemoryCount != 1 || status.VectorCount != 1 {
		t.Fatalf("expected exactly one surviving memory and vector, got %+v", status)
	}
	if err := service.DeleteScope(context.Background(), constants.MemoryScopeProject, "project-x"); err != nil {
		t.Fatalf("delete project scope: %v", err)
	}
	status, err = service.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.MemoryCount != 0 || status.VectorCount != 0 {
		t.Fatalf("expected empty store after cascade, got %+v", status)
	}
}

func TestRankedSearchJoinKeepsScopeAndOrder(t *testing.T) {
	embedder := newFakeEmbedder(3)
	embedder.vectorsByText["needle alpha"] = []float32{1, 0, 0}
	embedder.vectorsByText["needle beta"] = []float32{0.8, 0.2, 0}
	embedder.vectorsByText["needle outside"] = []float32{1, 0, 0}
	service := newTestService(t, semanticConfig, embedder)
	mc := newContext("ranked-owner", "")
	first := mustWrite(t, service, mc, constants.MemoryScopePartner, "needle alpha")
	second := mustWrite(t, service, mc, constants.MemoryScopePartner, "needle beta")
	mustWrite(t, service, newContext("other-owner", ""), constants.MemoryScopePartner, "needle outside")
	ctx := context.Background()
	if !service.store.ftsAvailable {
		t.Fatal("FTS5 is required for the direct ranked-query regression")
	}
	keyword, err := service.keyword.searchFTS(ctx, "needle", 10, constants.MemoryScopePartner, mc.PartnerID)
	if err != nil || len(keyword) != 2 {
		t.Fatalf("direct FTS query failed: %+v %v", keyword, err)
	}
	for _, item := range keyword {
		if item.Memory.ScopeID != mc.PartnerID || item.Memory.Data == "" {
			t.Fatalf("keyword scope/body=%+v", item)
		}
	}
	semantic, err := service.vectors.search(ctx, []float32{1, 0, 0}, 10, constants.MemoryScopePartner, mc.PartnerID)
	if err != nil || len(semantic) != 2 || semantic[0].Memory.ID != first.Memory.ID || semantic[1].Memory.ID != second.Memory.ID || semantic[0].Score <= semantic[1].Score {
		t.Fatalf("rank/scope query=%+v %v", semantic, err)
	}
}

func TestFreezeEmbedderKeepsSelectedConfiguration(t *testing.T) {
	ctx := context.Background()
	service, err := NewService(ServiceConfig{DBPath: filepath.Join(t.TempDir(), "memory.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	if _, err := service.UpdateEmbeddingSettings(ctx, EmbeddingSettingsUpdate{
		BaseURL: "https://embedding.example/v1/", Model: "model-a", APIKey: "old-key",
	}); err != nil {
		t.Fatal(err)
	}
	var frozenConfig EmbeddingConfig
	embedder := newFakeEmbedder(3)
	service.embedderFactory = func(config EmbeddingConfig) Embedder {
		frozenConfig = config
		return embedder
	}
	frozen, err := service.FreezeEmbedder(ctx, "model-a")
	if err != nil || frozen.ModelID != "model-a" || frozen.Fingerprint == "" || frozen.Embed == nil {
		t.Fatalf("freeze: %+v %v", frozen, err)
	}
	if _, err := service.UpdateEmbeddingSettings(ctx, EmbeddingSettingsUpdate{
		BaseURL: "https://other.example/v1", Model: "model-b", APIKey: "new-key",
	}); err != nil {
		t.Fatal(err)
	}
	vectors, err := frozen.Embed(ctx, []string{"original input"})
	if err != nil || len(vectors) != 1 || len(vectors[0]) != 3 {
		t.Fatalf("embed: %v %v", vectors, err)
	}
	if frozenConfig.BaseURL != "https://embedding.example/v1" || frozenConfig.Model != "model-a" || frozenConfig.APIKey != "old-key" {
		t.Fatal("frozen embedder did not retain the selected configuration")
	}
	if _, err := service.FreezeEmbedder(ctx, "model-a"); !errors.Is(err, status_error.MemoryEmbeddingConflict) {
		t.Fatalf("changed model should reject the old selection: %v", err)
	}
}

func TestFreezeEmbedderFingerprintIdentifiesVectorSpace(t *testing.T) {
	ctx := context.Background()
	service := newTestService(t, semanticConfig, newFakeEmbedder(3))
	current := semanticConfig
	service.configSource = func(context.Context) (EmbeddingConfig, error) { return current, nil }
	first, err := service.FreezeEmbedder(ctx, current.Model)
	if err != nil {
		t.Fatal(err)
	}
	current.APIKey = "rotated-key"
	current.BaseURL += "/"
	rotated, err := service.FreezeEmbedder(ctx, current.Model)
	if err != nil || rotated.Fingerprint != first.Fingerprint {
		t.Fatalf("credentials and trailing slash must not change vector space: %v", err)
	}
	current.BaseURL = "https://different.example/v1"
	moved, err := service.FreezeEmbedder(ctx, current.Model)
	if err != nil || moved.Fingerprint == first.Fingerprint {
		t.Fatalf("endpoint change must change vector space: %v", err)
	}
}

func TestFreezeEmbedderRejectsMissingConfigurationAndCanceledContext(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, newFakeEmbedder(3))
	if _, err := service.FreezeEmbedder(context.Background(), "model-a"); !errors.Is(err, status_error.MemoryEmbeddingNotConfigured) {
		t.Fatalf("missing configuration: %v", err)
	}
	service.configSource = func(context.Context) (EmbeddingConfig, error) { return semanticConfig, nil }
	if _, err := service.FreezeEmbedder(context.Background(), ""); !errors.Is(err, status_error.MemoryEmbeddingNotConfigured) {
		t.Fatalf("empty selection: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.FreezeEmbedder(ctx, semanticConfig.Model); !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("canceled snapshot: %v", err)
	}
}
