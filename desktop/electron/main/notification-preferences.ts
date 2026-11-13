import { readFile, rename, rm, writeFile } from "node:fs/promises";
import {
  notificationPreferencesSchema,
  type NotificationPreferences,
} from "@neuxbot/platform-contracts/workspace-attention";

/** 仅持有系统提醒偏好，业务关注状态由 Core 查询。 */
export async function loadNotificationPreferences(file: string) {
  const initial = await readFile(file, "utf8")
    .then((text) => notificationPreferencesSchema.parse(JSON.parse(text)))
    .catch((): NotificationPreferences => ({
      enabled: true,
      mode: "all",
      preview: false,
    }));
  let current = initial;
  let queue: Promise<unknown> = Promise.resolve();

  return {
    get: () => current,
    set(next: NotificationPreferences) {
      const saved = queue
        .catch(() => undefined)
        .then(async () => {
          const temporary = `${file}.tmp`;
          try {
            await writeFile(temporary, JSON.stringify(next), { mode: 0o600 });
            await rename(temporary, file);
          } catch (error) {
            await rm(temporary, { force: true }).catch(() => undefined);
            throw error;
          }
          current = next;
          return next;
        });
      queue = saved;
      return saved;
    },
  };
}
