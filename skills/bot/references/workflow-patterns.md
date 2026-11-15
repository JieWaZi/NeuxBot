# Workflow Orchestration and Validation Patterns

Use with the [authoring guide](workflow-authoring.md). Function signatures and limits come from the SDK returned by current preparation. This guide explains choices and composition rather than maintaining separate interface declarations.

## Advance each item; join only when needed

When each document's "review → verify" sequence depends only on that document, use one pipeline. Do not wait for every document's review before allowing the first verification. Cross-object comparison, global deduplication, shared-write conflict checks or checking whether the whole batch has zero results require a global join. Simple map/filter processing does not justify a barrier.

Fix the object list before concurrency, retaining input order and business identity. Tasks return their own results; calculate totals and coverage at the join. Do not push to shared arrays or mutate outer counters in concurrent closures. Explicit phase keeps ownership stable, and node IDs must not change with names.

Each pipeline stage REPLACES the previous value. The group returns each item's LAST stage value, not an automatically accumulated `{review, verification}` object. If the summary needs both, the verification stage must explicitly return `{id: original.id, review: previous, verification: verified}` after awaiting its worker. Returning that worker directly discards the earlier review from the pipeline output, even though its instance evidence remains available. Trace one concrete item through every stage and its downstream field reads before saving.

Define completion from the actual return contract: increment completed only when required review and verification values are present and valid. Missing required fields belong in the incomplete ledger. Do not fill them with null while marking the item complete, and do not substitute a deterministic recomputation for promised partner judgments. A run can succeed technically while its business report is incomplete; inspect the actual final fields, not just the run status.

pipeline/parallel default to fail-fast. When partial results are needed, explicitly choose `failureMode:'collect-null'`: a failed item retains its position as null while diagnostics remain on the original instance. In a pipeline, if verification fails after review succeeds, that review result remains in instance evidence. Null does not mean every substep was unexecuted; inspect the original instance for phase/step-level coverage. Valid JSON null may also be a business value. Require successful results to be non-null objects, then build a completed/needs-verification ledger from the original list and positions. Do not claim full coverage after filter(Boolean), or invent failure reasons from null. Without a summary node that continues execution, do not promise a complete report after failure.

Express expected missing business data as structured results with identity and reason; preserve real run evidence for unexpected failures. Account for all requested objects, file types, pages and pagination. Report unprocessed, failed, inapplicable and insufficiently evidenced items. Explicitly end an empty batch or request corrected input; do not invent successful items.

The review example ends with a throw when its all-success gate is unmet. That throw preserves failure evidence; it does not produce a successful return. If the user permits partial results, return coverage instead of using that final throw.

## Independent review and bounded improvement

A worker task includes scope, accessible original materials, action, return contract and acceptance criteria. Main orchestration owns dependencies and state; the model owns judgment. Splitting work should add coverage, expertise or independent verification.

The verifier reads original materials, conclusions to check and evaluation criteria, actively looks for counterexamples, and returns pass/reject/uncertain, evidence and unverified items. Use a separate worker execution. Different partners may add perspectives; two calls to the same existing partner still do not guarantee independent judgment. For blind review, initially withhold the first conclusion and compare in a later step. Keep read-only tasks read-only and divide file ownership for modification tasks. Isolated process working directories do not make writes to a shared business directory safe.

Before a loop, define its maximum rounds, exit conditions and business representation of the accumulated seen set. Include rejected or unsupported candidates in deduplication to avoid rediscovering them. Put set operations unsupported by the DSL in a pure functionTask. Stop when the goal is reached, no new information appears or the budget is reached, preserving remaining gaps.

limits.concurrency caps simultaneous execution; maxInstances caps cumulative nodes across the run. Use concurrency:1 for serial execution, not maxInstances:1. Count the instances needed for batch size × steps, questions and scripts. When no budget is specified, use bounded host defaults. Do not invent an unlimited Token budget or borrow a fixed concurrency limit from another tool.

## Separate three failure questions

| Situation | Next step |
| --- | --- |
| Source validation failure | Fix the reported structure/type/scope diagnostics and validate again; do not run real business work |
| Run failure or failed business check | Read the original Run, Revision and instance inputs/outputs to locate a model, script or data-contract problem |
| Unknown operation outcome or partial delivery | Reconcile the original operation identity, receipts and delivered portions; stop dependent work and establish the effects first |

`BUSINESS_CHECK_FAILED` / `WorkflowBusinessCheckFailed` means a business condition was not met. First inspect actual values, Schema and field access; do not remove the check or make it always true. If source needs correction, read current head, reconcile with the run referenced by the user and save a new revision. The original failure record is not rewritten.

"Retry the original run" differs from "run the corrected revision." The product's retry plan pins the original revision and creates a new Run, showing which steps are reused or repeated; it does not continue from an arbitrary source location. Safe user answers and partner results may be reused. Scripts, files, confirmations and external effects follow the actual plan. Do not promise that every model call reruns or that only the failed step reruns. Save new logic first, then start from the new revision through the user or an authorized run entry point.

Authoring permission does not include arbitrary run/retry permission. If no corresponding tool is available, direct the user to retry from the execution record; do not invent an MCP retry tool. Automatic node retry is also limited by the actual implementation: workers require read-only execution and pure safety conditions, and declaring idempotent does not prove automatic retry. Set policies only for supported operations that are safe to repeat.

Page through workflow_evidence_list to read original failed instances, nested cause, hint, source locations and external receipts. Do not blindly repeat completed or unknown writes. Finish by identifying the original/new revision, confirmed effects and unknown portions.
