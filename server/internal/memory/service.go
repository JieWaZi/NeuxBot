package memory

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

// ConfigSource 在每次操作时提供当前 Embedding 配置，使记忆设置变更即时生效。
type ConfigSource func(ctx context.Context) (EmbeddingConfig, error)

// ScopeExists 判断 memory.db 中的范围属主是否仍存在于主业务库。
type ScopeExists func(ctx context.Context, scope constants.MemoryScope, scopeID string) (bool, error)

// ServiceConfig 声明构造 Memory Service 所需的外部依赖。
type ServiceConfig struct {
	// DBPath 是 memory.db 的文件路径。
	DBPath string
	// ConfigSource 可覆盖内部存储中的 Embedding 配置，主要用于测试替身。
	ConfigSource ConfigSource
	// EmbedderFactory 按配置创建 Embedder；为空时使用 OpenAI 兼容实现。
	EmbedderFactory func(config EmbeddingConfig) Embedder
}

// Service 拥有记忆读写、检索和同库原子变更，执行能力由上层签发。
type Service struct {
	// store 是记忆 SQLite 存储层。
	store *store
	// vectors 是 sqlite-vec 向量存取实现。
	vectors *vectorStore
	// keyword 是关键词召回实现。
	keyword *keywordSearch
	// configSource 提供当前 Embedding 配置。
	configSource ConfigSource
	// embedderFactory 按配置创建 Embedder。
	embedderFactory func(config EmbeddingConfig) Embedder
	// mutationMu 串行化记忆变更、配置更新与索引重建，避免快照切换丢失并发写入。
	mutationMu sync.Mutex
}

// NewService 创建 Memory Service 并打开记忆数据库；不启动任何后台流程。
func NewService(config ServiceConfig) (*Service, error) {
	if strings.TrimSpace(config.DBPath) == "" {
		return nil, status_error.CoreMemoryInitializationFailed
	}
	store, err := openStore(config.DBPath)
	if err != nil {
		return nil, err
	}
	factory := config.EmbedderFactory
	if factory == nil {
		factory = func(config EmbeddingConfig) Embedder { return NewOpenAIEmbedder(config) }
	}
	service := &Service{
		store:           store,
		vectors:         &vectorStore{store: store},
		keyword:         &keywordSearch{store: store},
		configSource:    config.ConfigSource,
		embedderFactory: factory,
	}
	if service.configSource == nil {
		service.configSource = func(context.Context) (EmbeddingConfig, error) {
			return service.store.embeddingConfig()
		}
	}
	return service, nil
}

// Close 关闭记忆存储。
func (s *Service) Close() {
	_ = s.store.Close()
}

// contentHash 计算正文 MD5。
func contentHash(content string) string {
	sum := md5.Sum([]byte(content))
	return hex.EncodeToString(sum[:])
}

// validateContent 校验并规范化记忆正文。
func validateContent(content string) (string, error) {
	normalized := strings.TrimSpace(content)
	if normalized == "" {
		return "", status_error.BadRequest
	}
	if len([]rune(normalized)) > constants.MemoryContentMaxRunes {
		return "", status_error.BadRequest
	}
	return normalized, nil
}

// resolveScopeID 把请求范围解析为当前上下文的具体范围标识。
func resolveScopeID(scope constants.MemoryScope, mc Context) (string, error) {
	switch scope {
	case constants.MemoryScopePartner:
		if mc.PartnerID == "" {
			return "", status_error.BadRequest
		}
		return mc.PartnerID, nil
	case constants.MemoryScopeProject:
		if mc.ProjectID == "" {
			return "", status_error.MemoryProjectNotBound
		}
		return mc.ProjectID, nil
	default:
		return "", status_error.BadRequest
	}
}

// embeddingState 是一次操作解析出的 Embedding 可用状态。
type embeddingState struct {
	// embedder 是配置完整时的嵌入实现；未配置为 nil。
	embedder Embedder
	// config 是当前 Embedding 配置。
	config EmbeddingConfig
	// conflict 表示配置模型与已建索引指纹不一致。
	conflict bool
	// ready 表示语义索引完整覆盖当前记忆。
	ready bool
}

