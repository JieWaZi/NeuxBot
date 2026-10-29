# Choose the actual deliverable

Use this reference when a worker creates files or the user changes how its result should be read. Select the `files` feature in `workflow authoring prepare`; add `presentation` only for a composed in-app page. The tool includes the needed contracts. These examples illustrate delivery choices, not a fixed business template.

## One report file

```javascript
export const meta = {
  // Other agreed workflow declarations...
  nodeResultFiles: {
    write_report: { title: "Defect HTML report" },
  },
};
const report = await worker(reportInstructions, {
  id: "write_report",
  label: "Generate HTML report",
  phase: "report",
  resolve: agreedPartner,
  schema: { type: "string", format: "file", title: "HTML report" },
});
// report is the actual submitted path; an authorized delivery action consumes it.
```

The prompt asks the partner to write the requested document at a run-specific path in `flowContext.scope.workingDirectory`, verify the file and call `allycli workflow results submit --input result.json --request-id <stable-id>` with `data` equal to that path. The report's prose, evidence, links, layout and coverage live in the HTML. No `overview`, `attention` or `coverage_note` fields are required merely to support the viewer. If sending an attachment only needs its path, avoid making the agent reproduce the report as JSON; prepare any fixed delivery text in deterministic logic.

For a root file schema, the final workflow `outputSchema` can also be a file, or include the file plus actual delivery receipts when those are needed. Keep delivery verification distinct from file generation.

## Several deliverable files

Use an array with `items: { type: 'string', format: 'file' }` when the output is a set of files. Use an object only when downstream consumers need stable named file roles, such as `report` and `spreadsheet`, or other necessary routing data. Mark every delivered file explicitly; ordinary path strings and undeclared temporary files are not viewer entries. `nodeResultFiles` uses the schema-declared file inventory and requires no page Spec.

## Generated images and mixed files

A single image uses the same root file schema; a set uses file items. Let the partner create the requested images and submit the files themselves. The existing viewer handles images and mixed file sets; no JSON descriptions or extra page are needed.

## Structured content with supplementary files

Choose `nodeResultPages` when the user primarily reads composed structured content in NeuxBot. `FileList` may supplement it with agreed deliverables. Do not regenerate a second page containing the same report merely because a file has a path. Internal response caches, credentials and raw snapshots stay outside the delivery contract unless the user explicitly requested that data.

## Command examples

Use the common rules in [SKILL.md](../SKILL.md). In an authoring context, prepare
the file/presentation contract only for the requested delivery:

```sh
allycli workflow authoring prepare --primitives worker --features files,presentation
```

Inside the actual file-producing worker, read its result schema:

```sh
allycli workflow results schema
```

For the root file schema illustrated above, create the actual report and write
`file-result.json` with its verified path:

<!-- input: file-result.json -->
```json
{"data":"/workspace/output/run-example/report.html"}
```

```sh
allycli workflow results submit --input file-result.json --request-id file-result-01
```

These calls belong to different execution roles; use the role actually granted.
For a file array/object, match the discovered schema. Requested IM delivery
uses the complete create/get/wait sequence in [IM delivery](im-delivery.md#command-examples).
