// Package profile 提供伙伴档案 Markdown 的解析、定向修改和 ZIP 编解码能力。
package profile

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

const maxProfileDocumentBytes = 1024 * 1024

// WorkMethod 表示面向用户展示的一套具名工作过程。
type WorkMethod struct {
	// Name 是工作过程的名称。
	Name string `json:"name"`
	// Steps 是按执行顺序排列的步骤。
	Steps []string `json:"steps"`
}

// Expertise 表示一项带说明的擅长领域。
type Expertise struct {
	// Title 是擅长领域的简短名称。
	Title string `json:"title"`
	// Description 是对能力范围和价值的具体说明。
	Description string `json:"description"`
}

// Projection 是从三份权威档案解析出的展示摘要。
type Projection struct {
	// Expertise 是按原始顺序展示的擅长领域。
	Expertise []Expertise `json:"expertise"`
	// WorkStyle 是按原始顺序展示的工作风格。
	WorkStyle []string `json:"workStyle"`
	// WorkMethods 是按原始顺序展示的具名工作过程。
	WorkMethods []WorkMethod `json:"workMethods"`
}

// DocumentsInput 保存三份标准档案的完整 Markdown 输入。
type DocumentsInput struct {
	// Identity 是身份与擅长领域文档；nil 表示更新时保留原文。
	Identity *string `json:"identity,omitempty"`
	// Persona 是工作风格文档；nil 表示更新时保留原文。
	Persona *string `json:"persona,omitempty"`
	// Playbook 是工作方式文档；nil 表示更新时保留原文。
	Playbook *string `json:"playbook,omitempty"`
}

// ManualInput 保存手动填写模式下的结构化摘要输入。
type ManualInput struct {
	// Expertise 是需要写入身份档案固定区域的擅长领域。
	Expertise []Expertise `json:"expertise"`
	// WorkStyle 是需要写入角色风格档案固定区域的工作风格。
	WorkStyle []string `json:"workStyle"`
	// WorkMethods 是需要写入工作方式档案固定区域的具名过程。
	WorkMethods []WorkMethod `json:"workMethods"`
}

// Document 表示一份可读取或编辑的标准档案。
type Document struct {
	// Kind 是文档的稳定种类。
	Kind constants.ProfileDocumentKind `json:"kind"`
	// FileName 是展示和导出时使用的固定文件名。
	FileName string `json:"fileName"`
	// Content 是完整 Markdown 文本。
	Content string `json:"content"`
	// Editable 表示当前版本是否允许用户修改该文档。
	Editable bool `json:"editable"`
	// UpdatedAt 是文档最近修改时的 Unix 毫秒时间。
	UpdatedAt int64 `json:"updatedAt"`
}

// ValidateContent 校验单份档案的编码和大小限制。
func ValidateContent(content string) error {
	if !utf8.ValidString(content) {
		return status_error.ProfileDocumentEncodingInvalid
	}
	if len([]byte(content)) > maxProfileDocumentBytes {
		return status_error.ProfileDocumentTooLarge
	}
	return nil
}

// ParseDocuments 从三份标准档案解析面向页面的展示投影。
func ParseDocuments(documents map[constants.ProfileDocumentKind]string) Projection {
	return Projection{
		Expertise: parseExpertise(
			documents[constants.ProfileDocumentKindIdentity],
		),
		WorkStyle: parseListSection(
			documents[constants.ProfileDocumentKindPersona],
			"## 工作风格",
		),
		WorkMethods: parseWorkMethods(
			documents[constants.ProfileDocumentKindPlaybook],
		),
	}
}

