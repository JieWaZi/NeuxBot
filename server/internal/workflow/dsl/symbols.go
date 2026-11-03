package dsl

import (
	"fmt"
	"sort"

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/token"
)

// symbolScope 是编译时词法绑定，不进入运行时或数据库。
type symbolScope struct {
	// captures 保存本函数实际读取的外层变量。
	captures map[string]bool
	// parent 是外层源码作用域。
	parent *symbolScope
	// names 保存本层已经声明的变量。
	names map[string]bool
	// functionBoundary 是当前函数的局部边界，用于检测捕获写入。
	functionBoundary *symbolScope
	// task 表示当前函数是 parallel/pipeline 的任务。
	task bool
	// functionName 用于阻断直接递归。
	functionName string
}

func (s *symbolScope) child() *symbolScope {
	return &symbolScope{parent: s, names: map[string]bool{}, functionBoundary: s.functionBoundary, task: s.task, functionName: s.functionName}
}
func (s *symbolScope) find(name string) (*symbolScope, bool) {
	for scope := s; scope != nil; scope = scope.parent {
		if scope.names[name] {
			return scope, true
		}
	}
	return nil, false
}
func (s *symbolScope) local(owner *symbolScope) bool {
	for scope := s; scope != nil; scope = scope.parent {
		if scope == owner {
			return true
		}
		if scope == s.functionBoundary {
			return false
		}
	}
	return false
}

func (c *compiler) validateSymbols() {
	root := &symbolScope{names: map[string]bool{}}
	for _, name := range []string{"args", "flowContext", "undefined", "Math", "JSON", "Object", "Array", "String", "Number", "Boolean", "Date", "Error"} {
		root.names[name] = true
	}
	for _, primitive := range Registry() {
		root.names[primitive.Name] = true
	}
	c.symbolStatements(c.parsed.body, root)
}

func (c *compiler) symbolDeclare(target ast.Expression, scope *symbolScope) {
	if target == nil {
		return
	}
	switch target := target.(type) {
	case *ast.Identifier:
		name := target.Name.String()
		if name == "args" || name == "flowContext" || isPrimitive(name) {
			c.fail(target, "BINDING_RESERVED", "cannot shadow a workflow global: "+name)
		}
		if scope.names[name] {
			c.fail(target, "BINDING_DUPLICATE", "duplicate lexical binding: "+name)
		}
		scope.names[name] = true
	case *ast.ArrayPattern:
		for _, item := range target.Elements {
			c.symbolDeclare(item, scope)
		}
		c.symbolDeclare(target.Rest, scope)
	case *ast.ObjectPattern:
		for _, property := range target.Properties {
			switch property := property.(type) {
			case *ast.PropertyShort:
				c.symbolDeclare(&property.Name, scope)
				c.symbolExpression(property.Initializer, scope, false, "")
			case *ast.PropertyKeyed:
				if property.Computed {
					c.symbolExpression(property.Key, scope, false, "")
				}
				c.symbolDeclare(property.Value, scope)
			}
		}
		c.symbolDeclare(target.Rest, scope)
	case *ast.Binding:
		c.symbolDeclare(target.Target, scope)
		c.symbolExpression(target.Initializer, scope, false, "")
	case *ast.AssignExpression:
		c.symbolDeclare(target.Left, scope)
		c.symbolExpression(target.Right, scope, false, "")
	default:
		c.fail(target, "BINDING_UNSUPPORTED", fmt.Sprintf("unsupported binding %T", target))
	}
}

