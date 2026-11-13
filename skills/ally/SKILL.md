---
name: ally
description: Use NeuxBot's authorized knowledge, delivery, workflow, team, memory, plugin and question capabilities. Includes factual questions covered by available knowledge bases, document ingestion and Wiki maintenance. Ordinary file editing or shell work alone does not require this skill.
---

# NeuxBot

Use the skill directory and execution context supplied by the application.
The executable is `scripts/allycli` (`scripts/allycli.exe` on Windows), relative
to this `SKILL.md`. Invoke its exact quoted absolute path with the current
`--context <execution-file>` supplied by the host; every `allycli` example below
means that executable and explicit context.
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
| Host-bound workflow goals from the first message, edits and short follow-ups | [Workflow authoring](references/workflow-authoring.md) |
| Execute an explicitly authorized existing workflow | [Workflow execution](references/workflow-execution.md) |
| Submit the current workflow worker attempt's structured output | [Worker results](references/workflow-results.md) |

For other domains, read the relevant guide advertised by the current capability.

## Command syntax and examples

When the host selects CLI transport, read the matching reference below before
the first domain call. These references contain shell examples and complete
JSON input examples for their tasks. In all shell blocks, replace the leading `allycli` with the exact quoted executable
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
| Team coordination/reporting | [Lead](references/team-lead.md#command-examples), [member](references/team-member.md#command-examples) |
| Durable memory | [Memory](references/memory.md#command-examples) |
| Host-bound partner setup | [Partner onboarding](references/partner-onboarding.md#command-examples) |
| User-initiated profile optimization | [Partner profile optimization](references/partner-profile-optimization.md) |
| Structured questions | [Questions](references/user-questions.md#command-examples) |

## Discover and invoke

- Prefer the injected `ally_capabilities` MCP tools. They use the same operation
  contracts, authorization and receipts as CLI. When using CLI, include the
  exact `--context <execution-file>` supplied for this turn; do not reuse a
  prior turn's context or edit connection files.
- In a host-bound workflow authoring conversation, business goals describe a
  reusable workflow to create or modify, starting with the first message. Follow
  the authoring guide supplied in the host instructions. Native CLI workflow
  features or a one-off business result cannot replace the NeuxBot save receipt.
  Pure explanations need no save; short corrections stay in authoring mode.

- In a workflow execution with injected MCP, use its tools and input schemas
  directly. Authoring starts with `workflow_authoring_prepare`; reuse its
  guides, SDK and save contracts. Do not bootstrap CLI discovery alongside MCP
  or switch transports to bypass a tool error.
- When the host selects CLI transport, run `allycli capabilities` at first use.
  Discover only needed operations; `describe <operation-id>` owns their inputs,
  limits and completion semantics. Read `guide <guide-id>` only for relevant
  guidance not already supplied. On `AgentInputInvalid`, read the exact
  subcommand `--help` or `describe` once and correct the call; do not trial
  alternate flag spellings or treat a parse failure as an empty business result.
  The following flag syntax applies to CLI.
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

Use the absolute task directories supplied in the current turn. With no
directory selected, ordinary chats use the partner's fixed `Agents/<waker-id>/`
working directory. New persistent deliverables go in `outputs/<execution-id>/`, and exact input copies and
intermediate files go in `tmp/<execution-id>/`. Use the project entries supplied
by the host for this turn; real project locations are not moved. Workflow
scripts, state and run files follow the workflow workspace contract in the
relevant guide.

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
