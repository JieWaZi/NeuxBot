package dsl

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/dop251/goja/ast"
)

// CanvasManifest 是编译器对外提供的只读画布，不包含内部调度字段。
type CanvasManifest struct {
	// Title 是当前版本的业务名称。
	Title string `json:"title"`
	// Description 说明本流程交付的结果。
	Description string `json:"description"`
	// InputPropertyOrder 仅保留表单字段的源码顺序。
	InputPropertyOrder map[string][]string `json:"inputPropertyOrder,omitempty"`
	// Actions 是只读动作声明，供详情显示能力与风险。
	Actions []ActionDeclaration `json:"actions,omitempty"`

	// SDK 指明权威源码的语言契约。
	SDK string `json:"sdk"`
	// Phases 是用户可读的展示阶段。
	Phases []CanvasPhase `json:"phases"`
	// Nodes 是从完整源码派生的节点模板。
	Nodes []CanvasNode `json:"nodes"`
	// Edges 是从控制拓扑和数据流派生的阅读连线。
	Edges []CanvasEdge `json:"edges"`
	// Controls 是唯一的嵌套控制拓扑。
	Controls []CanvasControl `json:"controls"`
	// Scripts 只描述脚本身份、协议及字节摘要。
	Scripts []ScriptSummary `json:"scripts"`
	// InputSchema 校验启动参数。
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	// OutputSchema 校验最终业务结果。
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
}

// CanvasPhase 只保留阶段的业务说明，不重复派生控制连线。
type CanvasPhase struct {
	// ID 是源码声明的稳定阶段身份。
	ID string `json:"id"`
	// Title 是阶段的显示名称。
	Title string `json:"title"`
	// Detail 说明本阶段负责的业务范围。
	Detail string `json:"detail"`
}

// CanvasNode 是按 kind 使用封闭 execution 联合的只读调用模板。
type CanvasNode struct {
	// Description 用一句话说明本节点负责的业务作用。
	Description string `json:"description,omitempty"`
	// ID 是稳定节点身份或保留的输入结果身份。
	ID string `json:"id"`
	// Kind 区分系统节点与宿主调用原语。
	Kind string `json:"kind"`
	// Label 是静态解析后供用户阅读的业务名称。
	Label string `json:"label"`
	// PhaseID 关联业务展示阶段，系统节点省略。
	PhaseID string `json:"phaseId,omitempty"`
	// SourceRange 定位同版本原始源码，系统节点省略。
	SourceRange *SourceRange `json:"sourceRange,omitempty"`
	// Inputs 保留输入表达式及词法数据来源。
	Inputs []CanvasInput `json:"inputs"`
	// OutputSchema 校验直接业务返回值。
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	// SuccessCondition 是显式业务检查声明。
	SuccessCondition *SuccessCondition `json:"successCondition,omitempty"`
	// Execution 由 CanvasManifestSchema 按 kind 封闭校验，不能交给执行器作为命令。
	Execution map[string]any `json:"execution"`
}

// ExportCanvas 从冻结的内部编译产物提取公开画布，避免向页面暴露第二套旧字段。
func ExportCanvas(manifest Manifest) CanvasManifest {
	result := CanvasManifest{Title: manifest.Meta.Title, Description: manifest.Meta.Description, InputPropertyOrder: manifest.InputPropertyOrder, Actions: manifest.Meta.Actions, SDK: manifest.SDK, Phases: []CanvasPhase{}, Nodes: []CanvasNode{}, Edges: manifest.Edges, Controls: manifest.Controls, Scripts: manifest.Scripts, InputSchema: manifest.InputSchema, OutputSchema: manifest.OutputSchema}
	for _, phase := range manifest.Phases {
		result.Phases = append(result.Phases, CanvasPhase{ID: phase.ID, Title: phase.Title, Detail: phase.Detail})
	}
	for _, node := range manifest.Nodes {
		result.Nodes = append(result.Nodes, CanvasNode{Description: node.Description, ID: node.ID, Kind: node.Kind, Label: node.Label, PhaseID: node.PhaseID, SourceRange: node.SourceRange, Inputs: node.Inputs, OutputSchema: node.OutputSchema, SuccessCondition: node.SuccessCondition, Execution: node.Execution})
	}
	return result
}

// CanvasInput 描述一个输入表达式及其已知的数据来源。
type CanvasInput struct {
	// Target 是该表达式绑定的参数或字段路径。
	Target string `json:"target"`
	// Expression 是完整原始源码表达式，浏览器不执行它。
	Expression string `json:"expression"`
	// DependsOn 是通过词法数据流确定的上游节点。
	DependsOn []string `json:"dependsOn"`
}

