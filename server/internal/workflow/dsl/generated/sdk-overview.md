# Workflow SDK: overview

The source is authoritative. Use stable ASCII IDs for phases, nodes, groups and actions. Never change an existing ID just to rename its label. Meta is a pure JSON literal. Worker identities, schemas and tool policies must be statically resolvable.

The host compiles main logic to durable Go instructions, with JSON value semantics. This is a restricted DSL, not a full JavaScript VM. Assignment and captures copy values; do not write outer mutable captures from parallel tasks. Strings use Unicode code points. Regular expressions use the RE2 common subset (g/i/m/u). Date is frozen to run start. Math.random is unavailable in orchestration. Put general JavaScript or Python into functionTask.

Declare user-provided values in meta.inputSchema and consume them through args. Give every input a concise title and description in the user language. Keep secrets and placeholders out of defaults. Declare file references with type:string, format:file; a reference is not file content.

source save compiles and validates complete source, checks policies and expectedRevisionId, then commits an immutable revision and its receipt together. Use --source for the UTF-8 file and a stable --request-id. A successful immediate receipt with saved:true, revisionId and sourceDigest proves saving. Explicit saves create a new immutable revision, including unchanged source; replaying the same request returns the original receipt. If the response is lost, query receipts get for the original operation/request ID. Read current source again on revision conflict. Saving does not prove a real run or external delivery succeeded.

Name every node label for its business purpose in the user's language. Add an optional static description (at most 500 Unicode characters) as one sentence explaining what the step does for the user. Cards and details share this description. Do not label or describe a business step using implementation terms such as Function, python or trusted_local; those belong only in technical details. Missing action descriptions may reuse meta.actions description; other nodes have no invented description.

V2 exports static meta and optional scripts. Each script has language:python, protocol:json or text, and complete code as a string or interpolation-free String.raw template. Code bytes are frozen in the revision. A script action references scriptId. The host writes the complete input JSON value to stdin (missing input is null, false/0/empty string remain values). JSON stdout must contain exactly one value; stderr is logs. Text stdout is returned as a string. No automatic pip install. Script actions return {data,durationMs,process}.

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
interface WorkflowMeta {sdk:'ally.workflow/2';title:string;description:string;whenToUse?:string;inputSchema?:Schema;outputSchema?:Schema;phases:Array<{id:string;title:string;detail:string;next?:string[];canExit?:boolean}>;actions?:ActionDeclaration[];limits?:WorkflowLimits}
declare const args: Json;
declare const flowContext: Readonly<{workflowId:string;revisionId:string;runId:string;workflowName:string;runStartedAt:number;parentRunId:string|null;retrySourceRunId:string|null;triggerSource:'manual'|'schedule'|'local_api'|'agent'|'child';scope:{projectId:string|null;workingDirectory:string}}>;
declare function phase(id: string): void;
declare function log(message: string, options?: { level?: 'info' | 'warning' | 'error' }): void;
```
