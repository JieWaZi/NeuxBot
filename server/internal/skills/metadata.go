package skills

import (
	"bytes"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"gopkg.in/yaml.v3"
)

const maxSkillDocumentBytes = 1024 * 1024

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Metadata 保存标准 SKILL.md Frontmatter 中由 Runtime 识别的字段。
type Metadata struct {
	// Name 是 Runtime 发现和显式调用技能时使用的稳定声明名。
	Name string `yaml:"name"`
	// Description 是 Runtime 判断技能适用时机所需的说明。
	Description string `yaml:"description"`
}

// LearnedDraft 保存新建文档或针对已有版本的局部修订。
type LearnedDraft struct {
	// Markdown 是新建技能时的完整 SKILL.md。
	Markdown string `json:"markdown,omitempty"`
	// Revision 是修订技能时基于原始文档的变更。
	Revision *LearnedRevision `json:"revision,omitempty"`
}

// LearnedRevision 描述模型基于已验证经验建议的局部修订。
type LearnedRevision struct {
	// Description 仅在适用范围发生变化时替换原描述。
	Description string `json:"description,omitempty"`
	// Edits 使用原始正文中的唯一文本片段定位修改。
	Edits []LearnedEdit `json:"edits"`
}

// LearnedEdit 将原正文中一个明确片段替换为修订内容。
type LearnedEdit struct {
	// Before 是原始正文中唯一且非空的精确文本。
	Before string `json:"before"`
	// After 是保留原有语言的替换内容，可添加或移除局部说明。
	After *string `json:"after"`
}

// ReadMetadata 解析并校验技能包入口文件中的标准 Frontmatter。
func ReadMetadata(directory string) (Metadata, error) {
	content, err := os.ReadFile(filepath.Join(directory, "SKILL.md"))
	if err != nil {
		return Metadata{}, status_error.SkillUnavailable
	}
	frontmatter, _, err := splitMarkdown(content)
	if err != nil {
		return Metadata{}, err
	}
	return readMetadata(frontmatter)
}

// ReadVersionMarkdown 从受管版本目录有界读取标准文本，不跟随目录外的链接。
func ReadVersionMarkdown(directory string) ([]byte, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		logx.Errorf("open learned skill version directory: error=%v", err)
		return nil, status_error.SkillStorageError
	}
	defer root.Close()
	info, err := root.Lstat("SKILL.md")
	if err != nil {
		logx.Errorf("stat learned skill version document: error=%v", err)
		return nil, status_error.SkillStorageError
	}
	if !info.Mode().IsRegular() {
		return nil, status_error.SkillContentInvalid
	}
	file, err := root.Open("SKILL.md")
	if err != nil {
		logx.Errorf("open learned skill version document: error=%v", err)
		return nil, status_error.SkillStorageError
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxSkillDocumentBytes+1))
	if err != nil {
		logx.Errorf("read learned skill version document: error=%v", err)
		return nil, status_error.SkillStorageError
	}
	if len(content) > maxSkillDocumentBytes {
		return nil, status_error.SkillContentLimitExceeded
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return nil, status_error.SkillContentInvalid
	}
	return content, nil
}

// PrepareLearnedMarkdown 校验新建文档或将局部修订应用于不可变原文。
func PrepareLearnedMarkdown(original []byte, draft LearnedDraft, expectedName string) ([]byte, Metadata, error) {
	if original != nil {
		if draft.Markdown != "" || draft.Revision == nil {
			return nil, Metadata{}, status_error.SkillContentInvalid
		}
		return reviseLearnedMarkdown(original, *draft.Revision, expectedName)
	}
	if draft.Revision != nil || !validSkillText(draft.Markdown) {
		return nil, Metadata{}, status_error.SkillContentInvalid
	}
	frontmatter, body, err := splitMarkdown([]byte(draft.Markdown))
	if err != nil {
		return nil, Metadata{}, err
	}
	metadata, err := readMetadata(frontmatter)
	if err != nil {
		return nil, Metadata{}, err
	}
	if metadata.Name != expectedName || len(bytes.TrimSpace(body)) == 0 {
		return nil, Metadata{}, status_error.SkillContentInvalid
	}
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	setMetadataScalar(mapping, "name", metadata.Name)
	setMetadataScalar(mapping, "description", metadata.Description)
	return renderLearnedMarkdown(mapping, body, metadata)
}

