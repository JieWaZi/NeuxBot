# Workflow SDK: workflow

Await workers, human inputs, actions and child results. Parallel returns in input order. Pipeline advances each item immediately after its own preceding stage, then joins all items in original order. A null stage result is ordinary JSON, not a failure. Explicit throw stops the failed item.

Name every node label for its business purpose in the user's language. Labels, descriptions, IDs and phase references are static literals; keep per-item names and identity in node inputs, prompts and results instead of concatenating them into labels. Add an optional static description (at most 500 Unicode characters) as one sentence explaining what the step does for the user. Cards and details share this description. Keep implementation terms such as Function, python or trusted_local out of business labels and descriptions. Missing action descriptions may reuse meta.actions description; other nodes have no invented description.

```typescript
type Json = null | boolean | number | string | Json[] | { [key: string]: Json };
type Schema = { [key: string]: Json };
interface VisualNode { id: string; label: string; description?: string; phase: string; inputs?: string[]; outputs?: string[]; successCondition?:{path:string;equals:Json} }
interface ChildOptions { args?:Json; revisionId?:string; timeoutMs?:number }
interface ChildResult { runId:string; revisionId:string; output:Json }
/** Opaque durable handle passed only between startWorkflow and joinWorkflow. */
declare const workflowChildHandleBrand: unique symbol;
interface ChildHandle { readonly [workflowChildHandleBrand]: true }
declare function workflow(workflowId: string, options: VisualNode & ChildOptions): Promise<ChildResult>;
declare function startWorkflow(workflowId: string, options: VisualNode & ChildOptions & { mode: 'joined' | 'detached' }): ChildHandle;
declare function joinWorkflow(handle: ChildHandle): Promise<ChildResult>;
```
