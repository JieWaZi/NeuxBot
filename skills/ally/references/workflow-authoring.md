# Workflow Authoring

Turn the user's goal into an agreed workflow. Use their language for labels, input descriptions, questions and delivery.

## Prepare once

Preparation includes the relevant SDK, current source/revision, pending annotations, bounded resource pages and save contracts. Reuse these results and follow returned cursors when a required item is outside the first page.

Choose primitives for the actual work: `worker` for reasoning, research or generation by a real partner; `action` for declared operations; `functionTask` or DSL for deterministic processing; `askUser` for business decisions genuinely needed during a run. Keep distinct responsibilities visible without imposing a minimum number of phases or decorative nodes. A cohesive task may have one phase. Never replace requested agent work with canned function output. Resolve partners and supported runtime/account/model/reasoning choices from the prepared resources. Report an unavailable required partner as a missing dependency.

Declare behavior through `--features`: `presentation` for composed in-app pages, `files` for deliverable files, `schema` for advanced input controls, `api-retrieval` for API retrieval/credential reuse, `im` for authorized message delivery and `budgets` for explicit run limits. Declare Python with `--language python`. Basic inputs and saving are always included. Use `sdk get` only for a missing detail, with advertised topic names and primitive mappings.

## Settle material decisions

Reuse prior answers and explicit delegation. Investigate discoverable facts before asking. Clarify only choices that materially affect scope, phases, execution types, startup inputs, output or external effects. When available, use the advertised structured questions contract; cancellation, skipping and silence do not approve a choice. Continue independent work while awaiting answers.

For a new workflow or substantial redesign, give a compact plan with the outcome, phases, execution types, inputs and deliverable. Obtain agreement on unresolved material choices; an already approved plan or explicit delegation needs no repeated confirmation. A narrow edit does not reopen settled choices. Authoring questions do not become recurring `askUser` nodes.

## Preserve the input and delivery contract

Inventory all needed external values. Values the user will provide or replace belong in `meta.inputSchema` and are consumed through `args`, including initial setup and secrets. Examples do not authorize hardcoding; never save secret or placeholder defaults. Omit an input only when verified authorized configuration/context, upstream derivation or an explicitly fixed requirement supplies it. Check first-run behavior without a cache. Keep protocol mechanics and internal pagination out of the form unless the user wants to control them. Requiredness must match actual missing-value behavior.

When changing delivery, update the prompt, schemas, presentation declaration, downstream consumers and final return together. Remove abandoned fields, required entries, bindings and formatting steps; retain real branch/loop consumers and agreed external contracts. Preserve business evidence, coverage and uncertainty in the actual deliverable. The prepared SDK explains worker output declarations; file examples are included for file-producing workers.

## Save and report

Write complete source to a UTF-8 file and use the prepared save contract. A separate `source validate` is optional for preflight. Repair returned diagnostics before another save; a changed payload needs a new request ID. Reconcile a revision conflict against current source. The SDK's saving section defines the commit receipt and recovery path.

Compare the submitted source with the agreed goal and report any deviation. Distinguish saving, preflight/simulated tests, a real authenticated run and actual external delivery. Do not run or publish a workflow just to prove it was created without authorization. If the current execution lacks authoring capability, explain that saving needs an authorized NeuxBot workflow creation context.

## Authoring execution recovery

Authoring capability follows the current execution and does not expire while waiting for user answers. `WorkflowAuthoringSuperseded` means a newer execution owns the capability; `WorkflowAuthoringFinished` means this execution has ended. On `retryAdvice: new_authoring_execution`, stop retrying and explain that continuation requires a new message in the workflow authoring chat. The current agent cannot renew its own authority. Closing the editor, repeating preparation, changing request IDs or bypassing the context does not restore old authority. Handle `WorkflowRevisionConflict` separately by reading current source and reconciling changes.
