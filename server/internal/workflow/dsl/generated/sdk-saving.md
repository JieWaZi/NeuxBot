# Workflow SDK: saving

The source is authoritative. Use stable ASCII IDs for phases, nodes, groups and actions. Never change an existing ID just to rename its label. Meta is a pure JSON literal. Worker identities, schemas and tool policies must be statically resolvable.

When optimizing a failed run, read all allycli workflow evidence list pages, including run.failure, instance failures and actionCalls. Inspect nested cause, hint, sourceRange and delivery receipts including partsSent and unknown status before proposing a repair. Preserve original call identities and never repeat an already completed or uncertain external operation. Read the current revision and pending annotations before saving. Validate complete source, fix diagnostics, then allycli workflow source save using expectedRevisionId and a stable requestId. At most three repair attempts per turn. Only the tool save receipt proves the source is active.


source save compiles and validates complete source, checks policies and expectedRevisionId, then commits an immutable revision and its receipt together. Use --source for the UTF-8 file and a stable --request-id. A successful immediate receipt with saved:true, revisionId and sourceDigest proves saving. Explicit saves create a new immutable revision, including unchanged source; replaying the same request returns the original receipt. If the response is lost, query receipts get for the original operation/request ID. Read current source again on revision conflict. Saving does not prove a real run or external delivery succeeded.

```typescript
type Json = null | boolean | number | string | Json[] | { [key: string]: Json };
type Schema = { [key: string]: Json };
interface VisualNode { id: string; label: string; description?: string; phase: string; inputs?: string[]; outputs?: string[]; successCondition?:{path:string;equals:Json} }
```
