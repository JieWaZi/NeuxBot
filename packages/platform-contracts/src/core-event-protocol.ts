import { z } from "zod";

export const globalEventTopics = [
  "notifications",
  "work-board",
  "work-results",
  "entities",
  "automatic-tasks",
  "im",
  "runtimes",
  "system",
  "chat",
  "team",
  "workflow",
  "account-login",
  "plugins",
  "resources",
  "knowledge",
  "workspace",
] as const;
export type GlobalEventTopic = (typeof globalEventTopics)[number];
export type GlobalEventState =
  "connecting" | "open" | "reconnecting" | "unavailable" | "closed";
const identity = z
  .string()
  .min(1)
  .max(200)
  .regex(/^[\w:./-]+$/);
export const globalEventResourceSchema = z
  .object({
    kind: identity,
    id: identity.optional(),
    parentId: identity.optional(),
  })
  .strict();
export type GlobalEventResource = z.infer<typeof globalEventResourceSchema>;
export const globalEventSubscriptionSchema = z
  .object({
    subscriptionId: z.string().uuid(),
    topic: z.enum(globalEventTopics),
    resource: globalEventResourceSchema,
    after: z
      .union([z.number().int().nonnegative().safe(), z.string().max(512)])
      .optional(),
  })
  .strict()
  .refine(({ topic, resource }) => validGlobalEventResource(topic, resource), {
    message: "该主题未声明此资源类型",
  });
export const globalEventFrameSchema = z.object({
  protocolVersion: z.literal(1),
  epoch: z.string().min(1).max(64),
  connectionId: z.string().min(1).max(64),
  deliverySequence: z.number().int().nonnegative().safe(),
  topic: z.enum(globalEventTopics),
  type: z.string().min(1).max(100),
  resource: globalEventResourceSchema,
  eventId: z.string().max(512),
  sourceCursor: z
    .union([z.number().int().nonnegative().safe(), z.string().max(512)])
    .optional(),
  resourceVersion: z.number().int().nonnegative().safe().optional(),
  transient: z.boolean().optional(),
  payload: z.unknown(),
});
export type GlobalEventFrame = z.infer<typeof globalEventFrameSchema>;
export type GlobalEventSubscription = z.infer<
  typeof globalEventSubscriptionSchema
>;
export type GlobalEventDelivery =
  | { subscriptionId: string; kind: "event"; frame: GlobalEventFrame }
  | {
      subscriptionId: string;
      kind: "state";
      state: GlobalEventState;
      connectionChanged?: boolean;
    }
  | { subscriptionId: string; kind: "resync"; reason: string };
export type GlobalEventObserver = {
  after?: number | string;
  onEvent: (frame: GlobalEventFrame) => void;
  onState?: (state: GlobalEventState, connectionChanged?: boolean) => void;
  onResync?: (reason: string) => void | Promise<void>;
};
export function globalEventResourceKey(
  topic: GlobalEventTopic,
  resource: GlobalEventResource,
): string {
  return JSON.stringify([
    topic,
    resource.kind,
    resource.id ?? "",
    resource.parentId ?? "",
  ]);
}

/** 与 Core 同步的编译期资源白名单，归属仍由服务端原 Controller 验证。 */
function validGlobalEventResource(
  topic: GlobalEventTopic,
  resource: GlobalEventResource,
): boolean {
  if (resource.kind === "global")
    return (
      !resource.id &&
      !resource.parentId &&
      !["chat", "team", "workflow", "account-login", "workspace"].includes(
        topic,
      )
    );
  if (!resource.id) return false;
  switch (topic) {
    case "workspace":
      return resource.kind === "workspace_watch" && !resource.parentId;
    case "knowledge":
      return (
        resource.kind === "knowledge_library" && Boolean(resource.parentId)
      );
    case "resources":
      return (
        ["skill", "connector", "waker"].includes(resource.kind) &&
        !resource.parentId
      );
    case "plugins":
      return resource.kind === "plugin" && !resource.parentId;
    case "chat":
      return resource.kind === "chat" && !resource.parentId;
    case "team":
      return resource.kind === "thread" && Boolean(resource.parentId);
    case "workflow":
      return resource.kind === "workflow";
    case "account-login":
      return resource.kind === "account" && Boolean(resource.parentId);
    case "work-board":
    case "work-results":
      return (
        ["chat", "team_thread", "workflow_run"].includes(resource.kind) &&
        !resource.parentId
      );
    case "entities":
      return ["waker", "channel"].includes(resource.kind) && !resource.parentId;
    case "automatic-tasks":
      return (
        ["automatic_task", "waker", "channel"].includes(resource.kind) &&
        !resource.parentId
      );
    case "im":
      return (
        ["connection", "binding", "conversation", "login"].includes(
          resource.kind,
        ) && !resource.parentId
      );
    case "runtimes":
      return resource.kind === "runtime" && !resource.parentId;
    default:
      return false;
  }
}