// CanvasEdge 是从源码控制和数据关系派生的阅读连线。
type CanvasEdge struct {
	// ID 是当前版本内确定的连线身份。
	ID string `json:"id"`
	// From 是起点节点身份。
	From string `json:"from"`
	// To 是终点节点身份。
	To string `json:"to"`
	// Kind 区分控制顺序与数据依赖。
	Kind string `json:"kind"`
	// Condition 保留路径条件的源码。
	Condition string `json:"condition,omitempty"`
	// BindingPath 标识数据绑定目标。
	BindingPath string `json:"bindingPath,omitempty"`
}

// CanvasControl 是源码唯一的嵌套控制拓扑，不参与执行调度。
type CanvasControl struct {
	// ID 是此版本的确定控制身份。
	ID string `json:"id"`
	// Kind 区分调用、分支、循环和任务分组。
	Kind string `json:"kind"`
	// NodeID 指向可展示的实际调用节点。
	NodeID string `json:"nodeId,omitempty"`
	// ConditionExpression 保留判断或迭代对象的源码。
	ConditionExpression string `json:"conditionExpression,omitempty"`
	// Branches 保留各控制路径及原始顺序。
	Branches []CanvasBranch `json:"branches"`
}

// CanvasBranch 是一条互斥路径、循环体或分组任务。
type CanvasBranch struct {
	// Label 描述 true、false、body、catch 或任务序号。
	Label string `json:"label"`
	// Children 是此路径内依次执行的控制模板。
	Children []CanvasControl `json:"children"`
}

// canvasProjector 在编译后读取 AST，不运行用户表达式。
type canvasProjector struct {
	// compiler 提供同版本源码、执行程序和已验证节点。
	compiler *compiler
	// nodes 用源码位置关联稳定节点身份。
	nodes map[int]int
	// scopes 按词法层次保存变量来源与静态 callback，分支合并不泄漏局部声明。
	scopes []map[string]canvasBinding
	// returns 只保存主流程返回表达式，任务或纯函数 return 不成为最终结果。
	returns []CanvasInput
	// callbackDepth 区分主流程和独立函数体。
	callbackDepth int
}

