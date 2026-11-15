import { spawn, type ChildProcess } from "node:child_process";
import { randomUUID } from "node:crypto";
import { mkdir } from "node:fs/promises";
import { createServer } from "node:net";
import type { CoreConnection } from "../shared/contracts";

export function validateCoreURL(value: string): URL {
  const url = new URL(value);
  if (
    url.protocol !== "http:" ||
    url.hostname !== "127.0.0.1" ||
    url.username ||
    url.password ||
    (url.pathname !== "/" && url.pathname !== "") ||
    url.search ||
    url.hash
  )
    throw new Error("本地服务地址无效");
  return url;
}
async function availablePort(preferred: number): Promise<number> {
  for (let port = preferred; port <= 65535; port++) {
    const available = await new Promise<boolean>((resolve, reject) => {
      const server = createServer();
      server.once("error", (error: NodeJS.ErrnoException) =>
        error.code === "EADDRINUSE" ? resolve(false) : reject(error),
      );
      server.listen(port, "127.0.0.1", () => server.close(() => resolve(true)));
    });
    if (available) return port;
  }
  throw new Error("没有可用的本地服务端口");
}
async function probe(port: number, token: string): Promise<boolean> {
  try {
    const response = await fetch(
      `http://127.0.0.1:${port}/api/v1/system/health`,
      {
        headers: { Authorization: `Bearer ${token}` },
        signal: AbortSignal.timeout(500),
      },
    );
    if (!response.ok) {
      await response.body?.cancel();
      return false;
    }
    const health: unknown = await response.json();
    return (
      typeof health === "object" &&
      health !== null &&
      "status" in health &&
      (health.status === "ok" || health.status === "degraded")
    );
  } catch {
    return false;
  }
}
export class CoreSupervisor {
  private child?: ChildProcess;
  private startup?: Promise<CoreConnection>;
  private stopped = false;
  private stopping?: Promise<void>;
  constructor(
    private readonly options: {
      binary: string;
      dataDir: string;
      env?: NodeJS.ProcessEnv;
      startupTimeout?: number;
      stopTimeout?: number;
      log?: (chunk: string) => void;
    },
  ) {}
  connection(): Promise<CoreConnection> {
    if (this.stopped) return Promise.reject(new Error("本地服务已停止"));
    this.startup ??= this.start().catch((error) => {
      this.startup = undefined;
      throw error;
    });
    return this.startup;
  }
  private async start(): Promise<CoreConnection> {
    const env = this.options.env ?? process.env;
    const configured = env.NEUXBOT_CORE_URL;
    const token =
      env.NEUXBOT_LOCAL_TOKEN ?? `bot_${randomUUID().replaceAll("-", "")}`;
    if (!token.trim()) throw new Error("本地服务访问令牌无效");
    if (configured)
      return { baseUrl: validateCoreURL(configured).origin, token };
    const port = await availablePort(8432);
    await mkdir(this.options.dataDir, { recursive: true });
    if (this.stopped) throw new Error("本地服务启动已取消");
    const child = spawn(this.options.binary, [], {
      env: {
        ...env,
        NEUXBOT_DATA_DIR: this.options.dataDir,
        NEUXBOT_LOCAL_TOKEN: token,
        NEUXBOT_DESKTOP_PARENT_PID: String(process.pid),
        SERVER_HOST: "127.0.0.1",
        SERVER_PORT: String(port),
      },
      stdio: ["ignore", "pipe", "pipe"],
    });
    this.child = child;
    let failure: Error | undefined;
    child.once("error", (error) => {
      failure = error;
    });
    child.once("exit", (code) => {
      failure = new Error(`本地服务已退出 (${code})`);
      this.startup = undefined;
    });
    child.stdout?.on("data", (chunk: Buffer) =>
      this.options.log?.(chunk.toString()),
    );
    child.stderr?.on("data", (chunk: Buffer) =>
      this.options.log?.(chunk.toString()),
    );
    const deadline = Date.now() + (this.options.startupTimeout ?? 20000);
    try {
      while (Date.now() < deadline && !this.stopped) {
        if (failure) throw failure;
        if (await probe(port, token))
          return { baseUrl: `http://127.0.0.1:${port}`, token };
        await new Promise((resolve) => setTimeout(resolve, 50));
      }
      throw new Error(this.stopped ? "本地服务启动已取消" : "本地服务启动超时");
    } catch (error) {
      await this.stopChild(child);
      throw error;
    }
  }
  private async stopChild(child: ChildProcess): Promise<void> {
    if (child.exitCode !== null || child.signalCode !== null || !child.pid)
      return;
    await new Promise<void>((resolve) => {
      const timer = setTimeout(() => {
        child.kill("SIGKILL");
      }, this.options.stopTimeout ?? 3000);
      child.once("exit", () => {
        clearTimeout(timer);
        resolve();
      });
      child.kill("SIGTERM");
    });
    if (this.child === child) this.child = undefined;
  }
  stop(): Promise<void> {
    this.stopped = true;
    this.stopping ??= (async () => {
      if (this.child) await this.stopChild(this.child);
      await this.startup?.catch(() => undefined);
    })();
    return this.stopping;
  }
}
