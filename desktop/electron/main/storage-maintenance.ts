import { constants } from "node:fs";
import { randomUUID } from "node:crypto";
import {
  appendFile,
  lstat,
  mkdir,
  open,
  opendir,
  readdir,
  rename,
  unlink,
} from "node:fs/promises";
import path from "node:path";
import type { Session } from "electron";

const maximumLogBytes = 10 * 1024 * 1024;
const maximumLogFiles = 5;
const logRetentionMs = 15 * 24 * 60 * 60 * 1000;
const rotatedLog = /^desktop\.(\d+)\.[0-9a-f-]{36}\.log$/;

async function fileInfo(file: string) {
  try {
    return await lstat(file);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    return undefined;
  }
}

/** 保持桌面写入、轮转和清理同一串行所有者。 */
export function createDesktopLogs(
  directory: string,
  options = { rotationBytes: maximumLogBytes },
) {
  let tail = Promise.resolve();
  const active = path.join(directory, "desktop.log");
  const serial = <T>(work: () => Promise<T>): Promise<T> => {
    const result = tail.then(work);
    tail = result.then(
      () => undefined,
      () => undefined,
    );
    return result;
  };
  async function regularDirectory(create: boolean) {
    if (create) await mkdir(directory, { recursive: true });
    const info = await fileInfo(directory);
    if (info && !info.isDirectory())
      throw new Error("Desktop log directory is not owned");
    return Boolean(info);
  }
  async function archives() {
    if (!(await regularDirectory(false))) return [];
    const results = [];
    for (const name of await readdir(directory)) {
      const match = rotatedLog.exec(name);
      if (!match) continue;
      const file = path.join(directory, name);
      const info = await fileInfo(file);
      if (info?.isFile())
        results.push({ file, bytes: info.size, createdAt: Number(match[1]) });
    }
    return results.sort((left, right) => right.createdAt - left.createdAt);
  }
  return {
    append: (line: string) =>
      serial(async () => {
        await regularDirectory(true);
        const info = await fileInfo(active);
        if (info && !info.isFile())
          throw new Error("Desktop log is not a regular file");
        const now = Date.now();
        if (info && info.size + Buffer.byteLength(line) > options.rotationBytes)
          await rename(
            active,
            path.join(directory, `desktop.${now}.${randomUUID()}.log`),
          );
        await appendFile(active, line, { mode: 0o600 });
        const files = await archives();
        for (const [index, file] of files.entries()) {
          if (
            index >= maximumLogFiles ||
            now - file.createdAt >= logRetentionMs
          )
            await unlink(file.file);
        }
      }),
    preview: () =>
      serial(async () => {
        const archiveBytes = (await archives()).reduce(
          (sum, file) => sum + file.bytes,
          0,
        );
        const current = await fileInfo(active);
        return archiveBytes + (current?.isFile() ? current.size : 0);
      }),
    clear: () =>
      serial(async () => {
        let size = 0;
        for (const file of await archives()) {
          await unlink(file.file);
          size += file.bytes;
        }
        const current = await fileInfo(active);
        if (current?.isFile() && current.size > 0) {
          const handle = await open(
            active,
            constants.O_RDWR | (constants.O_NOFOLLOW ?? 0),
          );
          try {
            const actual = await handle.stat();
            if (
              !actual.isFile() ||
              actual.dev !== current.dev ||
              actual.ino !== current.ino
            )
              throw new Error("Desktop log changed during cleanup");
            await handle.truncate(0);
            size += actual.size;
          } finally {
            await handle.close();
          }
        }
        return size;
      }),
  };
}

export type DesktopLogs = ReturnType<typeof createDesktopLogs>;

const maximumCacheEntries = 16_384;

async function codeCacheBytes(storagePath: string | null): Promise<number> {
  if (!storagePath) return 0;
  const root = path.join(storagePath, "Code Cache");
  if (!(await fileInfo(root))?.isDirectory()) return 0;
  const pending = [root];
  let seen = 0;
  let bytes = 0;
  for (
    let directory = pending.pop();
    directory !== undefined;
    directory = pending.pop()
  ) {
    if (!(await fileInfo(directory))?.isDirectory()) continue;
    let handle;
    try {
      handle = await opendir(directory);
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ENOENT") continue;
      throw error;
    }
    for await (const entry of handle) {
      if (++seen > maximumCacheEntries) return bytes;
      const child = path.join(directory, entry.name);
      if (entry.isDirectory()) pending.push(child);
      else if (entry.isFile()) {
        const info = await fileInfo(child);
        if (info?.isFile()) bytes += info.size;
      }
    }
  }
  return bytes;
}

/** 预览本 Session 的 HTTP 缓存与代码缓存的已知大小。 */
export async function desktopCacheSize(session: Session): Promise<number> {
  const [http, code] = await Promise.all([
    session.getCacheSize(),
    codeCacheBytes(session.storagePath),
  ]);
  return http + code;
}

/** Electron 自己清理 HTTP 和代码缓存，登录与其他站点数据由原 Session 保留。 */
export async function clearDesktopCache(session: Session): Promise<number> {
  const before = await desktopCacheSize(session);
  await session.clearCache();
  await session.clearCodeCaches({});
  return Math.max(0, before - (await desktopCacheSize(session)));
}
