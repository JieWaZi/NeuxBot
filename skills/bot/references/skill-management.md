# Manage NeuxBot skills

Use these operations when the user asks to add, edit, uninstall, or delete a skill in NeuxBot. Find the intended skill by name and read its published version before editing. Resolve ambiguous matches with the user. Keep drafts in the workspace and submit the complete `SKILL.md` through the tool. Leave installed copies and the skill library files untouched.

Create publishes a skill and installs it for the current partner. The Markdown frontmatter must contain the same name and description passed to create. Revise publishes a new version of a revisable skill and preserves its other files. Supply the `versionId` you read as `expectedVersionId`. On conflict, read the latest version again before applying the change. The host asks for approval when a revision affects other partners.

Uninstall removes the skill from the current partner. Use delete only when the user requests removal from the library, including its versions, and wait for the host approval. A successful receipt proves the change. If the outcome is uncertain, look up the receipt with the original operation and request ID.

## Command examples

```sh
botcli skills packages list --query demo-release-note
botcli skills packages get --skill-id sample-skill-id
botcli skills packages create --input create-skill.json --request-id create-demo-skill
botcli skills packages revise --input revise-skill.json --request-id revise-demo-skill
botcli skills packages uninstall --skill-id sample-skill-id --expected-version-id sample-version-id --request-id uninstall-demo-skill
botcli skills packages delete --skill-id sample-skill-id --expected-version-id sample-version-id --request-id delete-demo-skill
```

<!-- input: create-skill.json -->
```json
{
  "name": "demo-release-note",
  "description": "Write a brief release note.",
  "content": "---\nname: demo-release-note\ndescription: Write a brief release note.\n---\n\n# Release note\n\nName the change and who it affects."
}
```

<!-- input: revise-skill.json -->
```json
{
  "skillId": "sample-skill-id",
  "expectedVersionId": "sample-version-id",
  "content": "---\nname: demo-release-note\ndescription: Write a brief release note.\n---\n\n# Release note\n\nName the change, who it affects, and how to verify it."
}
```
