package dsl

import (
	"reflect"
	"sort"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/token"
)

// IMTargetDependency 是从动作声明和执行程序派生的发送依赖，不执行用户代码。
type IMTargetDependency struct {
	// ActionID 是引用目标的确定性动作身份。
	ActionID string `json:"actionId"`
	// NodeID 是使用该动作的源码节点身份。
	NodeID string `json:"nodeId"`
	// Label 是节点的业务名称。
	Label string `json:"label"`
	// TargetID 是固定目标或明确声明的动态目标身份。
	TargetID string `json:"targetId"`
	// Range 定位发送节点的原始源码。
	Range SourceRange `json:"sourceRange"`
}

// ChildWorkflowDependency 是编译程序中的子流程资源引用，版本为空表示跟随当前版本。
type ChildWorkflowDependency struct {
	// DefinitionID 是被调用的子流程身份。
	DefinitionID string
	// RevisionID 是源码明确固定的子流程版本。
	RevisionID string
	// NodeID 是调用子流程的父节点身份。
	NodeID string
	// Label 是父调用节点的业务名称。
	Label string
	// Range 是父调用节点的源码位置。
	Range SourceRange
}

// ChildWorkflowDependencies 读取明确的子流程调用，供接纳与删除检查遍历同一资源图。
func ChildWorkflowDependencies(manifest Manifest, bundle ProgramBundle) []ChildWorkflowDependency {
	result := []ChildWorkflowDependency{}
	for _, instructions := range dependencyPrograms(bundle) {
		for _, instruction := range instructions {
			if instruction.Primitive != "workflow" && instruction.Primitive != "startWorkflow" {
				continue
			}
			if instruction.Expression == nil || len(instruction.Expression.Arguments) < 1 {
				continue
			}
			id := staticString(instruction.Expression.Arguments[0])
			if id == "" {
				continue
			}
			child := ChildWorkflowDependency{DefinitionID: id, NodeID: instruction.NodeID, Range: instruction.Range}
			if len(instruction.Expression.Arguments) > 1 {
				child.RevisionID = staticString(expressionProperty(instruction.Expression.Arguments[1], "revisionId"))
			}
			for _, node := range manifest.Nodes {
				if node.ID == instruction.NodeID {
					child.Label = node.LabelTemplate
				}
			}
			result = append(result, child)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].NodeID < result[j].NodeID })
	return result
}

