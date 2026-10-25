import { randomUUID } from "node:crypto";
import { spawn, type IPty } from "node-pty";
import { Terminal as HeadlessTerminal } from "@xterm/headless";
import { SerializeAddon } from "@xterm/addon-serialize";
import { realpath, stat } from "node:fs/promises";
async function canonicalDirectory(value: string): Promise<string> {
  const result = await realpath(value);
  if (!(await stat(result)).isDirectory()) throw new Error("工作目录不存在");
  return result;
}
import type {
  TerminalSnapshot,
  TerminalStarted,
} from "@neuxbot/platform-contracts/resources";

type TerminalEvents = {
  "terminal-output": import("@neuxbot/platform-contracts/resources").TerminalOutputEvent;
  "terminal-exit": import("@neuxbot/platform-contracts/resources").TerminalExitEvent;
};

const outputHighWatermark = 256 * 1024;
const outputLowWatermark = 64 * 1024;

type Session = {
  ownerKey: string;
  root: string;
  sessionId: string;
  shell: string;
  child: IPty;
  screen: HeadlessTerminal;
  serializer: SerializeAddon;
  sequence: number;
  pending: Promise<void>;
  pendingOutputSize: number;
  outputPaused: boolean;
  attachmentId?: string;
  exitCode?: number;
  state: "running" | "exited";
  closing: boolean;
  dataSubscription: { dispose(): void };
  exitSubscription: { dispose(): void };
};

/** 宿主拥有 PTY 与有界恢复屏幕，React 附件可以独立重建。 */
export class TerminalManager {
  private readonly sessions = new Map<string, Session>();
  private readonly byOwner = new Map<string, string>();
  private readonly opening = new Map<string, Promise<TerminalStarted>>();
  private readonly closed = new Set<string>();
  private disposed = false;

  constructor(
    private readonly emit: <K extends keyof TerminalEvents>(
      event: K,
      payload: TerminalEvents[K],
    ) => void | Promise<void>,
  ) {}

  async start(
    root: string,
    ownerKey: string,
    createWhenMissing: boolean,
  ): Promise<TerminalStarted> {
    if (this.disposed || this.closed.has(ownerKey))
      throw new Error("终端标签已关闭");
    const existing = this.byOwner.get(ownerKey);
    if (existing) {
      const session = this.sessions.get(existing);
      if (!session) throw new Error("终端会话不存在");
      if (session.root !== (await canonicalDirectory(root)))
        throw new Error("终端工作目录已变化");
      return { sessionId: session.sessionId, shell: session.shell };
    }
    const pending = this.opening.get(ownerKey);
    if (pending) return pending;
    if (!createWhenMissing) throw new Error("原终端会话已结束，请手动新建终端");
    const opening = this.create(root, ownerKey).finally(() => {
      this.opening.delete(ownerKey);
    });
    this.opening.set(ownerKey, opening);
    return opening;
  }

