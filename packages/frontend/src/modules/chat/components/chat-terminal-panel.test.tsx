import { StrictMode } from "react";
import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { attachTerminal, detachTerminal, fit, startTerminal, write, writeln } =
  vi.hoisted(() => ({
    attachTerminal: vi.fn(() =>
      Promise.resolve({
        sessionId: "session-1",
        shell: "/bin/sh",
        attachmentId: "attachment-1",
        screen: "",
        sequence: 0,
        state: "running",
      }),
    ),
    detachTerminal: vi.fn(() => Promise.resolve()),
    fit: vi.fn(),
    startTerminal: vi.fn(),
    write: vi.fn(),
    writeln: vi.fn(),
  }));

vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class {
    fit() {
      fit();
    }
  },
}));

vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    rows = 24;
    cols = 80;
    options: { theme?: unknown } = {};
    loadAddon() {}
    open() {}
    onData() {
      return { dispose() {} };
    }
    write(...args: unknown[]) {
      write(...args);
    }
    writeln(...args: unknown[]) {
      writeln(...args);
    }
    dispose() {}
  },
}));

vi.mock("@/shared/platform", () => ({
  attachTerminal,
  detachTerminal,
  resizeTerminal: vi.fn(() => Promise.resolve()),
  startTerminal,
  subscribeTerminalExit: vi.fn(() => () => undefined),
  subscribeTerminalSnapshot: vi.fn(() => () => undefined),
  subscribeTerminalOutput: vi.fn(() => () => undefined),
  writeTerminal: vi.fn(() => Promise.resolve()),
}));

vi.mock("@/shared/diagnostics/logger", () => ({
  logger: { error: vi.fn() },
}));

import { ChatTerminalPanel } from "./chat-terminal-panel";

class ResizeObserverStub {
  observe() {}
  disconnect() {}
}

describe("ChatTerminalPanel", () => {
  beforeEach(() => {
    startTerminal.mockReset();
    attachTerminal.mockClear();
    detachTerminal.mockClear();
    fit.mockClear();
    write.mockClear();
    writeln.mockClear();
    vi.stubGlobal("ResizeObserver", ResizeObserverStub);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("在写入终端快照前按当前分屏尺寸适配", async () => {
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(640);
    vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(480);
    startTerminal.mockResolvedValue({
      sessionId: "session-1",
      shell: "/bin/sh",
    });
    attachTerminal.mockResolvedValueOnce({
      sessionId: "session-1",
      shell: "/bin/sh",
      attachmentId: "attachment-1",
      screen: "saved screen",
      sequence: 0,
      state: "running",
    });

    await act(async () => {
      render(
        <ChatTerminalPanel
          chatId="chat-id"
          onSessionBound={vi.fn()}
          ownerKey="tab-1"
          workingDirectory="/tmp"
        />,
      );
    });

    expect(write).toHaveBeenCalledWith("saved screen");
    const firstWriteCall = write.mock.invocationCallOrder[0];
    expect(firstWriteCall).toBeDefined();
    expect(fit.mock.invocationCallOrder.at(-1)).toBeLessThan(
      firstWriteCall ?? 0,
    );
  });

  it("初次布局过窄时不把错误列数同步给终端进程", async () => {
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(16);
    vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(480);
    startTerminal.mockResolvedValue({
      sessionId: "session-1",
      shell: "/bin/sh",
    });

    await act(async () => {
      render(
        <ChatTerminalPanel
          chatId="chat-id"
          onSessionBound={vi.fn()}
          ownerKey="tab-1"
          workingDirectory="/tmp"
        />,
      );
    });

    expect(fit).not.toHaveBeenCalled();
  });

  it("在 StrictMode 重放 Effect 时复用同一标签身份且不关闭终端", async () => {
    startTerminal.mockResolvedValue({
      sessionId: "session-1",
      shell: "/bin/sh",
    });
    const onSessionBound = vi.fn();

    await act(async () => {
      render(
        <StrictMode>
          <ChatTerminalPanel
            chatId="chat-id"
            onSessionBound={onSessionBound}
            ownerKey="tab-1"
            workingDirectory="/tmp"
          />
        </StrictMode>,
      );
    });

    expect(startTerminal).toHaveBeenCalledWith(
      "/tmp",
      "chat-id",
      "tab-1",
      true,
    );
    expect(onSessionBound).toHaveBeenCalledWith("session-1");
    expect(writeln).not.toHaveBeenCalled();
  });

  it("保留桌面端返回的终端启动错误", async () => {
    startTerminal.mockRejectedValue("启动终端 Shell 失败: permission denied");

    await act(async () => {
      render(
        <ChatTerminalPanel
          chatId="chat-id"
          onSessionBound={vi.fn()}
          ownerKey="tab-1"
          workingDirectory="/tmp"
        />,
      );
    });

    expect(writeln).toHaveBeenCalledWith(
      expect.stringContaining("启动终端 Shell 失败: permission denied"),
    );
  });

  it("启动结果晚于视图卸载时不再绑定或附着终端", async () => {
    let resolveStart:
      ((started: { sessionId: string; shell: string }) => void) | undefined;
    startTerminal.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveStart = resolve;
        }),
    );
    const onSessionBound = vi.fn();
    const { unmount } = render(
      <ChatTerminalPanel
        chatId="chat-id"
        onSessionBound={onSessionBound}
        ownerKey="tab-1"
        workingDirectory="/tmp"
      />,
    );
    unmount();
    await act(async () =>
      resolveStart?.({ sessionId: "session-1", shell: "/bin/sh" }),
    );
    expect(onSessionBound).not.toHaveBeenCalled();
    expect(attachTerminal).not.toHaveBeenCalled();
  });
});