// ProjectionFromManual 直接把手动结构化输入规整为展示投影，供伙伴独立编辑
// 关于我时使用；它按与 ParseDocuments 相同的规则去除空白与空项，但不改写 MD 档案。
func ProjectionFromManual(manual ManualInput) Projection {
	projection := Projection{
		Expertise:   make([]Expertise, 0, len(manual.Expertise)),
		WorkStyle:   make([]string, 0, len(manual.WorkStyle)),
		WorkMethods: make([]WorkMethod, 0, len(manual.WorkMethods)),
	}
	for _, item := range manual.Expertise {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		projection.Expertise = append(projection.Expertise, Expertise{
			Title:       title,
			Description: strings.TrimSpace(item.Description),
		})
	}
	for _, style := range manual.WorkStyle {
		if normalized := strings.TrimSpace(style); normalized != "" {
			projection.WorkStyle = append(projection.WorkStyle, normalized)
		}
	}
	for _, method := range manual.WorkMethods {
		name := strings.TrimSpace(method.Name)
		if name == "" {
			continue
		}
		steps := make([]string, 0, len(method.Steps))
		for _, step := range method.Steps {
			if normalized := strings.TrimSpace(step); normalized != "" {
				steps = append(steps, normalized)
			}
		}
		projection.WorkMethods = append(projection.WorkMethods, WorkMethod{
			Name:  name,
			Steps: steps,
		})
	}
	return projection
}

func parseExpertise(content string) []Expertise {
	lines := normalizedLines(content)
	start, end, found := findSecondLevelSection(lines, "## 擅长领域")
	if !found {
		return []Expertise{}
	}
	items := make([]Expertise, 0)
	var current *Expertise
	for _, line := range lines[start+1 : end] {
		if strings.HasPrefix(line, "### ") {
			title := strings.TrimSpace(strings.TrimPrefix(line, "### "))
			if title == "" {
				current = nil
				continue
			}
			items = append(items, Expertise{Title: title})
			current = &items[len(items)-1]
			continue
		}
		if current == nil {
			continue
		}
		description := strings.TrimSpace(line)
		if description == "" {
			continue
		}
		if current.Description != "" {
			current.Description += "\n"
		}
		current.Description += description
	}
	return items
}

func parseListSection(content string, heading string) []string {
	lines := normalizedLines(content)
	start, end, found := findSecondLevelSection(lines, heading)
	if !found {
		return []string{}
	}
	items := make([]string, 0)
	for _, line := range lines[start+1 : end] {
		if item, ok := markdownListItem(line); ok {
			items = append(items, item)
		}
	}
	return items
}

func parseWorkMethods(content string) []WorkMethod {
	lines := normalizedLines(content)
	start, end, found := findSecondLevelSection(lines, "## 工作方式")
	if !found {
		return []WorkMethod{}
	}
	methods := make([]WorkMethod, 0)
	var current *WorkMethod
	for _, line := range lines[start+1 : end] {
		if strings.HasPrefix(line, "### ") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "### "))
			if name == "" {
				current = nil
				continue
			}
			methods = append(methods, WorkMethod{Name: name, Steps: []string{}})
			current = &methods[len(methods)-1]
			continue
		}
		if current == nil {
			continue
		}
		if item, ok := markdownListItem(line); ok {
			current.Steps = append(current.Steps, item)
		}
	}
	return methods
}

// PatchManual 只替换三份档案中的 NeuxBot 固定区域并保留其他内容。
func PatchManual(
	documents map[constants.ProfileDocumentKind]string,
	manual ManualInput,
) map[constants.ProfileDocumentKind]string {
	patched := cloneDocumentContents(documents)
	patched[constants.ProfileDocumentKindIdentity] = patchSecondLevelSection(
		patched[constants.ProfileDocumentKindIdentity],
		"## 擅长领域",
		renderExpertise(manual.Expertise),
	)
	patched[constants.ProfileDocumentKindPersona] = patchSecondLevelSection(
		patched[constants.ProfileDocumentKindPersona],
		"## 工作风格",
		renderUnorderedList(manual.WorkStyle),
	)
	patched[constants.ProfileDocumentKindPlaybook] = patchSecondLevelSection(
		patched[constants.ProfileDocumentKindPlaybook],
		"## 工作方式",
		renderWorkMethods(manual.WorkMethods),
	)
	return patched
}