func (c *compiler) projectCanvas() {
	m := &c.manifest
	m.SDK = constants.WorkflowSDKIdentifier
	m.Phases = m.Meta.Phases
	m.InputSchema = m.Meta.InputSchema
	m.OutputSchema = m.Meta.OutputSchema
	m.Scripts = scriptSummaries(c.bundle.Scripts)
	m.Edges = []CanvasEdge{}
	p := &canvasProjector{compiler: c, nodes: map[int]int{}, scopes: []map[string]canvasBinding{{"args": {dependencies: []string{"__workflow_input__"}}}}}
	actions := map[string]ActionDeclaration{}
	for _, action := range m.Meta.Actions {
		actions[action.ID] = action
	}
	instructions := map[string]Instruction{}
	for _, program := range dependencyPrograms(c.bundle) {
		for _, instruction := range program {
			if instruction.NodeID != "" {
				instructions[instruction.NodeID] = instruction
			}
		}
	}

	var appendJoins func([]ControlHint)
	appendJoins = func(hints []ControlHint) {
		for _, hint := range hints {
			if hint.Kind == constants.WorkflowControlKindJoinWorkflow {
				m.Nodes = append(m.Nodes, Node{ID: hint.ID, Kind: "joinWorkflow", PhaseID: hint.PhaseID, LabelTemplate: "等待子流程", Range: hint.Range, Options: map[string]any{"handleExpression": hint.Condition}})
			}
			for _, branch := range hint.Branches {
				appendJoins(branch.Hints)
			}
		}
	}
	appendJoins(m.ControlHints)
	for i := range m.Nodes {
		node := &m.Nodes[i]
		p.nodes[node.Range.Start.Offset] = i
		node.Label = node.LabelTemplate
		node.Description, _ = node.Options["description"].(string)
		location := node.Range
		node.SourceRange = &location
		node.Inputs = []CanvasInput{}
		node.Execution = map[string]any{}
		if schema, ok := node.Options["schema"].(map[string]any); ok {
			node.OutputSchema = schema
		}
		if raw, ok := node.Options["successCondition"].(map[string]any); ok {
			path, _ := raw["path"].(string)
			node.SuccessCondition = &SuccessCondition{Path: path, Equals: raw["equals"]}
		}
		switch node.Kind {
		case "joinWorkflow":
			node.Execution["handleExpression"] = node.Options["handleExpression"]
		case "worker":
			copyCanvasOptions(node.Execution, node.Options, "resolve", "toolPolicy")
			node.Execution["promptTemplate"] = node.PromptTemplate
		case "askUser":
			copyCanvasOptions(node.Execution, node.Options, "kind", "allowCustom", "allowSkip", "onTimeout", "timeoutMs", "minSelections", "maxSelections", "schema")
			if node.Execution["kind"] == nil {
				node.Execution["kind"] = "text"
			}
			node.Execution["promptTemplate"] = node.PromptTemplate
		case "action":
			instruction := instructions[node.ID]
			actionID := ""
			if instruction.Expression != nil && len(instruction.Expression.Arguments) > 0 {
				actionID = staticString(instruction.Expression.Arguments[0])
			}
			declaration := actions[actionID]
			if _, declared := node.Options["description"]; !declared {
				node.Description = declaration.Description
			}
			node.Execution["actionId"] = actionID
			node.Execution["actionType"] = declaration.Type
			if declaration.ScriptID != "" {
				node.Execution["scriptId"] = declaration.ScriptID
			}
			if declaration.Executable != "" {
				node.Execution["executable"] = declaration.Executable
			}
			if declaration.Handler != "" {
				node.Execution["handler"] = declaration.Handler
			}
			if len(declaration.AllowedOrigins) > 0 {
				node.Execution["allowedOrigins"] = declaration.AllowedOrigins
			}
			if len(declaration.Methods) > 0 {
				node.Execution["methods"] = declaration.Methods
			}
			node.OutputSchema = declaration.OutputSchema
		case "functionTask":
			copyCanvasOptions(node.Execution, node.Options, "language", "executionMode")
			if node.Execution["executionMode"] == nil {
				node.Execution["executionMode"] = "pure"
			}
			code, _ := node.Options["code"].(string)
			node.Execution["codeDigest"] = Digest([]byte(code))
			node.OutputSchema, _ = node.Options["outputSchema"].(map[string]any)
		case "workflow", "startWorkflow":
			instruction := instructions[node.ID]
			if instruction.Expression != nil && len(instruction.Expression.Arguments) > 0 {
				node.Execution["definitionId"] = staticString(instruction.Expression.Arguments[0])
			}
			copyCanvasOptions(node.Execution, node.Options, "revisionId", "mode")
			if node.Execution["mode"] == nil {
				node.Execution["mode"] = "joined"
			}
		}
	}
	p.statements(c.parsed.body)
	m.Controls = canvasControls(m.ControlHints)
	input := Node{ID: "__workflow_input__", Kind: "input", Label: "运行输入", LabelTemplate: "运行输入", Inputs: []CanvasInput{}, Options: map[string]any{}, Execution: map[string]any{"target": "args"}, OutputSchema: m.InputSchema}
	result := Node{ID: "__workflow_result__", Kind: "result", Label: "运行结果", LabelTemplate: "运行结果", Inputs: []CanvasInput{}, Options: map[string]any{}, Execution: map[string]any{"target": "result"}, OutputSchema: m.OutputSchema}
	result.Inputs = p.returns

	for i := range m.Nodes {
		m.Nodes[i].Inputs = uniqueCanvasInputs(m.Nodes[i].Inputs)
	}
	result.Inputs = uniqueCanvasInputs(result.Inputs)
	m.Nodes = append(m.Nodes, input, result)
	for _, node := range m.Nodes {
		for _, binding := range node.Inputs {
			for _, from := range binding.DependsOn {
				if from != node.ID {
					m.Edges = append(m.Edges, CanvasEdge{From: from, To: node.ID, Kind: "data", BindingPath: binding.Target})
				}
			}
		}
	}
	ends := c.canvasControlEdges(m.Controls, []string{input.ID}, "")
	for _, from := range ends {
		if from != result.ID {
			m.Edges = append(m.Edges, CanvasEdge{From: from, To: result.ID, Kind: "control"})
		}
	}
	seen := map[string]bool{}
	edges := []CanvasEdge{}
	for _, edge := range m.Edges {
		key := edge.From + "\x00" + edge.To + "\x00" + edge.Kind + "\x00" + edge.Condition + "\x00" + edge.BindingPath
		if !seen[key] {
			seen[key] = true
			edge.ID = fmt.Sprintf("edge.%d", len(edges)+1)
			edges = append(edges, edge)
		}
	}
	m.Edges = edges
}

