package dsl

import (
	"fmt"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/token"
)

// controlProjector 只遍历通过编译校验的固定 AST，不执行表达式或推断运行结果。
type controlProjector struct {
	// compiler 提供调用模板、源码范围和纯字面量解析。
	compiler *compiler
	// nodes 用原始调用起点关联已有的稳定节点身份。
	nodes map[int]Node
	// groups 用原始调用起点关联已有的稳定分组身份。
	groups map[int]Group
	// bindings 保留当前词法作用域的任务数组和 callback 引用。
	bindings map[string]ast.Expression
	// phase 是最近显式 phase 或调用所属阶段，用于定位纯控制退出。
	phase string
	// task 区分主流程返回与任务 continuation 完成。
	task bool
}

func (c *compiler) controlHints() []ControlHint {
	p := &controlProjector{compiler: c, nodes: map[int]Node{}, groups: map[int]Group{}, bindings: map[string]ast.Expression{}}
	if len(c.manifest.Meta.Phases) > 0 {
		p.phase = c.manifest.Meta.Phases[0].ID
	}
	for _, node := range c.manifest.Nodes {
		p.nodes[node.Range.Start.Offset] = node
	}
	for _, group := range c.manifest.Groups {
		p.groups[group.Range.Start.Offset] = group
	}
	return p.statements(c.parsed.body)
}

func (p *controlProjector) hint(kind constants.WorkflowControlKind, node ast.Node) ControlHint {
	r := p.compiler.parsed.sourceRange(node)
	return ControlHint{ID: fmt.Sprintf("control.%s.%d", kind, r.Start.Offset), Kind: kind, Range: r, PhaseID: p.phase}
}

func (p *controlProjector) child() *controlProjector {
	next := *p
	next.bindings = make(map[string]ast.Expression, len(p.bindings))
	for key, value := range p.bindings {
		next.bindings[key] = value
	}
	return &next
}

func (p *controlProjector) statements(statements []ast.Statement) []ControlHint {
	result := []ControlHint{}
	for _, statement := range statements {
		result = append(result, p.statement(statement)...)
	}
	return result
}

func (p *controlProjector) statement(statement ast.Statement) []ControlHint {
	if statement == nil {
		return nil
	}
	switch s := statement.(type) {
	case *ast.BlockStatement:
		child := p.child()
		result := child.statements(s.List)
		p.phase = child.phase
		return result
	case *ast.LexicalDeclaration:
		result := []ControlHint{}
		for _, binding := range s.List {
			if id, ok := binding.Target.(*ast.Identifier); ok {
				p.bindings[id.Name.String()] = binding.Initializer
			}
			result = append(result, p.expression(binding.Initializer)...)
		}
		return result
	case *ast.FunctionDeclaration:
		if s.Function.Name != nil {
			p.bindings[s.Function.Name.Name.String()] = s.Function
		}
	case *ast.ExpressionStatement:
		return p.expression(s.Expression)
	case *ast.IfStatement:
		before := p.expression(s.Test)
		hint := p.hint(constants.WorkflowControlKindDecision, s)
		hint.Condition = p.compiler.fragment(s.Test)
		hint.Branches = []ControlBranch{{Label: constants.WorkflowControlPathTrue, Hints: p.child().statement(s.Consequent)}, {Label: constants.WorkflowControlPathFalse, Hints: p.child().statement(s.Alternate)}}
		return append(before, hint)
	case *ast.WhileStatement:
		return p.loop(s, s.Test, s.Body, nil)
	case *ast.ForStatement:
		before := []ControlHint{}
		switch init := s.Initializer.(type) {
		case *ast.ForLoopInitializerExpression:
			before = p.expression(init.Expression)
		case *ast.ForLoopInitializerLexicalDecl:
			before = p.statement(&init.LexicalDeclaration)
		}
		return append(before, p.loop(s, s.Test, s.Body, s.Update)...)
	case *ast.ForOfStatement:
		before := p.expression(s.Source)
		hints := p.loop(s, nil, s.Body, nil)
		hints[0].Condition = p.compiler.fragment(s.Source)
		return append(before, hints...)
	case *ast.ReturnStatement:
		if p.task {
			switch s.Argument.(type) {
			case *ast.ArrowFunctionLiteral, *ast.FunctionLiteral:
				return p.callback(s.Argument)
			}
		}
		before := p.expression(s.Argument)
		kind := constants.WorkflowControlKindReturn
		if p.task {
			kind = constants.WorkflowControlKindTaskReturn
		}
		return append(before, p.hint(kind, s))
	case *ast.ThrowStatement:
		return append(p.expression(s.Argument), p.hint(constants.WorkflowControlKindThrow, s))
	case *ast.BranchStatement:
		kind := constants.WorkflowControlKindContinue
		if s.Token == token.BREAK {
			kind = constants.WorkflowControlKindBreak
		}
		return []ControlHint{p.hint(kind, s)}
	case *ast.TryStatement:
		hint := p.hint(constants.WorkflowControlKindTry, s)
		hint.Branches = []ControlBranch{{Label: constants.WorkflowControlPathBody, Hints: p.child().statement(s.Body)}}
		if s.Catch != nil {
			hint.Branches = append(hint.Branches, ControlBranch{Label: constants.WorkflowControlPathCatch, Hints: p.child().statement(s.Catch.Body)})
		}
		if s.Finally != nil {
			hint.Branches = append(hint.Branches, ControlBranch{Label: constants.WorkflowControlPathFinally, Hints: p.child().statement(s.Finally)})
		}
		return []ControlHint{hint}
	}
	return nil
}

