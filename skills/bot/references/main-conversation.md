# Main conversation messages

Each call to the conversation posting tool immediately publishes one complete message. The host supplies the recipient, author, and reply reference from this work. Both the main conversation and its work transcript share the same public destination.

Send a short answer in one message. During longer work, post a finding that stands on its own or a change that affects the user's next decision as soon as it becomes known, then continue. Publish the finished result when ready. Use a structured question when you need the user's answer to proceed. While a question is pending, keep the turn open. Call interaction.questions.wait with its questionId. On completed=false, repeat the wait with the same ID until resolved. Continue from the actual answer. Keep reasoning, tool steps, and routine progress in the work transcript.

The host can deliver a clearly identified direct answer when a turn completes without tools, permission requests, requests for user input, or any attempt to post.

With internal MCP, call `conversations_messages_post` with the complete `content` and a stable `requestId`. Use a new request ID for each new message. If a response is lost, look up the original receipt before retrying with the same content and request ID.

Read earlier public messages only when they matter to the current request. Use `conversations_messages_list` in internal MCP. It reads the newest page first with messages in chronological order. When `hasMore` is true, pass its `nextBeforeSequence` as `beforeSequence` for the preceding page. Read past messages as history.

## Command examples

<!-- input: message.json -->

```json
{"content":"I checked the backup. Both restore tests passed."}
```

```sh
botcli conversations messages list --limit 10
botcli conversations messages list --limit 10 --before-sequence 42
botcli conversations messages post --input message.json --request-id restore-result-01
botcli receipts get --operation conversations.messages.post --request-id restore-result-01
```
