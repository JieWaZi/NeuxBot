import { z } from "zod";

const ids = z
  .array(
    z
      .string()
      .trim()
      .min(1)
      .max(200)
      .refine((value) => value !== "*" && !/[\r\n\0]/.test(value)),
  )
  .max(1000);
export const knowledgeCapabilities = [
  "retrieve",
  "ingest",
  "organize",
] as const;
export const taskCapabilities = [
  "automatic_task.invoke",
  "automatic_task.results",
] as const;

/** 授权按能力与明确资源组合保存；空范围不会转换为全部资源。 */
export const apiKeyFormSchema = z
  .object({
    name: z.string().trim().min(1).max(120),
    expiresAt: z
      .string()
      .refine(
        (value) =>
          !value ||
          (Number.isFinite(Date.parse(value)) &&
            Date.parse(value) > Date.now()),
      ),
    knowledgeIDs: ids,
    knowledgeCapabilities: z.array(z.enum(knowledgeCapabilities)),
    taskIDs: ids,
    taskCapabilities: z.array(z.enum(taskCapabilities)),
  })
  .superRefine((value, ctx) => {
    for (const [resourceIDs, capabilities, path] of [
      [value.knowledgeIDs, value.knowledgeCapabilities, "knowledgeIDs"],
      [value.taskIDs, value.taskCapabilities, "taskIDs"],
    ] as const) {
      if (Boolean(resourceIDs.length) !== Boolean(capabilities.length))
        ctx.addIssue({ code: "custom", path: [path], message: "emptyGroup" });
    }
    if (!value.knowledgeIDs.length && !value.taskIDs.length)
      ctx.addIssue({
        code: "custom",
        path: ["knowledgeIDs"],
        message: "emptyGroup",
      });
    if (
      value.knowledgeCapabilities.length &&
      !value.knowledgeCapabilities.includes("retrieve")
    )
      ctx.addIssue({
        code: "custom",
        path: ["knowledgeCapabilities"],
        message: "readRequired",
      });
  });

export type APIKeyForm = z.infer<typeof apiKeyFormSchema>;

export function toAPIKeyGrants(value: APIKeyForm) {
  return [
    ...(value.knowledgeIDs.length
      ? [
          {
            resourceKind: "knowledge" as const,
            resourceIds: [...new Set(value.knowledgeIDs)],
            capabilities: [...new Set(value.knowledgeCapabilities)],
          },
        ]
      : []),
    ...(value.taskIDs.length
      ? [
          {
            resourceKind: "automatic_task" as const,
            resourceIds: [...new Set(value.taskIDs)],
            capabilities: [...new Set(value.taskCapabilities)],
          },
        ]
      : []),
  ];
}
