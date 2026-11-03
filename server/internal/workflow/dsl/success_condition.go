package dsl

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// SuccessCondition 是对经过 Schema 校验的业务数据执行的显式相等检查。
type SuccessCondition struct {
	// Path 是 JSON Pointer；空字符串表示整个业务值。
	Path string `json:"path"`
	// Equals 是必须精确匹配的 JSON 字面值，包括 null。
	Equals any `json:"equals"`
}

func validJSONPointer(path string) bool {
	if path == "" {
		return true
	}
	if !strings.HasPrefix(path, "/") {
		return false
	}
	for i := 0; i < len(path); i++ {
		if path[i] == '~' {
			if i+1 == len(path) || (path[i+1] != '0' && path[i+1] != '1') {
				return false
			}
			i++
		}
	}
	return true
}

// EvaluateSuccessCondition 针对节点业务值执行静态检查；不存在的路径与 JSON null 严格区分。
func EvaluateSuccessCondition(data any, raw any) bool {
	if raw == nil {
		return true
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(encoded, &fields) != nil || len(fields) != 2 {
		return false
	}
	pathRaw, hasPath := fields["path"]
	equals, hasEquals := fields["equals"]
	var path string
	if !hasPath || !hasEquals || json.Unmarshal(pathRaw, &path) != nil || !validJSONPointer(path) {
		return false
	}
	value := cloneJSON(data)
	if path != "" {
		for _, part := range strings.Split(path[1:], "/") {
			key := strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
			switch current := value.(type) {
			case map[string]any:
				var exists bool
				value, exists = current[key]
				if !exists {
					return false
				}
			case []any:
				index, err := strconv.Atoi(key)
				if err != nil || index < 0 || index >= len(current) || strconv.Itoa(index) != key {
					return false
				}
				value = current[index]
			default:
				return false
			}
		}
	}
	actual, err := json.Marshal(value)
	if err != nil {
		return false
	}
	var expected any
	if json.Unmarshal(equals, &expected) != nil {
		return false
	}
	canonical, _ := json.Marshal(expected)
	return bytes.Equal(actual, canonical)
}
