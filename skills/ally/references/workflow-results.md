# Worker Results

Read `workflow_results_schema` through the injected MCP for the current attempt. Use the actual exposed tool name and schema. Only when the host explicitly selects CLI transport, use the supplied executable with this turn's exact context and the equivalent `workflow results schema` operation. A tool error does not authorize switching transport or attempts.

Complete the actual work in the approved scope. Read promised files to verify existence and nonempty content; use run-specific paths so later runs cannot overwrite earlier delivery. Keep evidence and limitations in the fields required by the schema, without inventing results to make validation pass.

Submit with `workflow_results_submit`, passing `data` as the actual JSON value and a stable `requestId`. The value can be a string, array or object when the authoritative schema allows it; do not JSON-encode it into an extra string or invent a wrapper inside data. A file worker submits its actual file reference. Do not duplicate reports or create a separate presentation page. In host-selected CLI mode, the equivalent command is `workflow results submit --input <UTF-8-file> --request-id <stable-id>`, with a file containing {"data": <actual value>}.

If the response is lost, query `receipts_get` for operation `workflow.results.submit` and the original requestId (CLI mode: `receipts get`). Preserve payload and identity while the outcome is uncertain. Correct a rejected or invalid submission with a new requestId only while the attempt is valid and no candidate has been accepted. An accepted candidate cannot be replaced by different content, even with a new requestId.

A submission receipt means candidate accepted. After a successful submission, finish the current execution; the host confirms node success from the execution status and current attempt/fence. Stop dependent actions on an expired grant. Final chat text is not the result submission protocol, and a stale attempt must not submit for a new attempt.