func (p *controlProjector) loop(node ast.Node, test ast.Expression, body ast.Statement, update ast.Expression) []ControlHint {
	hint := p.hint(constants.WorkflowControlKindLoop, node)
	if test != nil {
		hint.Condition = p.compiler.fragment(test)
	}
	child := p.child()
	hint.Branches = []ControlBranch{{Label: constants.WorkflowControlPathTest, Hints: p.expression(test)}, {Label: constants.WorkflowControlPathBody, Hints: child.statement(body)}}
	if update != nil {
		hint.Branches = append(hint.Branches, ControlBranch{Label: constants.WorkflowControlPathUpdate, Hints: child.expression(update)})
	}
	return []ControlHint{hint}
}

func (p *controlProjector) expression(expression ast.Expression) []ControlHint {
	if expression == nil {
		return nil
	}
	switch e := expression.(type) {
	case *ast.AwaitExpression:
		return p.expression(e.Argument)
	case *ast.CallExpression:
		start := p.compiler.parsed.sourceRange(e).Start.Offset
		if id, ok := e.Callee.(*ast.Identifier); ok && id.Name.String() == "joinWorkflow" {
			hint := p.hint(constants.WorkflowControlKindJoinWorkflow, e)
			if len(e.ArgumentList) > 0 {
				hint.Condition = p.compiler.fragment(e.ArgumentList[0])
			}
			return append(p.expressions(e.ArgumentList), hint)
		}
		if node, ok := p.nodes[start]; ok {
			before := p.expressions(e.ArgumentList)
			p.phase = node.PhaseID
			hint := p.hint(constants.WorkflowControlKindNode, e)
			hint.NodeID = node.ID
			return append(before, hint)
		}
		if group, ok := p.groups[start]; ok {
			return p.group(e, group)
		}
		if id, ok := e.Callee.(*ast.Identifier); ok && id.Name.String() == "phase" && len(e.ArgumentList) == 1 {
			value, _ := p.compiler.literal(e.ArgumentList[0], true, map[string]bool{})
			p.phase, _ = value.(string)
		}
		return append(p.expression(e.Callee), p.expressions(e.ArgumentList)...)
	case *ast.BinaryExpression:
		left := p.expression(e.Left)
		right := p.expression(e.Right)
		operator := e.Operator.String()
		if len(right) > 0 && (operator == "&&" || operator == "||" || operator == "??") {
			hint := p.hint(constants.WorkflowControlKindDecision, e)
			hint.Condition = p.compiler.fragment(e.Left)
			if operator == "??" {
				hint.Condition += " == null"
			}
			truthy, falsy := right, []ControlHint{}
			if operator == "||" {
				truthy, falsy = falsy, truthy
			}
			hint.Branches = []ControlBranch{{Label: constants.WorkflowControlPathTrue, Hints: truthy}, {Label: constants.WorkflowControlPathFalse, Hints: falsy}}
			return append(left, hint)
		}
		return append(left, right...)
	case *ast.ConditionalExpression:
		before := p.expression(e.Test)
		truthy, falsy := p.child().expression(e.Consequent), p.child().expression(e.Alternate)
		if len(truthy)+len(falsy) == 0 {
			return before
		}
		hint := p.hint(constants.WorkflowControlKindDecision, e)
		hint.Condition = p.compiler.fragment(e.Test)
		hint.Branches = []ControlBranch{{Label: constants.WorkflowControlPathTrue, Hints: truthy}, {Label: constants.WorkflowControlPathFalse, Hints: falsy}}
		return append(before, hint)
	case *ast.AssignExpression:
		return append(p.expression(e.Left), p.expression(e.Right)...)
	case *ast.UnaryExpression:
		return p.expression(e.Operand)
	case *ast.ArrayLiteral:
		return p.expressions(e.Value)
	case *ast.ObjectLiteral:
		result := []ControlHint{}
		for _, property := range e.Value {
			switch field := property.(type) {
			case *ast.PropertyKeyed:
				if field.Computed {
					result = append(result, p.expression(field.Key)...)
				}
				result = append(result, p.expression(field.Value)...)
			case *ast.SpreadElement:
				result = append(result, p.expression(field.Expression)...)
			}
		}
		return result
	case *ast.SequenceExpression:
		return p.expressions(e.Sequence)
	case *ast.TemplateLiteral:
		return p.expressions(e.Expressions)
	case *ast.DotExpression:
		return p.expression(e.Left)
	case *ast.BracketExpression:
		return append(p.expression(e.Left), p.expression(e.Member)...)
	case *ast.OptionalChain:
		return p.expression(e.Expression)
	case *ast.Optional:
		return p.expression(e.Expression)
	case *ast.SpreadElement:
		return p.expression(e.Expression)
	case *ast.NewExpression:
		return p.expressions(e.ArgumentList)
	}
	// functionTask 代码和纯辅助函数不属于宿主控制流；callback 只在所属分组内展开。
	return nil
}

