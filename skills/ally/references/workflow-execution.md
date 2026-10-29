# Workflow Execution

Use `workflow definitions list` and `workflow definitions describe` to read the actual authorized definition, revision and input/output contracts. A known ID is not permission to invoke it. Do not invent resources or widen the execution configuration.

Prepare only inputs required by the selected immutable revision. A file reference must point to an actual permitted file; a path is not file content. Use the server's file contract rather than guessing types from extensions. Supply a stable request ID to `workflow runs start` and keep its receipt.

Relative file inputs resolve from the CLI process's current directory. Only schema-declared files are resolved and frozen; ordinary strings stay unchanged. The receipt binds the frozen file bytes: changing a file under the same request ID is a conflict. If the original file is no longer available or the start outcome is uncertain, query `allycli receipts get --operation workflow.runs.start --request-id <original-id>`.

Acceptance is not completion. Read the real run with `workflow runs get` or use bounded `workflow runs wait`. Preserve failure, partial evidence and uncertainty. A wait timeout does not cancel the run. Cancel only the authorized run when requested; cancellation cannot undo completed external effects.

Read output files through actual references in the result contract. Do not treat an arbitrary JSON string or final chat message as a successful external result.

## Command examples

Use the common rules in [SKILL.md](../SKILL.md). Discover the definition and
read its immutable revision and business input schema:

```sh
allycli workflow definitions list --search "Report" --page-num 1 --page-size 20
allycli workflow definitions describe --definition-id definition-example
```

If that revision declares a string `topic` input, `run.json` has this shape
(replace `args` with exactly the selected revision's required input):

<!-- input: run.json -->
```json
{"definitionId":"definition-example","revisionId":"revision-example","args":{"topic":"Release readiness"}}
```

```sh
allycli workflow runs start --input run.json --request-id run-start-01
allycli workflow runs get --run-id run-example
allycli workflow runs wait --run-id run-example --timeout 30s
allycli workflow evidence list --run-id run-example
allycli receipts get --operation workflow.runs.start --request-id run-start-01
```

Use the returned run ID and inspect the actual state and outputs. Evidence
listing requires its own advertised capability. Cancel only when requested:

```sh
allycli workflow runs cancel --run-id run-example --request-id run-cancel-01
```
