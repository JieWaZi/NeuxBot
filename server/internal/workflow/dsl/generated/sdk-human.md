# Workflow SDK: human

Await workers, human inputs, actions and child results. Parallel returns in input order. Pipeline advances each item immediately after its own preceding stage, then joins all items in original order. A null stage result is ordinary JSON, not a failure. Explicit throw stops the failed item.

Human defaults do not submit or approve. Approvals require a positive explicit value; skip and timeout preserve their source. Actions use separate operation permissions.

Name every node label for its business purpose in the user's language. Add an optional static description (at most 500 Unicode characters) as one sentence explaining what the step does for the user. Cards and details share this description. Do not label or describe a business step using implementation terms such as Function, python or trusted_local; those belong only in technical details. Missing action descriptions may reuse meta.actions description; other nodes have no invented description.

Use explicit kind:text/select/multiselect/form/confirm. choices may be a dynamic {value,label} array from a prior node or a candidate fileRef; the host freezes the evaluated snapshot. kind:form requires schema and excludes choices. allowCustom defaults false; allowSkip defaults false. Multiselect defaults minSelections:1 and maxSelections to candidate count; explicit minSelections:0 allows empty selection. HumanAnswer contains questionId,status,source,value?. Defaults only prefill. onTimeout defaults fail; explicit skip returns timed_out without value, default returns timed_out with validated defaultValue. Confirm can never auto-approve via timeout/default. Cancellation throws. Cache replay preserves original status/value and references the original answer.

Host budgets from the shared contract: source 1048576 bytes; node business output 1048576 bytes; inline human choices 500 items and 262144 bytes; script action deadline at most 600000 ms. Truncated stdout is never accepted as successful JSON. Use managed file references for larger results.

```typescript
type Json = null | boolean | number | string | Json[] | { [key: string]: Json };
type Schema = { [key: string]: Json };
interface VisualNode { id: string; label: string; description?: string; phase: string; inputs?: string[]; outputs?: string[]; successCondition?:{path:string;equals:Json} }
interface HumanOptions { kind:'text'|'select'|'multiselect'|'form'|'confirm'; choices?:Array<{value:string;label:string}>|{fileRef:string}; allowCustom?:boolean; schema?:Schema; minSelections?:number; maxSelections?:number; defaultValue?:Json; allowSkip?:boolean; timeoutMs?:number; onTimeout?:'fail'|'skip'|'default' }
interface HumanAnswer { questionId:string; status:'answered'|'skipped'|'timed_out'; source:'user'|'user_skip'|'timeout'|'cache_replay'; value?:Json }
declare function askUser(prompt: string, options: VisualNode & HumanOptions): Promise<HumanAnswer>;
```