// IMTargetDependencies 检查固定目标及动态范围；无法静态识别的 IM 调用必须声明 imTargetIds。
func IMTargetDependencies(manifest Manifest, bundle ProgramBundle) ([]IMTargetDependency, error) {
	result := []IMTargetDependency{}
	byNode := map[string]string{}
	programs := dependencyPrograms(bundle)
	for _, instructions := range programs {
		for _, instruction := range instructions {
			if instruction.Primitive == "action" && instruction.Expression != nil && len(instruction.Expression.Arguments) > 0 {
				byNode[instruction.NodeID] = staticString(instruction.Expression.Arguments[0])
			}
		}
	}
	for _, action := range manifest.Meta.Actions {
		if action.Type != "builtin" {
			continue
		}
		im := false
		for _, id := range action.ConnectorIDs {
			im = im || id == constants.IMBuiltinConnectorID
		}
		if !im {
			continue
		}
		targets := map[string]bool{}
		for _, id := range action.IMTargetIDs {
			if id == "" || len(id) > 32 {
				return nil, status_error.WorkflowSourceInvalid.WithField("reason", "invalid_im_target_dependency")
			}
			targets[id] = true
		}
		for _, instruction := range programs[bundle.ActionPrograms[action.Handler]] {
			if instruction.Primitive != "callTool" || instruction.Expression == nil || len(instruction.Expression.Arguments) != 1 {
				continue
			}
			call := instruction.Expression.Arguments[0]
			connector := staticString(expressionProperty(call, "connectorId"))
			tool := staticString(expressionProperty(call, "toolName"))
			if connector != "" && connector != constants.IMBuiltinConnectorID || tool != "" && tool != constants.IMBuiltinToolName {
				continue
			}
			input := expressionProperty(call, "input")
			operation := staticString(expressionProperty(input, "action"))
			if operation == constants.IMActionListTargets || operation == constants.IMActionGetDelivery {
				continue
			}
			target := staticString(expressionProperty(input, "targetId"))
			if target != "" {
				targets[target] = true
			} else if len(action.IMTargetIDs) == 0 {
				return nil, status_error.WorkflowSourceInvalid.WithField("reason", "dynamic_im_target_requires_declaration").WithField("field", action.ID)
			}
		}
		ids := []string{}
		for id := range targets {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, node := range manifest.Nodes {
			if node.Kind != "action" || byNode[node.ID] != action.ID {
				continue
			}
			for _, id := range ids {
				result = append(result, IMTargetDependency{ActionID: action.ID, NodeID: node.ID, Label: node.LabelTemplate, TargetID: id, Range: node.Range})
			}
		}
	}
	return result, nil
}

// dependencyPrograms 按实际词法位置解析绑定，并在任务创建处冻结它实际捕获的常量。
// 有写入的绑定始终视为动态，避免分支、循环后续轮次和 const 对象修改冒充固定目标。
func dependencyPrograms(bundle ProgramBundle) map[string][]Instruction {
	result := map[string][]Instruction{}
	var visit func(string, map[string]*Expression)
	visit = func(id string, inherited map[string]*Expression) {
		if _, found := result[id]; found {
			return
		}
		program := bundle.Programs[id]
		result[id] = []Instruction{}
		scopes := []map[string]*Expression{inherited, {}}
		for _, parameter := range program.Parameters {
			for _, name := range dependencyBindingNames(parameter) {
				scopes[1][name] = nil
			}
		}
		mutated := dependencyMutatedBindings(program)
		for pc, instruction := range program.Code {
			if instruction.Op == constants.WorkflowOpEnterScope {
				scopes = append(scopes, map[string]*Expression{})
			}
			if instruction.Op == constants.WorkflowOpLeaveScope && len(scopes) > 2 {
				scopes = scopes[:len(scopes)-1]
			}
			bindings := map[string]*Expression{}
			for _, scope := range scopes {
				for name, value := range scope {
					bindings[name] = value
				}
			}
			walkDependencyExpression(instruction.Expression, func(value *Expression) {
				if value.Kind != constants.WorkflowExprFunction {
					return
				}
				captures := map[string]*Expression{}
				for _, name := range bundle.Programs[value.Name].Captures {
					captures[name] = bindings[name]
				}
				visit(value.Name, captures)
			})
			instruction.Expression = resolveDependencyExpression(instruction.Expression, bindings, 0)
			if instruction.Op == constants.WorkflowOpDeclare && instruction.Target != nil {
				for _, name := range dependencyBindingNames(instruction.Target) {
					scopes[len(scopes)-1][name] = nil
				}
				if instruction.Target.Kind == constants.WorkflowExprIdentifier && !mutated[pc] && (instruction.Constant || len(instruction.Target.Name) > 0 && instruction.Target.Name[0] == '$') {
					scopes[len(scopes)-1][instruction.Target.Name] = instruction.Expression
				}
			}
			result[id] = append(result[id], instruction)
		}
	}
	visit(bundle.Entry, nil)
	for _, id := range bundle.ActionPrograms {
		visit(id, nil)
	}
	return result
}

// dependencyMutatedBindings 以声明位置区分同名局部变量；不执行分支或用户函数。
func dependencyMutatedBindings(program Program) map[int]bool {
	result := map[int]bool{}
	scopes := []map[string]int{{}}
	for pc, instruction := range program.Code {
		if instruction.Op == constants.WorkflowOpEnterScope {
			scopes = append(scopes, map[string]int{})
		}
		if instruction.Op == constants.WorkflowOpLeaveScope && len(scopes) > 1 {
			scopes = scopes[:len(scopes)-1]
		}
		walkDependencyExpression(instruction.Expression, func(value *Expression) {
			var target *Expression
			if len(value.Arguments) > 0 && (value.Kind == constants.WorkflowExprAssign || value.Kind == constants.WorkflowExprUnary && (value.Name == "++" || value.Name == "--")) {
				target = value.Arguments[0]
			}
			if value.Kind == constants.WorkflowExprCall && len(value.Arguments) > 0 {
				callee := value.Arguments[0]
				if callee.Kind == constants.WorkflowExprMember && len(callee.Arguments) == 2 && staticString(callee.Arguments[1]) == "push" {
					target = callee.Arguments[0]
				}
			}
			for _, name := range dependencyBindingNames(target) {
				for i := len(scopes) - 1; i >= 0; i-- {
					if declaration, found := scopes[i][name]; found {
						result[declaration] = true
						break
					}
				}
			}
		})
		if instruction.Op == constants.WorkflowOpDeclare {
			for _, name := range dependencyBindingNames(instruction.Target) {
				scopes[len(scopes)-1][name] = pc
			}
		}
	}
	return result
}

func dependencyBindingNames(value *Expression) []string {
	if value == nil {
		return nil
	}
	if value.Kind == constants.WorkflowExprIdentifier {
		return []string{value.Name}
	}
	if value.Kind == constants.WorkflowExprMember || value.Kind == constants.WorkflowExprDefault || value.Kind == constants.WorkflowExprSpread {
		if len(value.Arguments) > 0 {
			return dependencyBindingNames(value.Arguments[0])
		}
		return nil
	}
	names := []string{}
	if value.Kind == constants.WorkflowExprArray {
		for _, argument := range value.Arguments {
			names = append(names, dependencyBindingNames(argument)...)
		}
	}
	if value.Kind == constants.WorkflowExprObject {
		for _, property := range value.Properties {
			names = append(names, dependencyBindingNames(property.Value)...)
		}
	}
	return names
}

func walkDependencyExpression(value *Expression, visit func(*Expression)) {
	if value == nil {
		return
	}
	visit(value)
	for _, argument := range value.Arguments {
		walkDependencyExpression(argument, visit)
	}
	for _, property := range value.Properties {
		walkDependencyExpression(property.Key, visit)
		walkDependencyExpression(property.Value, visit)
	}
}

func resolveDependencyExpression(value *Expression, bindings map[string]*Expression, depth int) *Expression {
	if value == nil || depth > 64 {
		return nil
	}
	if value.Kind == constants.WorkflowExprIdentifier {
		if binding := bindings[value.Name]; binding != nil {
			return resolveDependencyExpression(binding, bindings, depth+1)
		}
		return value
	}
	copy := *value
	copy.Arguments = nil
	copy.Properties = nil
	for _, argument := range value.Arguments {
		copy.Arguments = append(copy.Arguments, resolveDependencyExpression(argument, bindings, depth+1))
	}
	for _, property := range value.Properties {
		copy.Properties = append(copy.Properties, Property{Key: resolveDependencyExpression(property.Key, bindings, depth+1), Value: resolveDependencyExpression(property.Value, bindings, depth+1), Spread: property.Spread})
	}
	if copy.Kind == constants.WorkflowExprMember && len(copy.Arguments) == 2 {
		return expressionProperty(copy.Arguments[0], staticString(copy.Arguments[1]))
	}
	return &copy
}

func expressionProperty(value *Expression, name string) *Expression {
	result, _ := dependencyProperty(value, name)
	return result
}

func dependencyProperty(value *Expression, name string) (*Expression, bool) {
	if value == nil || value.Kind != constants.WorkflowExprObject {
		return nil, false
	}
	var result *Expression
	found := false
	for _, property := range value.Properties {
		if property.Spread || staticString(property.Key) == "" {
			return nil, false
		}
		if staticString(property.Key) == name {
			result = property.Value
			found = true
		}
	}
	return result, found
}

func staticString(value *Expression) string {
	text, _ := staticDependencyString(value)
	return text
}

func staticDependencyString(value *Expression) (string, bool) {
	if value == nil {
		return "", false
	}
	if value.Kind == constants.WorkflowExprTemplate && len(value.Arguments) == 1 {
		return staticDependencyString(value.Arguments[0])
	}
	if value.Kind != constants.WorkflowExprLiteral {
		return "", false
	}
	text, ok := value.Value.(string)
	return text, ok
}

func (c *compiler) compileActions() {
	declaration, exists := c.constants["actions"]
	object, ok := declaration.(*ast.ObjectLiteral)
	if exists && !ok {
		c.fail(declaration, "ACTION_HANDLER_INVALID", "actions must be a literal map of independent handlers")
		return
	}
	if c.bundle.ActionPrograms == nil {
		c.bundle.ActionPrograms = map[string]string{}
	}
	if !exists {
		for _, action := range c.manifest.Meta.Actions {
			if action.Type == "builtin" {
				c.fail(nil, "ACTION_HANDLER_MISSING", "builtin handler is missing: "+action.Handler)
			}
		}
		return
	}
	for _, property := range object.Value {
		entry, ok := property.(*ast.PropertyKeyed)
		if !ok || entry.Computed || entry.Kind != ast.PropertyKindValue {
			c.fail(property, "ACTION_HANDLER_INVALID", "handlers need static keys and function values")
			continue
		}
		name, ok := propertyName(entry.Key)
		if !ok || forbiddenKey(name) || c.bundle.ActionPrograms[name] != "" {
			c.fail(entry, "ACTION_HANDLER_INVALID", "handler names must be unique static keys")
			continue
		}
		var parameters *ast.ParameterList
		var body ast.ConciseBody
		switch function := entry.Value.(type) {
		case *ast.ArrowFunctionLiteral:
			parameters, body = function.ParameterList, function.Body
		case *ast.FunctionLiteral:
			parameters, body = function.ParameterList, function.Body
		default:
			c.fail(entry, "ACTION_HANDLER_INVALID", "handler must be a function")
			continue
		}
		if parameters.Rest != nil || len(parameters.List) != 2 {
			c.fail(entry, "ACTION_HANDLER_INVALID", "handler needs ctx and input parameters")
			continue
		}
		contextName, ok := parameters.List[0].Target.(*ast.Identifier)
		if !ok {
			c.fail(entry, "ACTION_HANDLER_INVALID", "ctx must be a named parameter")
			continue
		}
		isolated := &symbolScope{names: map[string]bool{}}
		for _, global := range []string{"undefined", "Math", "JSON", "Object", "Array", "String", "Number", "Boolean", "Date", "Error"} {
			isolated.names[global] = true
		}
		c.symbolFunction(parameters, body, isolated, false, "", entry.Value)
		builder := &codeBuilder{compiler: c, actionContext: contextName.Name.String()}
		bindings := []*Expression{}
		for _, parameter := range parameters.List {
			bindings = append(bindings, builder.expression(parameter))
		}
		switch body := body.(type) {
		case *ast.BlockStatement:
			for _, statement := range body.List {
				builder.statement(statement)
			}
		case *ast.ExpressionBody:
			builder.emit(Instruction{Op: constants.WorkflowOpReturn, Expression: builder.expression(body.Expression)})
		}
		builder.emit(Instruction{Op: constants.WorkflowOpReturn, Expression: literalExpression(nil)})
		id := "action." + name
		c.bundle.Programs[id] = Program{Code: builder.code, Parameters: bindings, Async: true}
		c.bundle.ActionPrograms[name] = id
	}
	for _, action := range c.manifest.Meta.Actions {
		if action.Type == "builtin" && c.bundle.ActionPrograms[action.Handler] == "" {
			c.fail(declaration, "ACTION_HANDLER_MISSING", "builtin handler is missing: "+action.Handler)
		}
	}
}
func (b *codeBuilder) actionCall(call *ast.CallExpression, awaited bool) *Expression {
	member, ok := call.Callee.(*ast.DotExpression)
	if !ok || b.actionContext == "" {
		return nil
	}
	identifier, ok := member.Left.(*ast.Identifier)
	if !ok || identifier.Name.String() != b.actionContext {
		return nil
	}
	name := member.Identifier.Name.String()
	if name != "callTool" && name != "log" {
		return nil
	}
	if name == "callTool" && !awaited {
		b.compiler.fail(call, "AWAIT_REQUIRED", "await ctx.callTool")
	}
	if len(call.ArgumentList) != 1 {
		b.compiler.fail(call, "ARGUMENT_COUNT_INVALID", "ctx capability requires one argument")
	}
	args := []*Expression{}
	for _, argument := range call.ArgumentList {
		args = append(args, b.expression(argument))
	}
	destination := b.compiler.temporary()
	b.emit(Instruction{Op: constants.WorkflowOpEffect, Primitive: name, Destination: destination, Expression: &Expression{Kind: constants.WorkflowExprArray, Arguments: args}, Range: b.compiler.parsed.sourceRange(call)})
	return identifierExpression(destination)
}

// ActionSourceDetail 是从固定版本源码派生的只读操作实现，不读取或执行外部脚本。
type ActionSourceDetail struct {
	// ActionID 关联源码实际引用的动作声明，支持顶层常量引用。
	ActionID string `json:"actionId"`
	// CallSource 保留节点调用的完整原文，包括动态参数表达式。
	CallSource string `json:"callSource"`
	// HandlerSource 保留内置处理函数的完整原文，外部命令和 HTTP 操作没有此字段。
	HandlerSource string `json:"handlerSource,omitempty"`
}

// InspectActionSources 根据同版本 Manifest 的节点位置提取真实调用及内置处理函数。
func InspectActionSources(source string, manifest Manifest) (map[string]ActionSourceDetail, error) {
	nodes := map[int]Node{}
	for _, node := range manifest.Nodes {
		if node.Kind == "action" {
			nodes[node.Range.Start.Offset] = node
		}
	}
	if len(nodes) == 0 {
		return nil, nil
	}
	parsed, _, err := parseSource(source)
	if err != nil {
		return nil, err
	}
	compiled, _, err := Compile(source)
	if err != nil {
		return nil, err
	}
	actionIDs := map[string]string{}
	for _, program := range dependencyPrograms(compiled.Bundle) {
		for _, instruction := range program {
			if instruction.Primitive == "action" && instruction.Expression != nil && len(instruction.Expression.Arguments) > 0 {
				actionIDs[instruction.NodeID] = staticString(instruction.Expression.Arguments[0])
			}
		}
	}
	c := &compiler{parsed: parsed, constants: map[string]ast.Expression{}}
	for _, statement := range parsed.body {
		if declaration, ok := statement.(*ast.LexicalDeclaration); ok && declaration.Token == token.CONST {
			for _, binding := range declaration.List {
				if id, ok := binding.Target.(*ast.Identifier); ok {
					c.constants[id.Name.String()] = binding.Initializer
				}
			}
		}
	}
	declarations := map[string]ActionDeclaration{}
	for _, declaration := range manifest.Meta.Actions {
		declarations[declaration.ID] = declaration
	}
	handlers, _ := c.constants["actions"].(*ast.ObjectLiteral)
	details := map[string]ActionSourceDetail{}
	astContains(reflect.ValueOf(parsed.body), func(value ast.Node) bool {
		call, ok := value.(*ast.CallExpression)
		if !ok || len(call.ArgumentList) != 2 {
			return false
		}
		node, found := nodes[parsed.sourceRange(call).Start.Offset]
		if !found || parsed.sourceRange(call).End.Offset != node.Range.End.Offset {
			return false
		}
		actionID := actionIDs[node.ID]
		declaration, found := declarations[actionID]
		if !found {
			return false
		}
		detail := ActionSourceDetail{ActionID: actionID, CallSource: c.fragment(call)}
		if declaration.Type == "builtin" {
			handler := editProperty(handlers, declaration.Handler)
			if handler == nil {
				return false
			}
			detail.HandlerSource = c.fragment(handler)
		}
		details[node.ID] = detail
		return false
	})
	if len(details) != len(nodes) {
		return nil, status_error.WorkflowStateInvalid
	}
	return details, nil
}
