package dsl

import (
	"embed"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/workflow/catalog"
	"slices"
	"strings"
)

//go:embed testdata/f0.workflow.js testdata/weekly-report.workflow.js testdata/environment-review.workflow.js testdata/environment-release.workflow.js
var exampleFiles embed.FS

// sdkSection 显式维护每条知识的主题归属，不依赖英文段落前缀。
type sdkSection struct {
	// id 是规则在知识目录中的稳定身份。
	id string
	// topics 是需要该规则的主题。
	topics []string
	// content 是完整的创作规则。
	content string
}

func sdkSections() []sdkSection {
	return []sdkSection{
		{
			id:     "source",
			topics: []string{"overview", "saving"},
			content: "The source is authoritative. Use stable ASCII IDs for phases, nodes, groups and " +
				"actions. Never change an existing ID just to rename its label. Meta is a pure JSON " +
				"literal. Worker identities, schemas and tool policies must be statically resolvable.",
		},
		{
			id:     "dsl",
			topics: []string{"overview", "function"},
			content: "The host compiles main logic to durable Go instructions, with JSON value semantics. " +
				"This is a restricted DSL, not a full JavaScript VM. Assignment and captures copy " +
				"values; do not write outer mutable captures from parallel tasks. Strings use Unicode " +
				"code points. Regular expressions use the RE2 common subset (g/i/m/u). Date is frozen " +
				"to run start. Put general JavaScript or Python into functionTask.",
		},
		{
			id:     "function",
			topics: []string{"function"},
			content: "Function code is a complete program, not a bare function body: JavaScript must declare" +
				" function main(input) { return JSON; }; Python must define def main(input): and return" +
				" a JSON value. JavaScript main must be synchronous. Do not put a top-level return in " +
				"the code string. Only the returned value is the Function result. Node outputSchema is " +
				"optional. If supplied, use a JSON literal or a top-level literal constant; property " +
				"access such as meta.outputSchema is not statically resolvable. Omit it only when " +
				"node-level output validation is unnecessary.",
		},
		{
			id:     "schema",
			topics: []string{"schema"},
			content: "Every user-facing inputSchema property must include a short title and a concise " +
				"description in the user language. The input-parameter preview shows the title and " +
				"description separately. Titles are noun phrases, ideally 2-10 Chinese characters or " +
				"1-4 words; descriptions are one short sentence, ideally at most 40 Chinese characters " +
				"or 20 words. State what to provide and only the essential format, unit or " +
				"missing-input behavior. Do not repeat the title, prefix text with required/optional, " +
				"add long parenthetical explanations or describe the whole workflow. Express " +
				"requiredness only through the schema required array and actual default values through " +
				"schema default. Keep detailed business rules, branches and follow-up questions in " +
				"worker instructions or workflow logic; preserve essential user-facing constraints in " +
				"concise wording. For example, title: Requirements, description: Paste the current " +
				"requirements.; title: Environment, description: Provide one environment and version " +
				"per line.; title: Approval threshold, description: Ask for approval when remaining " +
				"high-risk items reach this count., default: 1. Include only inputs needed for the user" +
				" goal, and reuse these concise labels and descriptions when summarizing parameters in " +
				"chat. Use stable keys for code, never as the only form label. Declare file inputs and " +
				"final file outputs as {type: 'string', format: 'file', title, description}. Arrays use" +
				" items with this same file schema. Input forms upload files and run admission replaces" +
				" uploaded ally-file references with real absolute paths in the approved run scope. " +
				"Workers must read those paths using actual tools; never treat references as file " +
				"contents. Final file values must be paths to real files created inside " +
				"flowContext.scope.workingDirectory; the user and automatic task API can download these" +
				" files. Return actual paths rather than base64 data or made-up download URLs. Ordinary" +
				" strings are never inferred to be file outputs. Schema presentation supports " +
				"x-ui-control: input or textarea for strings, radio or select for scalar enum, switch " +
				"or checkbox for booleans, and checkboxes for arrays whose items.enum lists the allowed" +
				" values. x-ui-labels supplies user-facing choice labels in enum order. These " +
				"annotations only select controls; values still follow the declared JSON Schema types " +
				"and constraints. Use these same annotations in askUser schemas.",
		},
		{
			id:     "await",
			topics: []string{"human", "parallel", "pipeline", "workflow", "action"},
			content: "Await workers, human inputs, actions and child results. Parallel returns in input " +
				"order. Pipeline advances each item immediately after its own preceding stage, then " +
				"joins all items in original order. A null stage result is ordinary JSON, not a " +
				"failure. Explicit throw stops the failed item.",
		},
		{
			id:     "human",
			topics: []string{"human"},
			content: "Human defaults do not submit or approve. Approvals require a positive explicit value; " +
				"skip and timeout preserve their source. Actions use separate operation permissions.",
		},
		{
			id:     "workspace",
			topics: []string{"worker"},
			content: "Worker chats use the same managed runtime workspace and native instruction-file " +
				"loading as ordinary chats. Their runtime working directories are isolated; the " +
				"approved business directory is shared through project links and additional directory " +
				"authorization. Build shared file paths from flowContext.scope.workingDirectory and " +
				"pass those absolute paths to worker prompts and results. Never assume two workers have" +
				" the same process working directory. Use run-specific output paths, require actual " +
				"tools to create and read deliverable files, and verify existence and nonempty content.",
		},
		{
			id:     "results",
			topics: []string{"worker"},
			content: "A structured worker must call allycli workflow results submit with a stable " +
				"--request-id and an input JSON object whose data matches its schema. A submission is " +
				"only a candidate until its Execution succeeds. Do not rely on JSON pasted into a final" +
				" reply. For a root file schema, data is the actual file path. Add routing or summary " +
				"fields only for real downstream consumers.",
		},
		{
			id:     "im-delivery",
			topics: []string{"im"},
			content: "When the user requests delivery to WeChat or Feishu, discover approved chats with " +
				"allycli workflow resources list --kind imTargets. Discover the native IM operation " +
				"through allycli workflow resources list --kind builtinActions, tool im_delivery, " +
				"through a builtin action declaring its connector ID and awaiting ctx.callTool({callId," +
				" connectorId, toolName: 'im_delivery', input: {action: 'deliveries.create', targetId, " +
				"content, files}}). Fixed literal targetIds are derived as workflow resource " +
				"dependencies. Dynamic targetIds must declare the approved imTargetIds on the builtin " +
				"action; runtime delivery cannot use an undeclared target. Admission checks these " +
				"targets before any node runs. This durable action waits for delivery acceptance; query" +
				" deliveries.get to verify sent, failed or unknown status. Never invent recipients or " +
				"connector IDs. Resource IDs come from allycli workflow resources list; never invent or" +
				" silently replace them. Do not send messages or publish outside an explicitly " +
				"authorized action.",
		},
		{
			id:     "evidence",
			topics: []string{"saving"},
			content: "When optimizing a failed run, read all allycli workflow evidence list pages, including" +
				" run.failure, instance failures and actionCalls. Inspect nested cause, hint, " +
				"sourceRange and delivery receipts including partsSent and unknown status before " +
				"proposing a repair. Preserve original call identities and never repeat an already " +
				"completed or uncertain external operation. Read the current revision and pending " +
				"annotations before saving. Validate complete source, fix diagnostics, then allycli " +
				"workflow source save using expectedRevisionId and a stable requestId. At most three " +
				"repair attempts per turn. Only the tool save receipt proves the source is active.\n",
		},
		{
			id:     "inputs-basic",
			topics: []string{"overview"},
			content: "Declare user-provided values in meta.inputSchema and consume them through args. Give " +
				"every input a concise title and description in the user language. Keep secrets and " +
				"placeholders out of defaults. Declare file references with type:string, format:file; a" +
				" reference is not file content.",
		},
		{
			id:     "python",
			topics: []string{"function"},
			content: "Python requires a configured interpreter and executionMode:trusted_local. JavaScript " +
				"supports pure execution. Authoring prepare includes functionLanguages when language is" +
				" python.",
		},
		{
			id:     "function-timeouts",
			topics: []string{"function"},
			content: "JavaScript/Python functionTask defaults to 600000 ms (10 minutes), with the same hard " +
				"maximum. Omit timeoutMs unless the user agreed to a shorter deadline. The entire run " +
				"has a separate 24-hour host deadline; do not invent a shorter overall budget.",
		},
		{
			id:     "worker-timeouts",
			topics: []string{"worker"},
			content: "Workers have no independent timeout; worker timeoutMs is unsupported. User " +
				"cancellation and actual execution failure remain effective.",
		},
		{
			id:     "action-timeouts",
			topics: []string{"action"},
			content: "HTTP actions default to 30000 ms. Command/script actions default to 600000 ms, with " +
				"the same hard maximum. Omit timeoutMs unless the user agreed to a shorter deadline.",
		},
		{
			id:     "saving",
			topics: []string{"overview", "saving"},
			content: "source save compiles and validates complete source, checks policies and " +
				"expectedRevisionId, then commits an immutable revision and its receipt together. Use " +
				"--source for the UTF-8 file and a stable --request-id. A successful immediate receipt " +
				"with saved:true, revisionId and sourceDigest proves saving; unchanged:true means the " +
				"current revision already has that exact source. No source get is needed after success." +
				" If the response is lost, query receipts get for the original operation/request ID. " +
				"Read current source again on revision conflict. Saving does not prove a real run or " +
				"external delivery succeeded.",
		},
		{
			id:     "budgets",
			topics: []string{"budgets"},
			content: "Run budgets have different meanings: limits.concurrency caps simultaneous external " +
				"calls, while limits.maxInstances caps the cumulative number of node instances in the " +
				"entire run, including actions, functionTask, workers, human requests and " +
				"child-workflow calls. Every loop iteration or batch task consumes additional " +
				"instances; phase and log do not. Serial execution uses concurrency:1, never " +
				"maxInstances:1 for a multi-step workflow. Unless the user requested a smaller total " +
				"budget, omit maxInstances to use the bounded host default. If declaring it, count all " +
				"required steps and allow for loops, batches and branches. The compiler rejects budgets" +
				" below a provable minimum path, but dynamic paths may require more.",
		},
		{id: "worker-delivery", topics: []string{"worker"}, content: constants.WorkflowResultPageDecisionInstructions},
	}
}