func patchSecondLevelSection(content string, heading string, body string) string {
	lineBreak := "\n"
	if strings.Contains(content, "\r\n") {
		lineBreak = "\r\n"
	}
	lines := normalizedLines(content)
	start, end, found := findSecondLevelSection(lines, heading)
	replacement := []string{heading, ""}
	if body != "" {
		replacement = append(replacement, strings.Split(body, "\n")...)
	}
	if !found {
		for len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, replacement...)
		return strings.Join(lines, lineBreak)
	}
	lines = append(append(append([]string{}, lines[:start]...), replacement...), lines[end:]...)
	return strings.Join(lines, lineBreak)
}

func findSecondLevelSection(lines []string, heading string) (int, int, bool) {
	for index, line := range lines {
		if line != heading {
			continue
		}
		end := len(lines)
		for next := index + 1; next < len(lines); next++ {
			if strings.HasPrefix(lines[next], "## ") &&
				!strings.HasPrefix(lines[next], "### ") {
				end = next
				break
			}
		}
		return index, end, true
	}
	return 0, 0, false
}

func markdownListItem(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "- ") {
		item := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		return item, item != ""
	}
	dot := strings.Index(trimmed, ". ")
	if dot <= 0 {
		return "", false
	}
	for _, character := range trimmed[:dot] {
		if !unicode.IsDigit(character) {
			return "", false
		}
	}
	item := strings.TrimSpace(trimmed[dot+2:])
	return item, item != ""
}

func renderUnorderedList(items []string) string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		if normalized := strings.TrimSpace(item); normalized != "" {
			lines = append(lines, "- "+normalized)
		}
	}
	return strings.Join(lines, "\n")
}

func renderExpertise(items []Expertise) string {
	blocks := make([]string, 0, len(items))
	for _, item := range items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		lines := []string{"### " + title}
		if description := strings.TrimSpace(item.Description); description != "" {
			lines = append(lines, "", description)
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	return strings.Join(blocks, "\n\n")
}

func renderWorkMethods(methods []WorkMethod) string {
	blocks := make([]string, 0, len(methods))
	for _, method := range methods {
		name := strings.TrimSpace(method.Name)
		if name == "" {
			continue
		}
		lines := []string{"### " + name, ""}
		for index, step := range method.Steps {
			if normalized := strings.TrimSpace(step); normalized != "" {
				lines = append(lines, fmt.Sprintf("%d. %s", index+1, normalized))
			}
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	return strings.Join(blocks, "\n\n")
}

func normalizedLines(content string) []string {
	return strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
}

func cloneDocumentContents(
	documents map[constants.ProfileDocumentKind]string,
) map[constants.ProfileDocumentKind]string {
	cloned := make(map[constants.ProfileDocumentKind]string, len(documents))
	for kind, content := range documents {
		cloned[kind] = content
	}
	return cloned
}

// CreateZIP 按固定文件名和顺序创建不含记忆文档的档案 ZIP。
func CreateZIP(documents map[constants.ProfileDocumentKind]string) ([]byte, error) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, kind := range EditableKinds() {
		entry, err := writer.Create(FileName(kind))
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write([]byte(documents[kind])); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// EditableKinds 返回三份允许用户编辑和导出的伙伴档案种类。
func EditableKinds() []constants.ProfileDocumentKind {
	return []constants.ProfileDocumentKind{
		constants.ProfileDocumentKindIdentity,
		constants.ProfileDocumentKindPersona,
		constants.ProfileDocumentKindPlaybook,
	}
}

// FileName 返回文档种类对应的固定展示和导出文件名。
func FileName(kind constants.ProfileDocumentKind) string {
	name, _ := constants.ProfileDocumentFileName(kind)
	return name
}