  private async create(
    root: string,
    ownerKey: string,
  ): Promise<TerminalStarted> {
    const cwd = await canonicalDirectory(root);
    if (this.disposed || this.closed.has(ownerKey))
      throw new Error("终端标签已关闭");
    const shell =
      process.env.SHELL ||
      (process.platform === "win32"
        ? "powershell.exe"
        : process.platform === "darwin"
          ? "/bin/zsh"
          : "/bin/sh");
    const environment: NodeJS.ProcessEnv = {
      ...process.env,
      TERM: "xterm-256color",
    };
    delete environment.ELECTRON_RUN_AS_NODE;
    delete environment.ALLY_LOCAL_TOKEN;
    delete environment.ALLY_TERMINAL_ELECTRON_NODE;
    const child = spawn(shell, process.platform === "win32" ? [] : ["-l"], {
      cwd,
      cols: 100,
      rows: 24,
      name: "xterm-256color",
      env: environment,
    });
    const screen = new HeadlessTerminal({
      cols: 100,
      rows: 24,
      scrollback: 2_000,
      allowProposedApi: true,
    });
    const serializer = new SerializeAddon();
    screen.loadAddon(serializer);
    const sessionId = randomUUID();
    const session: Session = {
      ownerKey,
      root: cwd,
      sessionId,
      shell,
      child,
      screen,
      serializer,
      sequence: 0,
      pending: Promise.resolve(),
      pendingOutputSize: 0,
      outputPaused: false,
      state: "running",
      closing: false,
      dataSubscription: { dispose() {} },
      exitSubscription: { dispose() {} },
    };
    session.dataSubscription = child.onData((data) => {
      if (session.closing) return;
      const outputSize = Buffer.byteLength(data, "utf8");
      session.pendingOutputSize += outputSize;
      if (
        !session.outputPaused &&
        session.pendingOutputSize >= outputHighWatermark
      ) {
        session.outputPaused = true;
        child.pause();
      }
      session.pending = session.pending
        .then(
          () =>
            new Promise<void>((resolve) => {
              screen.write(data, resolve);
            }),
        )
        .then(async () => {
          session.pendingOutputSize -= outputSize;
          if (
            session.outputPaused &&
            session.pendingOutputSize <= outputLowWatermark &&
            !session.closing &&
            session.state === "running"
          ) {
            session.outputPaused = false;
            child.resume();
          }
          session.sequence += 1;
          if (session.attachmentId)
            await this.emit("terminal-output", {
              sessionId,
              sequence: session.sequence,
              data,
            });
        });
    });
    session.exitSubscription = child.onExit(({ exitCode }) => {
      session.state = "exited";
      session.exitCode = exitCode;
      void session.pending.then(() => {
        if (session.closing) return;
        session.dataSubscription.dispose();
        session.exitSubscription.dispose();
        if (session.attachmentId)
          this.emit("terminal-exit", { sessionId, code: exitCode });
      });
    });
    if (this.disposed || this.closed.has(ownerKey)) {
      child.kill();
      screen.dispose();
      throw new Error("终端标签已关闭");
    }
    this.sessions.set(sessionId, session);
    this.byOwner.set(ownerKey, sessionId);
    return { sessionId, shell };
  }

  async attach(sessionId: string): Promise<TerminalSnapshot> {
    const session = this.require(sessionId);
    await session.pending;
    if (session.closing) throw new Error("终端会话正在关闭");
    const attachmentId = randomUUID();
    session.attachmentId = attachmentId;
    return {
      sessionId,
      shell: session.shell,
      attachmentId,
      screen: session.serializer.serialize(),
      sequence: session.sequence,
      state: session.state,
      exitCode: session.exitCode,
    };
  }

  detach(sessionId: string, attachmentId: string): void {
    const session = this.sessions.get(sessionId);
    if (session?.attachmentId === attachmentId)
      session.attachmentId = undefined;
  }

  write(sessionId: string, attachmentId: string, data: string): void {
    this.requireControl(sessionId, attachmentId).child.write(data);
  }

  resize(
    sessionId: string,
    attachmentId: string,
    size: { cols: number; rows: number },
  ): void {
    const session = this.requireControl(sessionId, attachmentId);
    session.child.resize(size.cols, size.rows);
    session.screen.resize(size.cols, size.rows);
  }

  private require(id: string): Session {
    const session = this.sessions.get(id);
    if (!session) throw new Error("终端会话不存在");
    return session;
  }

  private requireControl(id: string, attachmentId: string): Session {
    const session = this.require(id);
    if (
      session.attachmentId !== attachmentId ||
      session.closing ||
      session.state !== "running"
    )
      throw new Error("当前视图没有终端控制权");
    return session;
  }

  async close(ownerKey: string): Promise<boolean> {
    this.closed.add(ownerKey);
    const pending = this.opening.get(ownerKey);
    if (pending) await pending.catch(() => undefined);
    const sessionId = this.byOwner.get(ownerKey);
    if (!sessionId) return false;
    const session = this.sessions.get(sessionId);
    if (!session) return false;
    this.byOwner.delete(ownerKey);
    this.sessions.delete(sessionId);
    session.closing = true;
    session.attachmentId = undefined;
    session.dataSubscription.dispose();
    session.exitSubscription.dispose();
    if (session.outputPaused && session.state === "running") {
      session.outputPaused = false;
      session.child.resume();
    }
    if (session.state === "running") session.child.kill();
    await session.pending;
    session.screen.dispose();
    return true;
  }

  dispose(): void {
    this.disposed = true;
    for (const ownerKey of this.byOwner.keys())
      void this.close(ownerKey).catch(() => undefined);
    for (const ownerKey of this.opening.keys())
      void this.close(ownerKey).catch(() => undefined);
  }
}