func (c *compiler) symbolStatements(statements []ast.Statement, scope *symbolScope) {
	for _, statement := range statements {
		c.symbolStatement(statement, scope)
	}
}
func (c *compiler) symbolStatement(statement ast.Statement, scope *symbolScope) {
	if statement == nil {
		return
	}
	switch s := statement.(type) {
	case *ast.EmptyStatement:
	case *ast.BlockStatement:
		if s == nil {
			return
		}
		c.symbolStatements(s.List, scope.child())
	case *ast.LexicalDeclaration:
		for _, binding := range s.List {
			name := ""
			if id, ok := binding.Target.(*ast.Identifier); ok {
				name = id.Name.String()
			}
			if name == "actions" || name == "scripts" {
				c.symbolDeclare(binding.Target, scope)
				continue
			}
			c.symbolExpression(binding.Initializer, scope, false, name)
			c.symbolDeclare(binding.Target, scope)
		}
	case *ast.FunctionDeclaration:
		if s.Function.Name != nil {
			name := s.Function.Name.Name.String()
			c.symbolDeclare(s.Function.Name, scope)
			c.symbolFunction(s.Function.ParameterList, s.Function.Body, scope, false, name, s.Function)
		}
	case *ast.ExpressionStatement:
		c.symbolExpression(s.Expression, scope, false, "")
	case *ast.ReturnStatement:
		c.symbolExpression(s.Argument, scope, false, "")
	case *ast.ThrowStatement:
		c.symbolExpression(s.Argument, scope, false, "")
	case *ast.IfStatement:
		c.symbolExpression(s.Test, scope, false, "")
		c.symbolStatement(s.Consequent, scope)
		c.symbolStatement(s.Alternate, scope)
	case *ast.WhileStatement:
		c.symbolExpression(s.Test, scope, false, "")
		c.symbolStatement(s.Body, scope)
	case *ast.ForStatement:
		local := scope.child()
		switch initializer := s.Initializer.(type) {
		case *ast.ForLoopInitializerLexicalDecl:
			for _, binding := range initializer.LexicalDeclaration.List {
				c.symbolExpression(binding.Initializer, local, false, "")
				c.symbolDeclare(binding.Target, local)
			}
		case *ast.ForLoopInitializerExpression:
			c.symbolExpression(initializer.Expression, local, false, "")
		}
		c.symbolExpression(s.Test, local, false, "")
		c.symbolStatement(s.Body, local)
		c.symbolExpression(s.Update, local, false, "")
	case *ast.ForOfStatement:
		c.symbolExpression(s.Source, scope, false, "")
		local := scope.child()
		switch into := s.Into.(type) {
		case *ast.ForDeclaration:
			c.symbolDeclare(into.Target, local)
		case *ast.ForIntoExpression:
			c.symbolWrite(into.Expression, scope)
		}
		c.symbolStatement(s.Body, local)
	case *ast.TryStatement:
		c.symbolStatement(s.Body, scope)
		if s.Catch != nil {
			local := scope.child()
			c.symbolDeclare(s.Catch.Parameter, local)
			c.symbolStatement(s.Catch.Body, local)
		}
		c.symbolStatement(s.Finally, scope)
	}
}

func (c *compiler) symbolFunction(parameters *ast.ParameterList, body ast.ConciseBody, outer *symbolScope, task bool, name string, node ast.Node) {
	local := outer.child()
	local.functionBoundary = local
	local.task = task
	local.functionName = name
	local.captures = map[string]bool{}
	if name != "" {
		local.names[name] = true
	}
	for _, parameter := range parameters.List {
		c.symbolDeclare(parameter.Target, local)
		c.symbolExpression(parameter.Initializer, local, false, "")
	}
	switch body := body.(type) {
	case *ast.BlockStatement:
		c.symbolStatements(body.List, local)
	case *ast.ExpressionBody:
		c.symbolExpression(body.Expression, local, task, "")
	}
	names := []string{}
	for name := range local.captures {
		names = append(names, name)
	}
	sort.Strings(names)
	c.captures[node] = names
}

