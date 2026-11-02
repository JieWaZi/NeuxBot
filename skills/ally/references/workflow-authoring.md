# Workflow Authoring

Turn the user's goal into a runnable, inspectable workflow that can be edited further. Source is authoritative; the canvas projects that source. Chat explanations and local drafts are not saved workflows. Use the user's language for phases, steps, questions and delivery descriptions.

## 1. Continue the task and gather facts first

Your role in a host-bound authoring conversation is to create or modify a reusable NeuxBot workflow. Treat a bare business goal, including the first message, as requirements for that workflow even when the user does not say "workflow". This role takes precedence over general partner advice to answer simple requests directly. Short corrections such as "add a step" or "change phase two" still refer to the current workflow. Pure explanations and questions need no save and must not create a revision. Requested changes require a successful save receipt.

Gather facts needed to design the reusable workflow; do not perform the business task and deliver a one-off result in place of authoring it. Native CLI workflow, task planning or automation features cannot replace NeuxBot source, canvas or saving. Real business execution is a separate explicitly authorized operation. Safe compilation and pure synthetic checks are authoring validation, not permission for a real run. Selected phases, nodes and run references identify the targets of this edit, not permission to change other steps.

When `ally_capabilities` MCP is mounted, use `workflow_authoring_prepare` to obtain current source, revision, annotations, relevant SDK, resource pages and save contracts. Do not probe through CLI first. Pass a `primitives` array of actual DSL calls, plus `features` and `language` only when needed. Use `workflow_source_get` for source-only reads without guessing a workflowId. Tool names may have a host prefix; use the actual tools. Use the executable and exact `--context` supplied for this turn only when the host explicitly selects CLI transport. An MCP failure does not authorize switching transport to bypass the error.

The host returns the authoritative absolute storage layout in `workflow.workspace` from preparation, or `workspace` from `workflow_source_get`. Use `rootDirectory` and its advertised `scriptsDirectory`, `stateDirectory`, `cacheDirectory`, `temporaryDirectory` and `runsDirectory`. These are under the configured managed-files root, normally `~/.neuxally/workflow-workspaces/<workflow-id>/`; never reconstruct this path or choose `/tmp`, `~/PingCodeWiki` or another directory. Store source drafts and auxiliary scripts in `scriptsDirectory`, temporary validation files in `temporaryDirectory`, persistent configuration in `stateDirectory`, and caches in `cacheDirectory`. The revision source/bundle remains authoritative; local script files are auxiliary copies. Do not add storage/cache/script directory inputs or ask the user to choose internal paths. A business output destination is separate: only an explicit user choice may place final business files outside the workflow root, and that exception never moves scripts or caches.

Preparation is the working reference: inspect its returned topics and content before another SDK call. Fetch only a missing topic, not each primitive again. Follow resource cursors when needed. For batch processing, independent review or failure recovery, read [orchestration patterns](workflow-patterns.md) before writing source. For script-to-choice-to-worker flows, read the review example linked below or the advertised `review` SDK example. Partners, models and directories come from visible resources. A workflowId can only verify the binding, not switch authorization. Explain missing authoring capability as a context problem; supplying an ID cannot fix it.

For old canvas references, read the original revision through `workflow_source_get` with revisionId, then align it with the current revision by stable identity. Preserve other saved edits; ask only when the target cannot be mapped uniquely. To diagnose a run, first read relevant pages of `workflow_evidence_list` for actual inputs, outputs, failure reasons and external operation receipts, then design the repair.

## 2. Define the business contract, then choose orchestration

First verify the objects to process, counts/pagination, existing configuration, partners, files and external interfaces. Do not start with a fixed three-phase layout. Organize the implementation plan around these five items, reusing prior agreement and explicit delegation:

| Item | Implementation detail |
| --- | --- |
| Goal and scope | Which objects to process; what completion means; what is outside this scope |
| Inputs and decisions | Startup inputs, discoverable configuration, derived values and decisions needed during a run |
| Phases and steps | Each step's responsibility, executor, prerequisite data, output and actual dependencies |
| Validation and failure | How to determine business success; whether an item failure stops execution or preserves other results |
| Delivery and effects | Return value/real files, coverage and omissions, and what will be written or sent |

