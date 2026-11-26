package memory

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

// ConfigSource 在每次操作时提供当前 Embedding 配置，使记忆设置变更即时生效。
type ConfigSource func(ctx context.Context) (EmbeddingConfig, error)

// PartnerExists 判断记忆属主是否仍存在于主业务库。
type PartnerExists func(ctx context.Context, partnerID string) (bool, error)

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
	if !utf8.ValidString(normalized) || utf8.RuneCountInString(normalized) > constants.MemoryContentMaxRunes {
		return "", status_error.BadRequest
	}
	return normalized, nil
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
func (s *Service) checkDimensions(vector []float32) error {
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

// Write 保存一条当前伙伴的普通记忆，正文完全匹配时返回原条目。
func (s *Service) Write(ctx context.Context, mc Context, content string) (WriteResult, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if replayed, err := s.replayAgentCommand(ctx); err != nil {
		return WriteResult{}, err
	} else if replayed {
		command := ctx.Value(agentCommandKey{}).(*agentCommand)
		return WriteResult{Memory: Memory{ID: command.memoryID}, Duplicate: command.duplicate}, nil
	}
	if strings.TrimSpace(mc.PartnerID) == "" {
		return WriteResult{}, status_error.BadRequest
	}
	normalized, err := validateContent(content)
	if err != nil {
		return WriteResult{}, err
	}
	hash := contentHash(normalized)
	source := explicitSource(mc)
	existing, found, err := findByHash(s.store.db, mc.PartnerID, hash)
	if err != nil {
		return WriteResult{}, err
	}
	if found {
		if command, ok := ctx.Value(agentCommandKey{}).(*agentCommand); ok {
			command.memoryID, command.duplicate = existing.ID, true
		}
		if err := s.store.withTx(ctx, func(tx *sql.Tx) error {
			if source != constants.MemoryTimelineSourceManual {
				return nil
			}
			if err := allowReviewHash(tx, mc.PartnerID, hash); err != nil {
				return err
			}
			if existing.Origin == constants.MemoryTimelineSourceReview && existing.control != memoryControlUser {
				existing.control = memoryControlUser
				existing.Revision++
				return s.store.updateMemory(tx, *existing)
			}
			return nil
		}); err != nil {
			return WriteResult{}, err
		}
		return WriteResult{Memory: *existing, Duplicate: true}, nil
	}
	state, err := s.resolveEmbedding(ctx)
	if err != nil {
		return WriteResult{}, err
	}
	vector, err := s.embedMemory(ctx, state, normalized, constants.MemoryEmbeddingActionAdd)
	if err != nil {
		return WriteResult{}, err
	}
	now := time.Now().UnixMilli()
	memory := Memory{ID: util.New(), PartnerID: mc.PartnerID, Data: normalized, Hash: hash,
		SourceRunID: mc.RunID, CreatedAt: now, UpdatedAt: now, Revision: 1, Origin: source, control: memoryControlUser}
	agentMemoryID(ctx, memory.ID)
	err = s.store.withTx(ctx, func(tx *sql.Tx) error {
		if source == constants.MemoryTimelineSourceManual {
			if err := allowReviewHash(tx, mc.PartnerID, hash); err != nil {
				return err
			}
		}
		return s.applyMemoryContent(tx, &memory, vector, state.config, HistoryEntry{
			Event: constants.MemoryEventAdd, Source: source, SourceRunID: mc.RunID,
		})
	})
	if err != nil {
		return WriteResult{}, err
	}
	return WriteResult{Memory: memory}, nil
}

// Search 检索当前执行的伙伴记忆。
func (s *Service) Search(ctx context.Context, mc Context, query string, topK int) (SearchResult, error) {
	normalized := strings.TrimSpace(query)
	if normalized == "" || strings.TrimSpace(mc.PartnerID) == "" {
		return SearchResult{}, status_error.BadRequest
	}
	if topK <= 0 {
		topK = constants.MemorySearchDefaultTopK
	}
	topK = min(topK, constants.MemorySearchMaxTopK)
	state, err := s.resolveEmbedding(ctx)
	if err != nil {
		return SearchResult{}, err
	}
	if state.embedder != nil && state.ready {
		vector, err := s.embedMemory(ctx, state, normalized, constants.MemoryEmbeddingActionSearch)
		if err != nil {
			return SearchResult{}, err
		}
		results, err := s.vectors.search(ctx, vector, topK, mc.PartnerID)
		if err != nil {
			return SearchResult{}, err
		}
		eligible := results[:0]
		for _, item := range results {
			if item.Score >= constants.MemorySearchScoreThreshold {
				eligible = append(eligible, item)
			}
		}
		return SearchResult{Results: eligible, Mode: constants.MemoryRetrievalModeSemantic}, nil
	}
	results, err := s.keyword.search(ctx, normalized, topK, mc.PartnerID)
	if err != nil {
		return SearchResult{}, err
	}
	return SearchResult{Results: results, Mode: constants.MemoryRetrievalModeKeyword, Conflict: state.conflict}, nil
}

// getAccessibleMemory 对未知条目和其他伙伴的条目统一返回找不到。
func getAccessibleMemory(queryer queryRower, memoryID, partnerID string) (*Memory, error) {
	memory, exists, err := getMemory(queryer, memoryID)
	if err != nil {
		return nil, err
	}
	if !exists || partnerID == "" || memory.PartnerID != partnerID {
		return nil, status_error.MemoryNotFound
	}
	return memory, nil
}

// Update 原子更新正文、变更历史、索引及可能受影响的核心快照。
func (s *Service) Update(ctx context.Context, mc Context, memoryID, content string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if replayed, err := s.replayAgentCommand(ctx); err != nil || replayed {
		return err
	}
	agentMemoryID(ctx, memoryID)
	memory, err := getAccessibleMemory(s.store.db, memoryID, mc.PartnerID)
	if err != nil {
		return err
	}
	normalized, err := validateContent(content)
	if err != nil {
		return err
	}
	newHash := contentHash(normalized)
	if existing, found, err := findByHash(s.store.db, mc.PartnerID, newHash); err != nil {
		return err
	} else if found && existing.ID != memory.ID {
		return status_error.BadRequest
	}
	state, err := s.resolveEmbedding(ctx)
	if err != nil {
		return err
	}
	vector, err := s.embedMemory(ctx, state, normalized, constants.MemoryEmbeddingActionUpdate)
	if err != nil {
		return err
	}
	previous := memory.Data
	memory.Data, memory.Hash, memory.SourceRunID = normalized, newHash, mc.RunID
	memory.UpdatedAt = time.Now().UnixMilli()
	memory.Revision++
	memory.control = memoryControlUser
	return s.store.withTx(ctx, func(tx *sql.Tx) error {
		if err := s.applyMemoryContent(tx, memory, vector, state.config, HistoryEntry{
			Event: constants.MemoryEventUpdate, OldMemory: previous, Source: explicitSource(mc), SourceRunID: mc.RunID,
		}); err != nil {
			return err
		}
		if memory.Core && previous != normalized {
			return updateCoreSnapshot(tx, mc.PartnerID)
		}
		return nil
	})
}

// Forget 删除当前知识和索引，保存变更历史并抑制自动重新添加同文事实。
func (s *Service) Forget(ctx context.Context, mc Context, memoryID string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if replayed, err := s.replayAgentCommand(ctx); err != nil || replayed {
		return err
	}
	agentMemoryID(ctx, memoryID)
	memory, err := getAccessibleMemory(s.store.db, memoryID, mc.PartnerID)
	if err != nil {
		return err
	}
	return s.store.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO partner_forgotten_memory (partner_id, hash) VALUES (?, ?) ON CONFLICT DO NOTHING`, mc.PartnerID, memory.Hash); err != nil {
			return status_error.DatabaseError
		}
		if err := s.applyMemoryForget(tx, *memory, HistoryEntry{Source: explicitSource(mc), SourceRunID: mc.RunID}); err != nil {
			return err
		}
		if memory.Core {
			return updateCoreSnapshot(tx, mc.PartnerID)
		}
		return nil
	})
}

func explicitSource(mc Context) constants.MemoryTimelineSource {
	if mc.RunID != "" {
		return constants.MemoryTimelineSourceMCP
	}
	return constants.MemoryTimelineSourceManual
}

func allowReviewHash(tx *sql.Tx, partnerID, hash string) error {
	if _, err := tx.Exec(`DELETE FROM partner_forgotten_memory WHERE partner_id = ? AND hash = ?`, partnerID, hash); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

func (s *Service) embedMemory(ctx context.Context, state embeddingState, content string, action constants.MemoryEmbeddingAction) ([]float32, error) {
	if state.embedder == nil || state.conflict {
		return nil, nil
	}
	vector, err := state.embedder.Embed(ctx, content, action)
	if err != nil {
		return nil, err
	}
	if len(vector) == 0 {
		return nil, status_error.MemoryEmbeddingResponseInvalid
	}
	return vector, s.checkDimensions(vector)
}

func (s *Service) applyMemoryContent(tx *sql.Tx, memory *Memory, vector []float32, config EmbeddingConfig, history HistoryEntry) error {
	if history.Event == constants.MemoryEventAdd {
		if err := s.store.insertMemory(tx, memory); err != nil {
			return err
		}
	} else {
		if err := s.store.updateMemory(tx, *memory); err != nil {
			return err
		}
		if err := s.vectors.deleteVector(tx, memory.ID); err != nil {
			return err
		}
	}
	if err := s.store.upsertFTS(tx, memory.ID, memory.Data); err != nil {
		return err
	}
	if vector != nil {
		if err := s.vectors.ensureVecTable(tx, len(vector)); err != nil {
			return err
		}
		for _, item := range []struct {
			// key 是已验证的嵌入配置元数据标识。
			key string
			// value 是与本次实际写入向量匹配的配置值。
			value string
		}{{metaKeyEmbeddingModel, config.Model}, {metaKeyEmbeddingIndexBaseURL, strings.TrimRight(config.BaseURL, "/")}, {metaKeyEmbeddingDimensions, strconv.Itoa(len(vector))}} {
			if err := s.store.setMeta(tx, item.key, item.value); err != nil {
				return err
			}
		}
		if err := s.vectors.insertVector(tx, memory.ID, vector, memory.PartnerID); err != nil {
			return err
		}
	}
	history.ID, history.MemoryID, history.PartnerID = util.New(), memory.ID, memory.PartnerID
	history.NewMemory, history.CreatedAt, history.UpdatedAt = memory.Data, memory.CreatedAt, memory.UpdatedAt
	return s.store.insertHistory(tx, history)
}

func (s *Service) applyMemoryForget(tx *sql.Tx, memory Memory, history HistoryEntry) error {
	if err := s.vectors.deleteVector(tx, memory.ID); err != nil {
		return err
	}
	if err := s.store.deleteFTS(tx, memory.ID); err != nil {
		return err
	}
	if err := s.store.deleteMemory(tx, memory.ID); err != nil {
		return err
	}
	history.ID, history.MemoryID, history.PartnerID = util.New(), memory.ID, memory.PartnerID
	history.OldMemory, history.CreatedAt, history.UpdatedAt = memory.Data, memory.CreatedAt, time.Now().UnixMilli()
	history.Event, history.IsDeleted = constants.MemoryEventDelete, true
	return s.store.insertHistory(tx, history)
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
			if err := s.vectors.insertVector(tx, memory.ID, vectors[index], memory.PartnerID); err != nil {
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
	_, err = state.embedder.Embed(ctx, "bot memory probe", constants.MemoryEmbeddingActionSearch)
	return err
}

// DuplicatePartner 将当前记忆和可复用索引复制给尚未发布的伙伴。
//
// 调用方须保证目标标识在主业务库尚未使用，并串行化同一目标的复制。
// 重试时，之前暂存的目标数据和本次副本在同一记忆事务中替换。
func (s *Service) DuplicatePartner(ctx context.Context, sourceID, targetID string) error {
	if strings.TrimSpace(sourceID) == "" || strings.TrimSpace(targetID) == "" || sourceID == targetID {
		return status_error.BadRequest
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	return s.store.withTx(ctx, func(tx *sql.Tx) error {
		entries, err := s.store.listPartnerMemoriesForCopy(ctx, tx, sourceID)
		if err != nil {
			return err
		}
		if err := s.deletePartner(tx, targetID); err != nil {
			return err
		}
		hasVectors, err := s.vectors.vecTableExists(tx)
		if err != nil {
			return err
		}
		now := time.Now().UnixMilli()
		for _, entry := range entries {
			entryCopy := Memory{
				ID: util.New(), PartnerID: targetID, Data: entry.Data, Hash: entry.Hash,
				CreatedAt: now, UpdatedAt: now, Core: entry.Core, Revision: 1, Origin: entry.Origin, control: entry.control,
			}
			if err := s.store.insertMemory(tx, &entryCopy); err != nil {
				return err
			}
			if hasVectors {
				if err := s.vectors.copyVector(ctx, tx, entry, entryCopy); err != nil {
					return err
				}
			}
		}
		if err := s.store.indexPartnerKeywords(ctx, tx, targetID); err != nil {
			return err
		}
		return updateCoreSnapshot(tx, targetID)
	})
}

// DeletePartner 清除伙伴的记忆、索引、历史及命令回执。
func (s *Service) DeletePartner(ctx context.Context, partnerID string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if strings.TrimSpace(partnerID) == "" {
		return status_error.BadRequest
	}
	return s.store.withTx(ctx, func(tx *sql.Tx) error {
		return s.deletePartner(tx, partnerID)
	})
}

func (s *Service) deletePartner(tx *sql.Tx, partnerID string) error {
	if err := s.vectors.deletePartnerVectors(tx, partnerID); err != nil {
		return err
	}
	if err := s.store.deletePartnerFTS(tx, partnerID); err != nil {
		return err
	}
	return s.store.deletePartnerData(tx, partnerID)
}

// CleanupOrphanPartners 重试清理已不存在伙伴的记忆数据。
func (s *Service) CleanupOrphanPartners(ctx context.Context, exists PartnerExists) error {
	if exists == nil {
		return status_error.BadRequest
	}
	owners, err := s.store.listPartnerOwners(ctx)
	if err != nil {
		return err
	}
	for _, owner := range owners {
		present, err := exists(ctx, owner)
		if err != nil {
			return err
		}
		if !present {
			if err := s.DeletePartner(ctx, owner); err != nil {
				return err
			}
		}
	}
	return nil
}

// ListMemories 返回伙伴当前记忆。
func (s *Service) ListMemories(ctx context.Context, filter ListFilter) (MemoryPage, error) {
	return s.store.listMemoryPage(ctx, normalizeListFilter(filter))
}

// ListTimeline 返回伙伴记忆的追加式变更事件。
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
