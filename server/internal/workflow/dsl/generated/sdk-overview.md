# Workflow SDK: overview

The source is authoritative. Use stable ASCII IDs for phases, nodes, groups and actions. Never change an existing ID just to rename its label. Meta is a pure JSON literal. Worker identities, schemas and tool policies must be statically resolvable.

The host compiles main logic to durable Go instructions, with JSON value semantics. This is a restricted DSL, not a full JavaScript VM. Assignment and captures copy values; do not write outer mutable captures from parallel tasks. Strings use Unicode code points. Regular expressions use the RE2 common subset (g/i/m/u). Date is frozen to run start. Math.random is unavailable in orchestration. Put general JavaScript or Python into functionTask.

Worker chats receive stable role instructions and current-turn context through the same execution boundary as ordinary chats. Their runtime working directories are isolated; use the project entries supplied by the host for this turn. Real project locations are not moved. Use flowContext.workspace for all internal paths: rootDirectory, scriptsDirectory, stateDirectory, cacheDirectory, temporaryDirectory, runsDirectory and runDirectory. Scripts/state/cache/tmp stay under the workflow root; never choose /tmp, a home folder or user-facing storage/cache arguments. Business files default to runDirectory; only a user-selected external destination uses scope.workingDirectory. Python reads ALLY_WORKFLOW_ROOT, ALLY_WORKFLOW_SCRIPTS_DIR, ALLY_WORKFLOW_STATE_DIR, ALLY_WORKFLOW_CACHE_DIR, ALLY_WORKFLOW_TEMP_DIR and ALLY_WORKFLOW_RUN_DIR from os.environ; the host fixes temporary and cache environment variables. Relative run-file references resolve only against their owner's runDirectory, never a same-named business/root file; CLI file inputs keep their CLI CWD semantics. Build absolute paths from these fields and pass those absolute paths to worker prompts and results. Never assume two workers have the same process working directory. Use run-specific output paths, require actual tools to create and read deliverable files, and verify existence and nonempty content.

Declare user-provided values in meta.inputSchema and consume them through args. Give every input a concise title and description in the user language. Keep secrets and placeholders out of defaults. Declare file references with type:string, format:file; a reference is not file content.

workflow.source.save compiles and validates complete source, checks policies and expectedRevisionId, then commits an immutable revision and its receipt together. Use the host-provided capability transport and advertised tool schema. When ally_capabilities MCP is injected, call workflow_source_save with the complete source string or exact patches and a stable requestId; CLI file flags are not MCP inputs. A successful immediate receipt with saved:true, revisionId and sourceDigest proves saving. Explicit saves create a new immutable revision, including unchanged source; replaying the same request returns the original receipt. If the response is lost, use MCP receipts_get with operation:workflow.source.save and the original requestId, or receipts get through a host-provided CLI fallback. Read current source again on revision conflict. Saving does not prove a real run or external delivery succeeded.

Name every node label for its business purpose in the user's language. Labels, descriptions, IDs and phase references are static literals; keep per-item names and identity in node inputs, prompts and results instead of concatenating them into labels. Add an optional static description (at most 500 Unicode characters) as one sentence explaining what the step does for the user. Cards and details share this description. Keep implementation terms such as Function, python or trusted_local out of business labels and descriptions. Missing action descriptions may reuse meta.actions description; other nodes have no invented description.

Workflow exports static meta and optional scripts. Each script has language:python, protocol:json or text, and complete code as a string or interpolation-free String.raw template. Code bytes are frozen in the revision. A script action references scriptId. The host executes the Python module directly; it does not call main(input). A file containing only def main(input): is incomplete for a script action. Read stdin and write stdout at module entry. The host writes the complete input JSON value to stdin (missing input is null, false/0/empty string remain values). JSON stdout must contain exactly one value; stderr is logs. Text stdout is returned as a string. No automatic pip install. Script actions return {data,durationMs,process}.

Minimal complete protocol: this module reads and echoes any one JSON value, including false, 0, empty string and null. Replace only the business processing when adapting it:

```python
import json, sys
value = json.load(sys.stdin)
json.dump(value, sys.stdout, ensure_ascii=False)
```

For multi-item review, independent verification or partial-failure reporting, read references/workflow-patterns.md in the supplied ally skill and request workflow_sdk_get with {kind:'example',topic:'review'} before writing source. The review example is fictional and uses a placeholder Waker that must be resolved from prepared resources. Advance each item's dependent stages with pipeline; join all items only for genuine cross-item comparison or synthesis. Preserve original identities and coverage when collecting failures. Copy the protocol and dependency structure, then implement the user's actual data source and work rather than reusing the fictional output.

```typescript
type WorkflowScripts = Record<string,{language:'python';protocol:'json'|'text';code:string}>;
type Json = null | boolean | number | string | Json[] | { [key: string]: Json };
type Schema = { [key: string]: Json };
interface VisualNode { id: string; label: string; description?: string; phase: string; inputs?: string[]; outputs?: string[]; successCondition?:{path:string;equals:Json} }
type ActionDeclaration = {id:string;label:string;description?:string;risk:'read'|'write'|'external';inputSchema?:Schema;outputSchema?:Schema} & ({type:'http';allowedOrigins:string[];methods:string[]} | {type:'script';scriptId:string;interpreter?:string;executable?:never} | {type:'script';executable:string;workingDirectory:'workflow_scope';scriptId?:never} | {type:'builtin';handler:string;connectorIds?:string[];imTargetIds?:string[]});
interface WorkflowLimits {
  /** Maximum simultaneous external calls; use 1 for serial execution. */
  concurrency?: number;
  /** Cumulative node instances across the whole run, including loop and batch items; omit for the bounded host default. */
  maxInstances?: number;
  /** Cumulative loop steps across the whole run. */
  maxLoopSteps?: number;
  /** Total run deadline in milliseconds. */
  timeoutMs?: number;
}
interface WorkflowMeta {sdk:'ally.workflow';title:string;description:string;whenToUse?:string;inputSchema?:Schema;outputSchema?:Schema;phases:Array<{id:string;title:string;detail:string;next?:string[];canExit?:boolean}>;actions?:ActionDeclaration[];limits?:WorkflowLimits}
declare const args: Json;
declare const flowContext: Readonly<{workflowId:string;revisionId:string;runId:string;workflowName:string;runStartedAt:number;parentRunId:string|null;retrySourceRunId:string|null;triggerSource:'manual'|'schedule'|'local_api'|'agent'|'child';scope:{projectId:string|null;workingDirectory:string};workspace:Readonly<{rootDirectory:string;scriptsDirectory:string;stateDirectory:string;cacheDirectory:string;temporaryDirectory:string;runsDirectory:string;runDirectory:string}>}>;
declare function phase(id: string): void;
declare function log(message: string, options?: { level?: 'info' | 'warning' | 'error' }): void;
```
