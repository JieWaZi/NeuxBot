# Workflow SDK: action

Await workers, human inputs, actions and child results. Parallel returns in input order. Pipeline advances each item immediately after its own preceding stage, then joins all items in original order. A null stage result is ordinary JSON, not a failure. Explicit throw stops the failed item.

Worker chats use the same managed runtime workspace and native instruction-file loading as ordinary chats. Their runtime working directories are isolated; the approved business directory is shared through project links and additional directory authorization. Use flowContext.workspace for all internal paths: rootDirectory, scriptsDirectory, stateDirectory, cacheDirectory, temporaryDirectory, runsDirectory and runDirectory. Scripts/state/cache/tmp stay under the workflow root; never choose /tmp, a home folder or user-facing storage/cache arguments. Business files default to runDirectory; only a user-selected external destination uses scope.workingDirectory. Python reads ALLY_WORKFLOW_ROOT, ALLY_WORKFLOW_SCRIPTS_DIR, ALLY_WORKFLOW_STATE_DIR, ALLY_WORKFLOW_CACHE_DIR, ALLY_WORKFLOW_TEMP_DIR and ALLY_WORKFLOW_RUN_DIR from os.environ; the host fixes temporary and cache environment variables. Relative run-file references resolve only against their owner's runDirectory, never a same-named business/root file; CLI file inputs keep their CLI CWD semantics. Build absolute paths from these fields and pass those absolute paths to worker prompts and results. Never assume two workers have the same process working directory. Use run-specific output paths, require actual tools to create and read deliverable files, and verify existence and nonempty content.

HTTP actions default to 30000 ms. Command/script actions default to 600000 ms, with the same hard maximum. Omit timeoutMs unless the user agreed to a shorter deadline.

Name every node label for its business purpose in the user's language. Labels, descriptions, IDs and phase references are static literals; keep per-item names and identity in node inputs, prompts and results instead of concatenating them into labels. Add an optional static description (at most 500 Unicode characters) as one sentence explaining what the step does for the user. Cards and details share this description. Keep implementation terms such as Function, python or trusted_local out of business labels and descriptions. Missing action descriptions may reuse meta.actions description; other nodes have no invented description.

V2 exports static meta and optional scripts. Each script has language:python, protocol:json or text, and complete code as a string or interpolation-free String.raw template. Code bytes are frozen in the revision. A script action references scriptId. The host executes the Python module directly; it does not call main(input). A file containing only def main(input): is incomplete for a script action. Read stdin and write stdout at module entry. The host writes the complete input JSON value to stdin (missing input is null, false/0/empty string remain values). JSON stdout must contain exactly one value; stderr is logs. Text stdout is returned as a string. No automatic pip install. Script actions return {data,durationMs,process}.

Minimal complete protocol: this module reads and echoes any one JSON value, including false, 0, empty string and null. Replace only the business processing when adapting it:

```python
import json, sys
value = json.load(sys.stdin)
json.dump(value, sys.stdout, ensure_ascii=False)
```

successCondition:{path,equals} checks the validated business value: action.data, direct worker JSON/text, or functionTask result. path is a JSON Pointer; equals is a literal JSON value. Failed checks throw BUSINESS_CHECK_FAILED. Ordinary false and null are data without an explicit check.

Host budgets from the shared contract: source 1048576 bytes; node business output 1048576 bytes; inline human choices 500 items and 262144 bytes; script action deadline at most 600000 ms. Truncated stdout is never accepted as successful JSON. Use managed file references for larger results.

```typescript
type WorkflowScripts = Record<string,{language:'python';protocol:'json'|'text';code:string}>;
type Json = null | boolean | number | string | Json[] | { [key: string]: Json };
type Schema = { [key: string]: Json };
interface VisualNode { id: string; label: string; description?: string; phase: string; inputs?: string[]; outputs?: string[]; successCondition?:{path:string;equals:Json} }
interface ActionOptions { input?:Json; timeoutMs?:number; url?:string; method?:string; headers?:Record<string,string>; body?:Json; secretHeaders?:Record<string,{secretRef:string}>; args?:string[]; env?:Record<string,string>; secretEnv?:Record<string,{secretRef:string}>; schema?:Schema }
interface ActionResult { data:Json; durationMs:number; process?:{exitCode:number;stdout:string;stderr:string;logsTruncated:boolean}; http?:{status:number;headers:Record<string,string>;body:string} }
type ActionDeclaration = {id:string;label:string;description?:string;risk:'read'|'write'|'external';inputSchema?:Schema;outputSchema?:Schema} & ({type:'http';allowedOrigins:string[];methods:string[]} | {type:'script';scriptId:string;interpreter?:string;executable?:never} | {type:'script';executable:string;workingDirectory:'workflow_scope';scriptId?:never} | {type:'builtin';handler:string;connectorIds?:string[];imTargetIds?:string[]});
declare function action(actionId: string, options: VisualNode & ActionOptions): Promise<ActionResult>;
```