// PrepareManagedMarkdown 准备用于聊天显式发布的完整技能文档，保留已有来源的其他元数据。
func PrepareManagedMarkdown(original []byte, replacement, expectedName, expectedDescription string) ([]byte, Metadata, error) {
	if original == nil {
		content, metadata, err := PrepareLearnedMarkdown(nil, LearnedDraft{Markdown: replacement}, expectedName)
		if err == nil && expectedDescription != "" && metadata.Description != expectedDescription {
			return nil, Metadata{}, status_error.SkillContentInvalid
		}
		return content, metadata, err
	}
	if !validSkillText(replacement) {
		return nil, Metadata{}, status_error.SkillContentInvalid
	}
	inputFrontmatter, body, err := splitMarkdown([]byte(replacement))
	if err != nil {
		return nil, Metadata{}, err
	}
	metadata, err := readMetadata(inputFrontmatter)
	if err != nil || metadata.Name != expectedName || len(bytes.TrimSpace(body)) == 0 {
		return nil, Metadata{}, status_error.SkillContentInvalid
	}
	originalFrontmatter, originalBody, err := splitMarkdown(original)
	if err != nil {
		return nil, Metadata{}, err
	}
	prior, err := readMetadata(originalFrontmatter)
	if err != nil || prior.Name != expectedName {
		return nil, Metadata{}, status_error.SkillContentInvalid
	}
	return composeSkillRevision(original, originalFrontmatter, originalBody, body, prior, metadata.Description)
}

func reviseLearnedMarkdown(original []byte, revision LearnedRevision, expectedName string) ([]byte, Metadata, error) {
	frontmatter, body, err := splitMarkdown(original)
	if err != nil {
		return nil, Metadata{}, err
	}
	metadata, err := readMetadata(frontmatter)
	if err != nil || metadata.Name != expectedName || !validSkillText(revision.Description) {
		return nil, Metadata{}, status_error.SkillContentInvalid
	}
	updated, err := applyLearnedEdits(body, revision.Edits)
	if err != nil {
		return nil, Metadata{}, err
	}
	return composeSkillRevision(original, frontmatter, body, updated, metadata, strings.TrimSpace(revision.Description))
}

func composeSkillRevision(original, frontmatter, body, updated []byte, metadata Metadata, description string) ([]byte, Metadata, error) {
	if description == "" || description == metadata.Description {
		if bytes.Equal(body, updated) {
			return nil, Metadata{}, status_error.SkillContentInvalid
		}
		prefix := original[:len(original)-len(body)]
		if len(prefix)+len(updated) > maxSkillDocumentBytes {
			return nil, Metadata{}, status_error.SkillContentLimitExceeded
		}
		return bytes.Join([][]byte{prefix, updated}, nil), metadata, nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal(frontmatter, &document); err != nil || len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return nil, Metadata{}, status_error.SkillContentInvalid
	}
	metadata.Description = description
	mapping := document.Content[0]
	setMetadataScalar(mapping, "description", description)
	return renderLearnedMarkdown(mapping, updated, metadata)
}

func applyLearnedEdits(original []byte, edits []LearnedEdit) ([]byte, error) {
	type occurrence struct {
		// start 标识原正文中唯一匹配的起始偏移。
		start int
		// edit 保存该位置的精确替换内容。
		edit LearnedEdit
	}
	body := string(original)
	locations := make([]occurrence, 0, len(edits))
	changed := 0
	for _, edit := range edits {
		if strings.TrimSpace(edit.Before) == "" || !validSkillText(edit.Before) || edit.After == nil || !validSkillText(*edit.After) {
			return nil, status_error.SkillContentInvalid
		}
		start := strings.Index(body, edit.Before)
		if start < 0 || strings.Contains(body[start+1:], edit.Before) {
			return nil, status_error.SkillContentInvalid
		}
		if !strings.Contains(*edit.After, edit.Before) {
			changed += utf8.RuneCountInString(edit.Before)
		}
		locations = append(locations, occurrence{start, edit})
	}
	// 原文的大部分内容必须原样保留；较大的重写由用户通过技能编辑完成。
	if changed > utf8.RuneCountInString(strings.TrimSpace(body))/4 {
		return nil, status_error.SkillContentInvalid
	}
	slices.SortFunc(locations, func(a, b occurrence) int { return a.start - b.start })
	var updated bytes.Buffer
	end := 0
	for _, location := range locations {
		if location.start < end {
			return nil, status_error.SkillContentInvalid
		}
		updated.Write(original[end:location.start])
		updated.WriteString(*location.edit.After)
		end = location.start + len(location.edit.Before)
		if updated.Len() > maxSkillDocumentBytes {
			return nil, status_error.SkillContentLimitExceeded
		}
	}
	updated.Write(original[end:])
	if len(bytes.TrimSpace(updated.Bytes())) == 0 {
		return nil, status_error.SkillContentInvalid
	}
	return updated.Bytes(), nil
}

func validSkillText(text string) bool {
	return utf8.ValidString(text) && !strings.ContainsRune(text, 0)
}