Clarify only unresolved choices that affect scope, external effects or delivery. Use host structured questions; authoring questions appear in the chat composer, just as in ordinary partner conversations. Runtime `askUser` decisions appear on the workflow canvas. Do not turn one-time authoring clarification into recurring `askUser` nodes. Do not reconfirm existing authorization. Cancellation, skipping and silence do not approve unresolved choices.

When such a choice is required to finish the requested workflow, read the [question contract](user-questions.md), submit it, and use its bounded observation loop. Continue independent investigation or a local draft while waiting. Save only after the material choice is answered, or the user explicitly authorizes a reduced scope. A pending wait result does not authorize silently replacing requested customer delivery with a file-only workflow. If the execution cannot continue waiting, report the unresolved decision and draft status without claiming the requested workflow is complete.

| Responsibility | Primitive |
| --- | --- |
| Reading, research, judgment, generation, independent review | `worker` calls an existing partner; fixed script commentary cannot substitute for it |
| Declared local scripts, HTTP or built-in operations | `action`, with explicit inputs, outputs and actual read/write/external risks |
| Deterministic calculation, transformation, deduplication, coverage statistics | Simple DSL expressions or `functionTask` |
| Choices, missing values or confirmations that can only be resolved during this run | `askUser`, with choices from real upstream data |
| Each object follows the same sequence of steps | `pipeline`; each item can advance as soon as its preceding step completes |
| Independent work followed by comparison or deduplication across all results | `parallel` followed by a join; use a barrier only for actual cross-item dependencies |

A phase may contain multiple steps. Phases express business progress; steps express observable responsibilities. Do not impose a minimum phase count or add decorative nodes for every string transformation. See [Orchestration and validation patterns](workflow-patterns.md) for batch processing, review and recovery.

## 3. Design the canvas, data and user decisions together

Use static `meta` and fill phase title/detail and node label/description/phase. Node labels are static readable strings such as "Review document", not expressions such as `'Review ' + candidate.title`; put per-item names in the prompt/input. Cards answer "what and why," such as "Fetch documents for review" or "Verify source evidence." Keep Function, python, trusted_local, code and internal IDs out of card copy. Descriptions are one sentence about the business task. Use stable ASCII IDs for source identity; changing a display name must not change the ID. Declare phase explicitly for concurrent nodes.

The host generates the canvas. Do not create separate canvas JSON, HTML pages, presentation declarations or "View output" entries. Keep data required by real downstream consumers and files requested by the user; do not wrap reports again for display. When changing business output, update prompts, Schema, downstream field access and the final return together, removing fields without consumers.

Values the user supplies or replaces at startup belong in `meta.inputSchema` and are read from `args`. Reuse verified authorized configuration, fetch live choices upstream and use `askUser` for decisions during a run. Check first-run behavior without a cache. Examples must not become fixed business data; secrets and placeholders must not become defaults.

Use text/select/multiselect/form/confirm questions according to the SDK. Fetch choices before asking, retain stable values and readable labels, and handle empty lists, count limits and mapping selections back to objects. `HumanAnswer` contains status/source/value: check status before consuming value. Defaults only prefill; skipping/timeouts are not user choices, and confirmation requires an explicit affirmative answer. Cancellation aborts; retry replay preserves the original answer source. Do not expose protocol details such as pagination cursors as business questions without a reason.

## 4. Implement and validate critical paths

Use `ally.workflow`. Main orchestration is a restricted, durable JS-shaped DSL, not full Node.js; arguments and values follow JSON semantics. Local scripts may live in Python `scripts` frozen with the revision and referenced by a script action: stdin is input JSON, JSON-protocol stdout emits exactly one value, and logs go to stderr. Read the action's `.data`. Use JavaScript `functionTask` for pure computation; its code must define synchronous `main(input)`. Do not introduce fs, Promise scheduling or external libraries into main orchestration, or mutate shared captured variables during concurrency.

