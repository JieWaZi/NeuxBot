package dsl

import (
	"sort"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

// schemaDeclaresFiles 只检查可从根结果到达的文件声明，不把未使用的定义或普通字符串当文件。
func schemaDeclaresFiles(schema map[string]any) bool {
	inspection := newSchemaInspection(schema)
	visited := map[string]bool{}
	var visit func(string) bool
	visit = func(path string) bool {
		if visited[path] {
			return false
		}
		visited[path] = true
		current := inspection.schema(path)
		if current == nil {
			return false
		}
		if current["format"] == constants.WorkflowFileFormat {
			return true
		}
		if ref, ok := current["$ref"].(string); ok && strings.HasPrefix(ref, "#/") && visit(ref[1:]) {
			return true
		}
		properties, _ := current["properties"].(map[string]any)
		for key := range properties {
			if visit(path + "/properties/" + pointerKey(key)) {
				return true
			}
		}
		for _, keyword := range []string{"items", "additionalProperties"} {
			if visit(path + "/" + keyword) {
				return true
			}
		}
		for _, keyword := range []string{"prefixItems", "allOf", "oneOf", "anyOf"} {
			branches, _ := current[keyword].([]any)
			for index := range branches {
				if visit(path + "/" + keyword + "/" + strconv.Itoa(index)) {
					return true
				}
			}
		}
		return false
	}
	return visit("")
}

// FileValues 返回当前 JSON 中由 schema 明确声明的文件字段，键为 JSON Pointer。
func FileValues(schema map[string]any, value any) map[string]string {
	s := newSchemaInspection(schema)
	files := map[string]string{}
	var visit func(string, string, any, int)
	visit = func(path, pointer string, value any, depth int) {
		if depth > 64 || value == nil {
			return
		}
		current := s.schema(path)
		if current == nil {
			return
		}
		if current["format"] == constants.WorkflowFileFormat {
			if text, ok := value.(string); ok {
				files[pointer] = text
			}
		}
		if ref, ok := current["$ref"].(string); ok && strings.HasPrefix(ref, "#/") {
			visit(ref[1:], pointer, value, depth+1)
		}
		if object, ok := value.(map[string]any); ok {
			properties, _ := current["properties"].(map[string]any)
			for key, child := range object {
				childPath := path + "/properties/" + pointerKey(key)
				if _, exists := properties[key]; !exists {
					childPath = path + "/additionalProperties"
				}
				visit(childPath, pointer+"/"+pointerKey(key), child, depth+1)
			}
		}
		if array, ok := value.([]any); ok {
			prefix, _ := current["prefixItems"].([]any)
			for index, child := range array {
				childPath := path + "/items"
				if index < len(prefix) {
					childPath = path + "/prefixItems/" + strconv.Itoa(index)
				}
				visit(childPath, pointer+"/"+strconv.Itoa(index), child, depth+1)
			}
		}
		for _, keyword := range []string{"allOf", "oneOf", "anyOf"} {
			branches, _ := current[keyword].([]any)
			for index := range branches {
				childPath := path + "/" + keyword + "/" + strconv.Itoa(index)
				if keyword == "allOf" || s.valid(childPath, value) {
					visit(childPath, pointer, value, depth+1)
				}
			}
		}
	}
	visit("", "", cloneJSON(value), 0)
	return files
}

// MapFiles 复制输入并只转换声明为文件的值，保留普通字符串及字段顺序语义。
func MapFiles(schema map[string]any, value any, convert func(string, string) (string, error)) (any, error) {
	result := cloneJSON(value)
	files := FileValues(schema, result)
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, pointer := range keys {
		converted, err := convert(pointer, files[pointer])
		if err != nil {
			return nil, err
		}
		if pointer == "" {
			result = converted
			continue
		}
		parts := strings.Split(pointer[1:], "/")
		parent := result
		for index, part := range parts {
			part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
			switch item := parent.(type) {
			case map[string]any:
				if index == len(parts)-1 {
					item[part] = converted
				} else {
					parent = item[part]
				}
			case []any:
				i, _ := strconv.Atoi(part)
				if index == len(parts)-1 {
					item[i] = converted
				} else {
					parent = item[i]
				}
			}
		}
	}
	return result, nil
}
