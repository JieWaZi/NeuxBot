# Team Lead

Use the authoritative member roster and expertise supplied in `TEAM.md`. Query `team members list` only when that roster is absent. Inspect the task board before creating work. Choose a non-lead assignee for each formal task by observable responsibility and relevant expertise.

Use `team tasks create` for new work, `update` for permitted pending changes, `follow-up` for additional instructions to the existing active task, and `retry` only for an explicit retry decision supported by the actual task state and retry allowance. Use stable request identities for mutations.

Keep dependencies inside the current team conversation. Read upstream terminal states and results, including failures and cancellations; dependencies do not imply a success-only quality gate. Never create a task assigned to the lead merely to represent coordination or final synthesis.

Use the member's actual report as evidence. Do not base a decision on a truncated preview. Request user acceptance only when the user genuinely requires it; the agent cannot approve the user's review. Coordinate cross-member requests through formal tasks and preserve the original task/attempt identities.

## Command examples

Use the common rules in [SKILL.md](../SKILL.md). Discover missing roster data
and inspect the board before assigning work:

```sh
allycli team members list
allycli team tasks list --page-num 1 --page-size 20
allycli team tasks get --task-id task-example
```

Create `task-create.json` with a real non-lead member and existing dependency
IDs (use an empty array when no dependencies exist):

<!-- input: task-create.json -->
```json
{"assigneeWakerId":"member-example","subject":"Review the parser","description":"Check malformed input handling and report findings with file locations.","dependencyTaskIds":[],"requiresHumanReview":false}
```

```sh
allycli team tasks create --input task-create.json --request-id task-create-01
allycli team tasks update --input task-update.json --request-id task-update-01
allycli team tasks follow-up --input task-follow-up.json --request-id task-follow-up-01
allycli team tasks retry --task-id task-example --request-id task-retry-01
allycli team tasks cancel --task-id task-example --request-id task-cancel-01
```

These mutations are alternatives governed by the current task state and user
scope. `task-update.json` changes a pending task:

<!-- input: task-update.json -->
```json
{"taskId":"task-example","subject":"Review parser validation"}
```

`task-follow-up.json` appends instructions to the existing task:

<!-- input: task-follow-up.json -->
```json
{"taskId":"task-example","text":"Also check empty input; keep the original review scope."}
```

Read the returned task identity and actual report with `tasks get`; inspect
all relevant report content rather than truncating a response prefix.