These are different entry points. A Python script action executes the entire module; defining `main(input)` alone does nothing. Its complete code must read stdin and emit stdout, for example:

```python
import json, sys
request = json.load(sys.stdin)
json.dump({"count": len(request["items"])}, sys.stdout)
```

If a script defines main, call it and serialize its return at module level. Python functionTask instead calls its defined main through the host. Before saving a script that only processes synthetic data, run the exact frozen code with a small JSON input and parse its stdout as one JSON value; empty stdout with exit code zero is a failure. For side-effecting scripts, do not run them without authorization; report the untested boundary rather than claim validation.

Worker resolve binds an existing Waker from actual resources and reuses the selected execution configuration. Give machine-consumed results an explicit Schema and read the returned value directly. Without a Schema, the result is text; do not guess wrappers such as `{content:…}`. See [Worker results](workflow-results.md) for formal submission. For runtime paths, use `flowContext.workspace`; default business files belong in `runDirectory`. Pass absolute paths to partners, whose technical Chat working directories are isolated. Use `flowContext.scope.workingDirectory` for final business files only when the user explicitly selected that external destination. Python reads the corresponding host-owned `ALLY_WORKFLOW_ROOT`, `ALLY_WORKFLOW_SCRIPTS_DIR`, `ALLY_WORKFLOW_STATE_DIR`, `ALLY_WORKFLOW_CACHE_DIR`, `ALLY_WORKFLOW_TEMP_DIR` and `ALLY_WORKFLOW_RUN_DIR` environment values; `tempfile` already uses the managed temporary directory. Do not override these or redirect internal state via `HOME` or temporary-directory variables.

Business checks must affect control flow: use `successCondition:{path,equals}` or an explicit throw. Check `.data` for actions and the direct result for workers. A false value, null, process exit code 0 or valid Schema does not by itself mean business success. Independent review verifies evidence against original materials rather than merely repeating the first review's conclusions.

Before saving, walk one concrete item from each node's actual return through all downstream field reads. A pipeline returns its last stage value; explicitly carry earlier judgments forward when the summary needs them. Test summary code with representative complete, missing-review and missing-verification values; missing required evidence must not count as completed. Also walk through first use without configuration, empty choices, unanswered/cancelled questions, single-item failures, failed checks and unknown side effects. Run a compilation preflight; safe pure scripts may be tested with small inputs. Compilation does not prove real model, authentication, network or delivery success. Do not start real business work merely to validate authoring without authorization.

The [document review example](workflow-review-example.workflow.js) demonstrates multiple steps in one phase, dynamic multiselect, per-item review/verification and a coverage summary. Its materials are fictional. Before applying it, replace placeholder partner identities with actual identities from preparation and implement the user's required data source. Do not treat the demonstration as a business implementation.

## 5. Save and close with evidence

Save full source or exact `{search,replace}` patches. Both require current expectedRevisionId and a stable requestId; patches also require baseSourceDigest. MCP source is full text, not a local path. Long source may first be written as a UTF-8 draft, then submitted as content according to the tool contract. `workflow_source_validate` is an optional preflight; saving itself also compiles and validates.

Claim the change took effect only after a formal receipt containing saved:true, revisionId and sourceDigest. Each new explicit save creates a revision even when source is unchanged. Replaying the same request returns its original receipt; do not add commit/finish. If a response is lost, call `receipts_get` with the original `operation:"workflow.source.save"` and requestId. Preserve the original payload and identity until the outcome is known. Use a new requestId for corrected source. On a revision conflict, reread and reconcile; do not overwrite someone else's saved changes.

Make at most three local diagnostic repair attempts. If they still fail, report the specific blocker and save status. For `WorkflowAuthoringSuperseded` / `WorkflowAuthoringFinished` with advice new_authoring_execution, stop retrying the old execution and explain that continuation requires another message in the workflow conversation. Only the host can establish new authorization. Waiting for a user answer does not itself require refreshing identity.

Finish by explaining the business change, whether saving succeeded, the validation performed and real execution/delivery still unverified. Check for omissions and deviations from the goal. Saving, run success and external delivery are separate facts.
