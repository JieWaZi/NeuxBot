# Structured User Questions

Ask only for a missing consequential decision that cannot be established from the task or available evidence. Reuse explicit instructions and already settled choices. Ordinary implementation choices can be made within the user's delegated scope.

Use the live `interaction.questions.create` contract for supported fields, choices and limits; describe it if missing. Give fields ordinary, distinct property names and set `allowSkip` according to the decision. The question belongs to the current execution and the user answers through the application. Read its answer state with get or bounded wait.

Do not submit answers yourself, infer consent from silence, or treat skipped, cancelled, invalidated or still pending as approval. A wait timeout stops observation only. Continue independent work where possible; keep dependent work waiting for the actual answer. A business question does not replace tool authorization or user acceptance of a completed task.

## Command examples

Use the common rules in [SKILL.md](../SKILL.md). For a genuinely unresolved
single-choice decision, write `question.json` using a supported string schema:

<!-- input: question.json -->
```json
{
  "message": "Which format should the report use?",
  "requestedSchema": {
    "type": "object",
    "properties": {
      "reportFormat": {
        "type": "string",
        "title": "Report format",
        "oneOf": [
          {"const":"html","title":"HTML"},
          {"const":"markdown","title":"Markdown"}
        ]
      }
    },
    "required": ["reportFormat"],
    "additionalProperties": false
  },
  "allowSkip": false
}
```

```sh
allycli interaction questions create --input question.json --request-id question-01
allycli interaction questions get --question-id question-example
allycli interaction questions wait --question-id question-example --timeout 30s
```

Use the create result's question ID. Continue dependent work only with the
actual answered state and submitted value. For free text use `type: "string"`
without choices; for multiple choices use `type: "array"` with string `items`
and the advertised choice schema. Describe only when that additional shape
is needed and missing from the current context.
