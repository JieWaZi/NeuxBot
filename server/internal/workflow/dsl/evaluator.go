package dsl

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// evaluator 只解释受限值表达式，无法读取文件、网络、进程或宿主对象。
type evaluator struct {
	// ctx 让纯计算和 callback 也响应运行取消。
	ctx context.Context
	// bundle 是本次运行固定的静态程序。
	bundle ProgramBundle
	// state 是当前 fiber 的词法绑定和预算。
	state *MachineState
	// callChain 用静态程序身份阻断辅助函数递归。
	callChain []string
}

func (e *evaluator) expression(expression *Expression) (Value, error) {
	if expression == nil {
		return JSONValue(nil), nil
	}
	if e.ctx.Err() != nil {
		return Value{}, status_error.WorkflowCancelled
	}
	e.state.PureSteps++
	if e.state.PureSteps > constants.WorkflowMaxPureSteps {
		return Value{}, status_error.WorkflowBudgetExceeded
	}
	switch expression.Kind {
	case constants.WorkflowExprLiteral:
		return JSONValue(expression.Value), nil
	case constants.WorkflowExprIdentifier:
		if binding, ok := e.lookup(expression.Name); ok {
			return copyValue(binding.Value), nil
		}
		if expression.Name == "undefined" {
			return Value{Kind: constants.WorkflowSlotMissing}, nil
		}
		for _, name := range []string{"Math", "JSON", "Object", "Array", "String", "Number", "Boolean", "Date", "Error"} {
			if name == expression.Name {
				return Value{Kind: constants.WorkflowSlotBuiltin, Data: name}, nil
			}
		}
		return Value{}, status_error.WorkflowStateInvalid.WithField("reason", "unbound identifier: "+expression.Name)
	case constants.WorkflowExprArray:
		values := []Value{}
		for _, argument := range expression.Arguments {
			if argument.Kind == constants.WorkflowExprSpread {
				value, err := e.expression(argument.Arguments[0])
				if err != nil {
					return Value{}, err
				}
				items, err := arrayValues(value)
				if err != nil {
					return Value{}, err
				}
				values = append(values, items...)
			} else {
				value, err := e.expression(argument)
				if err != nil {
					return Value{}, err
				}
				if value.Kind == constants.WorkflowSlotMissing {
					value = JSONValue(nil)
				}
				values = append(values, value)
			}
		}
		return valuesArray(values), nil
	case constants.WorkflowExprObject:
		object := map[string]any{}
		for _, property := range expression.Properties {
			value, err := e.expression(property.Value)
			if err != nil {
				return Value{}, err
			}
			if property.Spread {
				data, err := value.JSON()
				if err != nil {
					return Value{}, err
				}
				source, ok := data.(map[string]any)
				if !ok {
					return Value{}, status_error.WorkflowStateInvalid.WithField("reason", "object spread requires an object")
				}
				for key, item := range source {
					if forbiddenKey(key) {
						return Value{}, status_error.WorkflowStateInvalid
					}
					object[key] = item
				}
				continue
			}
			key, err := e.expression(property.Key)
			if err != nil {
				return Value{}, err
			}
			name := stringValue(key)
			if forbiddenKey(name) {
				return Value{}, status_error.WorkflowStateInvalid
			}
			if value.Kind == constants.WorkflowSlotMissing {
				continue
			}
			data, err := value.JSON()
			if err != nil {
				return Value{}, err
			}
			object[name] = data
		}
		return JSONValue(object), nil
	case constants.WorkflowExprMember:
		left, err := e.expression(expression.Arguments[0])
		if err != nil {
			return Value{}, err
		}
		if isNullish(left) && expression.Optional {
			return Value{Kind: constants.WorkflowSlotMissing}, nil
		}
		key, err := e.expression(expression.Arguments[1])
		if err != nil {
			return Value{}, err
		}
		return memberValue(left, stringValue(key), expression.Optional)
	case constants.WorkflowExprBinary:
		left, err := e.expression(expression.Arguments[0])
		if err != nil {
			return Value{}, err
		}
		if expression.Name == "&&" && !truthy(left) || expression.Name == "||" && truthy(left) || expression.Name == "??" && !isNullish(left) {
			return left, nil
		}
		right, err := e.expression(expression.Arguments[1])
		if err != nil {
			return Value{}, err
		}
		return binaryValue(expression.Name, left, right)
	case constants.WorkflowExprConditional:
		condition, err := e.expression(expression.Arguments[0])
		if err != nil {
			return Value{}, err
		}
		index := 2
		if truthy(condition) {
			index = 1
		}
		return e.expression(expression.Arguments[index])
	case constants.WorkflowExprUnary:
		value, err := e.expression(expression.Arguments[0])
		if err != nil {
			return Value{}, err
		}
		switch expression.Name {
		case "!":
			return JSONValue(!truthy(value)), nil
		case "+":
			number, err := numberValue(value)
			return JSONValue(number), err
		case "-":
			number, err := numberValue(value)
			return JSONValue(-number), err
		case "~":
			number, err := numberValue(value)
			return JSONValue(float64(^int32(number))), err
		case "++", "--":
			number, err := numberValue(value)
			if err != nil {
				return Value{}, err
			}
			delta := float64(1)
			if expression.Name == "--" {
				delta = -1
			}
			result := JSONValue(number + delta)
			if err := e.assign(expression.Arguments[0], result); err != nil {
				return Value{}, err
			}
			if expression.Postfix {
				return value, nil
			}
			return result, nil
		case "typeof":
			return JSONValue(valueType(value)), nil
		case "void":
			return Value{Kind: constants.WorkflowSlotMissing}, nil
		}
		return Value{}, status_error.WorkflowStateInvalid
	case constants.WorkflowExprAssign:
		value, err := e.expression(expression.Arguments[1])
		if err != nil {
			return Value{}, err
		}
		if expression.Name != "=" {
			left, err := e.expression(expression.Arguments[0])
			if err != nil {
				return Value{}, err
			}
			operator := strings.TrimSuffix(expression.Name, "=")
			value, err = binaryValue(operator, left, value)
			if err != nil {
				return Value{}, err
			}
		}
		if err := e.assign(expression.Arguments[0], value); err != nil {
			return Value{}, err
		}
		return value, nil
	case constants.WorkflowExprSequence:
		result := JSONValue(nil)
		for _, argument := range expression.Arguments {
			value, err := e.expression(argument)
			if err != nil {
				return Value{}, err
			}
			result = value
		}
		return result, nil
	case constants.WorkflowExprTemplate:
		var result strings.Builder
		for _, argument := range expression.Arguments {
			value, err := e.expression(argument)
			if err != nil {
				return Value{}, err
			}
			result.WriteString(stringValue(value))
			if result.Len() > constants.WorkflowOutputLimitBytes {
				return Value{}, status_error.WorkflowBudgetExceeded
			}
		}
		return JSONValue(result.String()), nil
	case constants.WorkflowExprFunction:
		captures := map[string]Binding{}
		program, ok := e.bundle.Programs[expression.Name]
		if !ok {
			return Value{}, status_error.WorkflowStateInvalid
		}
		for _, name := range program.Captures {
			if binding, found := e.lookup(name); found {
				binding.Value = copyValue(binding.Value)
				captures[name] = binding
			}
		}
		return Value{Kind: constants.WorkflowSlotFunction, ProgramID: expression.Name, Captures: captures}, nil
	case constants.WorkflowExprRegExp:
		return Value{Kind: constants.WorkflowSlotRegExp, Data: map[string]any{"pattern": expression.Name, "flags": expression.Value}}, nil
	case constants.WorkflowExprNew:
		name := expression.Arguments[0].Name
		arguments, err := e.arguments(expression.Arguments[1:])
		if err != nil {
			return Value{}, err
		}
		if name == "Date" {
			if len(arguments) > 0 {
				return Value{}, status_error.WorkflowCapabilityUnsupported.WithField("reason", "Date only supports the frozen run start")
			}
			return Value{Kind: constants.WorkflowSlotDate, Data: time.UnixMilli(e.state.StartedAt).UTC().Format("2006-01-02T15:04:05.000Z")}, nil
		}
		if name == "Error" {
			message := "workflow source threw an error"
			if len(arguments) > 0 {
				message = stringValue(arguments[0])
			}
			return JSONValue(map[string]any{"code": "SOURCE_THROW", "message": message}), nil
		}
		return Value{}, status_error.WorkflowStateInvalid
	case constants.WorkflowExprCall:
		callee := expression.Arguments[0]
		if callee.Kind == constants.WorkflowExprMember {
			receiver, err := e.expression(callee.Arguments[0])
			if err != nil {
				return Value{}, err
			}
			if isNullish(receiver) && callee.Optional {
				return Value{Kind: constants.WorkflowSlotMissing}, nil
			}
			key, err := e.expression(callee.Arguments[1])
			if err != nil {
				return Value{}, err
			}
			arguments, err := e.arguments(expression.Arguments[1:])
			if err != nil {
				return Value{}, err
			}
			return e.method(receiver, stringValue(key), arguments, callee.Arguments[0])
		}
		function, err := e.expression(callee)
		if err != nil {
			return Value{}, err
		}
		arguments, err := e.arguments(expression.Arguments[1:])
		if err != nil {
			return Value{}, err
		}
		return e.call(function, arguments)
	case constants.WorkflowExprDefault:
		return e.expression(expression.Arguments[0])
	default:
		return Value{}, status_error.WorkflowStateInvalid
	}
}

