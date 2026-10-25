import { z } from "zod";
import { globalEventSubscriptionSchema } from "./core-event-protocol";

import type { DesktopCommands } from "./contracts";

type CommandSchemaMap = {
  [K in keyof DesktopCommands]: z.ZodType<DesktopCommands[K]["args"]>;
};

const text = z.string();
const root = z.string().min(1);
const bounds = {
  x: z.number().finite(),
  y: z.number().finite(),
  width: z.number().positive().finite(),
  height: z.number().positive().finite(),
};
const file = { root, relativePath: text };
export const commandSchemas = {
  core_connection: z.undefined(),
  application_update_state: z.undefined(),
  check_application_update: z.undefined(),
  download_application_update: z.undefined(),
  open_application_installer: z.undefined(),
  subscribe_core_events: globalEventSubscriptionSchema,
  resync_core_events: z
    .object({
      subscriptionId: z.string().uuid(),
      after: z
        .union([z.number().int().nonnegative().safe(), z.string().max(512)])
        .optional(),
    })
    .strict(),
  unsubscribe_core_events: z
    .object({ subscriptionId: z.string().uuid() })
    .strict(),
  select_directory: z.undefined(),
  select_files: z.undefined(),
  stage_attachment: z.object({
    name: text
      .min(1)
      .refine((name) => name !== "." && name !== ".." && !/[\\/\0]/.test(name)),
    bytes: z.instanceof(Uint8Array),
  }),
  save_file: z.object({
    bytes: z.array(z.number().int().min(0).max(255)),
    fileName: text,
  }),
  open_workspace_file: z.object(file),
  browser_create: z.object({
    ...bounds,
    url: text,
    localRoot: text.optional(),
    chatId: text.optional(),
  }),
  browser_prepare_webview: z.object({
    url: text,
    localRoot: text.optional(),
    chatId: text.optional(),
  }),
  browser_navigate: z.object({ viewId: root, url: text }),
  browser_control: z.object({
    viewId: root,
    action: z.enum(["back", "forward", "reload"]),
  }),
  browser_set_bounds: z.object({
    viewId: root,
    ...bounds,
  }),
  browser_set_visible: z.object({ viewId: root, visible: z.boolean() }),
  browser_close: z.object({ viewId: root }),
  open_external_url: z.object({ url: text }),
  show_notification: z.object({ id: root, title: text, body: text }),
} satisfies CommandSchemaMap;
