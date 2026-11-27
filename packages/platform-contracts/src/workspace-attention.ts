import { z } from "zod";

export const notificationPreferencesSchema = z
  .object({
    enabled: z.boolean().default(true),
    mode: z.enum(["all", "needs-me", "off"]),
    preview: z.boolean(),
  })
  .strict();
export type NotificationPreferences = z.infer<
  typeof notificationPreferencesSchema
>;

const id = z.string().regex(/^[a-f0-9]{32}$/);
const optionalId = id.optional();
const messageOrRequestId = z.string().min(1).max(128).optional();
const workRef = z.object({
  kind: z.enum([
    "chat",
    "main_conversation",
    "team_thread",
    "workflow_run",
    "automatic_run",
  ]),
  id,
});
export const workspaceAttentionLiveSchema = z
  .object({
    operation: z.enum([
      "published",
      "completed",
      "failed",
      "request_opened",
      "request_closed",
    ]),
    work: workRef,
    chatId: optionalId,
    messageId: messageOrRequestId,
    requestId: messageOrRequestId,
    requestFence: z.number().int().nonnegative().safe().optional(),
    requestKind: z.enum(["approval", "input", "review"]).optional(),
    executionId: optionalId,
  })
  .refine((event) =>
    event.operation === "published"
      ? event.work.kind === "main_conversation" &&
        Boolean(event.messageId) &&
        !event.chatId
      : event.work.kind !== "main_conversation",
  );
export type WorkspaceAttentionLive = z.infer<
  typeof workspaceAttentionLiveSchema
>;
export const workspaceAttentionTargetSchema = z.object({
  valid: z.boolean(),
  unreadGeneration: z.number().int().nonnegative().safe().optional(),
  work: workRef.optional(),
  title: z.string().max(512).optional(),
  preview: z.string().max(1024).optional(),
  chatId: optionalId,
  wakerId: optionalId,
  channelId: optionalId,
  threadId: optionalId,
  definitionId: optionalId,
  runId: optionalId,
  taskId: optionalId,
  instanceId: optionalId,
  automaticTaskId: optionalId,
});
export type WorkspaceAttentionTarget = z.infer<
  typeof workspaceAttentionTargetSchema
>;
export type WorkspaceAttentionOpen =
  | (Omit<WorkspaceAttentionLive, "operation"> & {
      operation: Exclude<WorkspaceAttentionLive["operation"], "request_closed">;
    } & Omit<WorkspaceAttentionTarget, "valid" | "work" | "title" | "preview">)
  | null;
