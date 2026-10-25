package skills

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
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

// ReadMetadata 解析并校验技能包入口文件中的标准 Frontmatter。
func ReadMetadata(directory string) (Metadata, error) {
	content, err := os.ReadFile(filepath.Join(directory, "SKILL.md"))
	if err != nil {
		return Metadata{}, status_error.SkillUnavailable
	}
	if len(content) > maxSkillDocumentBytes {
		return Metadata{}, status_error.SkillContentLimitExceeded
	}
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	lines := bytes.Split(content, []byte("\n"))
	if len(lines) < 3 || strings.TrimSpace(string(lines[0])) != "---" {
		return Metadata{}, status_error.SkillContentInvalid
	}
	closing := -1
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(string(lines[index])) == "---" {
			closing = index
			break
		}
	}
	if closing < 0 {
		return Metadata{}, status_error.SkillContentInvalid
	}
	var metadata Metadata
	if err := yaml.Unmarshal(bytes.Join(lines[1:closing], []byte("\n")), &metadata); err != nil {
		return Metadata{}, status_error.SkillContentInvalid
	}
	metadata.Name = strings.TrimSpace(metadata.Name)
	metadata.Description = strings.TrimSpace(metadata.Description)
	if err := validateMetadata(metadata); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
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