func copyCanvasOptions(target, source map[string]any, keys ...string) {
	for _, key := range keys {
		if value, ok := source[key]; ok {
			target[key] = value
		}
	}
}

func canvasControls(hints []ControlHint) []CanvasControl {
	result := []CanvasControl{}
	for _, hint := range hints {
		kind := string(hint.Kind)
		nodeID := hint.NodeID
		switch hint.Kind {
		case constants.WorkflowControlKindNode:
			kind = "call"
		case constants.WorkflowControlKindReturn:
			kind = "call"
			nodeID = "__workflow_result__"
		case constants.WorkflowControlKindTaskReturn, constants.WorkflowControlKindBreak, constants.WorkflowControlKindContinue, constants.WorkflowControlKindThrow:
			continue
		case constants.WorkflowControlKindJoinWorkflow:
			kind = "call"
			nodeID = hint.ID
		}
		control := CanvasControl{ID: hint.ID, Kind: kind, NodeID: nodeID, ConditionExpression: hint.Condition, Branches: []CanvasBranch{}}
		for _, branch := range hint.Branches {
			control.Branches = append(control.Branches, CanvasBranch{Label: branch.Label, Children: canvasControls(branch.Hints)})
		}
		result = append(result, control)
	}
	return result
}

func (c *compiler) canvasControlEdges(controls []CanvasControl, previous []string, condition string) []string {
	return c.canvasControlEdgesWithReturns(controls, previous, condition, nil)
}

func (c *compiler) canvasControlEdgesWithReturns(controls []CanvasControl, previous []string, condition string, pendingReturns *[]string) []string {
	frontier := append([]string{}, previous...)
	for _, control := range controls {
		if control.Kind == "call" {
			if control.NodeID == "__workflow_result__" && pendingReturns != nil {
				*pendingReturns = append(*pendingReturns, frontier...)
				return nil
			}
			for _, from := range frontier {
				if from != control.NodeID {
					c.manifest.Edges = append(c.manifest.Edges, CanvasEdge{From: from, To: control.NodeID, Kind: "control", Condition: condition})
				}
			}
			frontier = []string{control.NodeID}
			if control.NodeID == "__workflow_result__" {
				return nil
			}
			continue
		}
		if control.Kind == "try" {
			var finally []CanvasControl
			hasFinally := false
			for _, branch := range control.Branches {
				if branch.Label == constants.WorkflowControlPathFinally {
					finally = branch.Children
					hasFinally = true
				}
			}
			if hasFinally {
				returned := []string{}
				normal := []string{}
				for _, branch := range control.Branches {
					if branch.Label != constants.WorkflowControlPathFinally {
						normal = append(normal, c.canvasControlEdgesWithReturns(branch.Children, frontier, condition, &returned)...)
					}
				}
				frontier = c.canvasControlEdgesWithReturns(finally, uniqueCanvasIDs(append(append([]string{}, normal...), returned...)), condition, pendingReturns)
				if len(returned) > 0 {
					if pendingReturns != nil {
						*pendingReturns = append(*pendingReturns, frontier...)
					} else {
						for _, from := range frontier {
							c.manifest.Edges = append(c.manifest.Edges, CanvasEdge{From: from, To: "__workflow_result__", Kind: "control", Condition: "return"})
						}
					}
				}
				if len(normal) == 0 {
					frontier = nil
				}
				continue
			}
		}
		if control.Kind == "pipeline" {
			for _, branch := range control.Branches {
				frontier = c.canvasControlEdgesWithReturns(branch.Children, frontier, condition, pendingReturns)
			}
			continue
		}
		before := append([]string{}, frontier...)
		ends := []string{}
		for _, branch := range control.Branches {
			if control.Kind == "try" && branch.Label == constants.WorkflowControlPathFinally {
				continue
			}
			label := condition
			if control.ConditionExpression != "" {
				label = control.ConditionExpression + " [" + branch.Label + "]"
			}
			ends = append(ends, c.canvasControlEdgesWithReturns(branch.Children, frontier, label, pendingReturns)...)
		}

		if len(control.Branches) > 0 {
			frontier = uniqueCanvasIDs(ends)
		}
		if control.Kind == "loop" {
			frontier = uniqueCanvasIDs(append(frontier, before...))
		}

	}
	return frontier
}

