# Worker Results

Read `allycli workflow results schema` for the current attempt. Create the actual result in the approved run scope and verify that promised files exist and are nonempty. Use run-specific output paths so a later run cannot overwrite an earlier delivery.

Submit through `allycli workflow results submit --input <file> --request-id <stable-id>`. The input is {"data": <actual value>}; the data can be a string, array or object when the authoritative schema allows it. A file worker should submit its actual file reference. Do not duplicate its report as JSON fields or add an extra report page without a real consumer.

A submission receipt means candidate accepted. After a successful submission, finish the current execution; the host confirms node success from the execution status and current attempt/fence. Stop dependent actions on an expired grant. Final chat text is not the result submission protocol, and a stale attempt must not submit for a new attempt.

## Command examples

Use the common rules in [SKILL.md](../SKILL.md). Discover the bound attempt's
result schema before constructing the result:

```sh
allycli workflow results schema
```

For a root string schema with `format: "file"`, write the verified output path
to `worker-result.json`. A different result schema requires its actual value:

<!-- input: worker-result.json -->
```json
{"data":"/workspace/output/run-example/report.html"}
```

```sh
allycli workflow results submit --input worker-result.json --request-id worker-result-01
allycli receipts get --operation workflow.results.submit --request-id worker-result-01
```

Use receipts for an uncertain submission. After a successful candidate receipt,
finish the current execution; do not submit again to force a terminal status.