// resolveEmbedding 读取当前配置并与已建索引指纹比对。
//
// 指纹只记录首次建立语义索引时的模型与维度；模型名不一致即进入冲突状态，
// 冲突状态下写入与更新不再写向量、检索降级关键词，由 reindex 统一修复。
func (s *Service) resolveEmbedding(ctx context.Context) (embeddingState, error) {
	config, err := s.configSource(ctx)
	if err != nil {
		return embeddingState{}, err
	}
	if !config.Complete() {
		return embeddingState{config: config}, nil
	}
	storedModel, exists, err := s.store.getMeta(metaKeyEmbeddingModel)
	if err != nil {
		return embeddingState{}, err
	}
	storedBaseURL, baseURLExists, err := s.store.getMeta(metaKeyEmbeddingIndexBaseURL)
	if err != nil {
		return embeddingState{}, err
	}
	conflict := exists && storedModel != config.Model ||
		baseURLExists && storedBaseURL != strings.TrimRight(config.BaseURL, "/")
	memoryCount, err := s.store.memoryCount()
	if err != nil {
		return embeddingState{}, err
	}
	vectorCount, err := s.vectors.vectorCount()
	if err != nil {
		return embeddingState{}, err
	}
	return embeddingState{
		embedder: s.embedderFactory(config),
		config:   config,
		conflict: conflict,
		ready:    !conflict && memoryCount == vectorCount,
	}, nil
}

// checkDimensions 校验嵌入向量维度与已建索引一致，不一致时拒绝混写索引。
func (s *Service) checkDimensions(op string, vector []float32) error {
	storedDimensions, exists, err := s.store.getMeta(metaKeyEmbeddingDimensions)
	if err != nil || !exists {
		return err
	}
	dimensions, err := strconv.Atoi(storedDimensions)
	if err != nil {
		return status_error.DatabaseError
	}
	if dimensions != len(vector) {
		return status_error.MemoryEmbeddingConflict
	}
	return nil
}

