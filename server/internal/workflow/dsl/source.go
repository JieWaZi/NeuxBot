// Package dsl 把受限创作源码编译为可恢复的 Go 指令，不执行 JavaScript 宿主逻辑。
package dsl

import (
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
	"github.com/dop251/goja/token"
)

// SourcePosition 同时保留 UTF-8 字节位置和编辑器的 UTF-16 行列。
type SourcePosition struct {
	// Offset 是原始源码的零基 UTF-8 字节偏移。
	Offset int `json:"offset"`
	// Line 是编辑器的一基行号。
	Line int `json:"line"`
	// Column 是编辑器的一基 UTF-16 列号。
	Column int `json:"column"`
}

// SourceRange 指向原始源码中的半开范围。
type SourceRange struct {
	// Start 是范围首字符的位置。
	Start SourcePosition `json:"start"`
	// End 是紧接范围末尾的位置。
	End SourcePosition `json:"end"`
}

// Diagnostic 是可以直接定位创作源码的编译反馈。
type Diagnostic struct {
	// Code 是稳定的机器可读诊断名称。
	Code string `json:"code"`
	// Severity 区分阻断保存的错误和只读提示。
	Severity string `json:"severity"`
	// Message 是创作可以修正的技术原因。
	Message string `json:"message"`
	// Range 是对应的原始源码位置。
	Range SourceRange `json:"range"`
	// NodeID 在可解析时关联稳定节点身份。
	NodeID string `json:"nodeId,omitempty"`
	// Field 是契约或配置中的错误字段。
	Field string `json:"field,omitempty"`
	// Hint 给出可以采用的修正方式。
	Hint string `json:"hint,omitempty"`
}

// parsedSource 隔离固定版本 AST 与工作流编译器的内部表示。
type parsedSource struct {
	// source 保留原始字节，避免保存时格式化导致摘要漂移。
	source string
	// body 是顶层 await/return 包装后的主流程语句。
	body []ast.Statement
	// prefixBytes 用于把包装器 AST 偏移映射回原文。
	prefixBytes int
	// exports 记录实际导出的声明位置，不通过源码内容推断权限。
	exports []int
}

func parseSource(source string) (*parsedSource, []Diagnostic, error) {
	if !utf8.ValidString(source) || len(source) > constants.WorkflowSourceLimitBytes {
		return nil, []Diagnostic{{Code: "SOURCE_ENCODING_OR_SIZE", Severity: "error", Message: "source must be UTF-8 and at most 1 MiB"}}, status_error.WorkflowSourceInvalid
	}
	adapted, exports := maskExports(source)
	prefix := "async function __allyWorkflow(){\n"
	program, err := parser.ParseFile(nil, "workflow.js", prefix+adapted+"\n}", 0)
	if err != nil {
		diagnostic := Diagnostic{Code: "SYNTAX_ERROR", Severity: "error", Message: err.Error()}
		if list, ok := err.(parser.ErrorList); ok && len(list) > 0 {
			position := list[0].Position
			line := position.Line - 1
			if line < 1 {
				line = 1
			}
			diagnostic.Range.Start = positionAt(source, offsetAt(source, line, position.Column))
			diagnostic.Range.End = diagnostic.Range.Start
		}
		return nil, []Diagnostic{diagnostic}, status_error.WorkflowSourceInvalid
	}
	declaration, ok := program.Body[0].(*ast.FunctionDeclaration)
	if !ok {
		return nil, nil, status_error.WorkflowStateInvalid
	}
	return &parsedSource{source: source, body: declaration.Function.Body.List, prefixBytes: len(prefix), exports: exports}, nil, nil
}

// maskExports 仅屏蔽词法层面的 export；字符串、模板和注释中的文本保持原样。
func maskExports(source string) (string, []int) {
	data := []byte(source)
	exports := []int{}
	regexpAllowed := true
	for i := 0; i < len(data); {
		switch data[i] {
		case '\'', '"', '`':
			i = skipSourceString(data, i)
			regexpAllowed = false
		case '/':
			if i+1 < len(data) && data[i+1] == '/' {
				i = skipSourceComment(data, i)
			} else if i+1 < len(data) && data[i+1] == '*' {
				i = skipSourceComment(data, i)
			} else if regexpAllowed {
				i = skipSourceRegexp(data, i)
				regexpAllowed = false
			} else {
				i++
				regexpAllowed = true
			}
		default:
			if identifierByte(data[i]) {
				start := i
				for i < len(data) && identifierByte(data[i]) {
					i++
				}
				word := string(data[start:i])
				if word == "export" {
					exports = append(exports, start)
					for j := start; j < i; j++ {
						data[j] = ' '
					}
				}
				regexpAllowed = word == "return" || word == "throw" || word == "await" || word == "typeof" || word == "void" || word == "case" || word == "in" || word == "of" || word == "export"
			} else {
				switch data[i] {
				case ')', ']', '}':
					regexpAllowed = false
				case ' ', '\t', '\n', '\r':
				default:
					regexpAllowed = true
				}
				i++
			}
		}
	}
	return string(data), exports
}

func skipSourceComment(data []byte, start int) int {
	i := start + 2
	if data[start+1] == '/' {
		for i < len(data) && data[i] != '\n' {
			i++
		}
		return i
	}
	for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
		i++
	}
	if i+1 < len(data) {
		return i + 2
	}
	return len(data)
}

