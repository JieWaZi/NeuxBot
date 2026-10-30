# IM Delivery

Use this guide when the user asks to send a message or artifact to an authorized chat, including an artifact created during this task.

Reuse a confirmed target ID. When discovery is needed, use `allycli im targets list`; names and external account identifiers are not interchangeable with target IDs. Resolve a real ambiguity with the user.

Create the requested artifact in the approved scope before sending it. Use a JSON input file containing targetId, content and files; files must exist and be readable. Do not put report bodies, secret values, or JSON strings into shell command text. Select a stable request ID before the first `im deliveries create` call.

Creation registers frozen content in the durable delivery queue. It does not prove that the platform accepted the message. Inspect `im deliveries get` or a bounded `im deliveries wait`. Only sent confirms platform acceptance; it does not prove that a human read it. A wait timeout leaves the original delivery running.

If the command response is lost, inspect `allycli receipts get --operation im.deliveries.create --request-id <original-id>`. Keep the original identity. A failed or unknown external effect must not be recreated under a new key. Report the actual state and actionable evidence; do not rerun the work merely to resend its result.

## Command examples

Use the common rules in [SKILL.md](../SKILL.md). Discover an authorized target,
create the actual report, then write `delivery.json` with its real path:

```sh
allycli im targets list --page-num 1 --page-size 20
```

<!-- input: delivery.json -->
```json
{"targetId":"target-example","content":"The requested report is attached.","files":["/workspace/output/report.html"]}
```

```sh
allycli im deliveries create --input delivery.json --request-id delivery-01
allycli im deliveries get --delivery-id delivery-example
allycli im deliveries wait --delivery-id delivery-example --timeout 30s
allycli receipts get --operation im.deliveries.create --request-id delivery-01
```

Use the returned delivery ID for get/wait. Use receipts only to recover the
original create outcome; `accepted` or a wait timeout does not mean `sent`.
