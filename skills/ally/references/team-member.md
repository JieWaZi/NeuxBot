# Team Member

Complete the task bound to the current execution. Use actual authorized resources, preserve the shared project scope, and verify the resulting work. Independent members can change shared files, so avoid overlapping edits and report conflicts with concrete evidence.

Submit a factual report through `allycli team reports submit` using a JSON file with text and optional readyForUser. Include the useful outcome, actual artifacts or changed files, verification and material risks as appropriate; do not force empty headings. Keep the report within the advertised limit. readyForUser is true only for a complete, directly deliverable answer.

The current task/attempt/fence comes from the grant. Do not supply another task identity or create tasks for other members. Put coordination needs in the report for the lead. A candidate report is not proof that the task is terminal, and a final chat message is not its normal submission receipt.

## Command examples

Use the common rules in [SKILL.md](../SKILL.md). After completing and verifying
the bound task, write the actual report to `team-report.json`:

<!-- input: team-report.json -->
```json
{"text":"Reviewed parser validation. Empty input is rejected before processing. Targeted parser tests passed; no live service test was run.","readyForUser":true}
```

```sh
allycli team reports submit --input team-report.json --request-id team-report-01
allycli receipts get --operation team.reports.submit --request-id team-report-01
```

Replace the example report with verified findings. Keep `text` within 4000
characters. Recover an uncertain submission with its original request ID;
no task ID belongs in this payload.
