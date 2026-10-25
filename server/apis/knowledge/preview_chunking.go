package knowledge

import (
	"context"
	"errors"
	"strconv"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 文本分块特征
type PreviewDocumentProfile struct {
	// 全文字符数
	TotalChars int `json:"total_chars"`
	// 全文行数
	TotalLines int `json:"total_lines"`
	// 平均行长
	AvgLineLen float64 `json:"avg_line_len"`
	// 行长标准差
	StdLineLen float64 `json:"std_line_len"`
	// 标题层级数量
	MdHeadingCounts map[string]interface{} `json:"md_heading_counts"`
	// 标题总数
	MdHeadingTotal int `json:"md_heading_total"`
	// 编号章节数量
	NumberedSectionCount int `json:"numbered_section_count"`
	// 大写短行数量
	AllCapsShortLineCount int `json:"all_caps_short_line_count"`
	// 空行段落数量
	BlankParagraphBreaks int `json:"blank_paragraph_breaks"`
	// 换页数量
	FormFeedCount int `json:"form_feed_count"`
	// 分隔行数量
	VisualSepCount int `json:"visual_sep_count"`
	// 德文章节数量
	GermanChapterCount int `json:"german_chapter_count"`
	// 英文章节数量
	EnglishChapterCount int `json:"english_chapter_count"`
	// 中文章节数量
	ChineseChapterCount int `json:"chinese_chapter_count"`
	// 重复页脚数量
	RepeatedFooterCount int `json:"repeated_footer_count"`
	// 包含表格
	HasTables bool `json:"has_tables"`
	// 包含代码
	HasCode bool `json:"has_code"`
	// 代码字符比例
	CodeRatio float64 `json:"code_ratio"`
	// 语言提示
	DetectedLangs []string `json:"detected_langs"`
}

// 分块预览结果
type PreviewChunkingResult struct {
	// 实际策略
	SelectedTier string `json:"selected_tier"`
	// 尝试策略
	TierChain []string `json:"tier_chain"`
	// 策略诊断
	Rejected []PreviewTierRejection `json:"rejected"`
	// 文档特征
	Profile *PreviewDocumentProfile `json:"profile"`
	// 真实分块
	Chunks []core.PreviewChunkResult `json:"chunks"`
	// 完整统计
	Stats core.PreviewChunkingStats `json:"stats"`
}

// 策略拒绝原因
type PreviewTierRejection struct {
	// 分块策略
	Tier string `json:"tier"`
	// 拒绝原因
	Reason string `json:"reason"`
}

func previewChunkingDTO(result *core.PreviewChunkingResponse) PreviewChunkingResult {
	out := PreviewChunkingResult{SelectedTier: string(result.SelectedTier), Chunks: result.Chunks, Stats: result.Stats}
	for _, tier := range result.TierChain {
		out.TierChain = append(out.TierChain, string(tier))
	}
	for _, entry := range result.Rejected {
		out.Rejected = append(out.Rejected, PreviewTierRejection{Tier: string(entry.Tier), Reason: entry.Reason})
	}
	if p := result.Profile; p != nil {
		out.Profile = &PreviewDocumentProfile{
			TotalChars:            p.TotalChars,
			TotalLines:            p.TotalLines,
			AvgLineLen:            p.AvgLineLen,
			StdLineLen:            p.StdLineLen,
			MdHeadingTotal:        p.MdHeadingTotal,
			NumberedSectionCount:  p.NumberedSectionCount,
			AllCapsShortLineCount: p.AllCapsShortLineCount,
			BlankParagraphBreaks:  p.BlankParagraphBreaks,
			FormFeedCount:         p.FormFeedCount,
			VisualSepCount:        p.VisualSepCount,
			GermanChapterCount:    p.GermanChapterCount,
			EnglishChapterCount:   p.EnglishChapterCount,
			ChineseChapterCount:   p.ChineseChapterCount,
			RepeatedFooterCount:   p.RepeatedFooterCount,
			HasTables:             p.HasTables,
			HasCode:               p.HasCode,
			CodeRatio:             p.CodeRatio,
			DetectedLangs:         p.DetectedLangs,
			MdHeadingCounts:       map[string]interface{}{},
		}
		for level, count := range p.MdHeadingCounts {
			out.Profile.MdHeadingCounts[strconv.Itoa(level)] = count
		}
	}
	return out
}

// 预览文档分块
type PreviewChunking struct {
	// MethodPost 声明样例预览的请求方法。
	ginx.MethodPost
	// 分块样例
	Body core.PreviewChunkingRequest `in:"body"`
}

func (o *PreviewChunking) Path() string { return "/chunking/preview" }
func (o *PreviewChunking) Output(ctx *gin.Context) (interface{}, error) {
	result, err := core.PreviewDocumentChunking(ctx.Request.Context(), o.Body)
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, status_error.OperationTimeout
	}
	if errors.Is(err, context.Canceled) {
		return nil, status_error.OperationCanceled
	}
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return previewChunkingDTO(result), nil
}