// sdkDeclaration 保存完整类型与适用主题，避免截断多行声明。
type sdkDeclaration struct {
	// name 是声明去重身份。
	name string
	// topics 是适用主题；空集合表示公共值类型。
	topics []string
	// content 是完整的 TypeScript 声明。
	content string
}

func sdkDeclarations() []sdkDeclaration {
	return []sdkDeclaration{
		{
			name:    "Json",
			topics:  []string{},
			content: "type Json = null | boolean | number | string | Json[] | { [key: string]: Json };",
		},
		{
			name:    "Schema",
			topics:  []string{},
			content: "type Schema = { [key: string]: Json };",
		},
		{
			name:   "VisualNode",
			topics: []string{},
			content: "interface VisualNode { id: string; label: string; phase: string; inputs?: string[]; " +
				"outputs?: string[] }",
		},
		{
			name:   "WakerResolve",
			topics: []string{"worker"},
			content: "interface WakerResolve { kind: 'waker'; wakerId: string; runtimeId?: string; " +
				"accountId?: string; model?: string; reasoning?: string; permissionLevel?: " +
				"'default'|'auto'|'full-access' }",
		},
		{
			name:   "ToolPolicy",
			topics: []string{"worker"},
			content: "interface ToolPolicy { connectors?: {mode:'inherit'} | " +
				"{mode:'allowlist';connectorIds:string[]}; nativeTools?: {mode:'inherit'} | " +
				"{mode:'allowlist';capabilityIds:string[]} }",
		},
		{
			name:   "WorkerOptions",
			topics: []string{"worker"},
			content: "interface WorkerOptions { resolve: WakerResolve; schema?: Schema; toolPolicy?: " +
				"ToolPolicy; skippable?: boolean; retry?: " +
				"{maxAttempts:number;safety:'pure'|'idempotent';idempotencyKey?:string} }",
		},
		{
			name:   "HumanOptions",
			topics: []string{"human"},
			content: "interface HumanOptions { options?: Array<{value:string;label:string}>; " +
				"multiSelect?:boolean; schema?:Schema; defaultValue?:Json; timeoutMs?:number; " +
				"allowSkip?:boolean }",
		},
		{
			name:   "HumanAnswer",
			topics: []string{"human"},
			content: "interface HumanAnswer { value:Json; skipped:boolean; source:'user'|'skip'|'timeout'; " +
				"requestId:string }",
		},
		{
			name:   "GroupOptions",
			topics: []string{"parallel", "pipeline"},
			content: "interface GroupOptions { id:string; failureMode?:'collect-null'|'fail-fast'; " +
				"concurrency?:number }",
		},
		{
			name:    "ChildOptions",
			topics:  []string{"workflow"},
			content: "interface ChildOptions { args?:Json; revisionId?:string; timeoutMs?:number }",
		},
		{
			name:   "ChildResult",
			topics: []string{"workflow"},
			content: "interface ChildResult { runId:string; revisionId:string; status:'succeeded'; " +
				"result:Json }",
		},
		{
			name:   "workflowChildHandleBrand",
			topics: []string{"workflow"},
			content: "/** Opaque durable handle passed only between startWorkflow and joinWorkflow. */\n" +
				"declare const workflowChildHandleBrand: unique symbol;",
		},
		{
			name:    "ChildHandle",
			topics:  []string{"workflow"},
			content: "interface ChildHandle { readonly [workflowChildHandleBrand]: true }",
		},
		{
			name:   "ActionOptions",
			topics: []string{"action"},
			content: "interface ActionOptions { input?:Json; timeoutMs?:number; url?:string; method?:string;" +
				" headers?:Record<string,string>; body?:Json; " +
				"secretHeaders?:Record<string,{secretRef:string}>; args?:string[]; " +
				"env?:Record<string,string>; secretEnv?:Record<string,{secretRef:string}>; " +
				"schema?:Schema }",
		},
		{
			name:   "ActionResult",
			topics: []string{"action"},
			content: "interface ActionResult { data:Json; durationMs:number; " +
				"http?:{status:number;headers:Record<string,string>;body:string} }",
		},
		{
			name:   "FunctionOptions",
			topics: []string{"function"},
			content: "/** code must define synchronous function main(input) for JavaScript, or def " +
				"main(input) for Python, returning JSON. */\ninterface FunctionOptions { " +
				"language:'javascript'|'python'; code:string; input?:Json; outputSchema?:Schema; " +
				"timeoutMs?:number; executionMode?:'pure'|'trusted_local' }",
		},
		{
			name:   "ActionDeclaration",
			topics: []string{"overview", "action"},
			content: "type ActionDeclaration = {id:string;label:string;description:string;risk:'read'|'write" +
				"'|'external';inputSchema?:Schema;outputSchema?:Schema} & " +
				"({type:'http';allowedOrigins:string[];methods:string[]} | " +
				"{type:'script';executable:string;workingDirectory:'workflow_scope'} | " +
				"{type:'builtin';handler:string;connectorIds?:string[];imTargetIds?:string[]});",
		},
		{
			name:   "WorkflowLimits",
			topics: []string{"overview", "budgets"},
			content: "interface WorkflowLimits {\n  /** Maximum simultaneous external calls; use 1 for serial" +
				" execution. */\n  concurrency?: number;\n  /** Cumulative node instances across the " +
				"whole run, including loop and batch items; omit for the bounded host default. */\n  " +
				"maxInstances?: number;\n  /** Cumulative loop steps across the whole run. */\n  " +
				"maxLoopSteps?: number;\n  /** Total run deadline in milliseconds. */\n  timeoutMs?: " +
				"number;\n}",
		},
		{
			name:   "PresentationPage",
			topics: []string{"overview", "presentation"},
			content: "interface PresentationPage {formatVersion:1;catalogVersion:'workflow-result-v1'|'workf" +
				"low-input-v1';title?:string;description?:string;spec:{root:string;elements:Record<stri" +
				"ng,Json>}}",
		},
		{
			name:   "WorkflowMeta",
			topics: []string{"overview"},
			content: "interface WorkflowMeta {nodeResultPages?:Record<string,PresentationPage>;nodeResultFil" +
				"es?:Record<string,{title:string;description?:string}>;nodeResultPageOmissions?:Record<" +
				"string,string>;inputPage?:PresentationPage;sdk:'ally.workflow/1';title:string;descript" +
				"ion:string;whenToUse?:string;inputSchema?:Schema;outputSchema?:Schema;phases:Array<{id" +
				":string;title:string;detail:string;next?:string[];canExit?:boolean}>;actions?:ActionDe" +
				"claration[];limits?:WorkflowLimits}",
		},
		{
			name:    "args",
			topics:  []string{"overview"},
			content: "declare const args: Json;",
		},
		{
			name:   "flowContext",
			topics: []string{"overview", "worker"},
			content: "declare const flowContext: Readonly<{workflowId:string;revisionId:string;runId:string;" +
				"workflowName:string;parentRunId:string|null;retrySourceRunId:string|null;triggerSource" +
				":'manual'|'schedule'|'local_api'|'agent'|'child';scope:{projectId:string|null;workingD" +
				"irectory:string}}>;",
		},
	}
}