// Write 保存已明确提供的结构化记忆，
// 叠加 exact hash 去重；写入、关键词索引、向量与历史处于同一事务。
func (s *Service) Write(
	ctx context.Context,
	mc Context,
	scope constants.MemoryScope,
	content string,
) (WriteResult, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if replayed, err := s.replayAgentCommand(ctx); err != nil {
		return WriteResult{}, err
	} else if replayed {
		command := ctx.Value(agentCommandKey{}).(*agentCommand)
		return WriteResult{Memory: Memory{ID: command.memoryID}, Duplicate: command.duplicate}, nil
	}

	normalized, err := validateContent(content)
	if err != nil {
		return WriteResult{}, err
	}
	scopeID, err := resolveScopeID(scope, mc)
	if err != nil {
		return WriteResult{}, err
	}
	hash := contentHash(normalized)
	if existing, found, err := s.store.findByHash(scope, scopeID, hash); err != nil {
		return WriteResult{}, err
	} else if found {
		if command, ok := ctx.Value(agentCommandKey{}).(*agentCommand); ok {
			command.memoryID = existing.ID
			command.duplicate = true
			if err := s.store.withTx(ctx, func(*sql.Tx) error { return nil }); err != nil {
				return WriteResult{}, err
			}
		}
		return WriteResult{Memory: *existing, Duplicate: true}, nil
	}

	state, err := s.resolveEmbedding(ctx)
	if err != nil {
		return WriteResult{}, err
	}
	var vector []float32
	if state.embedder != nil && !state.conflict {
		vector, err = state.embedder.Embed(ctx, normalized, constants.MemoryEmbeddingActionAdd)
		if err != nil {
			return WriteResult{}, err
		}
		if err := s.checkDimensions("write", vector); err != nil {
			return WriteResult{}, err
		}
	}

	now := time.Now().UnixMilli()
	memory := Memory{
		ID:          util.New(),
		Scope:       scope,
		ScopeID:     scopeID,
		Data:        normalized,
		Hash:        hash,
		SourceRunID: mc.RunID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	agentMemoryID(ctx, memory.ID)
	err = s.store.withTx(ctx, func(tx *sql.Tx) error {
		if vector != nil {
			if err := s.vectors.ensureVecTable(tx, len(vector)); err != nil {
				return err
			}
			if err := s.store.setMeta(tx, metaKeyEmbeddingModel, state.config.Model); err != nil {
				return err
			}
			if err := s.store.setMeta(tx, metaKeyEmbeddingIndexBaseURL, strings.TrimRight(state.config.BaseURL, "/")); err != nil {
				return err
			}
			if err := s.store.setMeta(
				tx,
				metaKeyEmbeddingDimensions,
				strconv.Itoa(len(vector)),
			); err != nil {
				return err
			}
		}
		if err := s.store.insertMemory(tx, &memory); err != nil {
			return err
		}
		if err := s.store.upsertFTS(tx, memory.ID, memory.Data); err != nil {
			return err
		}
		if vector != nil {
			if err := s.vectors.insertVector(tx, memory.ID, vector, memory.Scope, memory.ScopeID); err != nil {
				return err
			}
		}
		return s.store.insertHistory(tx, HistoryEntry{
			ID:          util.New(),
			MemoryID:    memory.ID,
			Scope:       memory.Scope,
			ScopeID:     memory.ScopeID,
			NewMemory:   memory.Data,
			Event:       constants.MemoryEventAdd,
			CreatedAt:   memory.CreatedAt,
			UpdatedAt:   memory.UpdatedAt,
			SourceRunID: memory.SourceRunID,
		})
	})
	if err != nil {
		return WriteResult{}, err
	}
	return WriteResult{Memory: memory}, nil
}

// searchLeg 是一次检索要覆盖的一个范围分支。
type searchLeg struct {
	// scope 是分支范围。
	scope constants.MemoryScope
	// scopeID 是分支范围标识。
	scopeID string
}

// resolveLegs 把检索范围展开为具体分支；scope=all 合并伙伴与项目两个分支。
func resolveLegs(scope constants.MemoryScope, mc Context) ([]searchLeg, error) {
	switch scope {
	case constants.MemoryScopePartner, constants.MemoryScopeProject:
		scopeID, err := resolveScopeID(scope, mc)
		if err != nil {
			return nil, err
		}
		return []searchLeg{{scope: scope, scopeID: scopeID}}, nil
	case constants.MemoryScopeAll, constants.MemoryScopeUnspecified:
		legs := []searchLeg{}
		if mc.PartnerID != "" {
			legs = append(legs, searchLeg{scope: constants.MemoryScopePartner, scopeID: mc.PartnerID})
		}
		if mc.ProjectID != "" {
			legs = append(legs, searchLeg{scope: constants.MemoryScopeProject, scopeID: mc.ProjectID})
		}
		if len(legs) == 0 {
			return nil, status_error.BadRequest
		}
		return legs, nil
	default:
		return nil, status_error.BadRequest
	}
}

// Search 执行范围内的记忆检索。
func (s *Service) Search(
	ctx context.Context,
	mc Context,
	query string,
	scope constants.MemoryScope,
	topK int,
) (SearchResult, error) {
	normalized := strings.TrimSpace(query)
	if normalized == "" {
		return SearchResult{}, status_error.BadRequest
	}
	if topK <= 0 {
		topK = constants.MemorySearchDefaultTopK
	}
	if topK > constants.MemorySearchMaxTopK {
		topK = constants.MemorySearchMaxTopK
	}
	legs, err := resolveLegs(scope, mc)
	if err != nil {
		return SearchResult{}, err
	}
	state, err := s.resolveEmbedding(ctx)
	if err != nil {
		return SearchResult{}, err
	}

	results := []ScoredMemory{}
	if state.embedder != nil && state.ready {
		vector, err := state.embedder.Embed(ctx, normalized, constants.MemoryEmbeddingActionSearch)
		if err != nil {
			return SearchResult{}, err
		}
		if err := s.checkDimensions("search", vector); err != nil {
			return SearchResult{}, err
		}
		for _, leg := range legs {
			legResults, err := s.vectors.search(ctx, vector, topK, leg.scope, leg.scopeID)
			if err != nil {
				return SearchResult{}, err
			}
			for _, item := range legResults {
				if item.Score < constants.MemorySearchScoreThreshold {
					continue
				}
				results = append(results, item)
			}
		}
		return SearchResult{
			Results: mergeTopK(results, topK),
			Mode:    constants.MemoryRetrievalModeSemantic,
		}, nil
	}

	for _, leg := range legs {
		legResults, err := s.keyword.search(ctx, normalized, topK, leg.scope, leg.scopeID)
		if err != nil {
			return SearchResult{}, err
		}
		results = append(results, legResults...)
	}
	return SearchResult{
		Results:  mergeTopK(results, topK),
		Mode:     constants.MemoryRetrievalModeKeyword,
		Conflict: state.conflict,
	}, nil
}

// mergeTopK 按相似度降序合并各范围分支并截取前 topK 条。
func mergeTopK(results []ScoredMemory, topK int) []ScoredMemory {
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > topK {
		results = results[:topK]
	}
	return results
}

// getAccessibleMemory 读取记忆并做范围权限校验；越权统一返回 NotFound，避免泄露其他范围的存在性。
func (s *Service) getAccessibleMemory(memoryID string, mc Context) (*Memory, error) {
	memory, exists, err := s.store.getMemory(memoryID)
	if err != nil {
		return nil, err
	}
	if !exists || !mc.Accessible(memory.Scope, memory.ScopeID) {
		return nil, status_error.MemoryNotFound
	}
	return memory, nil
}

// Update 更新指定记忆：重新嵌入、更新内容、记录 UPDATE history；不能改变 scope/scope_id。
func (s *Service) Update(
	ctx context.Context,
	mc Context,
	memoryID string,
	content string,
) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if replayed, err := s.replayAgentCommand(ctx); err != nil || replayed {
		return err
	}
	agentMemoryID(ctx, memoryID)

	memory, err := s.getAccessibleMemory(memoryID, mc)
	if err != nil {
		return err
	}
	normalized, err := validateContent(content)
	if err != nil {
		return err
	}
	newHash := contentHash(normalized)
	if existing, found, err := s.store.findByHash(memory.Scope, memory.ScopeID, newHash); err != nil {
		return err
	} else if found && existing.ID != memory.ID {
		return status_error.BadRequest
	}

	state, err := s.resolveEmbedding(ctx)
	if err != nil {
		return err
	}
	var vector []float32
	if state.embedder != nil && !state.conflict {
		vector, err = state.embedder.Embed(ctx, normalized, constants.MemoryEmbeddingActionUpdate)
		if err != nil {
			return err
		}
		if err := s.checkDimensions("update", vector); err != nil {
			return err
		}
	}

	previous := memory.Data
	memory.Data = normalized
	memory.Hash = newHash
	memory.SourceRunID = mc.RunID
	memory.UpdatedAt = time.Now().UnixMilli()
	return s.store.withTx(ctx, func(tx *sql.Tx) error {
		if err := s.store.updateMemory(tx, *memory); err != nil {
			return err
		}
		if err := s.store.upsertFTS(tx, memory.ID, memory.Data); err != nil {
			return err
		}
		if vector != nil {
			if err := s.vectors.ensureVecTable(tx, len(vector)); err != nil {
				return err
			}
			if err := s.store.setMeta(tx, metaKeyEmbeddingModel, state.config.Model); err != nil {
				return err
			}
			if err := s.store.setMeta(tx, metaKeyEmbeddingIndexBaseURL, strings.TrimRight(state.config.BaseURL, "/")); err != nil {
				return err
			}
			if err := s.store.setMeta(tx, metaKeyEmbeddingDimensions, strconv.Itoa(len(vector))); err != nil {
				return err
			}
			if err := s.vectors.deleteVector(tx, memory.ID); err != nil {
				return err
			}
			if err := s.vectors.insertVector(tx, memory.ID, vector, memory.Scope, memory.ScopeID); err != nil {
				return err
			}
		} else if err := s.vectors.deleteVector(tx, memory.ID); err != nil {
			return err
		}
		return s.store.insertHistory(tx, HistoryEntry{
			ID:          util.New(),
			MemoryID:    memory.ID,
			Scope:       memory.Scope,
			ScopeID:     memory.ScopeID,
			OldMemory:   previous,
			NewMemory:   memory.Data,
			Event:       constants.MemoryEventUpdate,
			CreatedAt:   memory.CreatedAt,
			UpdatedAt:   memory.UpdatedAt,
			SourceRunID: mc.RunID,
		})
	})
}

