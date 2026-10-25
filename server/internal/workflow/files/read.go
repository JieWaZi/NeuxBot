package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// Preview 保存范围验证后读取的文件事实，不承担 HTTP 展示及编码。
type Preview struct {
	// RelativePath 是冻结范围内的真实相对路径。
	RelativePath string
	// MediaType 是已验证内容的 MIME 类型。
	MediaType string
	// Size 是实际读取的字节数。
	Size int64
	// Digest 是实际内容的 SHA-256 摘要。
	Digest string
	// Reason 是未通过范围、格式或预算校验的原因。
	Reason string
	// Text 表示内容可作为 UTF-8 文本展示。
	Text bool
	// Content 是预算内的真实文件字节。
	Content []byte
}

// ReadPreview 复用文件范围边界读取并验证有界内容，不加载任何执行状态。
func ReadPreview(ctx context.Context, directory, candidate string) (Preview, error) {
	result := Preview{}
	file, relative, reason := openScopeFile(directory, candidate)
	if reason != "" {
		result.Reason = reason
		return result, nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		result.Reason = "not_regular_file"
		return result, nil
	}
	mediaType, text, supported := previewType(filepath.Ext(relative))
	if !supported {
		result.Reason = "unsupported_type"
		return result, nil
	}
	limit := int64(10 << 20)
	if text {
		limit = 1 << 20
	}
	if info.Size() > limit {
		result.Reason = "preview_limit"
		return result, nil
	}
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		logx.Errorf("read workflow preview failed: path=%s error=%v", relative, err)
		return result, status_error.WorkflowFileStorageError
	}
	if ctx.Err() != nil {
		return result, status_error.OperationCanceled
	}
	if int64(len(content)) > limit {
		result.Reason = "preview_limit"
		return result, nil
	}
	if text && !utf8.Valid(content) {
		result.Reason = "text_encoding"
		return result, nil
	}
	if !text {
		if mediaType == "application/pdf" && !bytes.HasPrefix(content, []byte("%PDF-")) {
			result.Reason = "type_mismatch"
			return result, nil
		}
		if strings.HasPrefix(mediaType, "image/") && !previewImageMatches(mediaType, content) {
			result.Reason = "type_mismatch"
			return result, nil
		}
	}
	digest := sha256.Sum256(content)
	result.RelativePath = relative
	result.MediaType = mediaType
	result.Size = int64(len(content))
	result.Digest = hex.EncodeToString(digest[:])
	result.Text = text
	result.Content = content
	return result, nil
}

func previewType(extension string) (string, bool, bool) {
	switch strings.ToLower(extension) {
	case ".txt", ".md", ".markdown", ".json", ".csv", ".log", ".js", ".ts", ".tsx", ".jsx", ".py", ".go", ".yaml", ".yml", ".html", ".css", ".xml", ".sql", ".sh":
		return "text/plain; charset=utf-8", true, true
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return mime.TypeByExtension(strings.ToLower(extension)), false, true
	case ".pdf":
		return "application/pdf", false, true
	default:
		return "", false, false
	}
}

func previewImageMatches(mediaType string, content []byte) bool {
	if mediaType != "image/webp" {
		config, format, err := image.DecodeConfig(bytes.NewReader(content))
		if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 32000000 {
			return false
		}
		return mediaType == "image/"+format
	}
	switch mediaType {
	case "image/webp":
		if len(content) < 20 || string(content[:4]) != "RIFF" || string(content[8:12]) != "WEBP" || uint64(binary.LittleEndian.Uint32(content[4:8]))+8 != uint64(len(content)) {
			return false
		}
		// WebP 必须包含完整有界的图像块，只有 RIFF 魔数的截断文件不能作为图片。
		for offset := 12; offset+8 <= len(content); {
			size := uint64(binary.LittleEndian.Uint32(content[offset+4 : offset+8]))
			end := uint64(offset) + 8 + size
			if end > uint64(len(content)) {
				return false
			}
			kind := string(content[offset : offset+4])
			if (kind == "VP8 " && size >= 10) || (kind == "VP8L" && size >= 5) || (kind == "ANMF" && size >= 16) {
				return true
			}
			offset = int(end + size%2)
		}
		return false
	default:
		return false
	}
}

// OpenScopeFile 通过目录句柄打开冻结范围内的普通文件，检查真实路径并阻止软链接逃逸。
func OpenScopeFile(directory, candidate string) (*os.File, error) {
	file, _, reason := openScopeFile(directory, candidate)
	if reason != "" {
		if reason == "path_invalid" {
			return nil, status_error.WorkflowFileInvalid
		}
		return nil, status_error.WorkflowFileNotFound
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, status_error.WorkflowFileNotFound
	}
	if info.Size() > constants.WorkflowFileMaximumBytes {
		file.Close()
		return nil, status_error.WorkflowBudgetExceeded.WithField("budget", "file_bytes")
	}
	return file, nil
}

func openScopeFile(directory, candidate string) (*os.File, string, string) {
	if candidate == "" || len(candidate) > 4096 || strings.ContainsRune(candidate, 0) {
		return nil, "", "path_invalid"
	}
	resolvedRoot, err := filepath.EvalSymlinks(directory)
	if directory == "" || err != nil {
		return nil, "", "scope_unavailable"
	}
	if filepath.Clean(resolvedRoot) != filepath.Clean(directory) {
		return nil, "", "scope_changed"
	}
	target := candidate
	if !filepath.IsAbs(target) {
		target = filepath.Join(directory, target)
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return nil, "", "missing"
	}
	relative, err := filepath.Rel(directory, resolved)
	if err != nil || !filepath.IsLocal(relative) {
		return nil, "", "outside_scope"
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, "", "scope_unavailable"
	}
	defer root.Close()
	file, err := root.Open(relative)
	if err != nil {
		return nil, "", "missing_or_scope_changed"
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, "", "not_regular_file"
	}
	return file, filepath.ToSlash(relative), ""
}