// SDKTopics 返回发现契约共用的主题目录。
func SDKTopics() []string { return catalog.TopicNames() }

// SDKExampleTopics 返回发现契约共用的完整示例目录。
func SDKExampleTopics() []string { return catalog.Examples() }

func sdkTypeSource() string {
	var result strings.Builder
	for _, declaration := range sdkDeclarations() {
		result.WriteString(declaration.content + "\n")
	}
	return result.String()
}

func sdkCreationKnowledge() string {
	var result strings.Builder
	result.WriteString("# NeuxBot Workflow SDK " + constants.WorkflowSDKVersion + "\n\n")
	for _, section := range sdkSections() {
		result.WriteString(section.content + "\n\n")
	}
	result.WriteString(presentationKnowledge())
	return result.String()
}

// SDKBundle 自动合并选中主题的规则与声明，完整 Catalog 仅在需要页面时加入。
func SDKBundle(topics []string) (string, error) {
	selected := map[string]bool{}
	for _, topic := range topics {
		if !slices.Contains(SDKTopics(), topic) {
			return "", status_error.WorkflowCapabilityUnsupported.WithField("topic", topic)
		}
		selected[topic] = true
	}
	needed := func(topics []string) bool {
		for _, topic := range topics {
			if selected[topic] {
				return true
			}
		}
		return false
	}
	var result strings.Builder
	for _, section := range sdkSections() {
		if needed(section.topics) {
			result.WriteString(section.content + "\n\n")
		}
	}
	result.WriteString("```typescript\n")
	for _, declaration := range sdkDeclarations() {
		if len(declaration.topics) == 0 || needed(declaration.topics) {
			result.WriteString(declaration.content + "\n")
		}
	}
	mapping := catalog.PrimitiveTopics()
	for _, primitive := range Registry() {
		if selected[mapping[primitive.Name]] {
			result.WriteString(primitive.Signature + "\n")
		}
	}
	result.WriteString("```\n")
	if selected["presentation"] {
		result.WriteString("\n" + presentationKnowledge())
	}
	return result.String(), nil
}

