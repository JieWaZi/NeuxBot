import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { WorkbenchProvider, useWorkbench } from "./workbench-store";

const storageKey = "ally.right-workbench.v2.chat:one";

function Probe() {
  const { snapshot } = useWorkbench("chat:one");
  return (
    <>
      <output data-testid="tab-id">{snapshot.tabs[0]?.id ?? "empty"}</output>
      <output data-testid="occurrence">
        {snapshot.tabs[0]?.occurrenceId ?? "empty"}
      </output>
      <output data-testid="terminal-session">
        {snapshot.tabs[0]?.terminalSessionId ?? "empty"}
      </output>
    </>
  );
}

function renderProbe() {
  return render(
    <WorkbenchProvider>
      <Probe />
    </WorkbenchProvider>,
  );
}

afterEach(() => localStorage.removeItem(storageKey));

describe("工作台重载边界", () => {
  it("重新挂载时保留终端身份并重建 Renderer 挂载身份", () => {
    localStorage.setItem(
      storageKey,
      JSON.stringify({
        version: 2,
        state: {
          tabs: [
            {
              id: "terminal-tab",
              kind: "terminal",
              contentId: "terminal:one",
              occurrenceId: "old-occurrence",
              navigationRevision: 1,
              title: "终端",
              rootPath: "/workspace",
              terminalSessionId: "native-session",
            },
          ],
          layout: {
            kind: "pane",
            id: "pane:root",
            tabIds: ["terminal-tab"],
            activeTabId: "terminal-tab",
          },
          focusedPaneId: "pane:root",
          fullscreen: false,
          width: 320,
          customWidth: true,
          side: "right",
        },
      }),
    );
    const first = renderProbe();
    const firstOccurrence = screen.getByTestId("occurrence").textContent;
    expect(screen.getByTestId("tab-id")).toHaveTextContent("terminal-tab");
    expect(screen.getByTestId("terminal-session")).toHaveTextContent(
      "native-session",
    );
    first.unmount();

    const second = renderProbe();
    expect(screen.getByTestId("tab-id")).toHaveTextContent("terminal-tab");
    expect(screen.getByTestId("terminal-session")).toHaveTextContent(
      "native-session",
    );
    expect(screen.getByTestId("occurrence").textContent).not.toBe(
      firstOccurrence,
    );
    second.unmount();
  });

  it("损坏的持久状态被清除后使用空工作台", () => {
    localStorage.setItem(storageKey, '{"version":2,"state":{');
    const view = renderProbe();
    expect(screen.getByTestId("tab-id")).toHaveTextContent("empty");
    expect(localStorage.getItem(storageKey)).toBeNull();
    view.unmount();
  });
});
