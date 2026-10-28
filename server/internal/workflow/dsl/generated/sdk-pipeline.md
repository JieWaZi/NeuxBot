# Workflow SDK: pipeline

Await workers, human inputs, actions and child results. Parallel returns in input order. Pipeline advances each item immediately after its own preceding stage, then joins all items in original order. A null stage result is ordinary JSON, not a failure. Explicit throw stops the failed item.

Group failureMode defaults fail-fast. collect-null returns null for failed items in input order, retaining failure identity and diagnostics on original instances; ordinary JSON null is not failure.

```typescript
type Json = null | boolean | number | string | Json[] | { [key: string]: Json };
type Schema = { [key: string]: Json };
interface VisualNode { id: string; label: string; description?: string; phase: string; inputs?: string[]; outputs?: string[]; successCondition?:{path:string;equals:Json} }
interface GroupOptions { id:string; failureMode?:'collect-null'|'fail-fast'; concurrency?:number }
declare function pipeline(items: Json[], stages: Array<(previous: Json, original: Json, index: number) => Promise<Json>>, options: GroupOptions): Promise<Array<Json | null>>;
```
