import type { HostService } from "@neuxbot/platform-contracts/services";
import { uploadFileBytesLimit } from "@neuxbot/platform-contracts/upload-byte-limits";
import { platform } from "@neuxbot/frontend/platform";
import { createCoreUploads } from "@neuxbot/frontend/core-uploads";
import { selectCoreDirectory } from "../shell/directory-picker";
import { webText } from "../i18n";

const uploads = createCoreUploads(() => platform().getConnection());

/** 浏览器只处理用户选择与下载，资源授权与执行留给 Core。 */
export const webHost: HostService = {
  ...uploads,
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
        if (file.size > uploadFileBytesLimit)
          throw new Error(webText("uploadTooLarge"));
        refs.push(
          await uploads.stage_attachment({
            name: file.name,
            bytes: new Uint8Array(await file.arrayBuffer()),
          }),
        );
      }
      return refs;
    } catch (error) {
      await Promise.allSettled(
        refs.map((sourcePath) => uploads.discard_attachment({ sourcePath })),
      );
      throw error;
    }
  },
  async open_external_url({ url }) {
    const target = new URL(url);
    if (target.protocol !== "https:" && target.protocol !== "http:")
      throw new Error(webText("unsafeLink"));
    window.open(target.href, "_blank", "noopener,noreferrer");
  },
  onFileDrop() {
    return () => undefined;
  },
};
