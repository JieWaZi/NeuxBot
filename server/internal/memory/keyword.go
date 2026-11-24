package memory

import (
	"context"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// keywordSearch 在未配置 Embedding 时提供关键词召回。
type keywordSearch struct {
	// store 是共享的记忆存储层。
	store *store
}

// ftsMinTokenRunes 是 trigram 分词器能形成三元组的最小字符数，短于此长度直接走 LIKE。
const ftsMinTokenRunes = 3

// search 在指定伙伴内按关键词召回记忆。
func (k *keywordSearch) search(
	ctx context.Context,
	query string,
	topK int,
	partnerID string,
) ([]ScoredMemory, error) {
	if k.store.ftsAvailable && utf8.RuneCountInString(query) >= ftsMinTokenRunes {
		// MATCH 语法错误等可预期问题直接降级到 LIKE，不向调用方暴露实现细节。
		if results, err := k.searchFTS(ctx, query, topK, partnerID); err == nil {
			return results, nil
		}
	}
	return k.searchLike(ctx, query, topK, partnerID)
}

// reviewKeywordLimit 限制对多轮原文的检索词数和 SQL 参数数。
const reviewKeywordLimit = 64

func (k *keywordSearch) searchReview(ctx context.Context, query string, topK int, partnerID string) ([]ScoredMemory, error) {
	if k.store.ftsAvailable {
		terms := reviewKeywordTerms(query, ftsMinTokenRunes)
		hits, err := k.searchFTS(ctx, strings.Join(terms, " "), topK, partnerID)
		if err == nil && len(hits) != 0 {
			return hits, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	terms := reviewKeywordTerms(query, 2)
	if len(terms) == 0 {
		return k.searchLike(ctx, query, topK, partnerID)
	}
	return k.searchLikeTerms(ctx, terms, topK, partnerID)
}

// reviewKeywordTerms 将连续汉字按重叠窗口切分，使更正前后仍共享主题词。
func reviewKeywordTerms(query string, hanWidth int) []string {
	words := strings.FieldsFunc(query, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	terms := make([]string, 0, reviewKeywordLimit)
	seen := make(map[string]struct{}, reviewKeywordLimit)
	add := func(term string) {
		if _, exists := seen[term]; !exists && len(terms) < reviewKeywordLimit {
			seen[term] = struct{}{}
			terms = append(terms, term)
		}
	}
	for _, word := range words {
		runes := []rune(word)
		for len(runes) != 0 && len(terms) < reviewKeywordLimit {
			han := unicode.Is(unicode.Han, runes[0])
			end := 1
			for end < len(runes) && unicode.Is(unicode.Han, runes[end]) == han {
				end++
			}
			if han {
				for i := 0; i+hanWidth <= end && len(terms) < reviewKeywordLimit; i++ {
					add(string(runes[i : i+hanWidth]))
				}
			} else if end >= hanWidth {
				add(strings.ToLower(string(runes[:end])))
			}
			runes = runes[end:]
		}
		if len(terms) == reviewKeywordLimit {
			break
		}
	}
	return terms
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
// 此处为检索特例：虚拟表负责排名，partner_memories 负责范围与正文，单次查询保持排名、范围和版本一致。
func (k *keywordSearch) searchFTS(
	ctx context.Context,
	query string,
	topK int,
	partnerID string,
) ([]ScoredMemory, error) {
	matchQuery := buildFTSQuery(query)
	if matchQuery == "" {
		return []ScoredMemory{}, nil
	}
	rows, err := k.store.db.QueryContext(
		ctx,
		`SELECT `+memoryColumnsAliased+`, bm25(partner_memory_fts) AS rank
		 FROM partner_memory_fts
		 JOIN partner_memories m ON m.id = partner_memory_fts.memory_id
		 WHERE partner_memory_fts MATCH ? AND m.partner_id = ?
		 ORDER BY rank ASC, m.updated_at DESC
		 LIMIT ?`,
		matchQuery,
		partnerID,
		topK,
	)
	if err != nil {
		return nil, status_error.DatabaseError
	}
	defer rows.Close()
	results := []ScoredMemory{}
	midpoint, steepness := bm25Parameters(query)
	for rows.Next() {
		var rank float64
		memory, err := scanMemory(rows, &rank)
		if err != nil {
			return nil, status_error.DatabaseError
		}
		rawScore := -rank
		if rawScore < 0 {
			rawScore = 0
		}
		results = append(results, ScoredMemory{
			Memory: *memory,
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
	partnerID string,
) ([]ScoredMemory, error) {
	return k.searchLikeTerms(ctx, []string{query}, topK, partnerID)
}

// searchLikeTerms 按单伙伴内命中的词数排序，避免短词只召回近期无关条目。
func (k *keywordSearch) searchLikeTerms(ctx context.Context, terms []string, topK int, partnerID string) ([]ScoredMemory, error) {
	escape := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	const match = `m.data LIKE '%' || ? || '%' ESCAPE '\'`
	matches := make([]string, len(terms))
	scores := make([]string, len(terms))
	args := make([]any, 0, len(terms)*2+2)
	args = append(args, partnerID)
	for i, term := range terms {
		matches[i] = match
		scores[i] = "(" + match + ")"
		args = append(args, escape.Replace(term))
	}
	order := "m.updated_at DESC"
	if len(terms) > 1 {
		order = strings.Join(scores, " + ") + " DESC, " + order
		args = append(args, args[1:]...)
	}
	args = append(args, topK)
	rows, err := k.store.db.QueryContext(ctx,
		`SELECT `+memoryColumnsAliased+` FROM partner_memories m
		 WHERE m.partner_id = ? AND (`+strings.Join(matches, " OR ")+`)
		 ORDER BY `+order+` LIMIT ?`, args...)
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
