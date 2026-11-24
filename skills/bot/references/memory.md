# Memory

Use memory when prior durable facts help the current task, or the user explicitly asks to remember, correct or forget information. Search the current partner’s memory before saving a fact. The host determines the partner from the authorized execution.

Write concise stable knowledge. Do not save access tokens, transient progress, whole conversation transcripts, or unverified guesses as durable facts. An explicit user request to remember or forget requires a real operation and receipt, not a verbal promise.

Use `memory entries search/get/create/update/forget`. Search is bounded retrieval and returns its actual semantic or keyword mode; do not call topK results complete coverage. Update replaces a known entry's content; forget removes current knowledge while retaining the change event. IDs must come from visible results, and belong to the current authorized partner. New entries start as regular memory.

## Command examples

Use the common path, JSON-file and identity rules in [SKILL.md](../SKILL.md).
Search first; use returned IDs for get/update/forget. These are independent
examples, not an instruction to run every mutation.

```sh
botcli memory entries search --query "report language" --top-k 5
botcli memory entries get --memory-id memory-example
botcli memory entries create --input memory-create.json --request-id memory-create-01
botcli memory entries update --input memory-update.json --request-id memory-update-01
botcli memory entries forget --memory-id memory-example --request-id memory-forget-01
```

`memory-create.json` contains the fact to save.

<!-- input: memory-create.json -->
```json
{"content":"Use English for release reports."}
```

`memory-update.json` replaces the content of the selected entry.

<!-- input: memory-update.json -->
```json
{"memoryId":"memory-example","content":"Use English for release reports and include verification results."}
```
