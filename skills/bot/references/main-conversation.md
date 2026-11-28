# Main conversation messages

Each call to the conversation posting tool immediately publishes one complete message. The host supplies the recipient, author, and reply reference from this work. Both the main conversation and its work transcript share the same public destination.

Send a short answer in one message. For longer work, tell the user the first step before starting slow work. Do not post a second acknowledgment if the Router already provided one. Publish each verified and independently useful finding as soon as it is ready, before collecting the next one. For five news items, post the title, significance and source link for each item in a separate message. Do not wait to combine all five or repeat the list at settlement. Use a structured question when you need the user's answer to proceed. While a question is pending, keep the turn open. Call interaction.questions.wait with its questionId. On completed=false, repeat the wait with the same ID until resolved. Continue from the actual answer. Keep reasoning, tool steps, and routine progress in the work transcript.

The host can recover a verified final answer at turn completion. Use the posting tool for messages the user should receive during the turn.

With internal MCP, call `conversations_messages_post` with the complete `content` and a stable `requestId`. When quoting would help the reader, set `quoteReply` on the post that explicitly answers the host-bound input. Use a new request ID for each new message. If a response is lost, look up the original receipt before retrying with the same request ID and payload.

Read earlier public messages only when they matter to the current request. Use `conversations_messages_list` in internal MCP. It reads the newest page first with messages in chronological order. When `hasMore` is true, pass its `nextBeforeSequence` as `beforeSequence` for the preceding page. Read past messages as history.

When both tools are available, use the posting tool for acknowledgments and independent findings and use `work.tickets.report` for ticket status. Submit `completed` once with the new conclusion or the exact text of an already posted conclusion. The host can link that report to the existing message. When only reporting is available, include the complete result and its evidence in the completion report. Retry with the original request ID and content. The equivalent internal MCP tool is `work_tickets_report`.

Use `work.tickets.delegate` when a requested local partner or team can handle a piece of the work. Choose a known subject ID and send a complete task with relevant text context. A delegated worker reports to its direct requester. If required results are pending, end the turn. The host resumes you when a direct child sends a question you can answer or the required children have reported. Use `work.tickets.results` with a report ID to read the full report, and `work.tickets.reply` with the waiting report ID to answer. The equivalent internal MCP tools are `work_tickets_delegate`, `work_tickets_results`, and `work_tickets_reply`.

## Command examples

<!-- input: message.json -->

```json
{"content":"Both restore tests passed.","quoteReply":true}
```

```sh
botcli conversations messages list --limit 10
botcli conversations messages list --limit 10 --before-sequence 42
botcli conversations messages post --input message.json --request-id restore-result-01
botcli receipts get --operation conversations.messages.post --request-id restore-result-01
botcli work tickets report --kind completed --content "Both restore tests passed" --request-id ticket-restore-result-01
botcli receipts get --operation work.tickets.report --request-id ticket-restore-result-01
```
