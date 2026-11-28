# Automatic tasks

Save a schedule only when the user asks for future execution. Use the current
capability schema and confirmed local timezone. Confirm necessary missing
details such as the repository or location. Rule creation does not run the
task immediately.

For a simple repeated check, schedule the current partner with a complete
instruction. For recurring checks that require a durable baseline, scripted
comparisons, or several steps, reuse or author a workflow and schedule that
workflow. Read the [workflow authoring guide](workflow-authoring.md) when
creating one. Use `workflow.definitions.create` to open a draft if needed,
and save its source before scheduling. Pass `workflow: {id, revisionId, args}`
with the saved IDs and any fixed inputs, together with the schedule. Use the
user's cadence if supplied. For a change subscription without a specified
cadence, choose a reasonable interval and confirm it in the saved task.

The workflow and its workers inherit the current authorized permission
ceiling. Prefer a check that can run under that permission. Scheduled runs of
local script actions and trusted Python can wait for approval under default
permissions. If the chosen check requires this, explain it when confirming
the schedule. The schedule is confirmed only after the rule tool returns its own
saved receipt and task link.

For change-only reporting, use `reportPolicy: "changes_only"`. Establish the
initial baseline without announcing old items when the user asks about new
changes. Scope persisted state to what is being checked. Return the exact
text `[NEUX_NO_CHANGES]` after a successful check with nothing to report. For
a workflow, return that text as the final value, or return a short report
with links when there is a change. Preserve a failed check as a failure.

Use the actual task ID from list or get to edit, pause or resume. Use a
separate request ID per mutation and recover a missing receipt with the
original request ID. Delete existing rules only when explicitly requested.
