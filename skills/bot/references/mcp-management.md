# Manage NeuxBot MCP connectors

Use these operations when the user asks to configure MCP in NeuxBot. Read the saved connector and its revision before editing. Leave native provider settings unchanged. Configuration is stored by NeuxBot and applied on a subsequent turn. Treat a successful save and a successful live connection test as separate results.

After approval, create tests the connector, saves it in the library, and installs it for the current partner. A successful test enables it for the next turn. If the test fails, the saved connector stays disabled for editing. Report the test result and enabled state from the receipt. Update uses `expectedRevision` from the last read and preserves omitted fields. The host checks runtime-changing edits to enabled connectors before applying them. Use the provided transport, address, or command. Ask the user for any missing operational details and direct credential entry to the secure NeuxBot settings. Do not put secrets in the address, command, arguments, or chat. The host presents approval when needed. Use test to check the saved connector and report its actual status.

Detach removes the current partner's link to a shared connector. Delete removes the connector itself and requires host approval. Use a stable request ID for every change. If the outcome is uncertain, read the original receipt before trying again.

## Command examples

```sh
botcli mcp connectors list --query demo-mcp
botcli mcp connectors get --connector-id sample-connector-id
botcli mcp connectors create --input create-connector.json --request-id create-demo-mcp
botcli mcp connectors update --input update-connector.json --request-id update-demo-mcp
botcli mcp connectors test --connector-id sample-connector-id
botcli mcp connectors detach --connector-id sample-connector-id --expected-revision sample-revision --request-id detach-demo-mcp
botcli mcp connectors delete --connector-id sample-connector-id --expected-revision sample-revision --request-id delete-demo-mcp
```

<!-- input: create-connector.json -->
```json
{
  "name": "demo-mcp",
  "transport": "streamable_http",
  "url": "https://example.com/mcp"
}
```

<!-- input: update-connector.json -->
```json
{
  "connectorId": "sample-connector-id",
  "expectedRevision": "sample-revision",
  "description": "Use this connector for release-note lookup."
}
```
