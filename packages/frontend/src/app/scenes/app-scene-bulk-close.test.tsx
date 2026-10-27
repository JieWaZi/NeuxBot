import { useLayoutEffect } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { SceneTabStrip } from "@/shared/ui/scene-tab-strip";
import { useAppSceneWorkspace } from "@/shared/workspace/app-scene-workspace";
import { AppSceneWorkspaceProvider } from "./app-scene-workspace-provider";

vi.mock("./route-scene-not-found", () => ({
  useRouteSceneNotFound: (tabs: unknown[]) => "0".repeat(tabs.length),
}));

function Tabs({
  onCloseFile,
  onSelectFile,
}: {
  onCloseFile: (id: string) => void;
  onSelectFile: (id: string) => void;
}) {
  const { snapshot, dispatch, syncWorkspace } = useAppSceneWorkspace();
  useLayoutEffect(() => {
    syncWorkspace({
      ownerTabId: "chat:one",
      ownerHref: "/chats/one",
      primaryWorkspaceTabId: "main",
      activeWorkspaceTabId: "main",
      onSelect: onSelectFile,
      onClose: onCloseFile,
      tabs: [
        { id: "main", label: "会话" },
        { id: "file-one", label: "文件甲", closeable: true },
        { id: "file-two", label: "文件乙", closeable: true },
      ],
    });
  }, [onCloseFile, onSelectFile, syncWorkspace]);
  return (
    <>
      <button
        onClick={() =>
          dispatch({
            type: "open",
            tab: {
              id: "settings",
              label: "设置",
              kind: "settings",
              href: "/settings/general",
              closeable: true,
            },
          })
        }
      >
        打开设置
      </button>
      <SceneTabStrip
        activeTabId={snapshot.activeTabId ?? ""}
        ariaLabel="场景标签"
        tabs={snapshot.tabs}
        onSelect={(tabId) => dispatch({ type: "select", tabId })}
        onClose={(tabId) => dispatch({ type: "close", tabId })}
        onCloseOthers={(tabId) => dispatch({ type: "close-others", tabId })}
      />
    </>
  );
}

function renderTabs() {
  const navigate = vi.fn();
  const onCloseFile = vi.fn();
  const onSelectFile = vi.fn();
  const view = render(
    <AppSceneWorkspaceProvider
      location={{ pathname: "/chats/one", search: "" }}
      navigate={navigate}
    >
      <Tabs onCloseFile={onCloseFile} onSelectFile={onSelectFile} />
    </AppSceneWorkspaceProvider>,
  );
  return { ...view, navigate, onCloseFile, onSelectFile };
}

describe("页签批量关闭", () => {
  it("关闭其他激活右键目标，调用被关闭工作台页签的原清理入口", async () => {
    const { onCloseFile, onSelectFile, navigate } = renderTabs();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "打开设置" }));
    fireEvent.contextMenu(screen.getByRole("tab", { name: "文件甲" }));
    await user.click(
      await screen.findByRole("menuitem", { name: "关闭其他页签" }),
    );
    expect(screen.getAllByRole("tab")).toHaveLength(2);
    expect(screen.getByRole("tab", { name: "文件甲" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(
      screen.queryByRole("tab", { name: "文件乙" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "设置" })).not.toBeInTheDocument();
    expect(onCloseFile).toHaveBeenCalledExactlyOnceWith("file-two");
    expect(onSelectFile).toHaveBeenCalledExactlyOnceWith("file-one");
    expect(navigate).not.toHaveBeenCalled();
  });
});
