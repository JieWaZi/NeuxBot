package profile

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func TestPatchManualPreservesOtherSectionsAndParsesProjection(t *testing.T) {
	documents := map[constants.ProfileDocumentKind]string{
		constants.ProfileDocumentKindIdentity: "# 身份\n\n保留说明\n\n## 擅长领域\n\n### 旧能力\n\n旧说明\n\n## 边界\n\n保持不变",
		constants.ProfileDocumentKindPersona:  "# 风格\n",
		constants.ProfileDocumentKindPlaybook: "# 方法\n",
	}
	patched := PatchManual(documents, ManualInput{
		Expertise: []Expertise{
			{Title: "需求分析", Description: "识别目标与约束"},
			{Title: "结果验证", Description: "用证据验证交付结果"},
		},
		WorkStyle: []string{"证据优先"},
		WorkMethods: []WorkMethod{
			{Name: "交付任务", Steps: []string{"理解目标", "执行验证"}},
		},
	})
	if !bytes.Contains(
		[]byte(patched[constants.ProfileDocumentKindIdentity]),
		[]byte("## 边界\n\n保持不变"),
	) {
		t.Fatal("manual patch removed an unrelated section")
	}
	projection := ParseDocuments(patched)
	if len(projection.Expertise) != 2 ||
		projection.Expertise[0].Title != "需求分析" ||
		projection.Expertise[0].Description != "识别目标与约束" {
		t.Fatalf("unexpected expertise: %+v", projection.Expertise)
	}
	if len(projection.WorkStyle) != 1 || projection.WorkStyle[0] != "证据优先" {
		t.Fatalf("unexpected work style: %+v", projection.WorkStyle)
	}
	if len(projection.WorkMethods) != 1 ||
		len(projection.WorkMethods[0].Steps) != 2 {
		t.Fatalf("unexpected work methods: %+v", projection.WorkMethods)
	}
}

func TestCreateZIPContainsOnlyThreeProfileDocuments(t *testing.T) {
	content, err := CreateZIP(map[constants.ProfileDocumentKind]string{
		constants.ProfileDocumentKindIdentity: "identity",
		constants.ProfileDocumentKindPersona:  "persona",
		constants.ProfileDocumentKindPlaybook: "playbook",
		constants.ProfileDocumentKindMemory:   "must not export",
	})
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	wantNames := []string{"IDENTITY.md", "PERSONA.md", "PLAYBOOK.md"}
	if len(reader.File) != len(wantNames) {
		t.Fatalf("zip entries = %d, want %d", len(reader.File), len(wantNames))
	}
	for index, file := range reader.File {
		if file.Name != wantNames[index] {
			t.Fatalf("entry %d = %q, want %q", index, file.Name, wantNames[index])
		}
		entry, openErr := file.Open()
		if openErr != nil {
			t.Fatalf("open entry: %v", openErr)
		}
		_, readErr := io.ReadAll(entry)
		closeErr := entry.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read entry: read=%v close=%v", readErr, closeErr)
		}
	}
}
