# Workflow Execution

Use the injected MCP tools and their schemas when available. Operation names
below map to underscore tool names, for example `workflow_runs_start` and
`workflow_runs_get`; tool prefixes come from the host. Use CLI syntax only
when the host explicitly selects CLI transport, with its supplied executable
and current context. Do not switch transport to bypass an error.

Use `workflow definitions list` and `workflow definitions describe` to read the actual authorized definition, revision and input/output contracts. A known ID is not permission to invoke it. Do not invent resources or widen the execution configuration.

Internal workspace paths are host-owned. The running DSL receives `flowContext.workspace` under the workflow’s stable root, with separate `runDirectory` for each run; public run responses do not expose the internal flow context. Scripts, state, cache and temporary files use the corresponding supplied directory fields; do not add those paths as business arguments or choose a new folder. Final files default to `runDirectory`; only an explicit user destination may use an external approved business directory.

Prepare only inputs required by the selected immutable revision. A file reference must point to an actual permitted file; a path is not file content. Use the server's file contract rather than guessing types from extensions. Supply a stable request ID to `workflow runs start` and keep its receipt.

Use actual authorized absolute file references. In CLI mode, relative file inputs resolve from the CLI process's current directory. Only schema-declared files are resolved and frozen; ordinary strings stay unchanged. The receipt binds the frozen file bytes: changing a file under the same request ID is a conflict. If the original file is no longer available or the start outcome is uncertain, query `receipts_get` with operation `workflow.runs.start` and the original requestId (CLI mode: `receipts get`).

Acceptance is not completion. Read the real run with `workflow runs get` or use bounded `workflow runs wait`. Preserve failure, partial evidence and uncertainty. A wait timeout does not cancel the run. Cancel only the authorized run when requested; cancellation cannot undo completed external effects.

Read output files through actual references in the result contract. Do not treat an arbitrary JSON string or final chat message as a successful external result.

For one user request, keep one accepted run and its immutable revision. Continue
observing that run after an observation timeout; do not change requestId to
start another. A failed summary only retries the summary. Use the actual run's
formal output, not a prior successful run. Explicit retry creates a new run of
the original revision with a visible reuse/rerun plan. Unknown script effects
must be reconciled before re-execution. Optimization reads the referenced
run evidence and current head, then saves a new revision without silently running.