func uniqueCanvasIDs(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func uniqueCanvasInputs(values []CanvasInput) []CanvasInput {
	result := []CanvasInput{}
	indexes := map[string]int{}
	for _, value := range values {
		key := value.Target + "\x00" + value.Expression
		if index, exists := indexes[key]; exists {
			result[index].DependsOn = uniqueCanvasIDs(append(result[index].DependsOn, value.DependsOn...))
			continue
		}
		indexes[key] = len(result)
		result = append(result, value)
	}
	return result
}

// canvasBinding 将一个词法声明关联到静态数据来源及可识别的函数体。
type canvasBinding struct {
	// dependencies 是此变量可能来自的稳定节点。
	dependencies []string
	// expression 保留可被后续分组调用的静态函数或任务数组。
	expression ast.Expression
}

// CanvasDigest 对实际公开画布的确定 JSON 字节取摘要，与内部 Manifest 完整性摘要分别计算。
func CanvasDigest(manifest CanvasManifest) string {
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return ""
	}
	return Digest(encoded)
}

func (p *canvasProjector) lookup(name string) canvasBinding {
	for index := len(p.scopes) - 1; index >= 0; index-- {
		if value, found := p.scopes[index][name]; found {
			return value
		}
	}
	return canvasBinding{}
}
func cloneCanvasScopes(scopes []map[string]canvasBinding) []map[string]canvasBinding {
	result := make([]map[string]canvasBinding, len(scopes))
	for index, scope := range scopes {
		result[index] = map[string]canvasBinding{}
		for name, value := range scope {
			value.dependencies = append([]string{}, value.dependencies...)
			result[index][name] = value
		}
	}
	return result
}
func mergeCanvasScopes(base, first, second []map[string]canvasBinding) []map[string]canvasBinding {
	result := cloneCanvasScopes(base)
	for index, scope := range result {
		for name, value := range scope {
			if entry, found := first[index][name]; found {
				value.dependencies = append(value.dependencies, entry.dependencies...)
			}
			if entry, found := second[index][name]; found {
				value.dependencies = append(value.dependencies, entry.dependencies...)
			}
			value.dependencies = uniqueCanvasIDs(value.dependencies)
			scope[name] = value
		}
	}
	return result
}
func (p *canvasProjector) declare(target ast.Expression, dependencies []string, expression ast.Expression) {
	switch node := target.(type) {
	case *ast.Identifier:
		p.scopes[len(p.scopes)-1][node.Name.String()] = canvasBinding{dependencies: dependencies, expression: expression}
	case *ast.Binding:
		p.declare(node.Target, dependencies, expression)
	case *ast.ArrayPattern:
		for _, element := range node.Elements {
			p.declare(element, dependencies, nil)
		}
		p.declare(node.Rest, dependencies, nil)
	case *ast.ObjectPattern:
		for _, property := range node.Properties {
			switch field := property.(type) {
			case *ast.PropertyKeyed:
				p.declare(field.Value, dependencies, nil)
			case *ast.PropertyShort:
				p.declare(&field.Name, dependencies, nil)
			}
		}
		p.declare(node.Rest, dependencies, nil)
	case *ast.AssignExpression:
		p.declare(node.Left, dependencies, expression)
	}
}
func (p *canvasProjector) assign(target ast.Expression, dependencies []string) {
	if id, ok := target.(*ast.Identifier); ok {
		for index := len(p.scopes) - 1; index >= 0; index-- {
			if entry, found := p.scopes[index][id.Name.String()]; found {
				entry.dependencies = dependencies
				p.scopes[index][id.Name.String()] = entry
				return
			}
		}
	}
}
func (p *canvasProjector) dependencies(expression ast.Expression) []string {
	result := []string{}
	add := func(value ast.Expression) { result = append(result, p.dependencies(value)...) }
	switch node := expression.(type) {
	case *ast.Identifier:
		result = append(result, p.lookup(node.Name.String()).dependencies...)
	case *ast.AwaitExpression:
		add(node.Argument)
	case *ast.DotExpression:
		add(node.Left)
	case *ast.BracketExpression:
		add(node.Left)
		add(node.Member)
	case *ast.OptionalChain:
		add(node.Expression)
	case *ast.Optional:
		add(node.Expression)
	case *ast.CallExpression:
		if index, found := p.nodes[p.compiler.parsed.sourceRange(node).Start.Offset]; found {
			return []string{p.compiler.manifest.Nodes[index].ID}
		}
		add(node.Callee)
		for _, argument := range node.ArgumentList {
			add(argument)
		}
	case *ast.ArrayLiteral:
		for _, value := range node.Value {
			add(value)
		}
	case *ast.ObjectLiteral:
		for _, property := range node.Value {
			switch field := property.(type) {
			case *ast.PropertyKeyed:
				if field.Computed {
					add(field.Key)
				}
				add(field.Value)
			case *ast.PropertyShort:
				add(&field.Name)
			case *ast.SpreadElement:
				add(field.Expression)
			}
		}
	case *ast.AssignExpression:
		add(node.Right)
	case *ast.BinaryExpression:
		add(node.Left)
		add(node.Right)
	case *ast.UnaryExpression:
		add(node.Operand)
	case *ast.ConditionalExpression:
		add(node.Test)
		add(node.Consequent)
		add(node.Alternate)
	case *ast.SequenceExpression:
		for _, value := range node.Sequence {
			add(value)
		}
	case *ast.TemplateLiteral:
		for _, value := range node.Expressions {
			add(value)
		}
	case *ast.SpreadElement:
		add(node.Expression)
	case *ast.NewExpression:
		for _, value := range node.ArgumentList {
			add(value)
		}
	case *ast.ArrowFunctionLiteral:
		result = append(result, p.callbackDependencies(node.Body)...)
		for _, name := range p.compiler.captures[node] {
			result = append(result, p.lookup(name).dependencies...)
		}
	case *ast.FunctionLiteral:
		result = append(result, p.callbackDependencies(node.Body)...)
		for _, name := range p.compiler.captures[node] {
			result = append(result, p.lookup(name).dependencies...)
		}
	}
	return uniqueCanvasIDs(result)
}
func (p *canvasProjector) callbackDependencies(body ast.ConciseBody) []string {
	result := []string{}
	// 回调的宿主返回有独立节点身份；内部局部变量已经在扫描该 callback 时解析。
	for _, node := range p.compiler.manifest.Nodes {
		location := p.compiler.parsed.sourceRange(body)
		if node.Range.Start.Offset >= location.Start.Offset && node.Range.End.Offset <= location.End.Offset {
			result = append(result, node.ID)
		}
	}
	return uniqueCanvasIDs(result)
}
func (p *canvasProjector) statements(statements []ast.Statement) {
	for _, statement := range statements {
		p.statement(statement)
	}
}
func (p *canvasProjector) statement(statement ast.Statement) {
	if statement == nil {
		return
	}
	switch node := statement.(type) {
	case *ast.LexicalDeclaration:
		for _, binding := range node.List {
			if id, ok := binding.Target.(*ast.Identifier); ok && (id.Name.String() == "meta" || id.Name.String() == "scripts" || id.Name.String() == "actions") {
				continue
			}
			// 函数声明的主体只在可识别的静态调用位置展开。
			switch binding.Initializer.(type) {
			case *ast.ArrowFunctionLiteral, *ast.FunctionLiteral:
			default:
				p.expression(binding.Initializer)
			}
			p.declare(binding.Target, p.dependencies(binding.Initializer), binding.Initializer)
		}
	case *ast.FunctionDeclaration:
		if node.Function.Name != nil {
			p.declare(node.Function.Name, p.dependencies(node.Function), node.Function)
		}
	case *ast.BlockStatement:
		if node == nil {
			return
		}
		p.scopes = append(p.scopes, map[string]canvasBinding{})
		p.statements(node.List)
		p.scopes = p.scopes[:len(p.scopes)-1]
	case *ast.IfStatement:
		p.expression(node.Test)
		base := cloneCanvasScopes(p.scopes)
		p.statement(node.Consequent)
		first := cloneCanvasScopes(p.scopes)
		p.scopes = cloneCanvasScopes(base)
		p.statement(node.Alternate)
		p.scopes = mergeCanvasScopes(base, first, p.scopes)
	case *ast.ForStatement:
		p.scopes = append(p.scopes, map[string]canvasBinding{})
		switch initial := node.Initializer.(type) {
		case *ast.ForLoopInitializerLexicalDecl:
			p.statement(&initial.LexicalDeclaration)
		case *ast.ForLoopInitializerExpression:
			p.expression(initial.Expression)
		}
		p.expression(node.Test)
		base := cloneCanvasScopes(p.scopes)
		p.statement(node.Body)
		p.expression(node.Update)
		p.scopes = mergeCanvasScopes(base, p.scopes, base)
		p.scopes = p.scopes[:len(p.scopes)-1]
	case *ast.ForOfStatement:
		p.expression(node.Source)
		p.scopes = append(p.scopes, map[string]canvasBinding{})
		if declaration, ok := node.Into.(*ast.ForDeclaration); ok {
			p.declare(declaration.Target, p.dependencies(node.Source), nil)
		}
		base := cloneCanvasScopes(p.scopes)
		p.statement(node.Body)
		p.scopes = mergeCanvasScopes(base, p.scopes, base)
		p.scopes = p.scopes[:len(p.scopes)-1]
	case *ast.WhileStatement:
		p.expression(node.Test)
		base := cloneCanvasScopes(p.scopes)
		p.statement(node.Body)
		p.scopes = mergeCanvasScopes(base, p.scopes, base)
	case *ast.TryStatement:
		base := cloneCanvasScopes(p.scopes)
		p.statement(node.Body)
		success := cloneCanvasScopes(p.scopes)
		if node.Catch != nil {
			p.scopes = cloneCanvasScopes(base)
			p.scopes = append(p.scopes, map[string]canvasBinding{})
			p.declare(node.Catch.Parameter, nil, nil)
			p.statement(node.Catch.Body)
			p.scopes = p.scopes[:len(p.scopes)-1]
			p.scopes = mergeCanvasScopes(base, success, p.scopes)
		}
		if node.Finally != nil {
			p.statement(node.Finally)
		}
	case *ast.ReturnStatement:
		p.expression(node.Argument)
		if p.callbackDepth == 0 && node.Argument != nil {
			p.returns = append(p.returns, CanvasInput{Target: "result", Expression: p.compiler.fragment(node.Argument), DependsOn: p.dependencies(node.Argument)})
		}
	case *ast.ThrowStatement:
		p.expression(node.Argument)
	case *ast.ExpressionStatement:
		p.expression(node.Expression)
	}
}
func (p *canvasProjector) callback(parameters *ast.ParameterList, body ast.ConciseBody, dependencies []string) {
	sources := make([][]string, len(parameters.List))
	for index := range sources {
		sources[index] = dependencies
	}
	p.callbackParameters(parameters, body, sources)
}
func (p *canvasProjector) callbackParameters(parameters *ast.ParameterList, body ast.ConciseBody, sources [][]string) {
	p.callbackDepth++
	p.scopes = append(p.scopes, map[string]canvasBinding{})
	for index, parameter := range parameters.List {
		var dependencies []string
		if index < len(sources) {
			dependencies = sources[index]
		}
		p.declare(parameter.Target, dependencies, nil)
	}
	switch node := body.(type) {
	case *ast.BlockStatement:
		p.statement(node)
	case *ast.ExpressionBody:
		p.expression(node.Expression)
	}
	p.scopes = p.scopes[:len(p.scopes)-1]
	p.callbackDepth--
}
func (p *canvasProjector) expression(expression ast.Expression) {
	switch node := expression.(type) {
	case *ast.AwaitExpression:
		p.expression(node.Argument)
	case *ast.ArrayLiteral:
		for _, value := range node.Value {
			p.expression(value)
		}
	case *ast.ObjectLiteral:
		for _, property := range node.Value {
			switch field := property.(type) {
			case *ast.PropertyKeyed:
				p.expression(field.Value)
			case *ast.SpreadElement:
				p.expression(field.Expression)
			}
		}
	case *ast.DotExpression:
		p.expression(node.Left)
	case *ast.BracketExpression:
		p.expression(node.Left)
		p.expression(node.Member)
	case *ast.OptionalChain:
		p.expression(node.Expression)
	case *ast.Optional:
		p.expression(node.Expression)
	case *ast.AssignExpression:
		p.expression(node.Right)
		dependencies := p.dependencies(node.Right)
		if node.Operator.String() != "=" {
			dependencies = uniqueCanvasIDs(append(dependencies, p.dependencies(node.Left)...))
		}
		p.assign(node.Left, dependencies)
	case *ast.BinaryExpression:
		p.expression(node.Left)
		p.expression(node.Right)
	case *ast.UnaryExpression:
		p.expression(node.Operand)
	case *ast.ConditionalExpression:
		p.expression(node.Test)
		p.expression(node.Consequent)
		p.expression(node.Alternate)
	case *ast.SequenceExpression:
		for _, value := range node.Sequence {
			p.expression(value)
		}
	case *ast.TemplateLiteral:
		for _, value := range node.Expressions {
			p.expression(value)
		}
	case *ast.SpreadElement:
		p.expression(node.Expression)
	case *ast.ArrowFunctionLiteral:
		p.callback(node.ParameterList, node.Body, nil)
	case *ast.FunctionLiteral:
		p.callback(node.ParameterList, node.Body, nil)
	case *ast.CallExpression:
		p.call(node)
	}
}
func (p *canvasProjector) call(call *ast.CallExpression) {
	if primitive, ok := call.Callee.(*ast.Identifier); ok && primitive.Name.String() == "pipeline" && len(call.ArgumentList) == 3 {
		p.pipeline(call)
		return
	}
	// map 的参数继承被迭代数据的来源，任务局部依赖仍按其词法语句顺序解析。
	if member, ok := call.Callee.(*ast.DotExpression); ok && member.Identifier.Name.String() == "map" && len(call.ArgumentList) == 1 {
		p.expression(member.Left)
		dependencies := p.dependencies(member.Left)
		switch callback := call.ArgumentList[0].(type) {
		case *ast.ArrowFunctionLiteral:
			p.callback(callback.ParameterList, callback.Body, dependencies)
		case *ast.FunctionLiteral:
			p.callback(callback.ParameterList, callback.Body, dependencies)
		default:
			p.expression(call.ArgumentList[0])
		}
	} else {
		for _, argument := range call.ArgumentList {
			if id, ok := argument.(*ast.Identifier); ok {
				if bound := p.lookup(id.Name.String()).expression; bound != nil {
					switch bound.(type) {
					case *ast.ArrayLiteral, *ast.ArrowFunctionLiteral, *ast.FunctionLiteral:
						p.expression(bound)
						continue
					}
				}
			}
			p.expression(argument)
		}
	}
	index, found := p.nodes[p.compiler.parsed.sourceRange(call).Start.Offset]
	if !found {
		return
	}
	target := &p.compiler.manifest.Nodes[index]
	add := func(name string, expression ast.Expression) {
		target.Inputs = append(target.Inputs, CanvasInput{Target: name, Expression: p.compiler.fragment(expression), DependsOn: p.dependencies(expression)})
	}
	if (target.Kind == "worker" || target.Kind == "askUser") && len(call.ArgumentList) > 0 {
		add("prompt", call.ArgumentList[0])
	}
	if target.Kind == "joinWorkflow" && len(call.ArgumentList) > 0 {
		add("handle", call.ArgumentList[0])
	}
	optionsIndex := 1
	if target.Kind == "functionTask" {
		optionsIndex = 0
	}
	if optionsIndex >= len(call.ArgumentList) {
		return
	}
	if object, ok := call.ArgumentList[optionsIndex].(*ast.ObjectLiteral); ok {
		for _, property := range object.Value {
			if field, ok := property.(*ast.PropertyKeyed); ok {
				key, _ := propertyName(field.Key)
				switch key {
				case "input", "args", "choices", "defaultValue", "url", "method", "headers", "body", "env":
					add(key, field.Value)
				}
			}
		}
	}
}

