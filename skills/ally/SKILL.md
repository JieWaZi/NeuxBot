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

## Command syntax and examples

Read the matching reference below before the first domain call. Each reference
contains shell examples and complete JSON input examples for its task. In all
shell blocks, replace the leading `allycli` with the exact quoted executable
path described above. Examples use POSIX shell syntax; use the current shell's
quoting and redirection on Windows. Example IDs, paths, versions and request
identities are fictional: use discovered resources and values from actual reads.
Create JSON files with the file-writing tool as UTF-8 before invoking `--input`;
a displayed JSON block does not create a file. Shell blocks illustrate separate
actions, not a script to execute wholesale. Perform only the requested actions.

CLI flags use kebab-case (`--library-id`, `--source-version-id`, `--top-k`);
JSON input uses camelCase (`libraryId`, `sourceVersionId`, `topK`). Copy the
reference's spelling. `--library` and `--libraryId` are not `--library-id`.
`--timeout 30s` is a CLI duration; JSON uses `"timeoutMs": 30000`.

| Task | Command examples |
| --- | --- |
| Knowledge lookup, ingestion and maintenance task | [Knowledge](references/wiki-guide.md#command-examples) |
| Page/directory repair inside the authorized scope | [Wiki repair](references/wiki-maintenance.md#command-examples) |
| Message/file delivery | [IM delivery](references/im-delivery.md#command-examples) |
| Workflow creation/editing | [Authoring](references/workflow-authoring.md#command-examples) |
| Workflow runs | [Execution](references/workflow-execution.md#command-examples) |
| Worker result submission | [Results](references/workflow-results.md#command-examples) |
| Workflow file delivery | [Output](references/output-delivery.md#command-examples) |
| Workflow API retrieval | [API retrieval](references/api-retrieval.md#command-examples) |
| Team coordination/reporting | [Lead](references/team-lead.md#command-examples), [member](references/team-member.md#command-examples) |
| Durable memory | [Memory](references/memory.md#command-examples) |
| Structured questions | [Questions](references/user-questions.md#command-examples) |

## Discover and invoke

- Run `allycli capabilities` at first use in an execution. Discover only the
  needed operations and reuse results. Use the reference example when it covers
  the needed input shape. Read `describe <operation-id>` only for missing input
  shapes, constraints or completion details; it owns the live contract.
  On `AgentInputInvalid`, read the exact subcommand `--help` or `describe` once, correct the call and reuse that
  contract. Do not trial alternate flag spellings or treat a parse failure as
  an empty business result.
- For workflow creation/editing, use `workflow authoring prepare --primitives
  <actual DSL calls>` when advertised. Reuse its guides, SDK and save contracts.
  Read `guide <guide-id>` only for relevant instructions not already supplied.
- Use short scalar flags or UTF-8 JSON through `--input <file>` / `--input -`.
  Keep long content and secrets out of shell command strings.
- Choose a stable `--request-id` before a mutation. If its outcome is unknown,
  query `receipts get` with the original operation and ID; preserve the payload
  and identity when retrying. Do not create another request to resolve uncertainty.

## Read complete results

For a potentially large response, redirect stdout to a task-local JSON file and
check the CLI exit status before parsing it. Read valid JSON projections and
bounded arrays from that saved response. Preserve `ok`, `completion`, resource
identity, retrieval mode, candidate identities and continuation fields. Read
remaining candidates or full evidence from the saved file as needed, without
rerunning the same business query. See the knowledge example for the exact flow.
Do not use `head -c`, `cut` or another byte prefix as a JSON result: it can lose
later candidates and metadata or break the JSON. A saved response is complete
only for that call; follow the operation's pagination for further results.

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