func skipSourceRegexp(data []byte, start int) int {
	i := start + 1
	inClass := false
	for i < len(data) {
		if data[i] == '\\' {
			i += 2
			continue
		}
		if data[i] == '\n' || data[i] == '\r' {
			return i
		}
		if data[i] == '[' {
			inClass = true
		} else if data[i] == ']' {
			inClass = false
		} else if data[i] == '/' && !inClass {
			i++
			for i < len(data) && identifierByte(data[i]) {
				i++
			}
			return i
		}
		i++
	}
	return len(data)
}

func skipSourceString(data []byte, start int) int {
	quote := data[start]
	i := start + 1
	for i < len(data) {
		if data[i] == '\\' {
			i += 2
			continue
		}
		if data[i] == quote {
			return i + 1
		}
		if quote == '`' && data[i] == '$' && i+1 < len(data) && data[i+1] == '{' {
			i = skipTemplateExpression(data, i+2)
			continue
		}
		i++
	}
	return len(data)
}

func skipTemplateExpression(data []byte, start int) int {
	depth := 1
	regexpAllowed := true
	for i := start; i < len(data); {
		switch data[i] {
		case '\'', '"', '`':
			i = skipSourceString(data, i)
			regexpAllowed = false
		case '/':
			if i+1 < len(data) && (data[i+1] == '/' || data[i+1] == '*') {
				i = skipSourceComment(data, i)
			} else if regexpAllowed {
				i = skipSourceRegexp(data, i)
				regexpAllowed = false
			} else {
				i++
				regexpAllowed = true
			}
		case '{':
			depth++
			i++
			regexpAllowed = true
		case '}':
			depth--
			i++
			if depth == 0 {
				return i
			}
			regexpAllowed = false
		default:
			if identifierByte(data[i]) {
				for i < len(data) && identifierByte(data[i]) {
					i++
				}
				regexpAllowed = false
			} else {
				if data[i] != ' ' && data[i] != '\t' && data[i] != '\n' {
					regexpAllowed = data[i] != ')' && data[i] != ']'
				}
				i++
			}
		}
	}
	return len(data)
}

func identifierByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '$' || c >= 128
}

func positionAt(source string, offset int) SourcePosition {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	line := strings.Count(source[:offset], "\n") + 1
	start := strings.LastIndex(source[:offset], "\n") + 1
	return SourcePosition{Offset: offset, Line: line, Column: len(utf16.Encode([]rune(source[start:offset]))) + 1}
}

func offsetAt(source string, line, column int) int {
	offset := 0
	for current := 1; current < line; current++ {
		end := strings.IndexByte(source[offset:], '\n')
		if end < 0 {
			return len(source)
		}
		offset += end + 1
	}
	// Goja 的 parser 列号按字节计算，编辑器列号由 positionAt 单独生成。
	offset += column - 1
	if offset > len(source) {
		return len(source)
	}
	return offset
}

func (p *parsedSource) sourceRange(node ast.Node) SourceRange {
	return SourceRange{Start: positionAt(p.source, int(node.Idx0())-1-p.prefixBytes), End: positionAt(p.source, int(node.Idx1())-1-p.prefixBytes)}
}

func (c *compiler) validateExports(meta ast.Node) {
	exported := map[string]int{}
	for _, offset := range c.parsed.exports {
		start := skipExportTrivia(c.parsed.source, offset+len("export"))
		matched := false
		for _, statement := range c.parsed.body {
			if c.parsed.sourceRange(statement).Start.Offset != start {
				continue
			}
			declaration, ok := statement.(*ast.LexicalDeclaration)
			if !ok || declaration.Token != token.CONST || len(declaration.List) != 1 {
				break
			}
			identifier, ok := declaration.List[0].Target.(*ast.Identifier)
			if !ok || (identifier.Name.String() != "meta" && identifier.Name.String() != "actions") {
				break
			}
			exported[identifier.Name.String()]++
			matched = true
			break
		}
		if !matched {
			c.diagnostics = append(c.diagnostics, Diagnostic{Code: "EXPORT_UNSUPPORTED", Severity: "error", Message: "only a single top-level const meta or const actions declaration may be exported", Range: SourceRange{Start: positionAt(c.parsed.source, offset), End: positionAt(c.parsed.source, offset+len("export"))}})
		}
	}
	if exported["meta"] != 1 {
		c.fail(meta, "META_EXPORT_REQUIRED", "exactly one top-level meta declaration must be exported")
	}
	if _, declared := c.constants["actions"]; declared && exported["actions"] != 1 {
		c.fail(c.constants["actions"], "ACTIONS_EXPORT_REQUIRED", "actions handlers must be exported explicitly")
	}
}

func skipExportTrivia(source string, offset int) int {
	for offset < len(source) {
		r, size := utf8.DecodeRuneInString(source[offset:])
		if unicode.IsSpace(r) {
			offset += size
			continue
		}
		if offset+1 < len(source) && source[offset] == '/' && (source[offset+1] == '/' || source[offset+1] == '*') {
			offset = skipSourceComment([]byte(source), offset)
			continue
		}
		break
	}
	return offset
}

// SaveReceipt 证明一份源码已经成为完整不可变版本。
type SaveReceipt struct {
	// DefinitionID 是工作流稳定身份。
	DefinitionID string `json:"definitionId"`
	// RevisionID 是实际生效的版本。
	RevisionID string `json:"revisionId"`
	// Ordinal 是用户可见版本序号。
	Ordinal int64 `json:"ordinal"`
	// SourceDigest 绑定精确保存的源码。
	SourceDigest string `json:"sourceDigest"`
	// Unchanged 表示新请求的源码与当前版完全相同。
	Unchanged bool `json:"unchanged"`
	// Replayed 表示返回同一请求的原提交回执。
	Replayed bool `json:"replayed"`
}