func (e *evaluator) arguments(expressions []*Expression) ([]Value, error) {
	values := []Value{}
	for _, expression := range expressions {
		if expression.Kind == constants.WorkflowExprSpread {
			value, err := e.expression(expression.Arguments[0])
			if err != nil {
				return nil, err
			}
			items, err := arrayValues(value)
			if err != nil {
				return nil, err
			}
			values = append(values, items...)
		} else {
			value, err := e.expression(expression)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
	}
	return values, nil
}
func (e *evaluator) lookup(name string) (Binding, bool) {
	for index := len(e.state.Scopes) - 1; index >= 0; index-- {
		if value, ok := e.state.Scopes[index][name]; ok {
			return value, true
		}
	}
	return Binding{}, false
}

func (e *evaluator) bind(target *Expression, value Value, constant, declare bool) error {
	if target == nil {
		return nil
	}
	if target.Kind == constants.WorkflowExprDefault {
		if value.Kind == constants.WorkflowSlotMissing {
			var err error
			value, err = e.expression(target.Arguments[1])
			if err != nil {
				return err
			}
		}
		return e.bind(target.Arguments[0], value, constant, declare)
	}
	switch target.Kind {
	case constants.WorkflowExprIdentifier:
		if declare {
			scope := e.state.Scopes[len(e.state.Scopes)-1]
			if _, found := scope[target.Name]; found {
				return status_error.WorkflowStateInvalid.WithField("reason", "duplicate lexical binding: "+target.Name)
			}
			scope[target.Name] = Binding{Value: copyValue(value), Constant: constant}
			return nil
		}
		return e.assign(target, value)
	case constants.WorkflowExprArray:
		items, err := arrayValues(value)
		if err != nil {
			return err
		}
		for index, item := range target.Arguments {
			if item.Kind == constants.WorkflowExprSpread {
				start := index
				if start > len(items) {
					start = len(items)
				}
				return e.bind(item.Arguments[0], valuesArray(items[start:]), constant, declare)
			}
			current := Value{Kind: constants.WorkflowSlotMissing}
			if index < len(items) {
				current = items[index]
			}
			if err := e.bind(item, current, constant, declare); err != nil {
				return err
			}
		}
		return nil
	case constants.WorkflowExprObject:
		data, err := value.JSON()
		if err != nil {
			return err
		}
		object, ok := data.(map[string]any)
		if !ok {
			return status_error.WorkflowStateInvalid.WithField("reason", "object destructuring requires an object")
		}
		used := map[string]bool{}
		for _, property := range target.Properties {
			if property.Spread {
				rest := map[string]any{}
				for key, value := range object {
					if !used[key] {
						rest[key] = value
					}
				}
				if err := e.bind(property.Value, JSONValue(rest), constant, declare); err != nil {
					return err
				}
				continue
			}
			key, err := e.expression(property.Key)
			if err != nil {
				return err
			}
			name := stringValue(key)
			used[name] = true
			current := Value{Kind: constants.WorkflowSlotMissing}
			if data, found := object[name]; found {
				current = JSONValue(data)
			}
			if err := e.bind(property.Value, current, constant, declare); err != nil {
				return err
			}
		}
		return nil
	case constants.WorkflowExprMember:
		if declare {
			return status_error.WorkflowStateInvalid
		}
		return e.assign(target, value)
	default:
		return status_error.WorkflowStateInvalid
	}
}

func (e *evaluator) assign(target *Expression, value Value) error {
	if target.Kind == constants.WorkflowExprArray || target.Kind == constants.WorkflowExprObject || target.Kind == constants.WorkflowExprDefault {
		return e.bind(target, value, false, false)
	}
	if target.Kind == constants.WorkflowExprIdentifier {
		for index := len(e.state.Scopes) - 1; index >= 0; index-- {
			if binding, found := e.state.Scopes[index][target.Name]; found {
				if binding.Constant || binding.ReadOnly {
					return status_error.WorkflowStateInvalid.WithField("reason", "binding is immutable: "+target.Name)
				}
				binding.Value = copyValue(value)
				e.state.Scopes[index][target.Name] = binding
				return nil
			}
		}
		return status_error.WorkflowStateInvalid.WithField("reason", "assignment target is unbound: "+target.Name)
	}
	if target.Kind != constants.WorkflowExprMember {
		return status_error.WorkflowStateInvalid
	}
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

func (e *evaluator) assignmentPath(target *Expression) (string, []string, error) {
	if target.Kind == constants.WorkflowExprIdentifier {
		return target.Name, nil, nil
	}
	if target.Kind != constants.WorkflowExprMember {
		return "", nil, status_error.WorkflowStateInvalid.WithField("reason", "mutation requires a root binding")
	}
	root, path, err := e.assignmentPath(target.Arguments[0])
	if err != nil {
		return "", nil, err
	}
	key, err := e.expression(target.Arguments[1])
	if err != nil {
		return "", nil, err
	}
	name := stringValue(key)
	if forbiddenKey(name) {
		return "", nil, status_error.WorkflowStateInvalid
	}
	return root, append(path, name), nil
}
func setJSONPath(object any, path []string, value any) (any, error) {
	if len(path) == 0 {
		return cloneJSON(value), nil
	}
	switch data := object.(type) {
	case map[string]any:
		child, err := setJSONPath(data[path[0]], path[1:], value)
		if err != nil {
			return nil, err
		}
		data[path[0]] = child
		return data, nil
	case []any:
		index, err := strconv.Atoi(path[0])
		if err != nil || index < 0 || index >= len(data) {
			return nil, status_error.WorkflowStateInvalid.WithField("reason", "array assignment index is out of range")
		}
		child, err := setJSONPath(data[index], path[1:], value)
		if err != nil {
			return nil, err
		}
		data[index] = child
		return data, nil
	}
	return nil, status_error.WorkflowStateInvalid.WithField("reason", "property assignment requires an existing object or array")
}

func memberValue(value Value, key string, optional bool) (Value, error) {
	if forbiddenKey(key) {
		return Value{}, status_error.WorkflowStateInvalid
	}
	if isNullish(value) {
		if optional {
			return Value{Kind: constants.WorkflowSlotMissing}, nil
		}
		return Value{}, status_error.WorkflowStateInvalid.WithField("reason", "cannot access a null or missing value")
	}
	if value.Kind == constants.WorkflowSlotBuiltin {
		return Value{Kind: constants.WorkflowSlotBuiltin, Data: stringValue(value) + "." + key}, nil
	}
	if value.Kind == constants.WorkflowSlotTasks {
		if key == "length" {
			return JSONValue(len(value.Items)), nil
		}
		index, err := strconv.Atoi(key)
		if err == nil && index >= 0 && index < len(value.Items) {
			return copyValue(value.Items[index]), nil
		}
		return Value{Kind: constants.WorkflowSlotMissing}, nil
	}
	if value.Kind != constants.WorkflowSlotJSON {
		return Value{}, status_error.WorkflowStateInvalid
	}
	switch data := value.Data.(type) {
	case map[string]any:
		if item, found := data[key]; found {
			return JSONValue(item), nil
		}
	case []any:
		if key == "length" {
			return JSONValue(len(data)), nil
		}
		index, err := strconv.Atoi(key)
		if err == nil && index >= 0 && index < len(data) {
			return JSONValue(data[index]), nil
		}
	case string:
		if key == "length" {
			return JSONValue(utf8.RuneCountInString(data)), nil
		}
		index, err := strconv.Atoi(key)
		runes := []rune(data)
		if err == nil && index >= 0 && index < len(runes) {
			return JSONValue(string(runes[index])), nil
		}
	}
	return Value{Kind: constants.WorkflowSlotMissing}, nil
}

func truthy(value Value) bool {
	if value.Kind == constants.WorkflowSlotMissing {
		return false
	}
	if value.Kind != constants.WorkflowSlotJSON {
		return true
	}
	switch data := value.Data.(type) {
	case nil:
		return false
	case bool:
		return data
	case float64:
		return data != 0 && !math.IsNaN(data)
	case string:
		return data != ""
	default:
		return true
	}
}
func isNullish(value Value) bool {
	return value.Kind == constants.WorkflowSlotMissing || value.Kind == constants.WorkflowSlotJSON && value.Data == nil
}
func stringValue(value Value) string {
	if value.Kind == constants.WorkflowSlotMissing {
		return "undefined"
	}
	switch data := value.Data.(type) {
	case nil:
		return "null"
	case string:
		return data
	case bool:
		if data {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(data, 'f', -1, 64)
	default:
		encoded, _ := json.Marshal(data)
		return string(encoded)
	}
}
func numberValue(value Value) (float64, error) {
	if value.Kind != constants.WorkflowSlotJSON {
		return 0, status_error.WorkflowStateInvalid
	}
	switch data := value.Data.(type) {
	case nil:
		return 0, nil
	case float64:
		if math.IsNaN(data) || math.IsInf(data, 0) {
			return 0, status_error.WorkflowSchemaInvalid
		}
		return data, nil
	case bool:
		if data {
			return 1, nil
		}
		return 0, nil
	case string:
		if strings.TrimSpace(data) == "" {
			return 0, nil
		}
		number, err := strconv.ParseFloat(strings.TrimSpace(data), 64)
		if err == nil && !math.IsInf(number, 0) && !math.IsNaN(number) {
			return number, nil
		}
	}
	return 0, status_error.WorkflowSchemaInvalid.WithField("reason", "finite numeric value required")
}
func valueType(value Value) string {
	if value.Kind == constants.WorkflowSlotMissing {
		return "undefined"
	}
	if value.Kind == constants.WorkflowSlotFunction || value.Kind == constants.WorkflowSlotBuiltin {
		return "function"
	}
	switch value.Data.(type) {
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		return "number"
	}
	return "object"
}

func binaryValue(operator string, left, right Value) (Value, error) {
	if operator == "&&" || operator == "||" || operator == "??" {
		return right, nil
	}
	if operator == "==" || operator == "!=" || operator == "===" || operator == "!==" {
		leftObject, rightObject := jsonComposite(left), jsonComposite(right)
		if leftObject || rightObject {
			strict := operator == "===" || operator == "!=="
			if (leftObject && rightObject) || (!strict && !isNullish(left) && !isNullish(right)) {
				return Value{}, status_error.WorkflowCapabilityUnsupported.WithField("reason", "object identity comparison is not supported")
			}
			return JSONValue(operator == "!=" || operator == "!=="), nil
		}
		equal := left.Kind == right.Kind && reflect.DeepEqual(left.Data, right.Data)
		if operator == "==" || operator == "!=" {
			if isNullish(left) && isNullish(right) {
				equal = true
			} else if !equal && !isNullish(left) && !isNullish(right) {
				a, ae := numberValue(left)
				b, be := numberValue(right)
				equal = ae == nil && be == nil && a == b
			}
		}
		if operator == "!=" || operator == "!==" {
			equal = !equal
		}
		return JSONValue(equal), nil
	}
	if operator == "+" {
		_, ls := left.Data.(string)
		_, rs := right.Data.(string)
		if ls || rs {
			return JSONValue(stringValue(left) + stringValue(right)), nil
		}
	}
	if a, ok := left.Data.(string); ok {
		if b, ok := right.Data.(string); ok {
			switch operator {
			case "<":
				return JSONValue(a < b), nil
			case "<=":
				return JSONValue(a <= b), nil
			case ">":
				return JSONValue(a > b), nil
			case ">=":
				return JSONValue(a >= b), nil
			}
		}
	}
	a, err := numberValue(left)
	if err != nil {
		return Value{}, err
	}
	b, err := numberValue(right)
	if err != nil {
		return Value{}, err
	}
	var result float64
	switch operator {
	case "+":
		result = a + b
	case "-":
		result = a - b
	case "*":
		result = a * b
	case "/":
		result = a / b
	case "%":
		result = math.Mod(a, b)
	case "**":
		result = math.Pow(a, b)
	case "<":
		return JSONValue(a < b), nil
	case "<=":
		return JSONValue(a <= b), nil
	case ">":
		return JSONValue(a > b), nil
	case ">=":
		return JSONValue(a >= b), nil
	case "&":
		result = float64(int32(a) & int32(b))
	case "|":
		result = float64(int32(a) | int32(b))
	case "^":
		result = float64(int32(a) ^ int32(b))
	case "<<":
		result = float64(int32(a) << uint32(int32(b)&31))
	case ">>":
		result = float64(int32(a) >> uint32(int32(b)&31))
	case ">>>":
		result = float64(uint32(int32(a)) >> uint32(int32(b)&31))
	default:
		return Value{}, status_error.WorkflowCapabilityUnsupported.WithField("reason", "unsupported operator: "+operator)
	}
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return Value{}, status_error.WorkflowSchemaInvalid.WithField("reason", "arithmetic result is not finite")
	}
	return JSONValue(result), nil
}

// jsonComposite 区分具有引用身份的 JSON 容器与标量，允许明确的 null 和跨类型比较。
func jsonComposite(value Value) bool {
	if value.Kind != constants.WorkflowSlotJSON {
		return false
	}
	switch value.Data.(type) {
	case map[string]any, []any:
		return true
	}
	return false
}

func (e *evaluator) call(function Value, arguments []Value) (Value, error) {
	if function.Kind == constants.WorkflowSlotBuiltin {
		return e.builtin(stringValue(function), arguments)
	}
	if function.Kind != constants.WorkflowSlotFunction {
		return Value{}, status_error.WorkflowStateInvalid.WithField("reason", "value is not a pure function")
	}
	for _, id := range e.callChain {
		if id == function.ProgramID {
			return Value{}, status_error.WorkflowCapabilityUnsupported.WithField("reason", "recursive pure functions are not supported")
		}
	}
	if len(e.callChain) >= 64 {
		return Value{}, status_error.WorkflowBudgetExceeded
	}
	program, ok := e.bundle.Programs[function.ProgramID]
	if !ok || program.Async {
		return Value{}, status_error.WorkflowCapabilityUnsupported.WithField("reason", "async task cannot be invoked as a pure callback")
	}
	state := MachineState{ProgramID: function.ProgramID, Scopes: []map[string]Binding{copyValue(Value{Captures: function.Captures}).Captures, {}}, LoopCounters: map[string]int{}, StartedAt: e.state.StartedAt, PureSteps: e.state.PureSteps, CallChain: append(append([]string{}, e.callChain...), function.ProgramID)}
	defer func() {
		// 抛出或预算失败的纯函数也消耗真实预算，catch 不能把已执行的计算抹掉。
		e.state.PureSteps = state.PureSteps
		e.state.LoopSteps += state.LoopSteps
	}()
	child := evaluator{ctx: e.ctx, bundle: e.bundle, state: &state, callChain: append(append([]string{}, e.callChain...), function.ProgramID)}
	for index, parameter := range program.Parameters {
		value := Value{Kind: constants.WorkflowSlotMissing}
		if index < len(arguments) {
			value = arguments[index]
		}
		if err := child.bind(parameter, value, false, true); err != nil {
			return Value{}, err
		}
	}
	for !state.Done {
		boundary, err := advancePure(&child)
		if err != nil {
			return Value{}, err
		}
		if boundary != nil {
			return Value{}, status_error.WorkflowCapabilityUnsupported.WithField("reason", "pure helper cannot perform a host effect")
		}
	}
	if state.Failure != nil {
		return Value{}, status_error.WorkflowEffectFailed.WithParams(map[string]any{"workflowThrownValue": state.Failure.Data})
	}
	return state.Result, nil
}

// advancePure 使用相同 Go 指令解释器，但保留辅助调用链的递归检查。
func advancePure(e *evaluator) (*EffectBoundary, error) {
	return Advance(e.ctx, e.bundle, e.state, constants.WorkflowCheckpointSteps)
}

func (e *evaluator) builtin(name string, arguments []Value) (Value, error) {
	first := Value{Kind: constants.WorkflowSlotMissing}
	if len(arguments) > 0 {
		first = arguments[0]
	}
	switch name {
	case "Boolean":
		return JSONValue(truthy(first)), nil
	case "String":
		return JSONValue(stringValue(first)), nil
	case "Number":
		value, err := numberValue(first)
		return JSONValue(value), err
	case "Array.isArray":
		if first.Kind == constants.WorkflowSlotTasks {
			return JSONValue(true), nil
		}
		_, ok := first.Data.([]any)
		return JSONValue(ok), nil
	case "JSON.stringify":
		data, err := first.JSON()
		if err != nil {
			return Value{}, err
		}
		encoded, err := json.Marshal(data)
		if err != nil || len(encoded) > constants.WorkflowOutputLimitBytes {
			return Value{}, status_error.WorkflowBudgetExceeded
		}
		return JSONValue(string(encoded)), nil
	case "JSON.parse":
		text, ok := first.Data.(string)
		if !ok || len(text) > constants.WorkflowOutputLimitBytes {
			return Value{}, status_error.WorkflowSchemaInvalid
		}
		var value any
		if json.Unmarshal([]byte(text), &value) != nil {
			return Value{}, status_error.WorkflowSchemaInvalid.WithField("reason", "JSON.parse requires valid JSON")
		}
		return JSONValue(value), nil
	case "Error":
		return JSONValue(map[string]any{"code": "SOURCE_THROW", "message": stringValue(first)}), nil
	}
	if strings.HasPrefix(name, "Math.") {
		numbers := []float64{}
		for _, argument := range arguments {
			number, err := numberValue(argument)
			if err != nil {
				return Value{}, err
			}
			numbers = append(numbers, number)
		}
		if len(numbers) == 0 {
			return Value{}, status_error.WorkflowSchemaInvalid
		}
		a := numbers[0]
		var result float64
		switch strings.TrimPrefix(name, "Math.") {
		case "abs":
			result = math.Abs(a)
		case "floor":
			result = math.Floor(a)
		case "ceil":
			result = math.Ceil(a)
		case "round":
			result = math.Floor(a + 0.5)
		case "trunc":
			result = math.Trunc(a)
		case "sqrt":
			result = math.Sqrt(a)
		case "sign":
			if a > 0 {
				result = 1
			} else if a < 0 {
				result = -1
			}
		case "min":
			result = a
			for _, n := range numbers[1:] {
				result = math.Min(result, n)
			}
		case "max":
			result = a
			for _, n := range numbers[1:] {
				result = math.Max(result, n)
			}
		case "pow":
			if len(numbers) != 2 {
				return Value{}, status_error.WorkflowSchemaInvalid
			}
			result = math.Pow(a, numbers[1])
		default:
			return Value{}, status_error.WorkflowCapabilityUnsupported.WithField("reason", "unsupported deterministic Math operation: "+name)
		}
		if math.IsNaN(result) || math.IsInf(result, 0) {
			return Value{}, status_error.WorkflowSchemaInvalid
		}
		return JSONValue(result), nil
	}
	return Value{}, status_error.WorkflowCapabilityUnsupported.WithField("reason", fmt.Sprintf("unsupported pure call %s", name))
}
