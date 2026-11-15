import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

/** 将没有本地路径的附件暂存到当前窗口独占的临时目录。 */
export function createAttachmentStaging(temporaryDirectory = tmpdir()) {
  let directory: Promise<string> | undefined;
  let disposed = false;
  let disposal: Promise<void> | undefined;
  const pending = new Set<Promise<string>>();

  return {
    async stage(name: string, bytes: Uint8Array): Promise<string> {
      if (disposed) throw new Error("附件暂存已关闭");
      if (!name || name === "." || name === ".." || /[\\/\0]/.test(name))
        throw new Error("附件名称无效");
      directory ??= mkdtemp(path.join(temporaryDirectory, "bot-attachments-"));
      const rootDirectory = directory;
      const operation = (async () => {
        const root = await rootDirectory;
        const folder = await mkdtemp(path.join(root, "file-"));
        const target = path.join(folder, name);
        await writeFile(target, bytes, { flag: "wx", mode: 0o600 });
        return target;
      })();
      pending.add(operation);
      try {
        return await operation;
      } finally {
        pending.delete(operation);
      }
    },
    dispose(): Promise<void> {
      if (disposal) return disposal;
      disposed = true;
      disposal = (async () => {
        await Promise.allSettled(pending);
        const root = await directory?.catch(() => undefined);
        if (root) await rm(root, { recursive: true, force: true });
      })();
      return disposal;
    },
  };
}
