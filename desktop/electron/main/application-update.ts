import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import {
  type FileHandle,
  mkdir,
  open,
  readdir,
  rename,
  rm,
  stat,
} from "node:fs/promises";
import path from "node:path";
import { z } from "zod";
import {
  applicationReleasesURL,
  type ApplicationUpdateError,
  type ApplicationUpdateState,
} from "../shared/application-update";

const repositoryURL = "https://github.com/JieWaZi/NeuxBot";
const latestReleaseURL =
  "https://api.github.com/repos/JieWaZi/NeuxBot/releases/latest";
const metadataLimit = 1024 * 1024;
const installerLimit = 512 * 1024 * 1024;
const checkInterval = 6 * 60 * 60 * 1000;
const stableVersion = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const assetSchema = z.object({
  name: z.string(),
  state: z.string(),
  size: z.number().int().nonnegative().max(installerLimit),
  digest: z.string().nullable().optional(),
});
const releaseSchema = z.object({
  tag_name: z.string(),
  draft: z.literal(false),
  prerelease: z.literal(false),
  body: z.string().nullable(),
  published_at: z.string().datetime(),
  assets: z.array(assetSchema),
});

type UpdateFetch = (
  url: string,
  options: {
    signal: AbortSignal;
    redirect: "manual";
    headers?: Record<string, string>;
  },
) => Promise<Response>;

type UpdateOptions = {
  version: string;
  packaged: boolean;
  platform: NodeJS.Platform;
  arch: string;
  cacheDirectory: string;
  fetch: UpdateFetch;
  openInstaller: (file: string) => Promise<string>;
  log?: (message: string) => void | Promise<void>;
};

type Candidate = {
  tag: string;
  name: string;
  size: number;
  digest?: string;
  checksums: boolean;
};

class UpdateFailure extends Error {
  constructor(readonly code: ApplicationUpdateError) {
    super(code);
  }
}

function newerVersion(next: string, current: string): boolean {
  if (!stableVersion.test(next) || !stableVersion.test(current))
    throw new UpdateFailure("invalid-release");
  const installed = current.split(".").map(BigInt);
  const parts = next.split(".").map(BigInt);
  for (let index = 0; index < parts.length; index++) {
    const availablePart = parts[index];
    const installedPart = installed[index];
    if (
      availablePart !== undefined &&
      installedPart !== undefined &&
      availablePart !== installedPart
    )
      return availablePart > installedPart;
  }
  return false;
}

/** 安装包由现有 DMG/NSIS 安装器接管，不在运行中改写应用或用户数据。 */
export class ApplicationUpdate {
  private state: ApplicationUpdateState;
  private candidate?: Candidate;
  private installer?: string;
  private operation?: Promise<ApplicationUpdateState>;
  private opening?: Promise<ApplicationUpdateState>;
  private listeners = new Set<(state: ApplicationUpdateState) => void>();
  private abort = new AbortController();
  private timer?: ReturnType<typeof setTimeout>;
  private started = false;

  constructor(private readonly options: UpdateOptions) {
    const supported =
      (options.platform === "darwin" &&
        ["arm64", "x64"].includes(options.arch)) ||
      (options.platform === "win32" && options.arch === "x64");
    this.state = {
      revision: 0,
      currentVersion: options.version,
      platform: options.platform,
      arch: options.arch,
      status: !options.packaged || !supported ? "unavailable" : "idle",
      unavailableReason: !options.packaged
        ? "development"
        : !supported
          ? "platform"
          : undefined,
      downloadedBytes: 0,
    };
  }

  snapshot = (): ApplicationUpdateState => structuredClone(this.state);