// SDKTopicOutputs 从显式知识目录生成主题与完整参考源码。
func SDKTopicOutputs() map[string][]byte {
	outputs := map[string][]byte{}
	for _, topic := range SDKTopics() {
		if topic == "presentation" {
			outputs["sdk-"+topic+".md"] = []byte(presentationKnowledge())
			continue
		}
		content, _ := SDKBundle([]string{topic})
		outputs["sdk-"+topic+".md"] = []byte("# Workflow SDK: " + topic + "\n\n" + content)
	}
	for _, topic := range SDKExampleTopics() {
		data, _ := exampleFiles.ReadFile("testdata/" + topic + ".workflow.js")
		outputs["example-"+topic+".js"] = data
	}
	return outputs
}

// SDKTopic 返回单个已生成主题；未知主题不会回退到完整 SDK。
func SDKTopic(topic string) (string, error) {
	if topic == "" {
		topic = "overview"
	}
	if !slices.Contains(SDKTopics(), topic) {
		return "", status_error.WorkflowCapabilityUnsupported.WithField("topic", topic)
	}
	return SDKArtifact("sdk-" + topic + ".md"), nil
}

// SDKExample 返回可编译的完整示例；资源身份仍须来自真实查询。
func SDKExample(topic string) (string, error) {
	if topic == "" {
		topic = "f0"
	}
	if !slices.Contains(SDKExampleTopics(), topic) {
		return "", status_error.WorkflowCapabilityUnsupported.WithField("topic", topic)
	}
	return SDKArtifact("example-" + topic + ".js"), nil
}
