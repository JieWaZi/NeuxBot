# Workflow SDK: im

When the user requests delivery to WeChat or Feishu, discover approved chats with workflow.resources.list with kind:imTargets. Discover the native IM operation through workflow.resources.list with kind:builtinActions, tool im_delivery, through a builtin action declaring its connector ID and awaiting ctx.callTool({callId, connectorId, toolName: 'im_delivery', input: {action: 'deliveries.create', targetId, content, files}}). Fixed literal targetIds are derived as workflow resource dependencies. Dynamic targetIds must declare the approved imTargetIds on the builtin action; runtime delivery cannot use an undeclared target. Admission checks these targets before any node runs. This durable action waits for delivery acceptance; query deliveries.get to verify sent, failed or unknown status. Never invent recipients or connector IDs. Resource IDs come from workflow.resources.list; never invent or silently replace them. Do not send messages or publish outside an explicitly authorized action.

```typescript
type Json = null | boolean | number | string | Json[] | { [key: string]: Json };
type Schema = { [key: string]: Json };
interface VisualNode { id: string; label: string; description?: string; phase: string; inputs?: string[]; outputs?: string[]; successCondition?:{path:string;equals:Json} }
```