// Forget 遗忘指定记忆：物理删除当前记忆与向量，保留 DELETE 历史。
func (s *Service) Forget(ctx context.Context, mc Context, memoryID string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if replayed, err := s.replayAgentCommand(ctx); err != nil || replayed {
		return err
	}
	agentMemoryID(ctx, memoryID)

	memory, err := s.getAccessibleMemory(memoryID, mc)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	return s.store.withTx(ctx, func(tx *sql.Tx) error {
		if err := s.vectors.deleteVector(tx, memory.ID); err != nil {
			return err
		}
		if err := s.store.deleteFTS(tx, memory.ID); err != nil {
			return err
		}
		if err := s.store.deleteMemory(tx, memory.ID); err != nil {
			return err
		}
		return s.store.insertHistory(tx, HistoryEntry{
			ID:          util.New(),
			MemoryID:    memory.ID,
			Scope:       memory.Scope,
			ScopeID:     memory.ScopeID,
			OldMemory:   memory.Data,
			Event:       constants.MemoryEventDelete,
			CreatedAt:   memory.CreatedAt,
			UpdatedAt:   now,
			IsDeleted:   true,
			SourceRunID: mc.RunID,
		})
	})
}

// reindexBatchSize 是重建索引时单次嵌入请求的批量大小。
const reindexBatchSize = 64

