package integration_knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"strings"
)

// 公开原件检索要求
type HybridSearchInput struct {
	// 检索文本
	QueryText string `json:"query_text" validate:"required,max=2000"`
	// 返回分块上限
	MatchCount int `json:"match_count" validate:"min=0,max=30"`
}

// 公开检索命中
type HybridSearchHit struct {
	// 分块标识
	ID string `json:"id"`
	// 原件内容
	Content string `json:"content"`
	// 来源版本标识
	KnowledgeID string `json:"knowledge_id"`
	// 原件文件名
	KnowledgeFilename string `json:"knowledge_filename"`
	// 实际检索分数
	Score float64 `json:"score"`
	// 检索方式
	MatchType int `json:"match_type"`
	// 父块标识
	ParentChunkID string `json:"parent_chunk_id"`
	// 原文起始位置
	StartAt int `json:"start_at"`
	// 原文结束位置
	EndAt int `json:"end_at"`
	// 分块序号
	ChunkIndex int `json:"chunk_index"`
}

// 公开原件检索响应
type HybridSearchResponse struct {
	// 请求成功
	Success bool `json:"success"`
	// 实际命中
	Data []HybridSearchHit `json:"data"`
}

// 检索知识原件
type PublishedHybridSearch struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"id" validate:"required"`
	// 检索要求
	Body HybridSearchInput `in:"body"`
}

func (*PublishedHybridSearch) Path() string { return "/knowledge-bases/:id/hybrid-search" }
func (o *PublishedHybridSearch) Output(ctx *gin.Context) (interface{}, error) {
	return hybridSearch(ctx, o.LibraryID, o.Body)
}

// 读取知识原件检索结果
type PublishedHybridSearchGET struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"id" validate:"required"`
	// 检索要求
	Body HybridSearchInput `in:"body"`
}

func (*PublishedHybridSearchGET) Path() string { return "/knowledge-bases/:id/hybrid-search" }
func (o *PublishedHybridSearchGET) Output(ctx *gin.Context) (interface{}, error) {
	return hybridSearch(ctx, o.LibraryID, o.Body)
}
func hybridSearch(ctx *gin.Context, library string, input HybridSearchInput) (interface{}, error) {
	if strings.TrimSpace(input.QueryText) == "" || input.MatchCount < 0 || input.MatchCount > 30 {
		return nil, status_error.KnowledgeInvalid
	}
	limit := input.MatchCount
	if limit == 0 {
		limit = 5
	}
	d := dependencies(ctx)
	var hits []bridge.DocumentReadHit
	var err error
	if d.knowledge != nil {
		hits, _, err = d.knowledge.SearchSourceChunks(ctx.Request.Context(), library, input.QueryText, limit)
	} else {
		hits, _, err = (bridge.LocalReadProvider{Engine: d.engine, Limit: limit}).SearchChunks(ctx.Request.Context(), bridge.LocalRef(library), input.QueryText)
	}
	if err != nil {
		return nil, err
	}
	out := HybridSearchResponse{Success: true, Data: []HybridSearchHit{}}
	for _, hit := range hits {
		source, e := d.engine.GetSource(ctx.Request.Context(), library, hit.Chunk.SourceVersionID)
		if e != nil {
			return nil, bridge.MapError(e)
		}
		out.Data = append(out.Data, HybridSearchHit{ID: hit.Chunk.ChunkID, Content: hit.Chunk.Content, KnowledgeID: hit.Chunk.SourceVersionID, KnowledgeFilename: source.Filename, Score: hit.Score, MatchType: int(hit.MatchType), ParentChunkID: hit.Chunk.ParentID, StartAt: hit.Chunk.Start, EndAt: hit.Chunk.End, ChunkIndex: hit.Chunk.Sequence})
	}
	return out, nil
}
