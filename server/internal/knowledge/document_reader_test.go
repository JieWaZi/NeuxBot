package knowledge

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/JieWaZi/neux-knowledge/core"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentTextExtractionAndCandidateCoverage(t *testing.T) {
	for _, tc := range []struct {
		// ext 是实际格式。
		ext string
		// raw 是原始文档字节。
		raw []byte
		// want 是必须保留的原文。
		want string
	}{
		{".pdf", testPDF("Every session expires."), "Every session expires."},
		{".docx", testDOCX(t, "Revocation takes effect immediately."), "Revocation takes effect immediately."},
	} {
		t.Run(tc.ext, func(t *testing.T) {
			units, complete, err := extractDocument(context.Background(), tc.raw, tc.ext)
			if err != nil || !complete || len(units) != 1 || !strings.Contains(units[0].Content, tc.want) {
				t.Fatalf("units=%+v complete=%v err=%v", units, complete, err)
			}
			sum := sha256.Sum256(tc.raw)
			artifact, err := (TextDocumentReader{}).Read(context.Background(), core.ReadRequest{SourceVersionID: "source", Filename: "source" + tc.ext, SHA256: hex.EncodeToString(sum[:]), Raw: tc.raw, Scope: "all"})
			if err != nil || artifact.Status != "complete" || len(artifact.Blocks) != 1 || !strings.Contains(artifact.Blocks[0].Content, tc.want) {
				t.Fatalf("artifact=%+v err=%v", artifact, err)
			}

		})
	}
}

func testDOCX(t *testing.T, text string) []byte {
	t.Helper()
	var b bytes.Buffer
	archive := zip.NewWriter(&b)
	part, err := archive.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(part, `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>%s</w:t></w:r></w:p></w:body></w:document>`, text)
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func testPDF(text string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"}
	stream := "BT /F1 12 Tf 72 720 Td (" + text + ") Tj ET"
	objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	for i, obj := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return b.Bytes()
}

func TestAcceptancePDFAndDOCXFixturesPreserveTextUnits(t *testing.T) {
	for _, tc := range []struct {
		// file 是验收样例文件。
		file string
		// ext 是样例格式。
		ext string
		// required 是实际样例原文。
		required []string
	}{
		{"revocation.pdf", ".pdf", []string{"Session revocation", "online access stops immediately", "cannot verify server revocation until reconnection", "Reconnection must check revocation"}},
		{"renewal.docx", ".docx", []string{"Session renewal policy", "Online sessions expire after 2 hours", "Offline sessions may retain local cached access for 24 hours", "Automatic renewal is not supported"}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			units, complete, err := extractDocument(context.Background(), raw, tc.ext)
			if err != nil || !complete {
				t.Fatalf("complete=%v err=%v", complete, err)
			}
			all := ""
			for _, unit := range units {
				all += unit.Content
			}
			for _, required := range tc.required {
				if !strings.Contains(all, required) {
					t.Fatalf("missing actual source text %q in %q", required, all)
				}
			}
		})
	}
}

func TestTextDocumentReaderStartedNotificationDoesNotDeclareCompletion(t *testing.T) {
	for _, test := range []struct {
		// name 是独立的阅读边界场景。
		name string
		// filename 选择实际文本或解析失败路径。
		filename string
		// cancelled 表示在阅读开始前已取消。
		cancelled bool
	}{{name: "complete markdown", filename: "source.md"}, {name: "failed PDF", filename: "source.pdf"}, {name: "cancelled before reading", filename: "source.md", cancelled: true}} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.cancelled {
				cancel()
			}
			raw := []byte("# actual source\nBody")
			sum := sha256.Sum256(raw)
			started := []string{}
			reader := TextDocumentReader{OnStarted: func(_ context.Context, request core.ReadRequest) { started = append(started, request.SourceVersionID) }}
			result, err := reader.Read(ctx, core.ReadRequest{SourceVersionID: "source", Filename: test.filename, Raw: raw, SHA256: hex.EncodeToString(sum[:]), Scope: "all"})
			if test.cancelled {
				if err == nil || len(started) != 0 || result.Status == "complete" {
					t.Fatalf("cancelled reading result=%+v started=%v error=%v", result, started, err)
				}
				return
			}
			if len(started) != 1 || started[0] != "source" {
				t.Fatalf("reading start notification=%v", started)
			}
			if test.filename == "source.pdf" && result.Status == "complete" {
				t.Fatal("failed read start was mistaken for completion")
			}
		})
	}
}
