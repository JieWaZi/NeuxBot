package dsl

import (
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func (e *evaluator) method(receiver Value, name string, args []Value, target *Expression) (Value, error) {
	if receiver.Kind == constants.WorkflowSlotBuiltin {
		prefix := stringValue(receiver)
		if prefix == "Object" && (name == "keys" || name == "values" || name == "entries") {
			if len(args) != 1 {
				return Value{}, status_error.WorkflowSchemaInvalid
			}
			object, ok := args[0].Data.(map[string]any)
			if !ok {
				return Value{}, status_error.WorkflowSchemaInvalid
			}
			keys := []string{}
			for key := range object {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			values := []any{}
			for _, key := range keys {
				switch name {
				case "keys":
					values = append(values, key)
				case "values":
					values = append(values, object[key])
				case "entries":
					values = append(values, []any{key, object[key]})
				}
			}
			return JSONValue(values), nil
		}
		return e.builtin(prefix+"."+name, args)
	}
	if receiver.Kind == constants.WorkflowSlotDate && name == "toISOString" && len(args) == 0 {
		return JSONValue(receiver.Data), nil
	}
	if receiver.Kind == constants.WorkflowSlotRegExp && name == "test" && len(args) == 1 {
		object, ok := receiver.Data.(map[string]any)
		if !ok {
			return Value{}, status_error.WorkflowStateInvalid
		}
		pattern, _ := object["pattern"].(string)
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return Value{}, status_error.WorkflowStateInvalid
		}
		return JSONValue(compiled.MatchString(stringValue(args[0]))), nil
	}
	if text, ok := receiver.Data.(string); ok && receiver.Kind == constants.WorkflowSlotJSON {
		return e.stringMethod(text, name, args)
	}
	items, err := arrayValues(receiver)
	if err != nil {
		return Value{}, status_error.WorkflowCapabilityUnsupported.WithField("reason", "unsupported receiver method: "+name)
	}
	switch name {
	case "map", "filter", "find", "some", "every":
		if len(args) != 1 {
			return Value{}, status_error.WorkflowSchemaInvalid
		}
		result := []Value{}
		for index, item := range items {
			value, err := e.call(args[0], []Value{item, JSONValue(index), receiver})
			if err != nil {
				return Value{}, err
			}
			switch name {
			case "map":
				result = append(result, value)
			case "filter":
				if truthy(value) {
					result = append(result, item)
				}
			case "find":
				if truthy(value) {
					return item, nil
				}
			case "some":
				if truthy(value) {
					return JSONValue(true), nil
				}
			case "every":
				if !truthy(value) {
					return JSONValue(false), nil
				}
			}
		}
		switch name {
		case "find":
			return Value{Kind: constants.WorkflowSlotMissing}, nil
		case "some":
			return JSONValue(false), nil
		case "every":
			return JSONValue(true), nil
		}
		return valuesArray(result), nil
	case "reduce":
		if len(args) < 1 || len(args) > 2 {
			return Value{}, status_error.WorkflowSchemaInvalid
		}
		index := 0
		var accumulator Value
		if len(args) == 2 {
			accumulator = args[1]
		} else {
			if len(items) == 0 {
				return Value{}, status_error.WorkflowSchemaInvalid
			}
			accumulator = items[0]
			index = 1
		}
		for ; index < len(items); index++ {
			var err error
			accumulator, err = e.call(args[0], []Value{accumulator, items[index], JSONValue(index), receiver})
			if err != nil {
				return Value{}, err
			}
		}
		return accumulator, nil
	case "join":
		separator := ","
		if len(args) > 0 {
			separator = stringValue(args[0])
		}
		parts := []string{}
		for _, item := range items {
			if isNullish(item) {
				parts = append(parts, "")
			} else {
				parts = append(parts, stringValue(item))
			}
		}
		text := strings.Join(parts, separator)
		if len(text) > constants.WorkflowOutputLimitBytes {
			return Value{}, status_error.WorkflowBudgetExceeded
		}
		return JSONValue(text), nil
	case "slice":
		start, end, err := sliceBounds(len(items), args)
		if err != nil {
			return Value{}, err
		}
		return valuesArray(items[start:end]), nil
	case "concat":
		result := append([]Value{}, items...)
		for _, argument := range args {
			if extra, err := arrayValues(argument); err == nil {
				result = append(result, extra...)
			} else {
				result = append(result, argument)
			}
		}
		return valuesArray(result), nil
	case "includes", "indexOf":
		if len(args) < 1 || len(args) > 2 {
			return Value{}, status_error.WorkflowSchemaInvalid
		}
		start := 0
		if len(args) == 2 {
			number, err := numberValue(args[1])
			if err != nil {
				return Value{}, err
			}
			start = int(number)
			if start < 0 {
				start = len(items) + start
			}
			if start < 0 {
				start = 0
			}
		}
		found := -1
		for index := start; index < len(items); index++ {
			if reflect.DeepEqual(items[index], args[0]) {
				found = index
				break
			}
		}
		if name == "includes" {
			return JSONValue(found >= 0), nil
		}
		return JSONValue(found), nil
	case "push":
		result := append(append([]Value{}, items...), args...)
		value := valuesArray(result)
		if value.Kind != constants.WorkflowSlotJSON {
			return Value{}, status_error.WorkflowStateInvalid.WithField("reason", "push only accepts JSON values")
		}
		if err := e.mutate(target, value); err != nil {
			return Value{}, err
		}
		return JSONValue(len(result)), nil
	}
	return Value{}, status_error.WorkflowCapabilityUnsupported.WithField("reason", "unsupported array method: "+name)
}

func (e *evaluator) mutate(target *Expression, value Value) error {
	root, path, err := e.assignmentPath(target)
	if err != nil {
		return err
	}
	data, err := value.JSON()
	if err != nil {
		return err
	}
	for index := len(e.state.Scopes) - 1; index >= 0; index-- {
		binding, found := e.state.Scopes[index][root]
		if !found {
			continue
		}
		if binding.ReadOnly {
			return status_error.WorkflowStateInvalid.WithField("reason", "context is read-only: "+root)
		}
		object, err := binding.Value.JSON()
		if err != nil {
			return err
		}
		object, err = setJSONPath(object, path, data)
		if err != nil {
			return err
		}
		binding.Value = JSONValue(object)
		e.state.Scopes[index][root] = binding
		return nil
	}
	return status_error.WorkflowStateInvalid
}

func sliceBounds(length int, args []Value) (int, int, error) {
	if len(args) > 2 {
		return 0, 0, status_error.WorkflowSchemaInvalid
	}
	start, end := 0, length
	for index, arg := range args {
		number, err := numberValue(arg)
		if err != nil {
			return 0, 0, err
		}
		bound := int(number)
		if bound < 0 {
			bound = length + bound
		}
		if bound < 0 {
			bound = 0
		}
		if bound > length {
			bound = length
		}
		if index == 0 {
			start = bound
		} else {
			end = bound
		}
	}
	if end < start {
		end = start
	}
	return start, end, nil
}

func (e *evaluator) stringMethod(text, name string, args []Value) (Value, error) {
	switch name {
	case "trim":
		if len(args) != 0 {
			return Value{}, status_error.WorkflowSchemaInvalid
		}
		return JSONValue(strings.TrimSpace(text)), nil
	case "toUpperCase":
		return JSONValue(strings.ToUpper(text)), nil
	case "toLowerCase":
		return JSONValue(strings.ToLower(text)), nil
	case "concat":
		var builder strings.Builder
		builder.WriteString(text)
		for _, argument := range args {
			builder.WriteString(stringValue(argument))
		}
		if builder.Len() > constants.WorkflowOutputLimitBytes {
			return Value{}, status_error.WorkflowBudgetExceeded
		}
		return JSONValue(builder.String()), nil
	case "slice":
		runes := []rune(text)
		start, end, err := sliceBounds(len(runes), args)
		if err != nil {
			return Value{}, err
		}
		return JSONValue(string(runes[start:end])), nil
	case "split":
		if len(args) == 0 {
			return JSONValue([]any{text}), nil
		}
		if len(args) > 2 {
			return Value{}, status_error.WorkflowSchemaInvalid
		}
		separator := stringValue(args[0])
		parts := []string{}
		if separator == "" {
			for _, r := range text {
				parts = append(parts, string(r))
			}
		} else {
			parts = strings.Split(text, separator)
		}
		if len(args) == 2 {
			number, err := numberValue(args[1])
			if err != nil || number < 0 {
				return Value{}, status_error.WorkflowSchemaInvalid
			}
			limit := int(number)
			if limit < len(parts) {
				parts = parts[:limit]
			}
		}
		values := []any{}
		for _, part := range parts {
			values = append(values, part)
		}
		return JSONValue(values), nil
	case "includes", "indexOf", "startsWith", "endsWith":
		if len(args) < 1 || len(args) > 2 {
			return Value{}, status_error.WorkflowSchemaInvalid
		}
		search := stringValue(args[0])
		runes := []rune(text)
		offset := 0
		if len(args) == 2 {
			number, err := numberValue(args[1])
			if err != nil {
				return Value{}, err
			}
			offset = int(number)
			if offset < 0 {
				offset = 0
			}
			if offset > len(runes) {
				offset = len(runes)
			}
		}
		part := string(runes[offset:])
		if name == "startsWith" {
			return JSONValue(strings.HasPrefix(part, search)), nil
		}
		if name == "endsWith" {
			end := len(runes)
			if len(args) == 2 {
				end = offset
			}
			return JSONValue(strings.HasSuffix(string(runes[:end]), search)), nil
		}
		index := strings.Index(part, search)
		if name == "includes" {
			return JSONValue(index >= 0), nil
		}
		if index < 0 {
			return JSONValue(-1), nil
		}
		return JSONValue(offset + utf8.RuneCountInString(part[:index])), nil
	case "replace":
		if len(args) != 2 {
			return Value{}, status_error.WorkflowSchemaInvalid
		}
		replacement := stringValue(args[1])
		if args[0].Kind == constants.WorkflowSlotRegExp {
			data, ok := args[0].Data.(map[string]any)
			if !ok {
				return Value{}, status_error.WorkflowStateInvalid
			}
			pattern, _ := data["pattern"].(string)
			flags, _ := data["flags"].(string)
			compiled, err := regexp.Compile(pattern)
			if err != nil {
				return Value{}, status_error.WorkflowStateInvalid
			}
			if strings.Contains(replacement, "$`") || strings.Contains(replacement, "$'") {
				return Value{}, status_error.WorkflowCapabilityUnsupported.WithField("reason", "regexp replacement only supports numbered captures and $&")
			}
			replacement = strings.ReplaceAll(replacement, "$&", "${0}")
			if strings.Contains(flags, "g") {
				return JSONValue(compiled.ReplaceAllString(text, replacement)), nil
			}
			index := compiled.FindStringSubmatchIndex(text)
			if index == nil {
				return JSONValue(text), nil
			}
			replaced := compiled.ExpandString(nil, replacement, text, index)
			return JSONValue(text[:index[0]] + string(replaced) + text[index[1]:]), nil
		}
		search := stringValue(args[0])
		replacement = strings.ReplaceAll(replacement, "$&", search)
		replacement = strings.ReplaceAll(replacement, "$$", "$")
		return JSONValue(strings.Replace(text, search, replacement, 1)), nil
	}
	return Value{}, status_error.WorkflowCapabilityUnsupported.WithField("reason", "unsupported string method: "+name)
}

// nativeRegexpPattern 转换共同子集的转义；不放宽 RE2 不支持的回溯或前后查找。
func nativeRegexpPattern(pattern, flags string) (string, bool) {
	var result strings.Builder
	for i := 0; i < len(pattern); i++ {
		if pattern[i] != '\\' {
			result.WriteByte(pattern[i])
			continue
		}
		if i+1 >= len(pattern) {
			return "", false
		}
		next := pattern[i+1]
		if next == 'u' && i+5 < len(pattern) {
			digits := pattern[i+2 : i+6]
			if !regexp.MustCompile(`^[0-9a-fA-F]{4}$`).MatchString(digits) {
				return "", false
			}
			result.WriteString(`\x{` + digits + `}`)
			i += 5
			continue
		}
		if next == '/' {
			result.WriteByte('/')
			i++
			continue
		}
		result.WriteByte('\\')
		result.WriteByte(next)
		i++
	}
	prefix := ""
	seen := map[rune]bool{}
	for _, flag := range flags {
		if seen[flag] {
			return "", false
		}
		seen[flag] = true
		switch flag {
		case 'i', 'm':
			prefix += string(flag)
		case 'g', 'u':
		default:
			return "", false
		}
	}
	if prefix != "" {
		return "(?" + prefix + ")" + result.String(), true
	}
	return result.String(), true
}
