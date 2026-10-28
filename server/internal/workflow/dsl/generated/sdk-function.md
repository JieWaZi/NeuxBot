# Workflow SDK: function

The host compiles main logic to durable Go instructions, with JSON value semantics. This is a restricted DSL, not a full JavaScript VM. Assignment and captures copy values; do not write outer mutable captures from parallel tasks. Strings use Unicode code points. Regular expressions use the RE2 common subset (g/i/m/u). Date is frozen to run start. Math.random is unavailable in orchestration. Put general JavaScript or Python into functionTask.

Function code is a complete program, not a bare function body: JavaScript must declare function main(input) { return JSON; }; Python must define def main(input): and return a JSON value. JavaScript main must be synchronous. Do not put a top-level return in the code string. Only the returned value is the Function result. Node outputSchema is optional. If supplied, use a JSON literal or a top-level literal constant; property access such as meta.outputSchema is not statically resolvable. Omit it only when node-level output validation is unnecessary.

Python requires a configured interpreter and executionMode:trusted_local. JavaScript supports pure execution. Authoring prepare includes functionLanguages when language is python.

JavaScript/Python functionTask defaults to 600000 ms (10 minutes), with the same hard maximum. Omit timeoutMs unless the user agreed to a shorter deadline. The entire run has a separate 24-hour host deadline; do not invent a shorter overall budget.

Name every node label for its business purpose in the user's language. Add an optional static description (at most 500 Unicode characters) as one sentence explaining what the step does for the user. Cards and details share this description. Do not label or describe a business step using implementation terms such as Function, python or trusted_local; those belong only in technical details. Missing action descriptions may reuse meta.actions description; other nodes have no invented description.

successCondition:{path,equals} checks the validated business value: action.data, direct worker JSON/text, or functionTask result. path is a JSON Pointer; equals is a literal JSON value. Failed checks throw BUSINESS_CHECK_FAILED. Ordinary false and null are data without an explicit check.

```typescript
type Json = null | boolean | number | string | Json[] | { [key: string]: Json };
type Schema = { [key: string]: Json };
interface VisualNode { id: string; label: string; description?: string; phase: string; inputs?: string[]; outputs?: string[]; successCondition?:{path:string;equals:Json} }
/** code must define synchronous function main(input) for JavaScript, or def main(input) for Python, returning JSON. */
interface FunctionOptions { language:'javascript'|'python'; code:string; input?:Json; outputSchema?:Schema; timeoutMs?:number; executionMode?:'pure'|'trusted_local' }
declare function functionTask(options: VisualNode & FunctionOptions): Promise<Json>;
```
