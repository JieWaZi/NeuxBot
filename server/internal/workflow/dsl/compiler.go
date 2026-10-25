package dsl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/token"
)

// compiler 为单份源码分配程序和临时 slot，不共享可变编译状态。
type compiler struct {
	// captures 是由词法分析得到的每个函数实际自由变量。
	captures map[ast.Node][]string
	// parsed 隔离固定的 Goja AST。
	parsed *parsedSource
	// constants 是允许静态配置引用的顶层不可变声明。
	constants map[string]ast.Expression
	// diagnostics 收集所有可定位的编译错误。
	diagnostics []Diagnostic
	// manifest 是此次源码的只读画布投影。
	manifest Manifest
	// bundle 是此次源码的持久执行程序。
	bundle ProgramBundle
	// identities 防止阶段、节点、分组和动作复用身份。
	identities map[string]bool
	// sequence 给内部程序和临时 slot 分配确定名称。
	sequence int
	// group 是当前识别的并行或管道模板身份。
	group string
	// loopDepth 是当前源码的循环嵌套层数。
	loopDepth int
}

// codeBuilder 持有一个 callback 或主流程的指令及局部跳转补丁。
type codeBuilder struct {
	// compiler 共享源码静态分析结果。
	compiler *compiler
	// code 是正在生成的指令序列。
	code []Instruction
	// loops 保存循环内 break 和 continue 的待补跳转。
	loops []*loopPatch
	// scopeDepth 是当前词法作用域深度。
	scopeDepth int
	// handlerDepth 是当前程序中仍生效的 try/catch/finally 层数。
	handlerDepth int
	// task 表示此程序是宿主识别的 parallel/pipeline continuation。
	task bool
	// helper 表示此程序是纯辅助函数，不能隐藏宿主调用。
	helper bool
	// actionContext 是独立 handler 的能力参数名，不允许主流程别名伪造。
	actionContext string
}

// loopPatch 记录一个循环的跳转和词法作用域边界。
type loopPatch struct {
	// breaks 是跳出循环的指令位置。
	breaks []int
	// continues 是进入下一轮的指令位置。
	continues []int
	// scopeDepth 是循环体外的作用域深度。
	scopeDepth int
	// handlerDepth 是循环体外仍生效的异常处理数量。
	handlerDepth int
}

// Compile 从完整原始源码一次生成同版本的 Manifest 与 Go 执行程序。
func Compile(source string) (Compilation, []Diagnostic, error) {
	p, diagnostics, err := parseSource(source)
	if err != nil {
		return Compilation{}, diagnostics, err
	}
	c := &compiler{parsed: p, constants: map[string]ast.Expression{}, identities: map[string]bool{}, captures: map[ast.Node][]string{}, bundle: ProgramBundle{Version: constants.WorkflowIRVersion, Entry: "main", Programs: map[string]Program{}}, manifest: Manifest{Nodes: []Node{}, Groups: []Group{}, DisplayEdges: []DisplayEdge{}}}
	var metaExpression ast.Expression
	for _, statement := range p.body {
		if declaration, ok := statement.(*ast.LexicalDeclaration); ok && declaration.Token == token.CONST {
			for _, binding := range declaration.List {
				if id, ok := binding.Target.(*ast.Identifier); ok {
					c.constants[id.Name.String()] = binding.Initializer
					if id.Name.String() == "meta" {
						metaExpression = binding.Initializer
					}
				}
			}
		}
	}
	if metaExpression == nil {
		c.fail(nil, "META_REQUIRED", "declare export const meta as a pure literal")
	} else {
		value, ok := c.literal(metaExpression, false, map[string]bool{})
		if !ok {
			c.fail(metaExpression, "META_LITERAL_REQUIRED", "meta must contain only JSON literals")
		} else {
			encoded, _ := json.Marshal(value)
			if json.Unmarshal(encoded, &c.manifest.Meta) != nil {
				c.fail(metaExpression, "META_INVALID", "meta fields have invalid types")
			}
		}
	}
	c.validateMeta(metaExpression)
	c.manifest.InputPropertyOrder = inputPropertyOrder(metaExpression)
	c.validateSymbols()
	c.compileActions()
	b := &codeBuilder{compiler: c}
	for _, statement := range p.body {
		b.statement(statement)
	}
	b.emit(Instruction{Op: constants.WorkflowOpReturn, Expression: literalExpression(nil)})
	c.bundle.Programs["main"] = Program{Code: b.code, Async: true}
	c.validateResourceIdentities()
	c.validatePages(metaExpression)
	if _, err := IMTargetDependencies(c.manifest, c.bundle); err != nil {
		c.fail(metaExpression, "IM_TARGET_DEPENDENCY_REQUIRED", "IM calls with dynamic targets must declare imTargetIds on their builtin action")
	}
	minimum := minimumInstances(c.bundle.Programs[c.bundle.Entry])
	if limit := c.manifest.Meta.Limits.MaxInstances; limit > 0 && limit < minimum {
		c.fail(metaExpression, "INSTANCE_BUDGET_TOO_LOW", instanceBudgetReason(limit, minimum))
	}
	sort.Slice(c.manifest.Nodes, func(i, j int) bool {
		return c.manifest.Nodes[i].Range.Start.Offset < c.manifest.Nodes[j].Range.Start.Offset
	})
	sort.Slice(c.manifest.Groups, func(i, j int) bool {
		return c.manifest.Groups[i].Range.Start.Offset < c.manifest.Groups[j].Range.Start.Offset
	})
	if len(c.diagnostics) > 0 {
		return Compilation{}, c.diagnostics, status_error.WorkflowSourceInvalid
	}
	c.manifest.ControlHints = c.controlHints()
	manifestJSON, _ := json.Marshal(c.manifest)
	irJSON, _ := json.Marshal(c.bundle)
	return Compilation{Manifest: c.manifest, Bundle: c.bundle, SourceDigest: Digest([]byte(source)), ManifestDigest: Digest(manifestJSON), IRDigest: Digest(irJSON)}, []Diagnostic{}, nil
}