func (p *controlProjector) expressions(expressions []ast.Expression) []ControlHint {
	result := []ControlHint{}
	for _, expression := range expressions {
		result = append(result, p.expression(expression)...)
	}
	return result
}

func (p *controlProjector) group(call *ast.CallExpression, group Group) []ControlHint {
	kind, index := constants.WorkflowControlKindParallel, 0
	before := []ControlHint{}
	if group.Kind == "pipeline" {
		kind, index = constants.WorkflowControlKindPipeline, 1
		before = p.expression(call.ArgumentList[0])
	}
	hint := p.hint(kind, call)
	hint.GroupID = group.ID
	hint.Concurrency = constants.WorkflowDefaultConcurrency
	hint.FailureMode = constants.WorkflowGroupDefaultFailureMode
	optionsIndex := 1
	if kind == constants.WorkflowControlKindPipeline {
		optionsIndex = 2
	}
	options, _ := p.compiler.literal(call.ArgumentList[optionsIndex], true, map[string]bool{})
	if fields, ok := options.(map[string]any); ok {
		if concurrency, ok := fields["concurrency"].(float64); ok {
			hint.Concurrency = int(concurrency)
		}
		if mode, ok := fields["failureMode"].(string); ok {
			hint.FailureMode = mode
		}
	}
	if kind == constants.WorkflowControlKindPipeline {
		hint.Condition = p.compiler.fragment(call.ArgumentList[0])
	}
	repeated := false
	if mapped, ok := call.ArgumentList[index].(*ast.CallExpression); ok {
		if member, ok := mapped.Callee.(*ast.DotExpression); ok && member.Identifier.Name.String() == "map" {
			repeated = true
			hint.Condition = p.compiler.fragment(member.Left)
		}
	}
	for number, expression := range p.tasks(call.ArgumentList[index], map[string]bool{}) {
		child := p.child()
		child.task = true
		hints := child.callback(expression)
		label := fmt.Sprint(number + 1)
		if repeated {
			label = constants.WorkflowControlPathItems
		}
		hint.Branches = append(hint.Branches, ControlBranch{Label: label, Hints: hints})
	}
	return append(before, hint)
}

func (p *controlProjector) tasks(expression ast.Expression, seen map[string]bool) []ast.Expression {
	switch e := expression.(type) {
	case *ast.ArrayLiteral:
		return e.Value
	case *ast.Identifier:
		name := e.Name.String()
		if !seen[name] {
			seen[name] = true
			return p.tasks(p.bindings[name], seen)
		}
	case *ast.CallExpression:
		// array.map 的 callback 是逐项任务模板，不能把未知输入个数编造成静态节点。
		if member, ok := e.Callee.(*ast.DotExpression); ok && member.Identifier.Name.String() == "map" && len(e.ArgumentList) == 1 {
			return []ast.Expression{e.ArgumentList[0]}
		}
	}
	return nil
}

func (p *controlProjector) callback(expression ast.Expression) []ControlHint {
	switch e := expression.(type) {
	case *ast.Identifier:
		if value := p.bindings[e.Name.String()]; value != nil && value != expression {
			child := p.child()
			delete(child.bindings, e.Name.String())
			return child.callback(value)
		}
	case *ast.FunctionLiteral:
		return p.statement(e.Body)
	case *ast.ArrowFunctionLiteral:
		switch body := e.Body.(type) {
		case *ast.BlockStatement:
			return p.statement(body)
		case *ast.ExpressionBody:
			if _, nested := body.Expression.(*ast.ArrowFunctionLiteral); nested {
				return p.callback(body.Expression)
			}
			return p.expression(body.Expression)
		}
	}
	return nil
}
