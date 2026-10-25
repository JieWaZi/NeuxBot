import type { HostService } from "@neuxbot/platform-contracts/services";
import { platform } from "@neuxbot/frontend/platform";
import { selectCoreDirectory } from "../shell/directory-picker";
import { webText } from "../i18n";

/** 浏览器只处理用户选择与下载，资源授权与执行留给 Core。 */
export const webHost: HostService = {
  async discard_attachment({ sourcePath }) {
    await discardUpload(sourcePath);
  },
  async save_file({ bytes, fileName }) {
    const url = URL.createObjectURL(new Blob([new Uint8Array(bytes)]));
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = fileName;
    anchor.click();
    window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
    return true;
  },
  async select_directory() {
    return selectCoreDirectory();
  },
  async select_files() {
    const files = await new Promise<File[]>((resolve) => {
      const input = document.createElement("input");
      input.type = "file";
      input.multiple = true;
      input.addEventListener(
        "change",
        () => resolve(Array.from(input.files ?? [])),
        { once: true },
      );
      input.addEventListener("cancel", () => resolve([]), { once: true });
      input.click();
    });
    const refs: string[] = [];
    try {
      for (const file of files) {
        if (file.size > 64 * 1024 * 1024)
          throw new Error(webText("uploadTooLarge"));
        refs.push(
          await stageFile(file.name, new Uint8Array(await file.arrayBuffer())),
        );
      }
      return refs;
    } catch (error) {
      await Promise.allSettled(refs.map(discardUpload));
      throw error;
    }
  },
  async stage_attachment({ name, bytes }) {
    return stageFile(name, bytes);
  },
  async open_external_url({ url }) {
    const target = new URL(url);
    if (target.protocol !== "https:" && target.protocol !== "http:")
      throw new Error(webText("unsafeLink"));
    window.open(target.href, "_blank", "noopener,noreferrer");
  },
  async show_notification() {
    return false;
  },
  onFileDrop() {
    return () => undefined;
  },
  onNotificationAction() {
    return () => undefined;
  },
};

async function stageFile(name: string, bytes: Uint8Array): Promise<string> {
  const connection = await platform().getConnection();
  const response = await fetch(
    `/api/v1/uploads?${new URLSearchParams({ name })}`,
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/octet-stream",
        "X-Ally-CSRF": connection.csrfToken ?? "",
      },
      body: new Uint8Array(bytes),
    },
  );
  if (response.status === 401) {
    window.dispatchEvent(new Event("ally:web-session-expired"));
    throw new Error(webText("expired"));
  }
  if (!response.ok) throw new Error(webText("uploadFailed"));
  const result: unknown = await response.json();
  if (
    !result ||
    typeof result !== "object" ||
    !("sourcePath" in result) ||
    typeof result.sourcePath !== "string"
  )
    throw new Error(webText("uploadFailed"));
  return result.sourcePath;
}

export async function discardUpload(sourcePath: string): Promise<void> {
  if (!sourcePath.startsWith("upload:")) return;
  const connection = await platform().getConnection();
  await fetch(`/api/v1/uploads?${new URLSearchParams({ sourcePath })}`, {
    method: "DELETE",
    credentials: "same-origin",
    headers: { "X-Ally-CSRF": connection.csrfToken ?? "" },
  });
}
