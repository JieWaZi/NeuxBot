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

For other domains, read the relevant guide advertised by the current capability.

## Discover and invoke

- Run `allycli capabilities` at first use in an execution. Discover only the
  needed operations and reuse results. Read `describe <operation-id>` when its
  current contract is missing; it owns inputs, limits and completion semantics.
- For workflow creation/editing, use `workflow authoring prepare --primitives
  <actual DSL calls>` when advertised. Reuse its guides, SDK and save contracts.
  Read `guide <guide-id>` only for relevant instructions not already supplied.
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
