import { z } from "zod";
import type { GlobalEventResource } from "@/shared/api/global-events";

const count = z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER);
const timestamp = count.max(8_640_000_000_000_000);
const optionalText = z.string().optional();
const optionalCount = count.optional();
const notificationKind = z.enum([
  "approval",
  "input",
  "review",
  "result",
  "automatic_failure",
]);
const targetSchema = z.object({
  kind: notificationKind,
  chatId: optionalText,
  wakerId: optionalText,
  channelId: optionalText,
  threadId: optionalText,
  taskId: optionalText,
  fence: optionalCount,
  definitionId: optionalText,
  runId: optionalText,
  instanceId: optionalText,
  requestId: optionalText,
  ownerKind: z.enum(["chat", "team_thread", "workflow_run"]).optional(),
  ownerId: optionalText,
  resultVersion: optionalCount,
  automaticRunId: optionalText,
  messageId: optionalText,
  reference: z
    .object({
      chatId: optionalText,
      executionId: optionalText,
      messageId: optionalText,
      workflowRunId: optionalText,
    })
    .optional(),
});
export const notificationItemSchema = z.object({
  id: z.string().min(1).max(200),
  kind: notificationKind,
  bucket: z.enum(["action", "unread"]),
  source: z.enum(["chat", "team", "workflow", "automatic", "im"]),
  title: z.string().refine((value) => Array.from(value).length <= 200),
  description: z.string().refine((value) => Array.from(value).length <= 1000),
  createdAt: timestamp,
  version: count.positive(),
  executor: z.object({ id: z.string(), kind: z.string(), name: z.string() }),
  target: targetSchema,
  dismissed: z.boolean(),
  failed: z.boolean().optional(),
});
export const notificationSnapshotSchema = z
  .object({
    epoch: z.string().regex(/^[a-f0-9]{32}$/),
    revision: count,
    synced: z.boolean(),
    actionCount: count,
    unreadResultCount: count,
    unreadFailureCount: count,
    totalCount: count,
    actionPreview: z.array(notificationItemSchema).max(5),
    unreadPreview: z.array(notificationItemSchema).max(5),
    generatedAt: timestamp,
  })
  .refine(
    (value) =>
      value.totalCount ===
      value.actionCount + value.unreadResultCount + value.unreadFailureCount,
  );
export const notificationListSchema = z.object({
  resources: z.array(notificationItemSchema),
  totalSize: count,
  pageNum: count.positive(),
  pageSize: count.positive().max(100),
  hasMore: z.boolean(),
  nextCursor: optionalText,
  epoch: z.string(),
  revision: count,
});
const affectedResourceSchema: z.ZodType<GlobalEventResource> = z.object({
  kind: z.string().min(1).max(100),
  id: z.string().max(100).optional(),
  parentId: z.string().max(100).optional(),
});
export const notificationUpdateSchema = z.object({
  snapshot: notificationSnapshotSchema,
  upserts: z.array(notificationItemSchema),
  removals: z.array(z.string()),
  affectedResources: z.array(affectedResourceSchema).max(128),
});
export type NotificationItem = z.infer<typeof notificationItemSchema>;
export type NotificationSnapshot = z.infer<typeof notificationSnapshotSchema>;
export type NotificationList = z.infer<typeof notificationListSchema>;

/** 同一 Core 生命周期只接纳较新投影，防止晚返回的 REST 覆盖推送。 */
export function newerNotificationSnapshot(
  previous: NotificationSnapshot | undefined,
  next: NotificationSnapshot,
) {
  return !previous ||
    previous.epoch !== next.epoch ||
    next.revision > previous.revision
    ? next
    : previous;
}
