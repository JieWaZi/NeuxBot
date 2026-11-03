package dsl

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/dop251/goja/ast"
)

// ScriptDefinition 保存版本内的独立本地程序及其协议。
type ScriptDefinition struct {
	// Language 是宿主支持的程序语言。
	Language string `json:"language"`
	// Protocol 指定 stdout 是单个 JSON 值还是文本。
	Protocol string `json:"protocol"`
	// Code 是从静态源码提取的完整 UTF-8 字节。
	Code string `json:"code"`
	// Digest 是 Code 原始字节的 SHA-256 摘要。
	Digest string `json:"digest"`
}

// ScriptSummary 是画布中不包含脚本正文的静态描述。
type ScriptSummary struct {
	// ID 引用版本内唯一脚本。
	ID string `json:"id"`
	// Language 是已校验的脚本语言。
	Language string `json:"language"`
	// Protocol 是标准输出协议。
	Protocol string `json:"protocol"`
	// Digest 关联完整脚本字节。
	Digest string `json:"digest"`
}

func (c *compiler) compileScripts() {
	c.bundle.Scripts = map[string]ScriptDefinition{}
	expression, exists := c.constants["scripts"]
	if !exists {
		return
	}
	object, ok := expression.(*ast.ObjectLiteral)
	if !ok {
		c.fail(expression, "SCRIPTS_LITERAL_REQUIRED", "scripts must be a static object literal")
		return
	}
	for _, property := range object.Value {
		entry, ok := property.(*ast.PropertyKeyed)
		if !ok || entry.Computed || entry.Kind != ast.PropertyKindValue {
			c.fail(property, "SCRIPT_INVALID", "scripts require unique static keys")
			continue
		}
		id, ok := propertyName(entry.Key)
		if !ok || forbiddenKey(id) || !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,63}$`).MatchString(id) || c.bundle.Scripts[id].Code != "" {
			c.fail(entry, "SCRIPT_INVALID", "script identity must be unique and stable")
			continue
		}
		definition, ok := entry.Value.(*ast.ObjectLiteral)
		if !ok {
			c.fail(entry.Value, "SCRIPT_INVALID", "script definition must be a static literal")
			continue
		}
		values := map[string]any{}
		valid := true
		for _, field := range definition.Value {
			keyed, ok := field.(*ast.PropertyKeyed)
			if !ok || keyed.Computed || keyed.Kind != ast.PropertyKindValue {
				valid = false
				continue
			}
			name, ok := propertyName(keyed.Key)
			if !ok || (name != "language" && name != "protocol" && name != "code") {
				valid = false
				continue
			}
			if _, duplicate := values[name]; duplicate {
				valid = false
				continue
			}
			value, ok := c.literal(keyed.Value, false, map[string]bool{})
			if name == "code" {
				value, ok = c.scriptCode(keyed.Value)
			}
			if !ok {
				valid = false
				continue
			}
			values[name] = value
		}
		script := ScriptDefinition{}
		data, _ := json.Marshal(values)
		if json.Unmarshal(data, &script) != nil || !valid || script.Language != "python" || (script.Protocol != "json" && script.Protocol != "text") || strings.TrimSpace(script.Code) == "" {
			c.fail(entry, "SCRIPT_INVALID", "script requires python language, json/text protocol and static nonempty code")
			continue
		}
		script.Digest = Digest([]byte(script.Code))
		c.bundle.Scripts[id] = script
	}
}

func (c *compiler) scriptCode(expression ast.Expression) (any, bool) {
	if text, ok := expression.(*ast.StringLiteral); ok {
		return text.Value.String(), true
	}
	template, ok := expression.(*ast.TemplateLiteral)
	if !ok || len(template.Expressions) != 0 || len(template.Elements) != 1 {
		return nil, false
	}
	tag, ok := template.Tag.(*ast.DotExpression)
	if !ok || tag.Identifier.Name.String() != "raw" {
		return nil, false
	}
	object, ok := tag.Left.(*ast.Identifier)
	if !ok || object.Name.String() != "String" {
		return nil, false
	}
	// 按原始范围取反引号之间的字节，避免 JS cooked 转义改变 Python 程序。
	fragment := c.fragment(template)
	first, last := strings.IndexByte(fragment, '`'), strings.LastIndexByte(fragment, '`')
	if first < 0 || last <= first {
		return nil, false
	}
	return fragment[first+1 : last], true
}

func scriptSummaries(scripts map[string]ScriptDefinition) []ScriptSummary {
	ids := make([]string, 0, len(scripts))
	for id := range scripts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]ScriptSummary, 0, len(ids))
	for _, id := range ids {
		script := scripts[id]
		result = append(result, ScriptSummary{ID: id, Language: script.Language, Protocol: script.Protocol, Digest: script.Digest})
	}
	return result
}
