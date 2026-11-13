# Partner profile optimization

When the user asks to change their partner's durable responsibilities, collaboration preferences, or working methods, read the current profile with `waker profile get`. Use the returned revision and current message to decide what to change. Ordinary work requests use the partner's current instructions.

Save the requested changes together. Include the revised overview fields when the request changes what they represent. Omit fields the user wants to keep. An empty overview array clears that field. Keep existing details grounded in the user's instructions, including exact names and file paths.

- `expertise`: cards in the user's language, each with a short `title` and a concrete `description`.
- `workStyle`: short, distinct preference labels in the user's language.
- `workMethods`: named workflows in the user's language, each with a `name` and ordered `steps`.
- `content`: the complete revised `AGENTS.md` body when instructions change. Use concise English and retain original identifiers and paths. Keep within 1,300 cl100k_base tokens. If the save rejects the length, shorten the body and retry.

Use the current execution context supplied by Core. Report the update after a successful receipt. Recover an uncertain result with the same request ID. If the profile changed, read it again and apply the request to the latest revision.

```sh
allycli waker profile get
```

Use the returned `data.revision` in the payload below. The example updates the body and one overview field.

<!-- input: profile-revision.json -->
```json
{
  "revision": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "workStyle": ["Conclusion first"],
  "content": "# Review partner\n\nVerify claims against primary sources. Lead with the verdict, followed by the evidence and open questions.\n"
}
```

```sh
allycli waker profile optimize --input profile-revision.json --request-id update-profile
```
