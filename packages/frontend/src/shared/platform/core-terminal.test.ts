import { afterEach, describe, expect, it, vi } from "vitest";
import { createCoreTerminalService } from "./core-terminal";
vi.mock("@/shared/api/generated/sdk.gen", () => ({
  startTerminal: vi.fn(),
  closeTerminal: vi.fn(async () => ({ data: {} })),
}));
class Socket {
  static OPEN = 1;
  static instances: Socket[] = [];
  readyState = 1;
  bufferedAmount = 0;
  sent: string[] = [];
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  constructor(
    readonly url: URL,
    readonly protocols: string[],
  ) {
    Socket.instances.push(this);
  }
  send(data: string) {
    this.sent.push(data);
  }
  close() {
    if (this.readyState === 3) return;
    this.readyState = 3;
    this.onclose?.();
  }
  frame(event: string, payload: unknown) {
    this.onmessage?.({ data: JSON.stringify({ event, payload }) });
  }
}
function socketAt(index: number): Socket {
  const socket = Socket.instances[index];
  if (!socket) throw new Error(`测试终端连接 ${index} 尚未创建`);
  return socket;
}
const snapshot = (attachmentId = "a", sequence = 2) => ({
  sessionId: "s",
  attachmentId,
  shell: "sh",
  screen: "screen",
  sequence,
  state: "running",
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
  Socket.instances = [];
});
describe("Core 终端视图", () => {
  it("Cookie 会话不向 URL 或子协议附加令牌，恢复屏幕时不重放输入", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", Socket);
    const terminal = createCoreTerminalService(async () => ({
      baseUrl: "http://127.0.0.1:8080",
      credentials: "same-origin",
    }));
    const output = vi.fn();
    const recovered = vi.fn();
    terminal.onTerminalOutput(output);
    terminal.onTerminalSnapshot?.(recovered);
    const attached = terminal.terminal_attach({ sessionId: "s" });
    await Promise.resolve();
    const first = socketAt(0);
    expect(first.protocols).toEqual(["ally.terminal.v1"]);
    expect(first.url.search).toBe("");
    first.frame("snapshot", snapshot());
    await attached;
    first.frame("terminal-output", {
      sessionId: "s",
      sequence: 2,
      data: "old",
    });
    first.frame("terminal-output", {
      sessionId: "s",
      sequence: 3,
      data: "new",
    });
    expect(output).toHaveBeenCalledTimes(1);
    await terminal.terminal_write({
      sessionId: "s",
      attachmentId: "a",
      data: "echo hello\r",
    });
    first.close();
    await vi.advanceTimersByTimeAsync(1000);
    const second = socketAt(1);
    second.frame("snapshot", snapshot("b", 4));
    await Promise.resolve();
    expect(recovered).toHaveBeenCalledWith(snapshot("b", 4));
    expect(second.sent).toEqual([]);
    await expect(
      terminal.terminal_write({
        sessionId: "s",
        attachmentId: "a",
        data: "old",
      }),
    ).rejects.toThrow();
    await terminal.terminal_detach({ sessionId: "s", attachmentId: "b" });
    await vi.advanceTimersByTimeAsync(30000);
    expect(Socket.instances).toHaveLength(2);
  });
  it("输出序号缺口触发重新获取屏幕，不把缺口后内容直接显示", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", Socket);
    const terminal = createCoreTerminalService(async () => ({
      baseUrl: "http://127.0.0.1:8080",
      token: "native",
    }));
    const output = vi.fn();
    terminal.onTerminalOutput(output);
    const attached = terminal.terminal_attach({ sessionId: "s" });
    await Promise.resolve();
    const first = socketAt(0);
    first.frame("snapshot", snapshot());
    await attached;
    first.frame("terminal-output", {
      sessionId: "s",
      sequence: 4,
      data: "gap",
    });
    expect(output).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1000);
    expect(Socket.instances).toHaveLength(2);
    await terminal.terminal_detach({ sessionId: "s", attachmentId: "a" });
  });
});
