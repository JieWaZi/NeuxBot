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

Internal storage is not a business input. Reuse `workflow.workspace` from preparation and `flowContext.workspace` during a run; Python uses the host's `ALLY_WORKFLOW_STATE_DIR` and `ALLY_WORKFLOW_CACHE_DIR`. Put persistent token/configuration state and caches there with private permissions. Do not invent `storage_dir`/`cache_dir` defaults such as `~/PingCodeWiki` or ask the user to select internal directories. If the user wants synchronized Wiki documents in a chosen folder, that folder is an explicit business output destination; keep the synchronization script, token cache and internal state in the workflow workspace.

## Evidence and failures

Implement credential and token storage with appropriate local permissions, atomic updates, expiry handling, and bounded retry behavior. Verify the API's actual invalid-token response. Do not expose secrets in commands, logs, source, or downloadable artifacts. Surface missing setup, incomplete pagination, and empty matches distinctly.

A simulated endpoint may verify cache reuse and pagination logic. It does not verify real credentials, permissions, or the external API. Deliver the saved workflow with that distinction intact.