  subscribe(listener: (state: ApplicationUpdateState) => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  private publish(change: Partial<ApplicationUpdateState>) {
    if (this.abort.signal.aborted) return;
    this.state = {
      ...this.state,
      ...change,
      revision: this.state.revision + 1,
    };
    for (const listener of this.listeners) listener(this.snapshot());
  }

  private async fail(error: unknown, fallback: ApplicationUpdateError) {
    if (this.abort.signal.aborted) return;
    const code = error instanceof UpdateFailure ? error.code : fallback;
    this.publish({
      status: this.installer ? "downloaded" : "error",
      error: code,
    });
    // 外部下载地址可能携带临时令牌，诊断只记录稳定错误身份。
    await this.options.log?.(`软件更新失败：${code}`);
  }

  start() {
    if (this.started || this.state.status === "unavailable") return;
    this.started = true;
    const schedule = (delay: number) => {
      if (this.abort.signal.aborted) return;
      this.timer = setTimeout(() => {
        void this.check().finally(() => schedule(checkInterval));
      }, delay);
      this.timer.unref();
    };
    schedule(15_000);
  }

  async dispose() {
    if (this.timer) clearTimeout(this.timer);
    this.abort.abort();
    this.listeners.clear();
    await Promise.allSettled([this.operation, this.opening]);
  }

  private async request(url: string, signal: AbortSignal): Promise<Response> {
    for (let redirects = 0; redirects <= 5; redirects++) {
      const target = new URL(url);
      const github = target.origin === "https://github.com";
      if (
        target.protocol !== "https:" ||
        target.username ||
        target.password ||
        target.port ||
        target.hash ||
        (github
          ? !target.pathname.startsWith("/JieWaZi/NeuxBot/releases/download/")
          : ![
              "https://api.github.com",
              "https://release-assets.githubusercontent.com",
              "https://objects.githubusercontent.com",
            ].includes(target.origin)) ||
        (target.origin === "https://api.github.com" && url !== latestReleaseURL)
      )
        throw new UpdateFailure("invalid-release");
      const response = await this.options.fetch(url, {
        signal,
        redirect: "manual",
        headers: {
          Accept:
            url === latestReleaseURL
              ? "application/vnd.github+json"
              : "application/octet-stream",
          "User-Agent": `NeuxBot/${this.options.version}`,
        },
      });
      if ([301, 302, 303, 307, 308].includes(response.status)) {
        await response.body?.cancel();
        const location = response.headers.get("location");
        if (!location) throw new UpdateFailure("network");
        url = new URL(location, url).href;
        continue;
      }
      // 调用方只读取失败状态，不消费其响应体，须及时释放下载连接。
      if (!response.ok) await response.body?.cancel().catch(() => undefined);
      return response;
    }
    throw new UpdateFailure("network");
  }

  private async text(response: Response): Promise<string> {
    if (!response.body) throw new UpdateFailure("invalid-release");
    const reader = response.body.getReader();
    const chunks: Uint8Array[] = [];
    let size = 0;
    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        size += value.byteLength;
        if (size > metadataLimit) throw new UpdateFailure("invalid-release");
        chunks.push(value);
      }
      return Buffer.concat(chunks).toString("utf8");
    } finally {
      await reader.cancel().catch(() => undefined);
      reader.releaseLock();
    }
  }

  check = (): Promise<ApplicationUpdateState> => {
    if (this.operation) return this.operation;
    if (this.opening) return this.opening;
    if (this.abort.signal.aborted || this.state.status === "unavailable")
      return Promise.resolve(this.snapshot());
    this.operation = this.checkRelease().finally(() => {
      this.operation = undefined;
    });
    return this.operation;
  };

  private async checkRelease(): Promise<ApplicationUpdateState> {
    this.publish({ status: "checking", error: undefined });
    try {
      const response = await this.request(
        latestReleaseURL,
        AbortSignal.any([this.abort.signal, AbortSignal.timeout(20_000)]),
      );
      if (response.status === 404) {
        this.candidate = undefined;
        this.installer = undefined;
        this.publish({
          status: "no-release",
          release: undefined,
          downloadedBytes: 0,
          checkedAt: new Date().toISOString(),
        });
        return this.snapshot();
      }
      if ([403, 429].includes(response.status))
        throw new UpdateFailure("rate-limited");
      if (!response.ok) throw new UpdateFailure("network");
      const content = await this.text(response);
      let body: unknown;
      try {
        body = JSON.parse(content);
      } catch {
        throw new UpdateFailure("invalid-release");
      }
      const parsed = releaseSchema.safeParse(body);
      if (!parsed.success) throw new UpdateFailure("invalid-release");
      const release = parsed.data;
      const version = release.tag_name.replace(/^v/, "");
      if (!newerVersion(version, this.options.version)) {
        this.candidate = undefined;
        this.installer = undefined;
        this.publish({
          status: "current",
          release: undefined,
          downloadedBytes: 0,
          checkedAt: new Date().toISOString(),
        });
        return this.snapshot();
      }
      const name =
        this.options.platform === "darwin"
          ? `NeuxBot-mac-${this.options.arch}.dmg`
          : `NeuxBot-windows-${this.options.arch}.exe`;
      const asset = release.assets.find(
        (item) =>
          item.name === name && item.state === "uploaded" && item.size > 0,
      );
      if (!asset) throw new UpdateFailure("missing-asset");
      const digest = asset.digest
        ?.match(/^sha256:([a-f\d]{64})$/i)?.[1]
        ?.toLowerCase();
      const checksums = release.assets.some(
        (item) => item.name === "SHA256SUMS" && item.state === "uploaded",
      );
      if (!digest && !checksums) throw new UpdateFailure("checksum");
      // 只有身份、大小和明确摘要均不变时，才继续提供已下载安装包。
      const keepInstaller = Boolean(
        this.installer &&
        this.candidate?.tag === release.tag_name &&
        this.candidate.name === name &&
        this.candidate.size === asset.size &&
        digest &&
        this.candidate.digest?.toLowerCase() === digest,
      );
      if (!keepInstaller) this.installer = undefined;
      this.candidate = {
        tag: release.tag_name,
        name,
        size: asset.size,
        digest,
        checksums,
      };
      this.publish({
        status: keepInstaller ? "downloaded" : "available",
        downloadedBytes: keepInstaller ? asset.size : 0,
        checkedAt: new Date().toISOString(),
        release: {
          version,
          url: `${applicationReleasesURL}/tag/${encodeURIComponent(release.tag_name)}`,
          notes: release.body ?? "",
          publishedAt: release.published_at,
          downloadSize: asset.size,
        },
      });
    } catch (error) {
      await this.fail(error, "network");
    }
    return this.snapshot();
  }

  private async hash(file: string): Promise<string> {
    const hash = createHash("sha256");
    for await (const chunk of createReadStream(file, {
      signal: this.abort.signal,
    }))
      hash.update(chunk);
    return hash.digest("hex");
  }

  private async completeDownload(
    candidate: Candidate,
    digest: string,
    target: string,
  ) {
    this.installer = target;
    this.candidate = { ...candidate, digest };
    // 更新缓存只保留本次安装包，避免每次升级遗留一个完整安装文件。
    const entries = await readdir(this.options.cacheDirectory, {
      withFileTypes: true,
    }).catch(() => []);
    for (const entry of entries) {
      if (
        entry.isDirectory() &&
        entry.name !== candidate.tag &&
        stableVersion.test(entry.name.replace(/^v/, ""))
      )
        await rm(path.join(this.options.cacheDirectory, entry.name), {
          recursive: true,
          force: true,
        }).catch(() => undefined);
    }
    this.publish({ status: "downloaded", downloadedBytes: candidate.size });
  }

  download = (): Promise<ApplicationUpdateState> => {
    if (this.operation) return this.operation;
    if (this.abort.signal.aborted || !this.candidate || this.installer)
      return Promise.resolve(this.snapshot());
    this.operation = this.downloadInstaller(this.candidate).finally(() => {
      this.operation = undefined;
    });
    return this.operation;
  };

  private async downloadInstaller(
    candidate: Candidate,
  ): Promise<ApplicationUpdateState> {
    this.publish({
      status: "downloading",
      downloadedBytes: 0,
      error: undefined,
    });
    const directory = path.join(this.options.cacheDirectory, candidate.tag);
    const target = path.join(directory, candidate.name);
    const partial = `${target}.part`;
    const assetURL = (name: string) =>
      `${repositoryURL}/releases/download/${encodeURIComponent(candidate.tag)}/${name}`;
    try {
      let digest = candidate.digest;
      if (!digest && candidate.checksums) {
        const response = await this.request(
          assetURL("SHA256SUMS"),
          AbortSignal.any([this.abort.signal, AbortSignal.timeout(20_000)]),
        );
        if (!response.ok) throw new UpdateFailure("checksum");
        const entries = (await this.text(response))
          .split(/\r?\n/)
          .map((line) => line.match(/^([a-f\d]{64}) [ *](.+)$/i))
          .filter((entry) => entry?.[2] === candidate.name);
        if (entries.length !== 1) throw new UpdateFailure("checksum");
        digest = entries[0]?.[1];
      }
      if (!digest) throw new UpdateFailure("checksum");
      digest = digest.toLowerCase();
      await mkdir(directory, { recursive: true, mode: 0o700 });
      const cached = await stat(target).catch(() => undefined);
      if (
        cached?.size === candidate.size &&
        (await this.hash(target)) === digest
      ) {
        await this.completeDownload(candidate, digest, target);
        return this.snapshot();
      }
      await rm(target, { force: true });
      await rm(partial, { force: true });
      const response = await this.request(
        assetURL(candidate.name),
        AbortSignal.any([this.abort.signal, AbortSignal.timeout(30 * 60_000)]),
      );
      if (!response.ok || !response.body) throw new UpdateFailure("download");
      const reader = response.body.getReader();
      let file: FileHandle | undefined;
      const hash = createHash("sha256");
      let received = 0;
      let lastProgress = 0;
      try {
        file = await open(partial, "wx", 0o600);
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          received += value.byteLength;
          if (received > candidate.size) throw new UpdateFailure("checksum");
          hash.update(value);
          await file.writeFile(value);
          if (Date.now() - lastProgress >= 200) {
            lastProgress = Date.now();
            this.publish({ downloadedBytes: received });
          }
        }
        if (received !== candidate.size || hash.digest("hex") !== digest)
          throw new UpdateFailure("checksum");
      } finally {
        // 已中断的流可能拒绝 cancel，文件句柄仍须关闭，尤其是 Windows。
        await reader.cancel().catch(() => undefined);
        reader.releaseLock();
        await file?.close();
      }
      await rename(partial, target);
      await this.completeDownload(candidate, digest, target);
    } catch (error) {
      await rm(partial, { force: true }).catch(() => undefined);
      await this.fail(error, "download");
    }
    return this.snapshot();
  }

  openInstaller = (): Promise<ApplicationUpdateState> => {
    if (this.opening) return this.opening;
    if (this.operation) return this.operation;
    this.opening = this.openVerifiedInstaller().finally(() => {
      this.opening = undefined;
    });
    return this.opening;
  };

  private async openVerifiedInstaller(): Promise<ApplicationUpdateState> {
    if (this.abort.signal.aborted || !this.installer || !this.candidate?.digest)
      return this.snapshot();
    try {
      const hash = await this.hash(this.installer).catch(() => undefined);
      if (hash !== this.candidate.digest.toLowerCase()) {
        this.installer = undefined;
        throw new UpdateFailure("checksum");
      }
      const error = await this.options.openInstaller(this.installer);
      if (error) throw new UpdateFailure("open-installer");
      this.publish({ status: "downloaded", error: undefined });
    } catch (error) {
      await this.fail(error, "open-installer");
    }
    return this.snapshot();
  }
}
