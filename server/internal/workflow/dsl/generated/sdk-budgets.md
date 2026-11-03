# Workflow SDK: budgets

Run budgets have different meanings: limits.concurrency caps simultaneous external calls, while limits.maxInstances caps the cumulative number of node instances in the entire run, including actions, functionTask, workers, human requests and child-workflow calls. Every loop iteration or batch task consumes additional instances; phase and log do not. Serial execution uses concurrency:1, never maxInstances:1 for a multi-step workflow. Unless the user requested a smaller total budget, omit maxInstances to use the bounded host default. If declaring it, count all required steps and allow for loops, batches and branches. The compiler rejects budgets below a provable minimum path, but dynamic paths may require more.

Host budgets from the shared contract: source 1048576 bytes; node business output 1048576 bytes; inline human choices 500 items and 262144 bytes; script action deadline at most 600000 ms. Truncated stdout is never accepted as successful JSON. Use managed file references for larger results.

```typescript
type Json = null | boolean | number | string | Json[] | { [key: string]: Json };
type Schema = { [key: string]: Json };
interface VisualNode { id: string; label: string; description?: string; phase: string; inputs?: string[]; outputs?: string[]; successCondition?:{path:string;equals:Json} }
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
```