// Reindex 用当前 Embedding 配置重建全部语义索引并原子更新指纹。
//
// 嵌入调用在事务外完成；失败时不改变任何现有状态，成功后单事务切换。
// 切换模型或修复冲突状态的唯一入口。
func (s *Service) Reindex(ctx context.Context) (ReindexResult, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	config, err := s.configSource(ctx)
	if err != nil {
		return ReindexResult{}, err
	}
	if !config.Complete() {
		return ReindexResult{}, status_error.MemoryEmbeddingNotConfigured
	}
	memories, err := s.store.listMemories()
	if err != nil {
		return ReindexResult{}, err
	}
	if len(memories) == 0 {
		err := s.store.withTx(ctx, func(tx *sql.Tx) error {
			if err := s.vectors.dropVecTable(tx); err != nil {
				return err
			}
			if err := s.store.setMeta(tx, metaKeyEmbeddingModel, config.Model); err != nil {
				return err
			}
			if err := s.store.setMeta(tx, metaKeyEmbeddingIndexBaseURL, strings.TrimRight(config.BaseURL, "/")); err != nil {
				return err
			}
			return s.store.deleteMeta(tx, metaKeyEmbeddingDimensions)
		})
		if err != nil {
			return ReindexResult{}, err
		}
		return ReindexResult{Model: config.Model}, nil
	}

	embedder := s.embedderFactory(config)
	vectors := make([][]float32, 0, len(memories))
	for start := 0; start < len(memories); start += reindexBatchSize {
		end := start + reindexBatchSize
		if end > len(memories) {
			end = len(memories)
		}
		texts := make([]string, 0, end-start)
		for _, memory := range memories[start:end] {
			texts = append(texts, memory.Data)
		}
		batch, err := embedder.EmbedBatch(ctx, texts, constants.MemoryEmbeddingActionAdd)
		if err != nil {
			return ReindexResult{}, err
		}
		if len(batch) != len(texts) {
			return ReindexResult{}, status_error.MemoryEmbeddingResponseInvalid
		}
		vectors = append(vectors, batch...)
	}
	dimensions := len(vectors[0])
	if dimensions == 0 {
		return ReindexResult{}, status_error.MemoryEmbeddingResponseInvalid
	}
	for _, vector := range vectors {
		if len(vector) != dimensions {
			return ReindexResult{}, status_error.MemoryEmbeddingResponseInvalid
		}
	}

	err = s.store.withTx(ctx, func(tx *sql.Tx) error {
		if err := s.vectors.recreateVecTable(tx, dimensions); err != nil {
			return err
		}
		for index, memory := range memories {
			if err := s.vectors.insertVector(tx, memory.ID, vectors[index], memory.Scope, memory.ScopeID); err != nil {
				return err
			}
		}
		if err := s.store.setMeta(tx, metaKeyEmbeddingModel, config.Model); err != nil {
			return err
		}
		if err := s.store.setMeta(tx, metaKeyEmbeddingIndexBaseURL, strings.TrimRight(config.BaseURL, "/")); err != nil {
			return err
		}
		return s.store.setMeta(tx, metaKeyEmbeddingDimensions, strconv.Itoa(dimensions))
	})
	if err != nil {
		return ReindexResult{}, err
	}
	return ReindexResult{
		VectorCount: int64(len(vectors)),
		Model:       config.Model,
		Dimensions:  dimensions,
	}, nil
}

