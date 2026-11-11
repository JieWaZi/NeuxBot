package dsl

import (
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/workflow/catalog"
	"slices"
	"strings"
)

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
				"to run start. Math.random is unavailable in orchestration. Put general JavaScript or Python into functionTask.",
		},
		{
			id:     "function",
			topics: []string{"function"},
			content: "functionTask code defines the main entry called by the host: JavaScript must declare" +
				" function main(input) { return JSON; }; Python must define def main(input): and return" +
				" a JSON value. The host calls main; this is different from a script action, which runs " +
				"the Python module directly and requires explicit JSON stdin/stdout. JavaScript main must be synchronous. Do not put a top-level return in " +
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
				" uploaded ally-file references with real absolute paths in the managed workflow workspace. " +
				"Workers must read those paths using actual tools; never treat references as file " +
				"contents. Final file values must be paths to real files created in " +
				"flowContext.workspace.runDirectory by default, or the business destination explicitly " +
				"selected by the user in flowContext.scope.workingDirectory; users and the API can download these" +
				" files. Return actual paths rather than base64 data or made-up download URLs. Ordinary" +
				" strings are never inferred to be file outputs. Input form controls support " +
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
			topics: []string{"overview", "worker", "action", "function"},
			content: "Worker chats receive stable role instructions and current-turn context through " +
				"the same execution boundary as ordinary chats. Their runtime working directories are " +
				"isolated; use the project entries supplied by the host for this turn. Real project " +
				"locations are not moved. Use flowContext.workspace for all internal paths: rootDirectory, " +
				"scriptsDirectory, stateDirectory, cacheDirectory, temporaryDirectory, runsDirectory and " +
				"runDirectory. Scripts/state/cache/tmp stay under the workflow root; never choose /tmp, " +
				"a home folder or user-facing storage/cache arguments. Business files default to runDirectory; " +
				"only a user-selected external destination uses scope.workingDirectory. Python reads " +
				"ALLY_WORKFLOW_ROOT, ALLY_WORKFLOW_SCRIPTS_DIR, ALLY_WORKFLOW_STATE_DIR, ALLY_WORKFLOW_CACHE_DIR, " +
				"ALLY_WORKFLOW_TEMP_DIR and ALLY_WORKFLOW_RUN_DIR from os.environ; the host fixes temporary " +
				"and cache environment variables. Relative run-file references resolve only against their " +
				"owner's runDirectory, never a same-named business/root file; CLI file inputs keep their " +
				"CLI CWD semantics. Build absolute paths from these fields and " +
				"pass those absolute paths to worker prompts and results. Never assume two workers have" +
				" the same process working directory. Use run-specific output paths, require actual " +
				"tools to create and read deliverable files, and verify existence and nonempty content.",
		},
		{
			id:     "worker-retry",
			topics: []string{"worker"},
			content: "Worker retry syntax is not proof that the current execution supports it. Read " +
				"execution.defaults and execution.workerRetry from workflow.authoring.prepare before " +
				"declaring retry. Automatic pure retry requires workspaceReadOnly=true; permissionLevel " +
				"and a pure declaration do not enable that constraint. Idempotent worker retry is " +
				"unsupported because the host cannot enforce per-tool deduplication. Validate and save " +
				"report every incompatible node for the current authoring configuration; run admission " +
				"checks the actual configuration again. Read-only workspace mode does not prove that " +
				"external tools are free of side effects. Preserve requested retry behavior and ask the " +
				"user before changing it or execution permissions; do not bypass a rejection by adding " +
				"replacement worker calls, loops or relabeling the task as pure.",
		},
		{
			id:     "results",
			topics: []string{"worker"},
			content: "A structured worker must invoke workflow.results.submit through the host-provided " +
				"capability transport with a stable requestId and a data value matching its schema. " +
				"When ally_capabilities MCP is injected, use workflow_results_schema and " +
				"workflow_results_submit with their advertised schemas. A submission is " +
				"only a candidate until its Execution succeeds. Do not rely on JSON pasted into a final" +
				" reply. For a root file schema, data is the actual file path. Add routing or summary " +
				"fields only for real downstream consumers.",
		},
		{
			id:     "im-delivery",
			topics: []string{"im"},
			content: "When the user requests delivery to WeChat or Feishu, discover approved chats with " +
				"workflow.resources.list with kind:imTargets. Discover the native IM operation " +
				"through workflow.resources.list with kind:builtinActions, tool im_delivery, " +
				"through a builtin action declaring its connector ID and awaiting ctx.callTool({callId," +
				" connectorId, toolName: 'im_delivery', input: {action: 'deliveries.create', targetId, " +
				"content, files}}). Fixed literal targetIds are derived as workflow resource " +
				"dependencies. Dynamic targetIds must declare the approved imTargetIds on the builtin " +
				"action; runtime delivery cannot use an undeclared target. Admission checks these " +
				"targets before any node runs. This durable action waits for delivery acceptance; query" +
				" deliveries.get to verify sent, failed or unknown status. Never invent recipients or " +
				"connector IDs. Resource IDs come from workflow.resources.list; never invent or" +
				" silently replace them. Do not send messages or publish outside an explicitly " +
				"authorized action.",
		},
		{
			id:     "evidence",
			topics: []string{"saving"},
			content: "When optimizing a failed run, read all workflow.evidence.list pages, including" +
				" run.failure, instance failures and actionCalls. Inspect nested cause, hint, " +
				"sourceRange and delivery receipts including partsSent and unknown status before " +
				"proposing a repair. Preserve original call identities and never repeat an already " +
				"completed or uncertain external operation. Read the current revision and pending " +
				"annotations before saving. Validate complete source, fix diagnostics, then invoke " +
				"workflow.source.save using expectedRevisionId and a stable requestId. At most three " +
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
			content: "workflow.source.save compiles and validates complete source, checks policies and " +
				"expectedRevisionId, then commits an immutable revision and its receipt together. " +
				"Use the host-provided capability transport and advertised tool schema. When " +
				"ally_capabilities MCP is injected, call workflow_source_save with the complete source " +
				"string or exact patches and a stable requestId; CLI file flags are not MCP inputs. " +
				"A successful immediate receipt " +
				"with saved:true, revisionId and sourceDigest proves saving. Explicit saves create a new immutable " +
				"revision, including unchanged source; replaying the same request returns the original receipt." +
				" If the response is lost, use MCP receipts_get with operation:workflow.source.save " +
				"and the original requestId, or receipts get through a host-provided CLI fallback. " +
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
		{id: "business-node-description", topics: []string{"overview", "worker", "human", "action", "function", "workflow"}, content: fmt.Sprintf("Name every node label for its business purpose in the user's language. Labels, descriptions, IDs and phase references are static literals; keep per-item names and identity in node inputs, prompts and results instead of concatenating them into labels. Add an optional static description (at most %d Unicode characters) as one sentence explaining what the step does for the user. Cards and details share this description. Keep implementation terms such as Function, python or trusted_local out of business labels and descriptions. Missing action descriptions may reuse meta.actions description; other nodes have no invented description.", constants.WorkflowNodeDescriptionMaxRunes)},
		{id: "worker-results", topics: []string{"worker"}, content: "Workers return final text without schema, or matching JSON directly with schema. There is no data/content/analysis wrapper; values can be read by later nodes and the final return."},
		{id: "scripts", topics: []string{"overview", "action"}, content: "Workflow exports static meta and optional scripts. Each script has language:python, protocol:json or text, and complete code as a string or interpolation-free String.raw template. Code bytes are frozen in the revision. A script action references scriptId. The host executes the Python module directly; it does not call main(input). A file containing only def main(input): is incomplete for a script action. Read stdin and write stdout at module entry. The host writes the complete input JSON value to stdin (missing input is null, false/0/empty string remain values). JSON stdout must contain exactly one value; stderr is logs. Text stdout is returned as a string. No automatic pip install. Script actions return {data,durationMs,process}.\n\nMinimal complete protocol: this module reads and echoes any one JSON value, including false, 0, empty string and null. Replace only the business processing when adapting it:\n\n```python\nimport json, sys\nvalue = json.load(sys.stdin)\njson.dump(value, sys.stdout, ensure_ascii=False)\n```"},
		{id: "batch-authoring", topics: []string{"overview", "parallel", "pipeline"}, content: "For multi-item review, independent verification or partial-failure reporting, read references/workflow-patterns.md in the supplied ally skill and request workflow_sdk_get with {kind:'example',topic:'review'} before writing source. The review example is fictional and uses a placeholder Waker that must be resolved from prepared resources. Advance each item's dependent stages with pipeline; join all items only for genuine cross-item comparison or synthesis. Preserve original identities and coverage when collecting failures. Copy the protocol and dependency structure, then implement the user's actual data source and work rather than reusing the fictional output."},
		{id: "human", topics: []string{"human"}, content: "Use explicit kind:text/select/multiselect/form/confirm. choices may be a dynamic {value,label} array from a prior node or a candidate fileRef; the host freezes the evaluated snapshot. kind:form requires schema and excludes choices. allowCustom defaults false; allowSkip defaults false. Multiselect defaults minSelections:1 and maxSelections to candidate count; explicit minSelections:0 allows empty selection. HumanAnswer contains questionId,status,source,value?. Defaults only prefill. onTimeout defaults fail; explicit skip returns timed_out without value, default returns timed_out with validated defaultValue. Confirm can never auto-approve via timeout/default. Cancellation throws. Cache replay preserves original status/value and references the original answer."},
		{id: "business-check", topics: []string{"action", "function", "worker"}, content: "successCondition:{path,equals} checks the validated business value: action.data, direct worker JSON/text, or functionTask result. path is a JSON Pointer; equals is a literal JSON value. Failed checks throw BUSINESS_CHECK_FAILED. Ordinary false and null are data without an explicit check."},
		{id: "host-budgets", topics: []string{"budgets", "action", "human"}, content: fmt.Sprintf("Host budgets from the shared contract: source %d bytes; node business output %d bytes; inline human choices %d items and %d bytes; script action deadline at most %d ms. Truncated stdout is never accepted as successful JSON. Use managed file references for larger results.", constants.WorkflowSourceLimitBytes, constants.WorkflowOutputLimitBytes, constants.WorkflowHumanMaximumChoices, constants.WorkflowHumanChoicesLimitBytes, constants.WorkflowScriptMaxTimeoutMillis)},
		{id: "groups", topics: []string{"parallel", "pipeline"}, content: "Group failureMode defaults fail-fast. collect-null returns null for failed items in input order, retaining failure identity and diagnostics on original instances; ordinary JSON null is not failure."},
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
		{name: "WorkflowScripts", topics: []string{"overview", "action"}, content: "type WorkflowScripts = Record<string,{language:'python';protocol:'json'|'text';code:string}>;"},
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
			content: "interface VisualNode { id: string; label: string; description?: string; phase: string; inputs?: string[]; " +
				"outputs?: string[]; successCondition?:{path:string;equals:Json} }",
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
				"ToolPolicy; retry?: " +
				"{maxAttempts:number;safety:'pure'|'idempotent';idempotencyKey?:string} }",
		},
		{
			name:   "HumanOptions",
			topics: []string{"human"},
			content: "interface HumanOptions { kind:'text'|'select'|'multiselect'|'form'|'confirm'; " +
				"choices?:Array<{value:string;label:string}>|{fileRef:string}; allowCustom?:boolean; " +
				"schema?:Schema; minSelections?:number; maxSelections?:number; defaultValue?:Json; " +
				"allowSkip?:boolean; timeoutMs?:number; onTimeout?:'fail'|'skip'|'default' }",
		},
		{
			name:   "HumanAnswer",
			topics: []string{"human"},
			content: "interface HumanAnswer { questionId:string; status:'answered'|'skipped'|'timed_out'; " +
				"source:'user'|'user_skip'|'timeout'|'cache_replay'; value?:Json }",
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
			name:    "ChildResult",
			topics:  []string{"workflow"},
			content: "interface ChildResult { runId:string; revisionId:string; output:Json }",
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
				"process?:{exitCode:number;stdout:string;stderr:string;logsTruncated:boolean}; " +
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
			content: "type ActionDeclaration = {id:string;label:string;description?:string;risk:'read'|'write" +
				"'|'external';inputSchema?:Schema;outputSchema?:Schema} & " +
				"({type:'http';allowedOrigins:string[];methods:string[]} | " +
				"{type:'script';scriptId:string;interpreter?:string;executable?:never} | " +
				"{type:'script';executable:string;workingDirectory:'workflow_scope';scriptId?:never} | " +
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
			name:   "WorkflowMeta",
			topics: []string{"overview"},
			content: "interface WorkflowMeta {sdk:'ally.workflow';title:string;description:string;" +
				"whenToUse?:string;inputSchema?:Schema;outputSchema?:Schema;phases:Array<{id:string;" +
				"title:string;detail:string;next?:string[];canExit?:boolean}>;actions?:ActionDeclaration[];limits?:WorkflowLimits}",
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
				"workflowName:string;runStartedAt:number;parentRunId:string|null;retrySourceRunId:string|null;triggerSource" +
				":'manual'|'schedule'|'local_api'|'agent'|'child';scope:{projectId:string|null;workingD" +
				"irectory:string};workspace:Readonly<{rootDirectory:string;scriptsDirectory:string;" +
				"stateDirectory:string;cacheDirectory:string;temporaryDirectory:string;runsDirectory:string;runDirectory:string}>}>;",
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
	result.WriteString("# NeuxBot Workflow SDK " + constants.WorkflowSDKIdentifier + "\n\n")
	for _, section := range sdkSections() {
		result.WriteString(section.content + "\n\n")
	}
	return strings.TrimRight(result.String(), "\n") + "\n"
}

// SDKBundle 自动合并选中主题的编排规则与声明。
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
	return result.String(), nil
}

// SDKTopicOutputs 从显式知识目录生成主题与完整参考源码。
func SDKTopicOutputs() map[string][]byte {
	outputs := map[string][]byte{}
	for _, topic := range SDKTopics() {

		content, _ := SDKBundle([]string{topic})
		outputs["sdk-"+topic+".md"] = []byte("# Workflow SDK: " + topic + "\n\n" + content)
	}
	for _, topic := range SDKExampleTopics() {
		outputs["example-"+topic+".js"] = []byte(SDKArtifact("example-" + topic + ".js"))
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
