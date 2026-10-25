---
name: ally
description: Use when factual or technical questions may be covered by available NeuxBot knowledge bases, or a task needs knowledge research, organizing chat attachments and existing materials, message and file delivery, workflows, team coordination, memory, plugins, or structured user questions. Ordinary file editing or shell work alone does not require this skill.
---

# NeuxBot

The application supplies a complete skill directory and execution context.
Resolve paths from the directory containing this `SKILL.md`, not the current
working directory. The executable is `scripts/allycli` in that directory
(`scripts/allycli.exe` on Windows); references are in its `references/` directory.
Use the exact absolute paths supplied in the execution instructions. For example:

```sh
"<absolute-skill-directory>/scripts/allycli" capabilities
```

Every `allycli` command below and in the references means this same quoted
absolute executable. `ALLYCLI_BINARY_PATH` also points to this skill-local
executable; use the current shell's environment syntax when invoking it.
Do not rely on bare command lookup, search global installations, install a
replacement, invent credentials, or call desktop administration endpoints.

For questions about topics covered by the user's available knowledge bases,
knowledge-base searches, Wiki revisions or requests to save or organize materials, read
[the knowledge reference](references/knowledge.md) from this same skill directory,
using the absolute reference path supplied in the execution instructions.
Do not look for `references/knowledge.md` relative to the working directory.
The user's chat request is
the entry point: resolve the target and requested sources through the current
CLI contracts. Use the knowledge reading workflow even when the user does not
explicitly say "search the knowledge base"; select the depth from their question
and the evidence gaps. It applies to every companion, without a specialist role
or a user-selected research mode. Respect an explicit choice of other sources.
Keep large inputs and results in files.

## Discover only what is needed

- At first use in an execution, run `allycli capabilities`. Its result is the
  current authorized surface, not permission to perform extra work.
- For workflow creation/editing, run `allycli workflow authoring prepare
--primitives <actual DSL calls>` when advertised. It returns the minimal
  guides/SDK, current source and save contract together. Reuse that result.
- For a contract still missing, run `allycli describe <operation-id>`.
  Use its schema, limits, completion meaning and advertised legal values.
- Read a relevant `allycli guide <guide-id>` only when its content was not
  already included in preparation. Live contracts determine allowed syntax.
- Reuse confirmed decisions. Investigate discoverable facts before asking.
  Do not run all discovery commands or read every guide by default.

## Call and verify

- Use simple flags for short scalar values and `--input <file>` or
  `--input -` for structured input. Put source text and secrets in files or
  stdin rather than shell command strings. Default output is JSON.
- For mutations, choose a stable `--request-id` before the first call.
  Preserve that ID and the same payload when resolving an uncertain result.
- Inspect `completion` and the resource's business state. An accepted run,
  registered delivery, or submitted result candidate is not final success.
  Follow the receipt's `nextStep` when supplied.
- Use the resource's `get` or bounded `wait` command when final confirmation
  is needed. A wait timeout changes neither the operation nor the resource.
- If a mutation loses its response, query `allycli receipts get` with the
  same operation and request ID. Do not create a new request just to retry.
- On a denied or expired context, stop the dependent action. Do not switch
  identities, modify the context file, or use a broader endpoint.
  `refresh_context` requires the host to provide a new authorized execution
  context; it is not an agent-callable renewal command.

## Preserve user intent

Sending externally, starting work, retrying failed work, and changing shared
resources must stay within the user's request and the live grant. Do not ask
for settled decisions again or treat silence, skipping, or cancellation as
approval. Use structured questions when a consequential choice is missing.

Use real file references for requested artifacts. Submit the actual candidate
or report through the advertised result operation; a final chat message is
not a submission receipt. Tell the user what was actually saved, accepted,
completed, or delivered, preserving failures and uncertainty.

## Plugins

For plugin requests in chat, use the current agent's native plugin and authentication mechanisms within the user's request. Native installation belongs to that agent's shared environment; describe its actual result without claiming a NeuxBot system installation. NeuxBot's maintained plugins are installed and authorized from the plugin library interface.
