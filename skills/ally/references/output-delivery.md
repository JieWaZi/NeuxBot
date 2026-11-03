# Business files and return values

Use the `files` authoring feature when a business step actually writes a file or an authorized delivery action needs its path. Files are workflow data; there is no separate result-page or viewer declaration.

Ask the partner to write the requested file in `flowContext.workspace.runDirectory` by default and verify it exists. Use an external `flowContext.scope.workingDirectory` only for the business destination explicitly selected by the user. Auxiliary scripts, configuration, caches and temporary files always stay in their advertised workspace directories, even when final files are delivered elsewhere. A structured worker may declare `{ type: 'string', format: 'file' }` and submit the real path through `workflow.results.submit`. Use an array of file values when a downstream consumer needs several files; use an object only when the business requires named roles or additional data.

Keep input and output schemas, downstream consumers and the final return consistent. Do not invent summary fields, render a duplicate report, or add formatting nodes just for a product viewer. File creation and external delivery are separate effects: an existing path does not prove it was sent. Only use authorized delivery targets, preserve delivery receipts, and keep credentials and internal caches outside business outputs.
