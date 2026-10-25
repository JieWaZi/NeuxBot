package dsl

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/dop251/goja/ast"
)

// SourceEdit 把用户表单修改定位到权威源码，不建立第二份可执行图数据。
type SourceEdit struct {
	// Target 区分基本信息、阶段和节点表单。
	Target string `json:"target"`
	// ID 是待修改阶段或节点的稳定身份，基本信息不需要身份。
	ID string `json:"id,omitempty"`
	// Fields 是表单实际修改的字段，未提交的表达式及注释保持原样。
	Fields map[string]any `json:"fields"`
	// Prompt 是用户修改的提示词模板内容，保留其中的变量插值。
	Prompt *string `json:"prompt,omitempty"`
}

// sourceReplacement 是原始 UTF-8 源码中的一处半开范围替换。
type sourceReplacement struct {
	// start 是被替换片段的起始字节。
	start int
	// end 是被替换片段之后的字节。
	end int
	// text 是经过 JSON 编码或模板转义的新值。
	text string
}

// EditSource 通过 AST 定位配置，仅替换表单触及的值；保存仍须经过完整编译及 CAS。
func EditSource(source string, edit SourceEdit) (string, error) {
	compiled, _, err := Compile(source)
	if err != nil {
		return "", err
	}
	parsed, _, err := parseSource(source)
	if err != nil {
		return "", err
	}
	var object *ast.ObjectLiteral
	var call *ast.CallExpression
	allowed := map[string]bool{}
	switch edit.Target {
	case "meta", "phase":
		for _, statement := range parsed.body {
			if declaration, ok := statement.(*ast.LexicalDeclaration); ok {
				for _, binding := range declaration.List {
					if id, ok := binding.Target.(*ast.Identifier); ok && id.Name.String() == "meta" {
						object, _ = binding.Initializer.(*ast.ObjectLiteral)
					}
				}
			}
		}
		if edit.Target == "meta" {
			for _, key := range []string{"title", "description", "whenToUse", "inputSchema", "outputSchema"} {
				allowed[key] = true
			}
		} else {
			phases, _ := editProperty(object, "phases").(*ast.ArrayLiteral)
			object = nil
			if phases != nil {
				for _, item := range phases.Value {
					candidate, ok := item.(*ast.ObjectLiteral)
					if !ok {
						continue
					}
					id, ok := editProperty(candidate, "id").(*ast.StringLiteral)
					if ok && id.Value.String() == edit.ID {
						object = candidate
						break
					}
				}
			}
			for _, key := range []string{"title", "detail", "next", "canExit"} {
				allowed[key] = true
			}
		}
	case "node":
		var selected *Node
		for _, node := range compiled.Manifest.Nodes {
			if node.ID == edit.ID {
				selected = &node
				break
			}
		}
		if selected == nil {
			return "", status_error.BadRequest
		}
		astContains(reflect.ValueOf(parsed.body), func(node ast.Node) bool {
			candidate, ok := node.(*ast.CallExpression)
			if ok && parsed.sourceRange(candidate).Start.Offset == selected.Range.Start.Offset {
				call = candidate
				return true
			}
			return false
		})
		index := 1
		if selected.Kind == "functionTask" {
			index = 0
		}
		if call != nil && len(call.ArgumentList) > index {
			object, _ = call.ArgumentList[index].(*ast.ObjectLiteral)
		}
		for _, key := range []string{"label", "timeoutMs", "inputs", "outputs"} {
			allowed[key] = true
		}
		switch selected.Kind {
		case "worker":
			for _, key := range []string{"resolve", "toolPolicy", "schema", "skippable", "retry"} {
				allowed[key] = true
			}
		case "askUser":
			for _, key := range []string{"options", "multiSelect", "defaultValue", "allowSkip", "schema"} {
				allowed[key] = true
			}
		case "functionTask":
			for _, key := range []string{"code", "language", "executionMode", "outputSchema"} {
				allowed[key] = true
			}
		}
		if edit.Prompt != nil && selected.Kind != "worker" && selected.Kind != "askUser" {
			return "", status_error.BadRequest
		}
	default:
		return "", status_error.BadRequest
	}
	if object == nil || (edit.Prompt != nil && call == nil) {
		return "", status_error.BadRequest
	}
	replacements := []sourceReplacement{}
	keys := make([]string, 0, len(edit.Fields))
	for key := range edit.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	missing := []string{}
	for _, key := range keys {
		if !allowed[key] {
			return "", status_error.BadRequest
		}
		encoded, err := json.Marshal(edit.Fields[key])
		if err != nil {
			return "", status_error.BadRequest
		}
		if value := editProperty(object, key); value != nil {
			r := parsed.sourceRange(value)
			replacements = append(replacements, sourceReplacement{start: r.Start.Offset, end: r.End.Offset, text: string(encoded)})
		} else {
			missing = append(missing, key+": "+string(encoded))
		}
	}
	if len(missing) > 0 {
		r := parsed.sourceRange(object)
		// 插入在现有属性前，保留对象末尾注释与尾逗号。
		text := strings.Join(missing, ", ")
		if len(object.Value) > 0 {
			text += ", "
		}
		replacements = append(replacements, sourceReplacement{start: r.Start.Offset + 1, end: r.Start.Offset + 1, text: text})
	}
	if edit.Prompt != nil {
		r := parsed.sourceRange(call.ArgumentList[0])
		// 表单统一提交可编辑模板，原表达式是否使用拼接不影响输入插值。
		text := "`" + escapeTemplateBackticks(*edit.Prompt) + "`"
		replacements = append(replacements, sourceReplacement{start: r.Start.Offset, end: r.End.Offset, text: text})
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start > replacements[j].start })
	for _, item := range replacements {
		source = source[:item.start] + item.text + source[item.end:]
	}
	return source, nil
}

func escapeTemplateBackticks(value string) string {
	var text strings.Builder
	backslashes := 0
	for _, character := range value {
		if character == '`' && backslashes%2 == 0 {
			text.WriteByte('\\')
		}
		text.WriteRune(character)
		if character == '\\' {
			backslashes++
		} else {
			backslashes = 0
		}
	}
	return text.String()
}

func editProperty(object *ast.ObjectLiteral, name string) ast.Expression {
	if object == nil {
		return nil
	}
	for _, property := range object.Value {
		if item, ok := property.(*ast.PropertyKeyed); ok {
			key, _ := propertyName(item.Key)
			if key == name {
				return item.Value
			}
		}
	}
	return nil
}
