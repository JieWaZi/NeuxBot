import { z } from "zod";
import { notificationPreferencesSchema } from "@neuxbot/platform-contracts/workspace-attention";
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
  get_window_appearance: z.undefined(),
  preview_desktop_storage: z.undefined(),
  clear_desktop_storage: z
    .object({ category: z.enum(["desktop-cache", "desktop-logs"]) })
    .strict(),
  set_window_appearance: z
    .object({
      theme: z.enum(["system", "light", "dark"]),
      language: z.enum(["zh-CN", "en-US"]),
      transparent: z.boolean(),
      scale: z.number().min(0.8).max(1.5).finite(),
    })
    .strict(),
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
  get_notification_preferences: z.undefined(),
  set_notification_preferences: notificationPreferencesSchema,
  send_test_notification: z.undefined(),
  open_notification_settings: z.undefined(),
} satisfies CommandSchemaMap;