func (c *compiler) symbolExpression(expression ast.Expression, scope *symbolScope, task bool, name string) {
	if expression == nil {
		return
	}
	switch e := expression.(type) {
	case *ast.Identifier:
		if owner, ok := scope.find(e.Name.String()); !ok {
			c.fail(e, "BINDING_UNKNOWN", "unknown native binding: "+e.Name.String())
		} else {
			c.symbolCapture(scope, owner, e.Name.String())
		}
	case *ast.ArrayLiteral:
		for _, item := range e.Value {
			c.symbolExpression(item, scope, task, "")
		}
	case *ast.ObjectLiteral:
		for _, property := range e.Value {
			switch p := property.(type) {
			case *ast.PropertyKeyed:
				if p.Computed {
					c.symbolExpression(p.Key, scope, false, "")
				}
				c.symbolExpression(p.Value, scope, false, "")
			case *ast.PropertyShort:
				c.symbolExpression(&p.Name, scope, false, "")
				c.symbolExpression(p.Initializer, scope, false, "")
			case *ast.SpreadElement:
				c.symbolExpression(p.Expression, scope, false, "")
			}
		}
	case *ast.DotExpression:
		c.symbolExpression(e.Left, scope, false, "")
	case *ast.BracketExpression:
		c.symbolExpression(e.Left, scope, false, "")
		c.symbolExpression(e.Member, scope, false, "")
	case *ast.OptionalChain:
		c.symbolExpression(e.Expression, scope, task, "")
	case *ast.Optional:
		c.symbolExpression(e.Expression, scope, task, "")
	case *ast.BinaryExpression:
		c.symbolExpression(e.Left, scope, false, "")
		c.symbolExpression(e.Right, scope, false, "")
	case *ast.UnaryExpression:
		if e.Operator == token.INCREMENT || e.Operator == token.DECREMENT {
			c.symbolWrite(e.Operand, scope)
		}
		c.symbolExpression(e.Operand, scope, false, "")
	case *ast.AssignExpression:
		c.symbolWrite(e.Left, scope)
		c.symbolExpression(e.Left, scope, false, "")
		c.symbolExpression(e.Right, scope, false, "")
	case *ast.ConditionalExpression:
		c.symbolExpression(e.Test, scope, false, "")
		c.symbolExpression(e.Consequent, scope, false, "")
		c.symbolExpression(e.Alternate, scope, false, "")
	case *ast.TemplateLiteral:
		for _, item := range e.Expressions {
			c.symbolExpression(item, scope, false, "")
		}
	case *ast.SpreadElement:
		c.symbolExpression(e.Expression, scope, task, "")
	case *ast.SequenceExpression:
		for _, item := range e.Sequence {
			c.symbolExpression(item, scope, false, "")
		}
	case *ast.AwaitExpression:
		c.symbolExpression(e.Argument, scope, task, "")
	case *ast.FunctionLiteral:
		c.symbolFunction(e.ParameterList, e.Body, scope, task, name, e)
	case *ast.ArrowFunctionLiteral:
		c.symbolFunction(e.ParameterList, e.Body, scope, task, name, e)
	case *ast.NewExpression:
		c.symbolExpression(e.Callee, scope, false, "")
		for _, argument := range e.ArgumentList {
			c.symbolExpression(argument, scope, false, "")
		}
	case *ast.CallExpression:
		c.symbolExpression(e.Callee, scope, false, "")
		host := ""
		if id, ok := e.Callee.(*ast.Identifier); ok {
			host = id.Name.String()
			if host == scope.functionName && host != "" {
				c.fail(e, "RECURSION_UNSUPPORTED", "recursive native helpers are not supported")
			}
		}
		if member, ok := e.Callee.(*ast.DotExpression); ok && member.Identifier.Name.String() == "push" {
			c.symbolWrite(member.Left, scope)
		}
		for index, argument := range e.ArgumentList {
			isTask := (host == "parallel" && index == 0) || (host == "pipeline" && index == 1)
			c.symbolExpression(argument, scope, isTask, "")
		}
		// map 构造函数可以返回 task，但其自身必须纯，不可在映射阶段执行 worker。
		if task {
			if member, ok := e.Callee.(*ast.DotExpression); ok && member.Identifier.Name.String() == "map" && len(e.ArgumentList) > 0 {
				function := e.ArgumentList[0]
				switch f := function.(type) {
				case *ast.ArrowFunctionLiteral:
					c.symbolFunction(f.ParameterList, f.Body, scope, false, "", f)
				case *ast.FunctionLiteral:
					c.symbolFunction(f.ParameterList, f.Body, scope, false, "", f)
				}
			}
		}
	}
}

func (c *compiler) symbolCapture(scope, owner *symbolScope, name string) {
	for scope != nil && scope.functionBoundary != nil {
		if scope.local(owner) {
			return
		}
		boundary := scope.functionBoundary
		boundary.captures[name] = true
		scope = boundary.parent
	}
}

func (c *compiler) symbolWrite(target ast.Expression, scope *symbolScope) {
	if target == nil {
		return
	}
	switch target := target.(type) {
	case *ast.Identifier:
		name := target.Name.String()
		owner, ok := scope.find(name)
		if !ok {
			c.fail(target, "BINDING_UNKNOWN", "assignment target is unbound: "+name)
			return
		}
		if name == "args" || name == "flowContext" {
			c.fail(target, "CONTEXT_READ_ONLY", "workflow globals cannot be modified")
		}
		if scope.task && !scope.local(owner) {
			c.fail(target, "CAPTURE_WRITE_UNSUPPORTED", "parallel/pipeline must return results instead of writing an outer capture: "+name)
		}
	case *ast.DotExpression:
		c.symbolWrite(target.Left, scope)
	case *ast.BracketExpression:
		c.symbolWrite(target.Left, scope)
	case *ast.ArrayPattern:
		for _, item := range target.Elements {
			c.symbolWrite(item, scope)
		}
	case *ast.ObjectPattern:
		for _, property := range target.Properties {
			if p, ok := property.(*ast.PropertyShort); ok {
				c.symbolWrite(&p.Name, scope)
			} else if p, ok := property.(*ast.PropertyKeyed); ok {
				c.symbolWrite(p.Value, scope)
			}
		}
	}
}
