package dsl

import (
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

// Meta 是从纯字面量解析的原生工作流声明。
type Meta struct {
	// SDK 是必须与宿主匹配的声明版本。
	SDK string `json:"sdk"`
	// Title 是内置伙伴根据用户目标生成的工作流标题。
	Title string `json:"title"`
	// Description 描述工作流的业务结果。
	Description string `json:"description"`
	// WhenToUse 描述适合启动此流程的场景。
	WhenToUse string `json:"whenToUse,omitempty"`
	// InputSchema 校验运行参数。
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	// OutputSchema 校验最终返回值。
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	// NodeResultPages 按稳定节点身份声明需要向用户展示的产出页面。
	NodeResultPages map[string]PageDefinition `json:"nodeResultPages,omitempty"`
	// NodeResultFiles 声明以真实文件交付的伙伴产出，复用宿主文件查看器。
	NodeResultFiles map[string]FileResultDefinition `json:"nodeResultFiles,omitempty"`
	// NodeResultPageOmissions 记录逐个评估伙伴输出后无需展示页面的具体理由。
	NodeResultPageOmissions map[string]string `json:"nodeResultPageOmissions,omitempty"`
	// InputPage 是可选的启动参数布局，字段语义仍由 InputSchema 决定。
	InputPage *PageDefinition `json:"inputPage,omitempty"`
	// Phases 是稳定身份的展示阶段。
	Phases []Phase `json:"phases"`
	// Actions 是源码允许调用的确定性动作声明。
	Actions []ActionDeclaration `json:"actions,omitempty"`
	// Limits 是不超过宿主上限的流程预算。
	Limits Limits `json:"limits,omitempty"`
}

// Limits 保存创作声明的运行预算。
type Limits struct {
	// Concurrency 限制运行外部调用的并发数。
	Concurrency int `json:"concurrency,omitempty"`
	// MaxInstances 限制源码节点实例化次数。
	MaxInstances int `json:"maxInstances,omitempty"`
	// MaxLoopSteps 限制循环推进次数。
	MaxLoopSteps int `json:"maxLoopSteps,omitempty"`
	// TimeoutMillis 限制运行总耗时。
	TimeoutMillis int64 `json:"timeoutMs,omitempty"`
}

// Phase 是源码声明的阶段身份与显示关系。
type Phase struct {
	// ID 是跨版本保持的阶段身份。
	ID string `json:"id"`
	// Title 是阶段标题。
	Title string `json:"title"`
	// Detail 是阶段职责的完整说明。
	Detail string `json:"detail"`
	// Next 是展示上的后继阶段身份。
	Next []string `json:"next,omitempty"`
	// CanExit 表示此阶段允许流程结束。
	CanExit bool `json:"canExit,omitempty"`
}

// ActionDeclaration 冻结确定性动作允许使用的宿主能力。
type ActionDeclaration struct {
	// ID 是动作稳定身份。
	ID string `json:"id"`
	// Label 是用户可读名称。
	Label string `json:"label"`
	// Description 是业务用途说明。
	Description string `json:"description"`
	// Risk 是读取、写入或外部效果的展示分类。
	Risk string `json:"risk"`
	// Type 选择 HTTP、argv 命令或内置处理器。
	Type string `json:"type"`
	// InputSchema 校验动作输入。
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	// OutputSchema 校验动作输出。
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	// AllowedOrigins 限制 HTTP 目标源。
	AllowedOrigins []string `json:"allowedOrigins,omitempty"`
	// Methods 限制允许的 HTTP 方法。
	Methods []string `json:"methods,omitempty"`
	// Executable 固定 argv 命令的可执行文件。
	Executable string `json:"executable,omitempty"`
	// WorkingDirectory 固定命令只能使用批准的流程目录。
	WorkingDirectory string `json:"workingDirectory,omitempty"`
	// Handler 指向内置动作的源码处理函数。
	Handler string `json:"handler,omitempty"`
	// ConnectorIDs 限制内置处理器可代理的连接器。
	ConnectorIDs []string `json:"connectorIds,omitempty"`
	// IMTargetIDs 声明动态 IM 投递可使用的聊天身份，固定目标另从编译程序派生。
	IMTargetIDs []string `json:"imTargetIds,omitempty"`
}

// Manifest 是只读画布与详情使用的源码投影。
type Manifest struct {
	// InputPropertyOrder 按输入 Schema 的 JSON Pointer 保留源码属性声明顺序，仅用于只读表单布局。
	InputPropertyOrder map[string][]string `json:"inputPropertyOrder,omitempty"`
	// Meta 是源码的完整声明。
	Meta Meta `json:"meta"`
	// Nodes 是按源码位置排列的调用模板。
	Nodes []Node `json:"nodes"`
	// Groups 是并行或管道分组模板。
	Groups []Group `json:"groups"`
	// DisplayEdges 是阶段间展示关系。
	DisplayEdges []DisplayEdge `json:"displayEdges"`
	// ControlHints 保留源码的控制结构与调用身份，不参与调度或成为第二份编辑数据。
	ControlHints []ControlHint `json:"controlHints"`
}

// ControlHint 是固定 AST 派生的只读控制结构，分支内顺序与原始求值顺序一致。
type ControlHint struct {
	// ID 是当前 revision 内确定的控制结构身份，不用作运行实例或跨版本节点身份。
	ID string `json:"id"`
	// Kind 区分调用、判断、循环、任务分组和控制退出。
	Kind constants.WorkflowControlKind `json:"kind"`
	// NodeID 关联已有的可编辑调用模板；逻辑门本身不能编辑。
	NodeID string `json:"nodeId,omitempty"`
	// GroupID 关联原始 parallel 或 pipeline 的稳定身份。
	GroupID string `json:"groupId,omitempty"`
	// Concurrency 是分组声明或宿主默认的最大并发数，非分组结构省略。
	Concurrency int `json:"concurrency,omitempty"`
	// FailureMode 是分组失败后的真实收敛策略，非分组结构省略。
	FailureMode string `json:"failureMode,omitempty"`
	// PhaseID 为调用与退出指定展示阶段，复合结构由其子调用确定阶段。
	PhaseID string `json:"phaseId,omitempty"`
	// Condition 保留判断或迭代对象的原始源码，用于解释路径条件。
	Condition string `json:"condition,omitempty"`
	// Range 定位原始 UTF-8 源码，避免在客户端重新解析 JavaScript。
	Range SourceRange `json:"range"`
	// Branches 保存互斥分支、独立任务、管道步骤或异常处理块。
	Branches []ControlBranch `json:"branches,omitempty"`
}

// ControlBranch 是控制结构内的一条静态路径或任务模板。
type ControlBranch struct {
	// Label 是 true、false、body、catch、finally 或任务序号等路径语义。
	Label string `json:"label"`
	// Hints 是此路径依次执行的控制结构，空数组表示直接通过。
	Hints []ControlHint `json:"hints"`
}

// DisplayEdge 表达阶段声明的展示连线。
type DisplayEdge struct {
	// From 是起点阶段身份。
	From string `json:"from"`
	// To 是终点阶段身份。
	To string `json:"to"`
}

// Node 是一个稳定的源码调用模板。
type Node struct {
	// ID 是跨版本保持的节点身份。
	ID string `json:"id"`
	// Kind 是 SDK primitive 名称。
	Kind string `json:"kind"`
	// PhaseID 是显式阶段引用。
	PhaseID string `json:"phaseId"`
	// LabelTemplate 保留动态标题的原始模板。
	LabelTemplate string `json:"labelTemplate"`
	// PromptTemplate 保留 prompt 的源码文本。
	PromptTemplate string `json:"promptTemplate,omitempty"`
	// Options 是静态可解析的配置，不含凭据值。
	Options map[string]any `json:"options"`
	// Range 指向原始源码调用。
	Range SourceRange `json:"range"`
	// GroupID 是所属并行或管道模板。
	GroupID string `json:"groupId,omitempty"`
	// LoopDepth 是源码嵌套循环层数。
	LoopDepth int `json:"loopDepth"`
}

// Group 描述静态并行和管道调用。
type Group struct {
	// ID 是分组的稳定身份。
	ID string `json:"id"`
	// Kind 区分并行与逐项管道。
	Kind string `json:"kind"`
	// Range 是分组源码位置。
	Range SourceRange `json:"range"`
}

// ProgramBundle 包含主流程和独立 callback 程序。
type ProgramBundle struct {
	// Version 是序列化指令格式版本。
	Version int `json:"version"`
	// Entry 是主流程程序身份。
	Entry string `json:"entry"`
	// Programs 是按身份寻址的程序集合。
	Programs map[string]Program `json:"programs"`
	// ActionPrograms 绑定独立内置处理器的静态入口。
	ActionPrograms map[string]string `json:"actionPrograms,omitempty"`
}

// Program 是一组可恢复的 Go 指令。
type Program struct {
	// Captures 是此程序实际读取的外层绑定，避免复制无关上下文和其他 continuation。
	Captures []string `json:"captures,omitempty"`
	// Parameters 是复制传入的参数绑定模式。
	Parameters []*Expression `json:"parameters,omitempty"`
	// Code 是通过 PC 推进的指令序列。
	Code []Instruction `json:"code"`
	// Async 表示仅能作为宿主识别的任务 continuation 使用。
	Async bool `json:"async"`
}

// Instruction 只保存可序列化数据，不包含 VM 栈或 Go 闭包。
type Instruction struct {
	// Op 是 Go Runner 解释的指令名称。
	Op string `json:"op"`
	// Expression 是纯数据表达式或效果参数。
	Expression *Expression `json:"expression,omitempty"`
	// Target 是变量或解构绑定目标。
	Target *Expression `json:"target,omitempty"`
	// Destination 是效果返回值的内部 slot 名称。
	Destination string `json:"destination,omitempty"`
	// Constant 表示此声明不允许重新赋值。
	Constant bool `json:"constant,omitempty"`
	// Next 是跳转或真分支目标 PC。
	Next int `json:"next,omitempty"`
	// Alternate 是假分支或异常处理目标 PC。
	Alternate int `json:"alternate,omitempty"`
	// Finally 是异常或正常退出时的 finally 目标 PC。
	Finally int `json:"finally,omitempty"`
	// ScopeDrop 是循环控制跳转需要退出的程序内词法作用域数量。
	ScopeDrop int `json:"scopeDrop,omitempty"`
	// HandlerDepth 是循环控制目标仍保留的外层异常处理数量。
	HandlerDepth int `json:"handlerDepth,omitempty"`
	// Primitive 是宿主效果名称。
	Primitive string `json:"primitive,omitempty"`
	// NodeID 是效果关联的源码稳定身份。
	NodeID string `json:"nodeId,omitempty"`
	// Range 是该指令的原始源码位置。
	Range SourceRange `json:"range"`
}

// Expression 是受限 JSON 表达式树。
type Expression struct {
	// Kind 是表达式种类。
	Kind string `json:"kind"`
	// Value 是字面量 JSON 数据。
	Value any `json:"value,omitempty"`
	// Name 是变量、运算符或 callback 程序身份。
	Name string `json:"name,omitempty"`
	// Arguments 是按求值顺序存放的子表达式。
	Arguments []*Expression `json:"arguments,omitempty"`
	// Properties 是对象字段或对象解构目标。
	Properties []Property `json:"properties,omitempty"`
	// Optional 表示成员访问允许空值。
	Optional bool `json:"optional,omitempty"`
	// Postfix 区分前置和后置自增自减。
	Postfix bool `json:"postfix,omitempty"`
}

// Property 是对象表达式中的单个字段。
type Property struct {
	// Key 是固定或计算出的字段名。
	Key *Expression `json:"key,omitempty"`
	// Value 是字段数据或绑定目标。
	Value *Expression `json:"value"`
	// Spread 表示展开一个对象。
	Spread bool `json:"spread,omitempty"`
}

// Compilation 是同时保存源码投影与执行程序的完整编译结果。
type Compilation struct {
	// Manifest 是不可编辑的定义投影。
	Manifest Manifest `json:"manifest"`
	// Bundle 是与同一源码绑定的 Go 执行程序。
	Bundle ProgramBundle `json:"bundle"`
	// SourceDigest 是原始 UTF-8 字节摘要。
	SourceDigest string `json:"sourceDigest"`
	// ManifestDigest 是确定序列化的投影摘要。
	ManifestDigest string `json:"manifestDigest"`
	// IRDigest 是确定序列化的执行程序摘要。
	IRDigest string `json:"irDigest"`
}

func cloneJSON(value any) any {
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var copied any
	if json.Unmarshal(data, &copied) != nil {
		return nil
	}
	return copied
}
