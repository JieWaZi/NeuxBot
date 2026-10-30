# API retrieval example

User request: “Create a workflow that retrieves records from an external service. I will provide the application identifier and secret. Cache the token locally so it is not fetched every time.”

Treat this as an authoring example, not an authoritative description of a particular API. Verify the chosen service's current official API documentation before implementation.

## Resolve retrieval scope

Reuse an explicit retrieval scope or delegated implementation choice. If the result extent remains unresolved, ask:

“What should one run retrieve? I recommend automatically fetching all work items matching your chosen scope, with pagination handled internally. Fetching a single page is another option if you want to inspect pages manually.”

Await that answer before dependent implementation. Resolve only consequential remaining choices, such as the business scope, the deliverable, and initial credential provisioning and reuse. Obtain API facts from documentation and actual configuration.

## Candidate plan to discuss

| Phase               | Purpose and output                                                                                               | Execution                                                             |
| ------------------- | ---------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| Prepare access      | Check configured credentials and reuse or refresh a valid token; do not expose the token as a user-facing result | Code/action                                                           |
| Retrieve work items | Apply the agreed scope, fetch the agreed result set, and report completeness                                     | Code/action                                                           |
| Prepare the result  | Produce the agreed fields and output artifact                                                                    | Deterministic code; a partner only if semantic analysis was requested |

This is a candidate responsibility split, not a mandatory three-phase template. Adapt it to the actual request. A request to summarize risks or explain delays introduces real partner work; simple retrieval does not.

## Parameter review

- The promised identifier and secret are input fields consumed through `args`, even when they are needed only for initial setup or token refresh. Token reuse does not provide missing credentials. Use an existing configured connector instead only when that authorized source is verified and matches the user's intended input contract. Confirm any proposed persistent storage of the secret separately from the already requested token cache.
- A supplied query scope is an input, with a real non-sensitive default when appropriate. Keep it fixed only when the user explicitly requested a fixed scope; merely providing one example is not such a request.
- Page numbers and page size normally remain internal when the user wants all matching results.
- Never mark initial credentials “optional” solely because a cache might exist. Define and verify the actual setup path and behavior when configuration is missing.
- Give business inputs concise labels and useful defaults. Do not introduce project, keyword, or output-format selectors without a corresponding user need.

## Evidence and failures

Implement credential and token storage with appropriate local permissions, atomic updates, expiry handling, and bounded retry behavior. Verify the API's actual invalid-token response. Do not expose secrets in commands, logs, source, or downloadable artifacts. Surface missing setup, incomplete pagination, and empty matches distinctly.

A simulated endpoint may verify cache reuse and pagination logic. It does not verify real credentials, permissions, or the external API. Deliver the saved workflow with that distinction intact.

## Command examples

Use the common rules in [SKILL.md](../SKILL.md). Prepare deterministic API work
with the API/credential guidance; add `worker` only for requested reasoning:

```sh
allycli workflow authoring prepare --primitives functionTask --features api-retrieval,files
```

Implement against verified API documentation and write complete source to
`workflow.js`. The initial identifier/secret remain declared business inputs,
not shell arguments or source constants. Optional preflight and authorized save:

```sh
allycli workflow source validate --source workflow.js
allycli workflow source save --source workflow.js --expected-revision-id revision-example --request-id api-source-save-01
allycli receipts get --operation workflow.source.save --request-id api-source-save-01
```

Use the actual revision from preparation. This creates the workflow; a user-
authorized run uses [execution](workflow-execution.md#command-examples) with
inputs matching its revision. `allycli` has no generic HTTP/API passthrough.