// Digest 返回权威字节的 SHA-256 摘要。
func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func (c *compiler) fail(node ast.Node, code, message string) {
	d := Diagnostic{Code: code, Severity: "error", Message: message}
	if node != nil {
		d.Range = c.parsed.sourceRange(node)
	}
	c.diagnostics = append(c.diagnostics, d)
}

// validateResourceIdentities 用执行位置的词法常量核验资源，不把外层同名声明当成实际参数。
func (c *compiler) validateResourceIdentities() {
	instructions := map[string]Instruction{}
	for _, program := range dependencyPrograms(c.bundle) {
		for _, instruction := range program {
			if instruction.Primitive == "action" || instruction.Primitive == "workflow" || instruction.Primitive == "startWorkflow" {
				instructions[instruction.NodeID] = instruction
			}
		}
	}
	for index := range c.manifest.Nodes {
		node := &c.manifest.Nodes[index]
		instruction, found := instructions[node.ID]
		if !found || instruction.Expression == nil || len(instruction.Expression.Arguments) == 0 {
			continue
		}
		id := staticString(instruction.Expression.Arguments[0])
		code, message := "", ""
		if node.Kind == "action" {
			declared := false
			for _, action := range c.manifest.Meta.Actions {
				declared = declared || id != "" && id == action.ID
			}
			if !declared {
				code, message = "ACTION_NOT_DECLARED", "action id must reference a static meta.actions identity"
			}
		} else if id == "" {
			code, message = "CHILD_ID_STATIC_REQUIRED", "child workflow must reference a static resource identity"
		} else if len(instruction.Expression.Arguments) > 1 {
			if revision, present := dependencyProperty(instruction.Expression.Arguments[1], "revisionId"); present {
				if revisionID, static := staticDependencyString(revision); static {
					node.Options["revisionId"] = revisionID
				} else {
					code, message = "STATIC_OPTION_REQUIRED", "SDK option must be static: revisionId"
				}
			}
		}
		if code != "" {
			c.diagnostics = append(c.diagnostics, Diagnostic{Code: code, Severity: "error", Message: message, Range: instruction.Range, NodeID: node.ID})
		}
	}
}

