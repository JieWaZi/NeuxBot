import {
  app,
  Notification,
  shell,
  type NotificationConstructorOptions,
} from "electron";
import { release } from "node:os";

export function createDesktopNotification(
  content: NotificationConstructorOptions,
) {
  return Notification.isSupported() ? new Notification(content) : undefined;
}

export async function sendTestNotification() {
  const notification = createDesktopNotification({
    title: "NeuxBot",
    body: app.getLocale().startsWith("zh")
      ? "这是一条测试通知。"
      : "This is a test notification.",
  });
  if (!notification) throw new Error("Desktop notifications are unavailable");

  await new Promise<void>((resolve, reject) => {
    const shown = () => finish(resolve);
    const failed = () =>
      finish(() =>
        reject(new Error("Test notification could not be delivered")),
      );
    const timeout = setTimeout(failed, 5000);
    const finish = (settle: () => void) => {
      clearTimeout(timeout);
      notification.removeListener("show", shown);
      notification.removeListener("failed", failed);
      settle();
    };
    notification.once("show", shown);
    notification.once("failed", failed);
    try {
      notification.show();
    } catch (error) {
      finish(() => reject(error));
    }
  });
}

export async function openNotificationSettings() {
  if (process.platform === "darwin") {
    // Monterey uses System Preferences; Ventura and later use System Settings.
    const pane =
      Number.parseInt(release(), 10) < 22
        ? "com.apple.preference.notifications"
        : "com.apple.Notifications-Settings.extension";
    await shell.openExternal(`x-apple.systempreferences:${pane}`);
  } else if (process.platform === "win32") {
    await shell.openExternal("ms-settings:notifications");
  } else {
    throw new Error("Notification settings are unavailable on this platform");
  }
}