func renderLearnedMarkdown(mapping *yaml.Node, body []byte, metadata Metadata) ([]byte, Metadata, error) {
	encoded, err := yaml.Marshal(mapping)
	if err != nil {
		return nil, Metadata{}, status_error.SkillContentInvalid
	}
	markdown := bytes.Join([][]byte{[]byte("---\n"), encoded, []byte("---\n"), body}, nil)
	if len(markdown) > maxSkillDocumentBytes {
		return nil, Metadata{}, status_error.SkillContentLimitExceeded
	}
	return markdown, metadata, nil
}

func splitMarkdown(content []byte) ([]byte, []byte, error) {
	if len(content) > maxSkillDocumentBytes {
		return nil, nil, status_error.SkillContentLimitExceeded
	}
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	lines := bytes.Split(content, []byte("\n"))
	if len(lines) < 3 || strings.TrimSpace(string(lines[0])) != "---" {
		return nil, nil, status_error.SkillContentInvalid
	}
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(string(lines[index])) == "---" {
			return bytes.Join(lines[1:index], []byte("\n")), bytes.Join(lines[index+1:], []byte("\n")), nil
		}
	}
	return nil, nil, status_error.SkillContentInvalid
}

func readMetadata(frontmatter []byte) (Metadata, error) {
	var metadata Metadata
	if err := yaml.Unmarshal(frontmatter, &metadata); err != nil {
		return Metadata{}, status_error.SkillContentInvalid
	}
	metadata.Name = strings.TrimSpace(metadata.Name)
	metadata.Description = strings.TrimSpace(metadata.Description)
	if err := validateMetadata(metadata); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

// NextPatchVersion 在技能库使用的数字版本中递增修订段。
func NextPatchVersion(value string) (string, bool) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(value), "v"), ".")
	if len(parts) != 3 {
		return "", false
	}
	numbers := make([]*big.Int, len(parts))
	for index, part := range parts {
		number, ok := new(big.Int).SetString(part, 10)
		if !ok || number.Sign() < 0 {
			return "", false
		}
		numbers[index] = number
	}
	numbers[2].Add(numbers[2], big.NewInt(1))
	return numbers[0].String() + "." + numbers[1].String() + "." + numbers[2].String(), true
}

// UpdateMetadata 结构化更新 SKILL.md 的名称和描述，同时保留其他 Frontmatter 字段与正文。
func UpdateMetadata(directory string, metadata Metadata) error {
	metadata.Name = strings.TrimSpace(metadata.Name)
	metadata.Description = strings.TrimSpace(metadata.Description)
	if err := validateMetadata(metadata); err != nil {
		return err
	}
	path := filepath.Join(directory, "SKILL.md")
	content, err := os.ReadFile(path)
	if err != nil {
		return status_error.SkillUnavailable
	}
	if len(content) > maxSkillDocumentBytes {
		return status_error.SkillContentLimitExceeded
	}
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	lines := bytes.Split(content, []byte("\n"))
	if len(lines) < 3 || strings.TrimSpace(string(lines[0])) != "---" {
		return status_error.SkillContentInvalid
	}
	closing := -1
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(string(lines[index])) == "---" {
			closing = index
			break
		}
	}
	if closing < 0 {
		return status_error.SkillContentInvalid
	}
	var document yaml.Node
	if err := yaml.Unmarshal(bytes.Join(lines[1:closing], []byte("\n")), &document); err != nil {
		return status_error.SkillContentInvalid
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return status_error.SkillContentInvalid
	}
	setMetadataScalar(document.Content[0], "name", metadata.Name)
	setMetadataScalar(document.Content[0], "description", metadata.Description)
	frontmatter, err := yaml.Marshal(&document)
	if err != nil {
		return status_error.SkillUnavailable
	}
	updated := make([]byte, 0, len(content)+64)
	updated = append(updated, []byte("---\n")...)
	updated = append(updated, frontmatter...)
	updated = append(updated, []byte("---")...)
	if closing+1 < len(lines) {
		updated = append(updated, '\n')
		updated = append(updated, bytes.Join(lines[closing+1:], []byte("\n"))...)
	}
	if len(updated) > maxSkillDocumentBytes {
		return status_error.SkillContentLimitExceeded
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		return status_error.SkillStorageError
	}
	return nil
}

func validateMetadata(metadata Metadata) error {
	if len(metadata.Name) > 64 || !skillNamePattern.MatchString(metadata.Name) {
		return status_error.SkillContentInvalid
	}
	if metadata.Description == "" {
		return status_error.SkillContentInvalid
	}
	return nil
}

func setMetadataScalar(mapping *yaml.Node, key string, value string) {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value != key {
			continue
		}
		mapping.Content[index+1].Kind = yaml.ScalarNode
		mapping.Content[index+1].Tag = "!!str"
		mapping.Content[index+1].Value = value
		return
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}