func (c *compiler) unique(node ast.Node, id string) bool {
	if !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,63}$`).MatchString(id) {
		c.fail(node, "STABLE_ID_REQUIRED", "identity must be 1–64 ASCII characters beginning with a letter")
		return false
	}
	if c.identities[id] {
		c.fail(node, "ID_DUPLICATE", "identity is already used: "+id)
		return false
	}
	c.identities[id] = true
	return true
}

func (c *compiler) validateMeta(node ast.Node) {
	m := c.manifest.Meta
	if m.SDK != constants.WorkflowSDKVersion {
		c.fail(node, "SDK_UNSUPPORTED", "meta.sdk must be "+constants.WorkflowSDKVersion)
	}
	if strings.TrimSpace(m.Title) == "" || len(m.Phases) == 0 {
		c.fail(node, "META_INVALID", "title and at least one phase are required")
	}
	phases := map[string]bool{}
	hasEdges := false
	for _, p := range m.Phases {
		c.unique(node, p.ID)
		phases[p.ID] = true
		hasEdges = hasEdges || len(p.Next) > 0
		if p.Title == "" || p.Detail == "" {
			c.fail(node, "PHASE_INVALID", "phase title and detail are required")
		}
	}
	for i, p := range m.Phases {
		if hasEdges && p.Next == nil && !p.CanExit {
			c.fail(node, "PHASE_NEXT_REQUIRED", "declare next or canExit for every phase")
		}
		for _, next := range p.Next {
			if !phases[next] {
				c.fail(node, "PHASE_UNKNOWN", "unknown next phase: "+next)
			} else {
				c.manifest.DisplayEdges = append(c.manifest.DisplayEdges, DisplayEdge{From: p.ID, To: next})
			}
		}
		if !hasEdges && i+1 < len(m.Phases) {
			c.manifest.DisplayEdges = append(c.manifest.DisplayEdges, DisplayEdge{From: p.ID, To: m.Phases[i+1].ID})
		}
	}
	for _, schema := range []map[string]any{m.InputSchema, m.OutputSchema} {
		if _, err := ResolveSchema(schema); err != nil {
			c.fail(node, "SCHEMA_UNSUPPORTED", "invalid or unsupported workflow schema")
		}
	}
	for _, a := range m.Actions {
		c.unique(node, a.ID)
		if a.Label == "" || a.Description == "" || (a.Risk != "read" && a.Risk != "write" && a.Risk != "external") {
			c.fail(node, "ACTION_INVALID", "action requires label, description and read/write/external risk")
		}
		switch a.Type {
		case "http":
			if len(a.AllowedOrigins) == 0 || len(a.Methods) == 0 {
				c.fail(node, "ACTION_INVALID", "HTTP action requires allowedOrigins and methods")
			}
		case "script":
			if a.Executable == "" || a.WorkingDirectory != "workflow_scope" {
				c.fail(node, "ACTION_INVALID", "script action requires an executable and workflow_scope")
			}
		case "builtin":
			if a.Handler == "" {
				c.fail(node, "ACTION_INVALID", "builtin action requires a handler")
			}
		default:
			c.fail(node, "ACTION_INVALID", "action type must be http, script or builtin")
		}
		for _, schema := range []map[string]any{a.InputSchema, a.OutputSchema} {
			if _, err := ResolveSchema(schema); err != nil {
				c.fail(node, "SCHEMA_UNSUPPORTED", "invalid action schema")
			}
		}
	}
	l := m.Limits
	if l.Concurrency < 0 || l.Concurrency > 8 || l.MaxInstances < 0 || l.MaxInstances > constants.WorkflowMaxInstances || l.MaxLoopSteps < 0 || l.MaxLoopSteps > constants.WorkflowMaxLoopSteps || l.TimeoutMillis < 0 || l.TimeoutMillis > constants.WorkflowDefaultTimeoutMillis {
		c.fail(node, "LIMIT_INVALID", "declared budgets exceed host limits")
	}
	c.validateExports(node)
}

func (c *compiler) temporary() string { c.sequence++; return fmt.Sprintf("$%d", c.sequence) }
func literalExpression(value any) *Expression {
	return &Expression{Kind: constants.WorkflowExprLiteral, Value: value}
}
func identifierExpression(name string) *Expression {
	return &Expression{Kind: constants.WorkflowExprIdentifier, Name: name}
}

func (c *compiler) literal(expression ast.Expression, references bool, seen map[string]bool) (any, bool) {
	switch x := expression.(type) {
	case *ast.StringLiteral:
		return x.Value.String(), true
	case *ast.NumberLiteral:
		encoded, _ := json.Marshal(x.Value)
		var n any
		if json.Unmarshal(encoded, &n) != nil {
			return nil, false
		}
		return n, true
	case *ast.BooleanLiteral:
		return x.Value, true
	case *ast.NullLiteral:
		return nil, true
	case *ast.UnaryExpression:
		value, ok := c.literal(x.Operand, references, seen)
		number, isNumber := value.(float64)
		if ok && isNumber && x.Operator == token.MINUS {
			return -number, true
		}
		return nil, false
	case *ast.Identifier:
		name := x.Name.String()
		if !references || seen[name] {
			return nil, false
		}
		target, ok := c.constants[name]
		if !ok {
			return nil, false
		}
		seen[name] = true
		value, ok := c.literal(target, true, seen)
		delete(seen, name)
		return value, ok
	case *ast.ArrayLiteral:
		values := []any{}
		for _, item := range x.Value {
			value, ok := c.literal(item, references, seen)
			if !ok {
				return nil, false
			}
			values = append(values, value)
		}
		return values, true
	case *ast.ObjectLiteral:
		values := map[string]any{}
		for _, property := range x.Value {
			p, ok := property.(*ast.PropertyKeyed)
			if !ok || p.Computed || p.Kind != ast.PropertyKindValue {
				return nil, false
			}
			key, ok := propertyName(p.Key)
			if !ok || forbiddenKey(key) {
				return nil, false
			}
			if _, duplicate := values[key]; duplicate {
				c.fail(p, "PROPERTY_DUPLICATE", "literal property is declared more than once: "+key)
				return nil, false
			}
			value, ok := c.literal(p.Value, references, seen)
			if !ok {
				return nil, false
			}
			values[key] = value
		}
		return values, true
	case *ast.TemplateLiteral:
		if len(x.Expressions) == 0 && x.Tag == nil {
			return x.Elements[0].Parsed.String(), true
		}
	}
	return nil, false
}

func propertyName(expression ast.Expression) (string, bool) {
	switch x := expression.(type) {
	case *ast.Identifier:
		return x.Name.String(), true
	case *ast.StringLiteral:
		return x.Value.String(), true
	case *ast.NumberLiteral:
		return fmt.Sprint(x.Value), true
	}
	return "", false
}
func forbiddenKey(name string) bool {
	return name == "__proto__" || name == "prototype" || name == "constructor"
}

func (b *codeBuilder) emit(i Instruction) int { b.code = append(b.code, i); return len(b.code) - 1 }
func (b *codeBuilder) statement(statement ast.Statement) {
	if statement == nil {
		return
	}
	c := b.compiler
	location := c.parsed.sourceRange(statement)
	switch s := statement.(type) {
	case *ast.EmptyStatement:
	case *ast.BlockStatement:
		b.emit(Instruction{Op: constants.WorkflowOpEnterScope, Range: location})
		b.scopeDepth++
		for _, item := range s.List {
			b.statement(item)
		}
		b.scopeDepth--
		b.emit(Instruction{Op: constants.WorkflowOpLeaveScope, Range: location})
	case *ast.LexicalDeclaration:
		for _, binding := range s.List {
			b.binding(binding, s.Token == token.CONST, location)
		}
	case *ast.VariableStatement:
		c.fail(s, "VAR_UNSUPPORTED", "use const or let declarations")
	case *ast.FunctionDeclaration:
		if s.Function.Name == nil {
			c.fail(s, "FUNCTION_NAME_REQUIRED", "named helper function required")
			return
		}
		target := identifierExpression(s.Function.Name.Name.String())
		value := b.expression(s.Function)
		b.emit(Instruction{Op: constants.WorkflowOpDeclare, Target: target, Expression: value, Constant: true, Range: location})
	case *ast.ExpressionStatement:
		value := b.expression(s.Expression)
		b.emit(Instruction{Op: constants.WorkflowOpEvaluate, Expression: value, Range: location})
	case *ast.ReturnStatement:
		value := b.expression(s.Argument)
		b.emit(Instruction{Op: constants.WorkflowOpReturn, Expression: value, Range: location})
	case *ast.ThrowStatement:
		value := b.expression(s.Argument)
		b.emit(Instruction{Op: constants.WorkflowOpThrow, Expression: value, Range: location})
	case *ast.IfStatement:
		test := b.expression(s.Test)
		branch := b.emit(Instruction{Op: constants.WorkflowOpBranch, Expression: test, Next: len(b.code) + 1, Range: location})
		b.statement(s.Consequent)
		end := b.emit(Instruction{Op: constants.WorkflowOpJump})
		b.code[branch].Alternate = len(b.code)
		b.statement(s.Alternate)
		b.code[end].Next = len(b.code)
	case *ast.WhileStatement:
		b.loop(s.Test, s.Body, nil, location)
	case *ast.ForStatement:
		b.emit(Instruction{Op: constants.WorkflowOpEnterScope})
		b.scopeDepth++
		switch init := s.Initializer.(type) {
		case nil:
		case *ast.ForLoopInitializerLexicalDecl:
			for _, binding := range init.LexicalDeclaration.List {
				b.binding(binding, init.LexicalDeclaration.Token == token.CONST, location)
			}
		case *ast.ForLoopInitializerExpression:
			b.emit(Instruction{Op: constants.WorkflowOpEvaluate, Expression: b.expression(init.Expression)})
		default:
			c.fail(s, "FOR_INITIALIZER_UNSUPPORTED", "use a let/const for initializer")
		}
		b.loop(s.Test, s.Body, s.Update, location)
		b.scopeDepth--
		b.emit(Instruction{Op: constants.WorkflowOpLeaveScope})
	case *ast.ForOfStatement:
		if s.Await {
			c.fail(s, "FOR_AWAIT_UNSUPPORTED", "iterate a JSON array with for-of")
			return
		}
		items := c.temporary()
		index := c.temporary()
		b.emit(Instruction{Op: constants.WorkflowOpDeclare, Target: identifierExpression(items), Expression: b.expression(s.Source), Constant: true})
		b.emit(Instruction{Op: constants.WorkflowOpDeclare, Target: identifierExpression(index), Expression: literalExpression(float64(0))})
		start := len(b.code)
		b.emit(Instruction{Op: constants.WorkflowOpLoop, NodeID: fmt.Sprintf("loop.%d", location.Start.Offset), Range: location})
		test := &Expression{Kind: constants.WorkflowExprBinary, Name: "<", Arguments: []*Expression{identifierExpression(index), {Kind: constants.WorkflowExprMember, Arguments: []*Expression{identifierExpression(items), literalExpression("length")}}}}
		branch := b.emit(Instruction{Op: constants.WorkflowOpBranch, Expression: test, Next: len(b.code) + 1})
		patch := &loopPatch{scopeDepth: b.scopeDepth, handlerDepth: b.handlerDepth}
		b.loops = append(b.loops, patch)
		c.loopDepth++
		b.emit(Instruction{Op: constants.WorkflowOpEnterScope})
		b.scopeDepth++
		value := &Expression{Kind: constants.WorkflowExprMember, Arguments: []*Expression{identifierExpression(items), identifierExpression(index)}}
		switch into := s.Into.(type) {
		case *ast.ForDeclaration:
			b.emit(Instruction{Op: constants.WorkflowOpDeclare, Target: b.expression(into.Target), Expression: value, Constant: into.IsConst})
		case *ast.ForIntoExpression:
			b.emit(Instruction{Op: constants.WorkflowOpEvaluate, Expression: &Expression{Kind: constants.WorkflowExprAssign, Name: "=", Arguments: []*Expression{b.expression(into.Expression), value}}})
		default:
			c.fail(s, "FOR_BINDING_UNSUPPORTED", "use let or const for-of binding")
		}
		b.statement(s.Body)
		b.scopeDepth--
		b.emit(Instruction{Op: constants.WorkflowOpLeaveScope})
		continuation := len(b.code)
		b.emit(Instruction{Op: constants.WorkflowOpEvaluate, Expression: &Expression{Kind: constants.WorkflowExprUnary, Name: "++", Arguments: []*Expression{identifierExpression(index)}}})
		b.emit(Instruction{Op: constants.WorkflowOpJump, Next: start})
		b.code[branch].Alternate = len(b.code)
		b.patchLoop(patch, len(b.code), continuation)
		c.loopDepth--
		b.loops = b.loops[:len(b.loops)-1]
	case *ast.BranchStatement:
		if s.Label != nil || len(b.loops) == 0 {
			c.fail(s, "BRANCH_UNSUPPORTED", "break/continue must be inside an unlabelled loop")
			return
		}
		loop := b.loops[len(b.loops)-1]
		jump := b.emit(Instruction{Op: constants.WorkflowOpLoopControl, ScopeDrop: b.scopeDepth - loop.scopeDepth, HandlerDepth: loop.handlerDepth, Range: location})
		if s.Token == token.BREAK {
			loop.breaks = append(loop.breaks, jump)
		} else {
			loop.continues = append(loop.continues, jump)
		}
	case *ast.TryStatement:
		entry := b.emit(Instruction{Op: constants.WorkflowOpTry, Alternate: -1, Finally: -1, Range: location})
		b.handlerDepth++
		b.statement(s.Body)
		normal := b.emit(Instruction{Op: constants.WorkflowOpEndTry})
		if s.Catch != nil {
			b.code[entry].Alternate = len(b.code)
			b.emit(Instruction{Op: constants.WorkflowOpEnterScope})
			b.scopeDepth++
			if s.Catch.Parameter != nil {
				b.emit(Instruction{Op: constants.WorkflowOpDeclare, Target: b.expression(s.Catch.Parameter), Expression: identifierExpression("$error")})
			}
			b.statement(s.Catch.Body)
			b.scopeDepth--
			b.emit(Instruction{Op: constants.WorkflowOpLeaveScope})
			b.emit(Instruction{Op: constants.WorkflowOpEndTry})
		}
		finalPC := len(b.code)
		if s.Finally != nil {
			b.code[entry].Finally = finalPC
			b.statement(s.Finally)
			b.emit(Instruction{Op: constants.WorkflowOpFinally})
		}
		b.handlerDepth--
		end := len(b.code)
		b.code[entry].Next = end
		b.code[normal].Next = finalPC
		for i := entry + 1; i < finalPC; i++ {
			if b.code[i].Op == constants.WorkflowOpEndTry && b.code[i].Next == 0 {
				b.code[i].Next = finalPC
			}
		}
	default:
		c.fail(statement, "STATEMENT_UNSUPPORTED", fmt.Sprintf("unsupported native statement %T; move general JavaScript into functionTask", statement))
	}
}

func (b *codeBuilder) binding(binding *ast.Binding, constant bool, location SourceRange) {
	if id, ok := binding.Target.(*ast.Identifier); ok && id.Name.String() == "actions" {
		return
	}
	value := b.expression(binding.Initializer)
	target := b.expression(binding.Target)
	b.emit(Instruction{Op: constants.WorkflowOpDeclare, Target: target, Expression: value, Constant: constant, Range: location})
}
func (b *codeBuilder) patchLoop(p *loopPatch, end, continuation int) {
	for _, i := range p.breaks {
		b.code[i].Next = end
	}
	for _, i := range p.continues {
		b.code[i].Next = continuation
	}
}
func (b *codeBuilder) loop(test ast.Expression, body ast.Statement, update ast.Expression, location SourceRange) {
	c := b.compiler
	start := len(b.code)
	b.emit(Instruction{Op: constants.WorkflowOpLoop, NodeID: fmt.Sprintf("loop.%d", location.Start.Offset), Range: location})
	condition := literalExpression(true)
	if test != nil {
		condition = b.expression(test)
	}
	branch := b.emit(Instruction{Op: constants.WorkflowOpBranch, Expression: condition, Next: len(b.code) + 1})
	patch := &loopPatch{scopeDepth: b.scopeDepth, handlerDepth: b.handlerDepth}
	b.loops = append(b.loops, patch)
	c.loopDepth++
	b.statement(body)
	continuation := len(b.code)
	if update != nil {
		b.emit(Instruction{Op: constants.WorkflowOpEvaluate, Expression: b.expression(update)})
	}
	b.emit(Instruction{Op: constants.WorkflowOpJump, Next: start})
	b.code[branch].Alternate = len(b.code)
	b.patchLoop(patch, len(b.code), continuation)
	b.loops = b.loops[:len(b.loops)-1]
	c.loopDepth--
}

func (b *codeBuilder) expression(expression ast.Expression) *Expression {
	if expression == nil {
		return literalExpression(nil)
	}
	c := b.compiler
	switch e := expression.(type) {
	case *ast.NullLiteral:
		return literalExpression(nil)
	case *ast.BooleanLiteral:
		return literalExpression(e.Value)
	case *ast.NumberLiteral:
		return literalExpression(e.Value)
	case *ast.StringLiteral:
		return literalExpression(e.Value.String())
	case *ast.Identifier:
		name := e.Name.String()
		if forbiddenKey(name) || name == "eval" || name == "require" || name == "Promise" || name == "Function" || name == "globalThis" || name == "process" || name == "fetch" || name == "setTimeout" {
			c.fail(e, "GLOBAL_UNSUPPORTED", "unsupported native global: "+name)
		}
		return identifierExpression(name)
	case *ast.ArrayLiteral:
		return &Expression{Kind: constants.WorkflowExprArray, Arguments: b.orderedExpressions(e.Value)}
	case *ast.ArrayPattern:
		values := []*Expression{}
		for _, x := range e.Elements {
			values = append(values, b.expression(x))
		}
		if e.Rest != nil {
			values = append(values, &Expression{Kind: constants.WorkflowExprSpread, Arguments: []*Expression{b.expression(e.Rest)}})
		}
		return &Expression{Kind: constants.WorkflowExprArray, Arguments: values}
	case *ast.ObjectLiteral:
		return b.object(e.Value)
	case *ast.ObjectPattern:
		result := b.object(e.Properties)
		if e.Rest != nil {
			result.Properties = append(result.Properties, Property{Value: b.expression(e.Rest), Spread: true})
		}
		return result
	case *ast.Binding:
		target := b.expression(e.Target)
		if e.Initializer != nil {
			return &Expression{Kind: constants.WorkflowExprDefault, Arguments: []*Expression{target, b.expression(e.Initializer)}}
		}
		return target
	case *ast.DotExpression:
		name := e.Identifier.Name.String()
		if forbiddenKey(name) {
			c.fail(e, "PROPERTY_UNSUPPORTED", "prototype access is not available")
		}
		return &Expression{Kind: constants.WorkflowExprMember, Arguments: []*Expression{b.expression(e.Left), literalExpression(name)}}
	case *ast.BracketExpression:
		return &Expression{Kind: constants.WorkflowExprMember, Arguments: []*Expression{b.expression(e.Left), b.expression(e.Member)}}
	case *ast.OptionalChain:
		return b.expression(e.Expression)
	case *ast.Optional:
		result := b.expression(e.Expression)
		result.Optional = true
		return result
	case *ast.BinaryExpression:
		if containsAwait(e.Right) {
			if e.Operator.String() == "&&" || e.Operator.String() == "||" || e.Operator.String() == "??" {
				left := b.materialize(b.expression(e.Left))
				test := left
				if e.Operator.String() == "||" {
					test = &Expression{Kind: constants.WorkflowExprUnary, Name: "!", Arguments: []*Expression{left}}
				}
				if e.Operator.String() == "??" {
					test = &Expression{Kind: constants.WorkflowExprBinary, Name: "==", Arguments: []*Expression{left, literalExpression(nil)}}
				}
				branch := b.emit(Instruction{Op: constants.WorkflowOpBranch, Expression: test, Next: len(b.code) + 1})
				right := b.expression(e.Right)
				b.emit(Instruction{Op: constants.WorkflowOpEvaluate, Expression: &Expression{Kind: constants.WorkflowExprAssign, Name: "=", Arguments: []*Expression{left, right}}})
				b.code[branch].Alternate = len(b.code)
				return left
			}
			left := b.materialize(b.expression(e.Left))
			right := b.expression(e.Right)
			return &Expression{Kind: constants.WorkflowExprBinary, Name: e.Operator.String(), Arguments: []*Expression{left, right}}
		}
		return &Expression{Kind: constants.WorkflowExprBinary, Name: e.Operator.String(), Arguments: []*Expression{b.expression(e.Left), b.expression(e.Right)}}
	case *ast.UnaryExpression:
		if e.Operator == token.DELETE {
			c.fail(e, "DELETE_UNSUPPORTED", "use JSON object reconstruction instead of delete")
		}
		return &Expression{Kind: constants.WorkflowExprUnary, Name: e.Operator.String(), Postfix: e.Postfix, Arguments: []*Expression{b.expression(e.Operand)}}
	case *ast.AssignExpression:
		return &Expression{Kind: constants.WorkflowExprAssign, Name: e.Operator.String(), Arguments: []*Expression{b.expression(e.Left), b.expression(e.Right)}}
	case *ast.ConditionalExpression:
		if containsAwait(e.Consequent) || containsAwait(e.Alternate) {
			test := b.expression(e.Test)
			result := b.materialize(literalExpression(nil))
			branch := b.emit(Instruction{Op: constants.WorkflowOpBranch, Expression: test, Next: len(b.code) + 1})
			consequent := b.expression(e.Consequent)
			b.emit(Instruction{Op: constants.WorkflowOpEvaluate, Expression: &Expression{Kind: constants.WorkflowExprAssign, Name: "=", Arguments: []*Expression{result, consequent}}})
			jump := b.emit(Instruction{Op: constants.WorkflowOpJump})
			b.code[branch].Alternate = len(b.code)
			alternate := b.expression(e.Alternate)
			b.emit(Instruction{Op: constants.WorkflowOpEvaluate, Expression: &Expression{Kind: constants.WorkflowExprAssign, Name: "=", Arguments: []*Expression{result, alternate}}})
			b.code[jump].Next = len(b.code)
			return result
		}
		return &Expression{Kind: constants.WorkflowExprConditional, Arguments: []*Expression{b.expression(e.Test), b.expression(e.Consequent), b.expression(e.Alternate)}}
	case *ast.SequenceExpression:
		return &Expression{Kind: constants.WorkflowExprSequence, Arguments: b.orderedExpressions(e.Sequence)}
	case *ast.SpreadElement:
		return &Expression{Kind: constants.WorkflowExprSpread, Arguments: []*Expression{b.expression(e.Expression)}}
	case *ast.TemplateLiteral:
		if e.Tag != nil {
			c.fail(e, "TEMPLATE_TAG_UNSUPPORTED", "tagged templates are unavailable")
		}
		values := []*Expression{}
		for i, element := range e.Elements {
			values = append(values, literalExpression(element.Parsed.String()))
			if i < len(e.Expressions) {
				expression := b.expression(e.Expressions[i])
				for _, later := range e.Expressions[i+1:] {
					if containsAwait(later) {
						expression = b.materialize(expression)
						break
					}
				}
				values = append(values, expression)
			}
		}
		return &Expression{Kind: constants.WorkflowExprTemplate, Arguments: values}
	case *ast.RegExpLiteral:
		pattern, supported := nativeRegexpPattern(e.Pattern, e.Flags)
		if !supported {
			c.fail(e, "REGEXP_UNSUPPORTED", "only g/i/m/u regular expression flags are supported")
		}
		if _, err := regexp.Compile(pattern); err != nil {
			c.fail(e, "REGEXP_UNSUPPORTED", "regular expression must use the RE2 subset")
		}
		return &Expression{Kind: constants.WorkflowExprRegExp, Name: pattern, Value: e.Flags}
	case *ast.FunctionLiteral:
		return b.function(e.ParameterList, e.Body, e.Async, e)
	case *ast.ArrowFunctionLiteral:
		return b.function(e.ParameterList, e.Body, e.Async, e)
	case *ast.NewExpression:
		name, ok := e.Callee.(*ast.Identifier)
		if !ok || (name.Name.String() != "Date" && name.Name.String() != "Error") {
			c.fail(e, "NEW_UNSUPPORTED", "only frozen Date and error data are available")
		}
		args := []*Expression{b.expression(e.Callee)}
		for _, argument := range e.ArgumentList {
			args = append(args, b.expression(argument))
		}
		return &Expression{Kind: constants.WorkflowExprNew, Arguments: args}
	case *ast.AwaitExpression:
		call, ok := e.Argument.(*ast.CallExpression)
		if !ok {
			c.fail(e, "AWAIT_UNSUPPORTED", "await a direct SDK call")
			return literalExpression(nil)
		}
		if result := b.actionCall(call, true); result != nil {
			return result
		}
		name, ok := call.Callee.(*ast.Identifier)
		if !ok || !isPrimitive(name.Name.String()) {
			c.fail(e, "HOST_CALL_HIDDEN", "await must target a direct SDK primitive")
			return literalExpression(nil)
		}
		return b.hostCall(call, true)
	case *ast.CallExpression:
		if result := b.actionCall(e, false); result != nil {
			return result
		}
		if name, ok := e.Callee.(*ast.Identifier); ok && isPrimitive(name.Name.String()) {
			return b.hostCall(e, false)
		}
		callee := b.expression(e.Callee)
		for _, argument := range e.ArgumentList {
			if containsAwait(argument) {
				if callee.Kind == constants.WorkflowExprMember {
					callee.Arguments[0] = b.materialize(callee.Arguments[0])
					callee.Arguments[1] = b.materialize(callee.Arguments[1])
				} else {
					callee = b.materialize(callee)
				}
				break
			}
		}
		args := append([]*Expression{callee}, b.orderedExpressions(e.ArgumentList)...)
		return &Expression{Kind: constants.WorkflowExprCall, Arguments: args}
	default:
		c.fail(expression, "EXPRESSION_UNSUPPORTED", fmt.Sprintf("unsupported native expression %T; use functionTask", expression))
		return literalExpression(nil)
	}
}

func (b *codeBuilder) object(properties []ast.Property) *Expression {
	result := &Expression{Kind: constants.WorkflowExprObject, Properties: []Property{}}
	for index, property := range properties {
		laterAwait := false
		for _, later := range properties[index+1:] {
			if astContains(reflect.ValueOf(later), func(node ast.Node) bool { _, ok := node.(*ast.AwaitExpression); return ok }) {
				laterAwait = true
				break
			}
		}
		switch p := property.(type) {
		case *ast.PropertyKeyed:
			if p.Kind != ast.PropertyKindValue {
				b.compiler.fail(p, "ACCESSOR_UNSUPPORTED", "object accessors and methods are unavailable")
			}
			key := b.expression(p.Key)
			if !p.Computed {
				if name, ok := propertyName(p.Key); ok {
					if forbiddenKey(name) {
						b.compiler.fail(p, "PROPERTY_UNSUPPORTED", "prototype keys are unavailable")
					}
					key = literalExpression(name)
				}
			}
			if p.Computed && containsAwait(p.Value) {
				key = b.materialize(key)
			}
			value := b.expression(p.Value)
			if laterAwait {
				key = b.materialize(key)
				value = b.materialize(value)
			}
			result.Properties = append(result.Properties, Property{Key: key, Value: value})
		case *ast.PropertyShort:
			value := b.expression(&p.Name)
			if p.Initializer != nil {
				value = &Expression{Kind: constants.WorkflowExprDefault, Arguments: []*Expression{value, b.expression(p.Initializer)}}
			}
			if laterAwait {
				value = b.materialize(value)
			}
			result.Properties = append(result.Properties, Property{Key: literalExpression(p.Name.Name.String()), Value: value})
		case *ast.SpreadElement:
			value := b.expression(p.Expression)
			if laterAwait {
				value = b.materialize(value)
			}
			result.Properties = append(result.Properties, Property{Spread: true, Value: value})
		default:
			b.compiler.fail(property, "PROPERTY_UNSUPPORTED", "unsupported object field")
		}
	}
	return result
}

func (b *codeBuilder) function(parameters *ast.ParameterList, body ast.ConciseBody, async bool, node ast.Node) *Expression {
	c := b.compiler
	id := c.temporary()
	child := &codeBuilder{compiler: c, task: c.group != "", helper: c.group == ""}
	bindings := []*Expression{}
	if parameters.Rest != nil {
		c.fail(node, "REST_PARAMETER_UNSUPPORTED", "use explicit JSON parameters")
	}
	for _, parameter := range parameters.List {
		bindings = append(bindings, child.expression(parameter))
	}
	switch body := body.(type) {
	case *ast.BlockStatement:
		for _, statement := range body.List {
			child.statement(statement)
		}
	case *ast.ExpressionBody:
		child.emit(Instruction{Op: constants.WorkflowOpReturn, Expression: child.expression(body.Expression)})
	}
	child.emit(Instruction{Op: constants.WorkflowOpReturn, Expression: literalExpression(nil)})
	hasEffects := false
	for _, instruction := range child.code {
		hasEffects = hasEffects || instruction.Op == constants.WorkflowOpEffect
	}
	c.bundle.Programs[id] = Program{Parameters: bindings, Code: child.code, Async: async || hasEffects, Captures: c.captures[node]}
	return &Expression{Kind: constants.WorkflowExprFunction, Name: id}
}

func isPrimitive(name string) bool {
	for _, primitive := range Registry() {
		if primitive.Name == name {
			return true
		}
	}
	return false
}

func (b *codeBuilder) hostCall(call *ast.CallExpression, awaited bool) *Expression {
	c := b.compiler
	name := call.Callee.(*ast.Identifier).Name.String()
	if b.actionContext != "" {
		c.fail(call, "ACTION_CAPABILITY_UNSUPPORTED", "handlers may only use ctx.log and ctx.callTool")
	}
	location := c.parsed.sourceRange(call)
	counts := map[string]int{"phase": 1, "worker": 2, "askUser": 2, "parallel": 2, "pipeline": 3, "workflow": 2, "startWorkflow": 2, "joinWorkflow": 1, "action": 2, "functionTask": 1}
	if expected, ok := counts[name]; ok && len(call.ArgumentList) != expected {
		c.fail(call, "ARGUMENT_COUNT_INVALID", fmt.Sprintf("%s requires %d arguments", name, expected))
	}
	if name == "log" && (len(call.ArgumentList) < 1 || len(call.ArgumentList) > 2) {
		c.fail(call, "ARGUMENT_COUNT_INVALID", "log requires one message and optional level options")
	}
	if b.helper {
		c.fail(call, "HOST_CALL_HIDDEN", "pure helper functions cannot call SDK primitives; use an explicit task/stage body")
	}
	if name != "phase" && name != "log" && name != "startWorkflow" && !awaited && !b.task {
		c.fail(call, "AWAIT_REQUIRED", "await "+name+" or use it inside an async task")
	}
	var nodeID string
	oldGroup := c.group
	if name == "parallel" || name == "pipeline" {
		index := 1
		if name == "pipeline" {
			index = 2
		}
		opts := c.staticOptions(call, index)
		for key := range opts {
			if key != "id" && key != "failureMode" && key != "concurrency" {
				c.fail(call, "OPTION_UNKNOWN", "unknown group option: "+key)
			}
		}
		if mode, found := opts["failureMode"]; found && mode != "collect-null" && mode != "fail-fast" {
			c.fail(call, "FAILURE_MODE_INVALID", "group failureMode must be collect-null or fail-fast")
		}
		if value, found := opts["concurrency"]; found {
			number, ok := value.(float64)
			if !ok || number < 1 || number > 8 || number != float64(int(number)) {
				c.fail(call, "CONCURRENCY_INVALID", "group concurrency must be an integer from 1 to 8")
			}
		}
		nodeID, _ = opts["id"].(string)
		c.unique(call, nodeID)
		c.group = nodeID
		c.manifest.Groups = append(c.manifest.Groups, Group{ID: nodeID, Kind: name, Range: location})
	} else if name == "phase" {
		if len(call.ArgumentList) != 1 {
			c.fail(call, "PHASE_INVALID", "phase requires one static id")
		} else {
			value, ok := c.literal(call.ArgumentList[0], true, map[string]bool{})
			id, _ := value.(string)
			found := false
			for _, phase := range c.manifest.Meta.Phases {
				found = found || phase.ID == id
			}
			if !ok || !found {
				c.fail(call, "PHASE_UNKNOWN", "phase references an undeclared static id")
			}
		}
	} else if name != "log" && name != "joinWorkflow" {
		index := 1
		if name == "functionTask" {
			index = 0
		}
		opts := c.staticOptions(call, index)
		c.validateNodeOptions(call, name, opts)
		nodeID, _ = opts["id"].(string)
		c.unique(call, nodeID)
		phaseID, _ := opts["phase"].(string)
		found := false
		for _, phase := range c.manifest.Meta.Phases {
			found = found || phase.ID == phaseID
		}
		if !found {
			c.fail(call, "PHASE_UNKNOWN", "node requires an explicit declared phase id")
		}
		label, _ := opts["label"].(string)
		prompt := ""
		if name != "functionTask" && len(call.ArgumentList) > 0 {
			prompt = c.fragment(call.ArgumentList[0])
		}
		if label == "" {
			label = c.optionFragment(call, index, "label")
			if label == "" {
				c.fail(call, "LABEL_REQUIRED", "node label is required")
			}
		}
		if name == "worker" {
			resolve, ok := opts["resolve"].(map[string]any)
			if !ok || resolve["kind"] != "waker" || resolve["wakerId"] == nil || resolve["wakerId"] == "" {
				c.fail(call, "RESOLVE_STATIC_REQUIRED", "worker requires a static waker resolve")
			}
		}
		if name == "functionTask" {
			language, _ := opts["language"].(string)
			code, _ := opts["code"].(string)
			if (language != "javascript" && language != "python") || code == "" {
				c.fail(call, "FUNCTION_INVALID", "functionTask requires static javascript/python language and code")
			}
		}
		if schema, ok := opts["schema"].(map[string]any); ok {
			if _, err := ResolveSchema(schema); err != nil {
				c.fail(call, "SCHEMA_UNSUPPORTED", "node schema is invalid")
			}
		}
		c.manifest.Nodes = append(c.manifest.Nodes, Node{ID: nodeID, Kind: name, PhaseID: phaseID, LabelTemplate: label, PromptTemplate: prompt, Options: opts, Range: location, GroupID: c.group, LoopDepth: c.loopDepth})
	}
	arguments := b.orderedExpressions(call.ArgumentList)
	c.group = oldGroup
	destination := c.temporary()
	b.emit(Instruction{Op: constants.WorkflowOpEffect, Primitive: name, NodeID: nodeID, Expression: &Expression{Kind: constants.WorkflowExprArray, Arguments: arguments}, Destination: destination, Range: location})
	return identifierExpression(destination)
}

func (b *codeBuilder) materialize(expression *Expression) *Expression {
	name := b.compiler.temporary()
	b.emit(Instruction{Op: constants.WorkflowOpDeclare, Target: identifierExpression(name), Expression: expression})
	return identifierExpression(name)
}

// containsAwait 只在固定 AST adapter 内遍历语法字段，不反射发现任何业务实现。
func containsAwait(expression ast.Expression) bool {
	return astContains(reflect.ValueOf(expression), func(node ast.Node) bool { _, ok := node.(*ast.AwaitExpression); return ok })
}

func astContains(value reflect.Value, predicate func(ast.Node) bool) bool {
	if !value.IsValid() {
		return false
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return false
		}
		if value.CanInterface() {
			if node, ok := value.Interface().(ast.Node); ok && predicate(node) {
				return true
			}
		}
		return astContains(value.Elem(), predicate)
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			if field.Name == "DeclarationList" || field.Name == "Source" {
				continue
			}
			if astContains(value.Field(i), predicate) {
				return true
			}
		}
	case reflect.Slice:
		for i := 0; i < value.Len(); i++ {
			if astContains(value.Index(i), predicate) {
				return true
			}
		}
	}
	return false
}

func (c *compiler) fragment(node ast.Node) string {
	r := c.parsed.sourceRange(node)
	return c.parsed.source[r.Start.Offset:r.End.Offset]
}
func (c *compiler) optionFragment(call *ast.CallExpression, index int, name string) string {
	if len(call.ArgumentList) <= index {
		return ""
	}
	object, ok := call.ArgumentList[index].(*ast.ObjectLiteral)
	if !ok {
		return ""
	}
	for _, property := range object.Value {
		if p, ok := property.(*ast.PropertyKeyed); ok {
			key, _ := propertyName(p.Key)
			if key == name {
				return c.fragment(p.Value)
			}
		}
	}
	return ""
}
func (c *compiler) staticOptions(call *ast.CallExpression, index int) map[string]any {
	result := map[string]any{}
	if len(call.ArgumentList) <= index {
		c.fail(call, "OPTIONS_REQUIRED", "SDK call requires an options literal")
		return result
	}
	object, ok := call.ArgumentList[index].(*ast.ObjectLiteral)
	if !ok {
		c.fail(call, "OPTIONS_LITERAL_REQUIRED", "SDK options must be an object literal")
		return result
	}
	for _, property := range object.Value {
		p, ok := property.(*ast.PropertyKeyed)
		if !ok || p.Computed {
			c.fail(property, "OPTIONS_FIELD_UNSUPPORTED", "SDK options require fixed fields")
			continue
		}
		key, ok := propertyName(p.Key)
		if !ok {
			continue
		}
		value, static := c.literal(p.Value, true, map[string]bool{})
		if static {
			result[key] = value
		} else {
			if key == "revisionId" {
				if primitive, ok := call.Callee.(*ast.Identifier); ok && (primitive.Name.String() == "workflow" || primitive.Name.String() == "startWorkflow") {
					// 子版本在完整程序编译后按实际词法绑定核验。
					continue
				}
			}
			switch key {
			case "input", "args", "url", "method", "headers", "body", "env", "label":
			default:
				c.fail(p.Value, "STATIC_OPTION_REQUIRED", "SDK option must be static: "+key)
				diagnostic := &c.diagnostics[len(c.diagnostics)-1]
				diagnostic.Field = key
				diagnostic.Hint = "Use a JSON literal or a top-level literal constant; property access is not statically resolvable."
			}
		}
	}
	return result
}

func (b *codeBuilder) orderedExpressions(expressions []ast.Expression) []*Expression {
	later := make([]bool, len(expressions))
	hasAwait := false
	for index := len(expressions) - 1; index >= 0; index-- {
		later[index] = hasAwait
		hasAwait = hasAwait || containsAwait(expressions[index])
	}
	result := make([]*Expression, 0, len(expressions))
	for index, expression := range expressions {
		value := b.expression(expression)
		if later[index] {
			value = b.materialize(value)
		}
		result = append(result, value)
	}
	return result
}
