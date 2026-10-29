# Workflow SDK: worker

Worker chats use the same managed runtime workspace and native instruction-file loading as ordinary chats. Their runtime working directories are isolated; the approved business directory is shared through project links and additional directory authorization. Use flowContext.workspace for all internal paths: rootDirectory, scriptsDirectory, stateDirectory, cacheDirectory, temporaryDirectory, runsDirectory and runDirectory. Scripts/state/cache/tmp stay under the workflow root; never choose /tmp, a home folder or user-facing storage/cache arguments. Business files default to runDirectory; only a user-selected external destination uses scope.workingDirectory. Python reads ALLY_WORKFLOW_ROOT, ALLY_WORKFLOW_SCRIPTS_DIR, ALLY_WORKFLOW_STATE_DIR, ALLY_WORKFLOW_CACHE_DIR, ALLY_WORKFLOW_TEMP_DIR and ALLY_WORKFLOW_RUN_DIR from os.environ; the host fixes temporary and cache environment variables. Relative run-file references resolve only against their owner's runDirectory, never a same-named business/root file; CLI file inputs keep their CLI CWD semantics. Build absolute paths from these fields and pass those absolute paths to worker prompts and results. Never assume two workers have the same process working directory. Use run-specific output paths, require actual tools to create and read deliverable files, and verify existence and nonempty content.

A structured worker must invoke workflow.results.submit through the host-provided capability transport with a stable requestId and a data value matching its schema. When ally_capabilities MCP is injected, use workflow_results_schema and workflow_results_submit with their advertised schemas. A submission is only a candidate until its Execution succeeds. Do not rely on JSON pasted into a final reply. For a root file schema, data is the actual file path. Add routing or summary fields only for real downstream consumers.

Workers have no independent timeout; worker timeoutMs is unsupported. User cancellation and actual execution failure remain effective.

Name every node label for its business purpose in the user's language. Labels, descriptions, IDs and phase references are static literals; keep per-item names and identity in node inputs, prompts and results instead of concatenating them into labels. Add an optional static description (at most 500 Unicode characters) as one sentence explaining what the step does for the user. Cards and details share this description. Keep implementation terms such as Function, python or trusted_local out of business labels and descriptions. Missing action descriptions may reuse meta.actions description; other nodes have no invented description.

Workers return final text without schema, or matching JSON directly with schema. There is no data/content/analysis wrapper; values can be read by later nodes and the final return.

successCondition:{path,equals} checks the validated business value: action.data, direct worker JSON/text, or functionTask result. path is a JSON Pointer; equals is a literal JSON value. Failed checks throw BUSINESS_CHECK_FAILED. Ordinary false and null are data without an explicit check.

```typescript
type Json = null | boolean | number | string | Json[] | { [key: string]: Json };
type Schema = { [key: string]: Json };
interface VisualNode { id: string; label: string; description?: string; phase: string; inputs?: string[]; outputs?: string[]; successCondition?:{path:string;equals:Json} }
interface WakerResolve { kind: 'waker'; wakerId: string; runtimeId?: string; accountId?: string; model?: string; reasoning?: string; permissionLevel?: 'default'|'auto'|'full-access' }
interface ToolPolicy { connectors?: {mode:'inherit'} | {mode:'allowlist';connectorIds:string[]}; nativeTools?: {mode:'inherit'} | {mode:'allowlist';capabilityIds:string[]} }
interface WorkerOptions { resolve: WakerResolve; schema?: Schema; toolPolicy?: ToolPolicy; retry?: {maxAttempts:number;safety:'pure'|'idempotent';idempotencyKey?:string} }
declare const flowContext: Readonly<{workflowId:string;revisionId:string;runId:string;workflowName:string;runStartedAt:number;parentRunId:string|null;retrySourceRunId:string|null;triggerSource:'manual'|'schedule'|'local_api'|'agent'|'child';scope:{projectId:string|null;workingDirectory:string};workspace:Readonly<{rootDirectory:string;scriptsDirectory:string;stateDirectory:string;cacheDirectory:string;temporaryDirectory:string;runsDirectory:string;runDirectory:string}>}>;
declare function worker<T extends Json = string>(prompt: string, options: VisualNode & WorkerOptions): Promise<T>;
```