// Status 返回记忆服务当前状态，供独立记忆页面展示。
func (s *Service) Status(ctx context.Context) (Status, error) {
	config, err := s.configSource(ctx)
	if err != nil {
		return Status{}, err
	}
	status := Status{
		Mode:       constants.MemoryRetrievalModeKeyword,
		Configured: config.Complete(),
	}
	storedModel, modelExists, err := s.store.getMeta(metaKeyEmbeddingModel)
	if err != nil {
		return Status{}, err
	}
	if modelExists {
		status.Model = storedModel
	}
	storedDimensions, dimensionsExist, err := s.store.getMeta(metaKeyEmbeddingDimensions)
	if err != nil {
		return Status{}, err
	}
	if dimensionsExist {
		if parsed, err := strconv.Atoi(storedDimensions); err == nil {
			status.Dimensions = parsed
		}
	}
	if status.Configured && modelExists && storedModel != config.Model {
		status.Conflict = true
	}
	storedBaseURL, baseURLExists, err := s.store.getMeta(metaKeyEmbeddingIndexBaseURL)
	if err != nil {
		return Status{}, err
	}
	if status.Configured && baseURLExists && storedBaseURL != strings.TrimRight(config.BaseURL, "/") {
		status.Conflict = true
	}
	if status.Configured && !status.Conflict {
		status.Mode = constants.MemoryRetrievalModeSemantic
	}
	if status.MemoryCount, err = s.store.memoryCount(); err != nil {
		return Status{}, err
	}
	if status.VectorCount, err = s.vectors.vectorCount(); err != nil {
		return Status{}, err
	}
	status.NeedsReindex = status.Configured && !status.Conflict && status.MemoryCount != status.VectorCount
	if status.NeedsReindex {
		status.Mode = constants.MemoryRetrievalModeKeyword
	}
	return status, nil
}

// ProbeEmbedding 探测当前 Embedding 配置是否可用，供独立记忆设置测试使用。
func (s *Service) ProbeEmbedding(ctx context.Context) error {
	state, err := s.resolveEmbedding(ctx)
	if err != nil {
		return err
	}
	if state.embedder == nil {
		return status_error.MemoryEmbeddingNotConfigured
	}
	_, err = state.embedder.Embed(ctx, "ally memory probe", constants.MemoryEmbeddingActionSearch)
	return err
}

// DeleteScope 级联删除指定范围的全部记忆、向量、关键词索引与历史。
//
// 用于伙伴或项目删除场景；级联删除连同 history 一起清除（属主已不存在），
// 属主不存在后清理其不可再访问的内容与变更记录。
func (s *Service) DeleteScope(ctx context.Context, scope constants.MemoryScope, scopeID string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	if scope != constants.MemoryScopePartner && scope != constants.MemoryScopeProject {
		return status_error.BadRequest
	}
	if strings.TrimSpace(scopeID) == "" {
		return status_error.BadRequest
	}
	return s.store.withTx(ctx, func(tx *sql.Tx) error {
		if err := s.vectors.deleteScopeVectors(tx, scope, scopeID); err != nil {
			return err
		}
		if err := s.store.deleteScopeFTS(tx, scope, scopeID); err != nil {
			return err
		}
		return s.store.deleteScopeData(tx, scope, scopeID)
	})
}

// CleanupOrphanScopes 重试清理主业务库中已不存在的伙伴或项目记忆。
//
// 删除 API 与 memory.db 不共享事务；若即时清理失败，残留范围会在下次启动时
// 被本方法重新发现并清除，避免仅记录日志后永久遗留。
func (s *Service) CleanupOrphanScopes(ctx context.Context, exists ScopeExists) error {
	if exists == nil {
		return status_error.BadRequest
	}
	owners, err := s.store.listScopeOwners(ctx)
	if err != nil {
		return err
	}
	for _, owner := range owners {
		present, err := exists(ctx, owner.Scope, owner.ScopeID)
		if err != nil {
			return err
		}
		if present {
			continue
		}
		if err := s.DeleteScope(ctx, owner.Scope, owner.ScopeID); err != nil {
			return err
		}
	}
	return nil
}

// ListMemories 返回指定伙伴或项目范围内的一页当前记忆。
func (s *Service) ListMemories(ctx context.Context, filter ListFilter) (MemoryPage, error) {
	return s.store.listMemoryPage(ctx, normalizeListFilter(filter))
}

// ListTimeline 返回指定伙伴或项目范围内的一页追加式变更事件。
func (s *Service) ListTimeline(ctx context.Context, filter ListFilter) (TimelinePage, error) {
	return s.store.listTimelinePage(ctx, normalizeListFilter(filter))
}

