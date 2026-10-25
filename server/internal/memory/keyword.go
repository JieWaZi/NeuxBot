package memory

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

// keywordSearch 在未配置 Embedding 时提供关键词召回。
type keywordSearch struct {
	// store 是共享的记忆存储层。
	store *store
}

// ftsMinTokenRunes 是 trigram 分词器能形成三元组的最小字符数，短于此长度直接走 LIKE。
const ftsMinTokenRunes = 3

// search 在指定范围内按关键词召回记忆。
func (k *keywordSearch) search(
	ctx context.Context,
	query string,
	topK int,
	scope constants.MemoryScope,
	scopeID string,
) ([]ScoredMemory, error) {
	if k.store.ftsAvailable && utf8.RuneCountInString(query) >= ftsMinTokenRunes {
		// MATCH 语法错误等可预期问题直接降级到 LIKE，不向调用方暴露实现细节。
		if results, err := k.searchFTS(ctx, query, topK, scope, scopeID); err == nil {
			return results, nil
		}
	}
	return k.searchLike(ctx, query, topK, scope, scopeID)
}

// buildFTSQuery 把用户查询转换为 FTS5 MATCH 表达式：分词、转义、OR 连接。
//
// trigram 分词器对中文等无空格语言按子串匹配；多词查询取并集，排序交给 bm25。
func buildFTSQuery(query string) string {
	tokens := strings.Fields(query)
	quoted := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.ReplaceAll(token, `"`, `""`)
		if token == "" {
			continue
		}
		quoted = append(quoted, `"`+token+`"`)
	}
	return strings.Join(quoted, " OR ")
}

// searchFTS 使用 FTS5 trigram 索引召回，按 bm25 升序（越小越好）排序。
// 此处为检索特例：虚拟表负责排名，memories 负责范围与正文，单次查询保持排名、范围和版本一致。
func (k *keywordSearch) searchFTS(
	ctx context.Context,
	query string,
	topK int,
	scope constants.MemoryScope,
	scopeID string,
) ([]ScoredMemory, error) {
	matchQuery := buildFTSQuery(query)
	if matchQuery == "" {
		return []ScoredMemory{}, nil
	}
	rows, err := k.store.db.QueryContext(
		ctx,
		`SELECT `+memoryColumnsAliased+`, bm25(memory_fts) AS rank
		 FROM memory_fts
		 JOIN memories m ON m.id = memory_fts.memory_id
		 WHERE memory_fts MATCH ? AND m.scope = ? AND m.scope_id = ?
		 ORDER BY rank ASC, m.updated_at DESC
		 LIMIT ?`,
		matchQuery,
		string(scope),
		scopeID,
		topK,
	)
	if err != nil {
		return nil, status_error.DatabaseError
	}
	defer rows.Close()
	results := []ScoredMemory{}
	midpoint, steepness := bm25Parameters(query)
	for rows.Next() {
		memory := Memory{}
		var rank float64
		if err := rows.Scan(
			&memory.Seq,
			&memory.ID,
			&memory.Scope,
			&memory.ScopeID,
			&memory.Data,
			&memory.Hash,
			&memory.SourceRunID,
			&memory.CreatedAt,
			&memory.UpdatedAt,
			&rank,
		); err != nil {
			return nil, status_error.DatabaseError
		}
		rawScore := -rank
		if rawScore < 0 {
			rawScore = 0
		}
		results = append(results, ScoredMemory{
			Memory: memory,
			Score:  normalizeBM25(rawScore, midpoint, steepness),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, status_error.DatabaseError
	}
	return results, nil
}

// searchLike 使用转义 LIKE 子串扫描兜底，按最近更新排序。
func (k *keywordSearch) searchLike(
	ctx context.Context,
	query string,
	topK int,
	scope constants.MemoryScope,
	scopeID string,
) ([]ScoredMemory, error) {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	rows, err := k.store.db.QueryContext(
		ctx,
		`SELECT `+memoryColumnsAliased+`
		 FROM memories m
		 WHERE m.scope = ? AND m.scope_id = ? AND m.data LIKE '%' || ? || '%' ESCAPE '\'
		 ORDER BY m.updated_at DESC
		 LIMIT ?`,
		string(scope),
		scopeID,
		escaped,
		topK,
	)
	if err != nil {
		return nil, status_error.DatabaseError
	}
	memories, err := scanMemories(rows)
	if err != nil {
		return nil, status_error.DatabaseError
	}
	results := make([]ScoredMemory, 0, len(memories))
	for _, memory := range memories {
		results = append(results, ScoredMemory{Memory: memory, Score: 1})
	}
	return results, nil
}

// bm25Parameters 按查询词数返回 归一化使用的 sigmoid 中点与陡峭度。
func bm25Parameters(query string) (float64, float64) {
	termCount := len(strings.Fields(query))
	if termCount == 0 {
		termCount = 1
	}
	switch {
	case termCount <= 3:
		return 5, 0.7
	case termCount <= 6:
		return 7, 0.6
	case termCount <= 9:
		return 9, 0.5
	case termCount <= 15:
		return 10, 0.5
	default:
		return 12, 0.5
	}
}

// normalizeBM25 使用 logistic sigmoid 把原始 BM25 分数映射到 [0,1]。
func normalizeBM25(rawScore float64, midpoint float64, steepness float64) float64 {
	return 1 / (1 + math.Exp(-steepness*(rawScore-midpoint)))
}
