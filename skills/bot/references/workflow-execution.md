# Workflow Execution

Use the injected MCP tools and their schemas when available. Operation names
below map to underscore tool names, for example `workflow_runs_start` and
`workflow_runs_get`; tool prefixes come from the host. Use CLI syntax only
when the host explicitly selects CLI transport, with its supplied executable
and current context. Do not switch transport to bypass an error.

Use `workflow definitions describe` to read the target definition, revision and input/output contracts. Use the exact definition and revision from the user's workflow reference or the host's authorization. After saving a repair in this turn, use the exact revision from the save receipt if the user asked to run the repair. In an unscoped local chat, use `workflow definitions list` to locate a workflow by name and `describe` to resolve its revision. Pass the resolved `definitionId` and `revisionId` to `workflow runs start`.

For a requested repair in a local chat, stay in the same conversation. Locate the relevant run with `workflow runs list` when needed, read `workflow evidence list`, then read the workflow's current source with `workflow source get`. The run evidence supplies its `definitionId`. Pass it as `workflowId` on source calls when there is no default target. Follow the authoring guide to save against the current revision. Start the repaired revision only when the user requests a run.

Internal workspace paths are host-owned. The running DSL receives `flowContext.workspace` under the workflow’s stable root, with separate `runDirectory` for each run; public run responses do not expose the internal flow context. Scripts, state, cache and temporary files use the corresponding supplied directory fields; do not add those paths as business arguments or choose a new folder. Final files default to `runDirectory`; only an explicit user destination may use an external approved business directory.

Use the user’s message, conversation, defaults and authorized files to prepare the inputs required by the selected revision. Ask in the current chat when required information is missing. A file reference must point to an actual permitted file; a path is not file content. Use the server's file contract rather than guessing types from extensions. Supply a stable request ID to `workflow runs start` and keep its receipt.

Use actual authorized absolute file references. In CLI mode, relative file inputs resolve from the CLI process's current directory. Only schema-declared files are resolved and frozen; ordinary strings stay unchanged. The receipt binds the frozen file bytes: changing a file under the same request ID is a conflict. If the original file is no longer available or the start outcome is uncertain, query `receipts_get` with operation `workflow.runs.start` and the original requestId (CLI mode: `receipts get`).

Observe the accepted run with `workflow runs wait`. When the wait returns an incomplete observation, keep observing the same run. If the run is waiting for the user, its real request is already available in the chat. Continue waiting for the terminal state before giving the final answer. Use `workflow runs get` when you need to inspect the current evidence. Preserve failure, partial evidence and uncertainty. Cancel only the authorized run when requested.

Use the conversation to identify the run the user means. If its ID is already known, use it directly. Use `workflow runs list` only when you need to locate it. When the user asks to run it again and the source is clear, call `workflow runs repeat` with that run ID and a stable `requestId`. A previous normal success starts from the beginning. A failure or cancellation uses the original retry plan and excludes uncertain effects. Both keep the source revision and frozen inputs. Ask which run the user means if several match. Inspect a prior result with `workflow runs get`. If the receipt, revision or required file is unavailable, tell the user which item is missing. If the user asks to change the inputs, start with the requested changes using an authorized workflow selection.

Read output files through actual references in the result contract. Do not treat an arbitrary JSON string or final chat message as a successful external result.

For one user request, keep one accepted run and its immutable revision. Continue
observing that run after an observation timeout; do not change requestId to
start another. A failed summary only retries the summary. Use the actual run's
formal output, not a prior successful run. Explicit retry creates a new run of
the original revision with a visible reuse/rerun plan. Unknown script effects
must be reconciled before re-execution. Optimization reads the referenced
run evidence and current head, then saves a new revision without silently running.
