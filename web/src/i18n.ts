import { i18n } from "@/shared/i18n";
import { useSyncExternalStore } from "react";

const messages = {
  directoryTitle: "选择本机工作目录",
  directoryDescription:
    "输入运行 Core 的这台电脑上的绝对路径。所选目录将用于伙伴工作与文件操作。",
  directoryPath: "目录绝对路径",
  directoryInvalid: "请输入以 / 或 Windows 盘符开头的绝对路径。",
  cancel: "取消",
  confirm: "使用此目录",
  uploadTooLarge: "单个文件不能超过 64 MiB。",
  uploadFailed: "文件上传失败，请重试。",
  title: "连接本机工作台",
  description: "浏览器直接连接这台电脑上的伙伴与工作。",
  connecting: "正在连接",
  retry: "重试连接",
  expired: "本机会话已到期，正在重新连接。",
  limited: "本机会话数量已达上限，请重启本机服务后重试。",
  offline: "无法连接本机服务。请确认启动终端中的服务仍在运行。",
  denied: "当前网页来源未获授权，请使用启动终端显示的地址。",
  incompatible: "网页与本机服务版本不匹配，请使用同一版本的发布包。",
  unavailable: "当前 Web 版本尚未支持此能力。",
  unsafeLink: "只能打开 HTTP 或 HTTPS 网页。",
} as const;
i18n.addResourceBundle("zh-CN", "web", messages, true, true);
export const englishMessages = {
  directoryTitle: "Choose a local working directory",
  directoryDescription:
    "Enter an absolute path on the computer running Core. The directory will be used for partner work and file operations.",
  directoryPath: "Absolute directory path",
  directoryInvalid:
    "Enter an absolute path starting with / or a Windows drive letter.",
  cancel: "Cancel",
  confirm: "Use this directory",
  uploadTooLarge: "Individual files cannot exceed 64 MiB.",
  uploadFailed: "Could not upload the file. Please retry.",
  title: "Connect to your local workspace",
  description:
    "Your browser connects directly to partners and work on this computer.",
  connecting: "Connecting",
  retry: "Retry connection",
  expired: "Your local session expired. Reconnecting.",
  limited:
    "The local session limit has been reached. Restart the local service and retry.",
  offline:
    "Could not connect to the local service. Make sure it is still running in the startup terminal.",
  denied:
    "This page's origin is not authorized. Use the URL shown in the startup terminal.",
  incompatible:
    "The web app and local service versions do not match. Use the same release for both.",
  unavailable: "This capability is not available in the web app yet.",
  unsafeLink: "Only HTTP and HTTPS links can be opened.",
} satisfies Record<keyof typeof messages, string>;
i18n.addResourceBundle("en-US", "web", englishMessages, true, true);
/** Web 专属文案归独立 namespace，业务页面沿用公共翻译。 */
export function webText(key: keyof typeof messages): string {
  return i18n.t(`web:${key}` as never);
}

function subscribeLanguage(listener: () => void) {
  i18n.on("languageChanged", listener);
  return () => {
    i18n.off("languageChanged", listener);
  };
}
export function useWebText() {
  useSyncExternalStore(subscribeLanguage, () => i18n.language);
  return webText;
}