// FreezeEmbedder 固定当前选中的模型与凭据，不将记忆索引的就绪状态带给调用方。
func (s *Service) FreezeEmbedder(ctx context.Context, modelID string) (FrozenEmbedder, error) {
	switch ctx.Err() {
	case context.Canceled:
		return FrozenEmbedder{}, status_error.OperationCanceled
	case context.DeadlineExceeded:
		return FrozenEmbedder{}, status_error.OperationTimeout
	}
	// 与设置变更共用锁，避免把不同配置版本的地址、模型和密钥拼成一个快照。
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	config, err := s.configSource(ctx)
	if err != nil {
		return FrozenEmbedder{}, err
	}
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	config.Model = strings.TrimSpace(config.Model)
	modelID = strings.TrimSpace(modelID)
	if !config.Complete() || modelID == "" {
		return FrozenEmbedder{}, status_error.MemoryEmbeddingNotConfigured
	}
	if config.Model != modelID {
		return FrozenEmbedder{}, status_error.MemoryEmbeddingConflict
	}
	if err := validateEmbeddingBaseURL(config.BaseURL); err != nil {
		return FrozenEmbedder{}, err
	}
	embedder := s.embedderFactory(config)
	if embedder == nil {
		return FrozenEmbedder{}, status_error.MemoryEmbeddingUnavailable
	}
	fingerprint := sha256.Sum256([]byte(config.BaseURL + "\x00" + config.Model))
	return FrozenEmbedder{
		ModelID:     config.Model,
		Fingerprint: hex.EncodeToString(fingerprint[:]),
		Embed: func(ctx context.Context, texts []string) ([][]float32, error) {
			return embedder.EmbedBatch(ctx, texts, constants.MemoryEmbeddingActionAdd)
		},
	}, nil
}

// GetEmbeddingSettings 返回不含明文凭据的嵌入配置。
func (s *Service) GetEmbeddingSettings() (EmbeddingSettings, error) {
	config, err := s.store.embeddingConfig()
	if err != nil {
		return EmbeddingSettings{}, err
	}
	return EmbeddingSettings{
		BaseURL:          config.BaseURL,
		Model:            config.Model,
		APIKeyConfigured: config.APIKey != "",
	}, nil
}

// UpdateEmbeddingSettings 保存嵌入配置；空 APIKey 保留旧值，ClearAPIKey 显式清除。
func (s *Service) UpdateEmbeddingSettings(ctx context.Context, update EmbeddingSettingsUpdate) (EmbeddingSettings, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if update.ClearAPIKey && update.APIKey != "" {
		return EmbeddingSettings{}, status_error.BadRequest
	}
	baseURL := strings.TrimSpace(update.BaseURL)
	if err := validateEmbeddingBaseURL(baseURL); err != nil {
		return EmbeddingSettings{}, err
	}
	current, err := s.store.embeddingConfig()
	if err != nil {
		return EmbeddingSettings{}, err
	}
	current.BaseURL = baseURL
	current.Model = strings.TrimSpace(update.Model)
	if update.ClearAPIKey {
		current.APIKey = ""
	} else if update.APIKey != "" {
		current.APIKey = update.APIKey
	}
	if err := s.store.setEmbeddingConfig(ctx, current); err != nil {
		return EmbeddingSettings{}, err
	}
	return EmbeddingSettings{BaseURL: current.BaseURL, Model: current.Model, APIKeyConfigured: current.APIKey != ""}, nil
}

// validateEmbeddingBaseURL 校验 OpenAI 兼容服务基础地址；空值表示关闭语义检索。
func validateEmbeddingBaseURL(value string) error {
	if value == "" {
		return nil
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return status_error.BadRequest
	}
	return nil
}

func normalizeListFilter(filter ListFilter) ListFilter {
	if filter.Limit <= 0 {
		filter.Limit = constants.MemoryManagementDefaultPageSize
	}
	if filter.Limit > constants.MemoryManagementMaxPageSize {
		filter.Limit = constants.MemoryManagementMaxPageSize
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	filter.Query = strings.TrimSpace(filter.Query)
	return filter
}

// listHistoryForTest 返回一条记忆的历史，供模块内测试断言。
func (s *Service) listHistoryForTest(memoryID string) ([]HistoryEntry, error) {
	return s.store.listHistory(memoryID)
}
