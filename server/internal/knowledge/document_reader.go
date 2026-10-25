package knowledge

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/JieWaZi/neux-knowledge/core"
	pdf "github.com/ledongthuc/pdf"
	"io"
	"sort"
	"strings"
)

// documentUnit 保留可核对的文本单元和实际位置。
type documentUnit struct {
	// Index 是稳定阅读顺序。
	Index int `json:"index"`
	// Content 是原文提取器读到的完整文本。
	Content string `json:"content"`
	// Locator 只保存实际页码或XML段落位置。
	Locator core.SourceLocator `json:"locator"`
}

func readerCoverage(kind string, complete bool, total, read int) json.RawMessage {
	out, _ := json.Marshal(map[string]any{"reader": "ally-extracted-text-v1", "format": kind, "scope": "all", "complete": complete, "totalUnits": total, "readUnits": read})
	return out
}

// extractDocument 只核对文本层；扫描、解析错误和未覆盖内容不能升级完整。
func extractDocument(ctx context.Context, raw []byte, ext string) (units []documentUnit, complete bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			units = nil
			complete = false
			err = fmt.Errorf("文档文本解析失败")
		}
	}()
	if len(raw) > 32<<20 {
		return nil, false, fmt.Errorf("文档超过原件预算")
	}
	if ext == ".pdf" {
		reader, err := pdf.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			return nil, false, err
		}
		pages := reader.NumPage()
		if pages < 1 || pages > 1000 {
			return nil, false, fmt.Errorf("PDF页数超出可核对范围")
		}
		complete = true
		for page := 1; page <= pages; page++ {
			if err := ctx.Err(); err != nil {
				return nil, false, err
			}
			text, err := reader.Page(page).GetPlainText(nil)
			if err != nil {
				return nil, false, err
			}
			if strings.TrimSpace(text) == "" {
				complete = false
				continue
			}
			units = append(units, documentUnit{Index: len(units), Content: text, Locator: core.SourceLocator{Kind: "page", Page: page}})
		}
		return units, complete, nil
	}
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, false, err
	}
	parts := []*zip.File{}
	for _, file := range archive.File {
		if file.Name == "word/document.xml" || strings.HasPrefix(file.Name, "word/header") || strings.HasPrefix(file.Name, "word/footer") || file.Name == "word/footnotes.xml" || file.Name == "word/endnotes.xml" {
			parts = append(parts, file)
		}
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Name < parts[j].Name })
	if len(parts) == 0 {
		return nil, false, fmt.Errorf("DOCX缺少正文")
	}
	complete = true
	for _, part := range parts {
		if part.UncompressedSize64 > 16<<20 {
			return nil, false, fmt.Errorf("DOCX文本部件超出预算")
		}
		handle, err := part.Open()
		if err != nil {
			return nil, false, err
		}
		data, err := io.ReadAll(io.LimitReader(handle, 16<<20+1))
		handle.Close()
		if err != nil || len(data) > 16<<20 {
			return nil, false, fmt.Errorf("DOCX正文读取失败")
		}
		decoder := xml.NewDecoder(bytes.NewReader(data))
		var text strings.Builder
		paragraph := 0
		inText := false
		for {
			if err := ctx.Err(); err != nil {
				return nil, false, err
			}
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, false, err
			}
			switch token := token.(type) {
			case xml.StartElement:
				switch token.Name.Local {
				case "p":
					text.Reset()
					paragraph++
				case "t":
					inText = true
				case "tab":
					text.WriteString("\t")
				case "br":
					text.WriteString("\n")
				case "drawing", "pict", "altChunk":
					complete = false
				}
			case xml.EndElement:
				if token.Name.Local == "t" {
					inText = false
				}
				if token.Name.Local == "p" && strings.TrimSpace(text.String()) != "" {
					units = append(units, documentUnit{Index: len(units), Content: text.String(), Locator: core.SourceLocator{Kind: "section", Section: fmt.Sprintf("%s#paragraph-%d", part.Name, paragraph)}})
				}
			case xml.CharData:
				if inText {
					text.Write(token)
				}
			}
		}
	}
	return units, complete, nil
}
