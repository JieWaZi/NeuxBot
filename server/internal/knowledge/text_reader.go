package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
)

// TextDocumentReader 直接提取可核对的文本层；模型在同次整理阶段读取文件，不回传全文副本。
type TextDocumentReader struct {
	// OnStarted 在已持久绑定的阅读入口通知宿主刷新；只表示开始，不代表读取成功。
	OnStarted func(context.Context, core.ReadRequest)
}

// Read 只把真实 PDF 页或 DOCX 段落转换为证据块，扫描和部分文本不能通过覆盖检查。
func (reader TextDocumentReader) Read(ctx context.Context, in core.ReadRequest) (core.ReadArtifact, error) {
	if err := ctx.Err(); err != nil {
		return core.ReadArtifact{}, err
	}
	if reader.OnStarted != nil {
		reader.OnStarted(ctx, in)
	}
	ext := strings.ToLower(filepath.Ext(in.Filename))
	if ext != ".pdf" && ext != ".docx" {
		return (core.MarkdownReader{}).Read(ctx, in)
	}
	artifact := core.ReadArtifact{SourceVersionID: in.SourceVersionID, SHA256: in.SHA256, Scope: in.Scope, Status: "unsupported"}
	sum := sha256.Sum256(in.Raw)
	if in.Scope != "all" || hex.EncodeToString(sum[:]) != in.SHA256 {
		return artifact, status_error.KnowledgeInvalid
	}
	units, complete, err := extractDocument(ctx, in.Raw, ext)
	if err != nil || !complete || len(units) == 0 {
		artifact.Diagnostic = "document has no complete verifiable text layer"
		artifact.Coverage = readerCoverage(ext, false, len(units), 0)
		return artifact, nil
	}
	runes := 0
	for _, u := range units {
		runes += utf8.RuneCountInString(u.Content)
	}
	if in.MaxRunes > 0 && runes > in.MaxRunes {
		artifact.Status = "partial"
		artifact.Diagnostic = "document exceeds the accepted reading budget"
		artifact.Coverage = readerCoverage(ext, false, len(units), 0)
		return artifact, nil
	}
	offset := 0
	for _, u := range units {
		artifact.Blocks = append(artifact.Blocks, &core.Chunk{ID: fmt.Sprintf("text-%d", u.Index), ChunkIndex: u.Index, StartAt: offset, Content: u.Content, ChunkType: "text", Locator: u.Locator})
		offset += len(u.Content)
	}
	artifact.Status = "complete"
	artifact.Coverage = readerCoverage(ext, true, len(units), len(units))
	return artifact, nil
}