func (p *canvasProjector) pipeline(call *ast.CallExpression) {
	p.expression(call.ArgumentList[0])
	source := p.dependencies(call.ArgumentList[0])
	previous := source
	stages := call.ArgumentList[1]
	seen := map[string]bool{}
	for {
		name, ok := stages.(*ast.Identifier)
		if !ok || seen[name.Name.String()] {
			break
		}
		seen[name.Name.String()] = true
		replacement := p.lookup(name.Name.String()).expression
		if replacement == nil {
			break
		}
		stages = replacement
	}
	array, ok := stages.(*ast.ArrayLiteral)
	if !ok {
		p.expression(stages)
		return
	}
	for _, stage := range array.Value {
		if name, ok := stage.(*ast.Identifier); ok {
			if replacement := p.lookup(name.Name.String()).expression; replacement != nil {
				stage = replacement
			}
		}
		var parameters *ast.ParameterList
		var body ast.ConciseBody
		switch callback := stage.(type) {
		case *ast.ArrowFunctionLiteral:
			parameters, body = callback.ParameterList, callback.Body
		case *ast.FunctionLiteral:
			parameters, body = callback.ParameterList, callback.Body
		}
		if parameters == nil {
			p.expression(stage)
			continue
		}
		p.callbackParameters(parameters, body, [][]string{previous, source, source})
		if dependencies := p.callbackDependencies(body); len(dependencies) > 0 {
			previous = dependencies
		}
	}
}
