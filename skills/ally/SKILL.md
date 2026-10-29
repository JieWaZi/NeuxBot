---
name: ally
description: Use NeuxBot's authorized knowledge, delivery, workflow, team, memory, plugin and question capabilities. Includes factual questions covered by available knowledge bases, document ingestion and Wiki maintenance. Ordinary file editing or shell work alone does not require this skill.
---

# NeuxBot

Use the skill directory and execution context supplied by the application.
The executable is `scripts/allycli` (`scripts/allycli.exe` on Windows), relative
to this `SKILL.md`, also supplied as `ALLYCLI_BINARY_PATH`. Invoke its exact
quoted absolute path; every `allycli` example below means that executable.
Resolve reference paths from this skill directory, not the working directory.

## Read the relevant mode

Read only the applicable section or guide. When the host already injected a
Wiki maintenance workflow, execute it directly without submitting another
maintenance task.

| Task | Guidance |
| --- | --- |
| Questions covered by available knowledge, including general factual questions | [Read existing knowledge](references/wiki-guide.md#read-existing-knowledge) |
| Save or organize uploaded documents or existing originals | [Organize documents](references/wiki-guide.md#organize-documents) |
| Consolidate existing Wiki pages/directories or maintain a library | [Maintain existing Wiki](references/wiki-guide.md#maintain-existing-wiki) |
| A precise page correction, or execution inside an existing maintenance task | [Wiki maintenance and page revision](references/wiki-maintenance.md) |
| Ingestion followed by Wiki maintenance | [Combined requests](references/wiki-guide.md#combined-requests) |
| Observe an admitted knowledge task and deliver its final summary | [Knowledge task results](references/wiki-guide.md#knowledge-task-results) |
| Create, edit or diagnose the host-bound workflow, including short follow-ups | [Workflow authoring](references/workflow-authoring.md) |
| Execute an explicitly authorized existing workflow | [Workflow execution](references/workflow-execution.md) |
| Submit the current workflow worker attempt's structured output | [Worker results](references/workflow-results.md) |

For other domains, read the relevant guide advertised by the current capability.

## Discover and invoke

- Prefer the injected `ally_capabilities` MCP tools. They use the same operation
  contracts, authorization and receipts as CLI. When using CLI, include the
  exact `--context <execution-file>` supplied for this turn; do not reuse a
  prior turn's environment or edit connection files.
- A bound workflow authoring conversation stays in authoring mode for short
  corrections and follow-ups. Read the bound source without inventing an ID.
  Requested edits must reach a successful save receipt; prose and local files
  alone do not update the workflow or canvas. Pure questions need no save.

- In a workflow execution with injected MCP, use its tools and input schemas
  directly. Authoring starts with `workflow_authoring_prepare`; reuse its
  guides, SDK and save contracts. Do not bootstrap CLI discovery alongside MCP
  or switch transports to bypass a tool error.
- When the host selects CLI transport, run `allycli capabilities` at first use.
  Discover only needed operations; `describe <operation-id>` owns their inputs,
  limits and completion semantics. Read `guide <guide-id>` only for relevant
  guidance not already supplied. The following flag syntax applies to CLI.
- Use short scalar flags or UTF-8 JSON through `--input <file>` / `--input -`.
  Keep long content and secrets out of shell command strings.
- Choose a stable `--request-id` before a mutation. If its outcome is unknown,
  query `receipts get` with the original operation and ID; preserve the payload
  and identity when retrying. Do not create another request to resolve uncertainty.

## Scope and outcomes

The capability list defines the available surface, not permission for extra
work. Preserve the user's scope, explicit source choices and settled decisions.
Use structured questions for consequential missing decisions; silence, skipping
or cancellation is not approval.

Keep the selected runtime policy. When the local endpoint requires approval,
use its native approval flow for the exact command. Stop dependent work on a
denied or expired context. Only the host can provide `refresh_context`; do not
change identities, credentials, sandbox settings or endpoints to bypass it.
Do not search global installations or install a replacement CLI.

An accepted task or candidate submission is not final success. Follow its
`nextStep` and read the actual resource with `get` or a bounded advertised
`wait`. A timeout ends one observation window and leaves the work running;
follow the relevant guide's observation loop before giving the final response.
Report terminal, interrupted and uncertain outcomes accurately. Submit through
an advertised workflow/team result operation when required; chat text is not
that submission receipt.

For plugin requests, use the current agent's native plugin/authentication
mechanisms within the user's request. Distinguish native installation from
NeuxBot-managed plugins, which are installed through the plugin library.
