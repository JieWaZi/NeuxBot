import { useLayoutEffect } from "react";
import {
  fireEvent,
  render as renderView,
  screen,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";
import type { ReactElement } from "react";
const render = (ui: ReactElement) =>
  renderView(ui, { wrapper: TooltipProvider });
import { SceneTabStrip } from "@/shared/ui/scene-tab-strip";
import { useAppSceneWorkspace } from "@/shared/workspace/app-scene-workspace";
import { AppSceneWorkspaceProvider } from "./app-scene-workspace-provider";

vi.mock("./route-scene-not-found", () => ({
  useRouteSceneNotFound: (tabs: unknown[]) => "0".repeat(tabs.length),
}));

function Tabs({
  onCloseFile,
  onSelectFile,
  onSelectSecondFile,
}: {
  onCloseFile: (id: string) => void;
  onSelectFile: (id: string) => void;
  onSelectSecondFile: (id: string) => void;
}) {
  const { snapshot, dispatch, syncWorkspace } = useAppSceneWorkspace();
  const firstOwnerOpen = snapshot.tabs.some((tab) => tab.id === "chat:one");
  useLayoutEffect(() => {
    if (!firstOwnerOpen) return;
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
  }, [firstOwnerOpen, onCloseFile, onSelectFile, syncWorkspace]);
  const secondOwnerOpen = snapshot.tabs.some((tab) => tab.id === "chat:two");
  useLayoutEffect(() => {
    if (!secondOwnerOpen) return;
    syncWorkspace({
      ownerTabId: "chat:two",
      ownerHref: "/chats/two",
      primaryWorkspaceTabId: "main",
      activeWorkspaceTabId: "main",
      onSelect: onSelectSecondFile,
      onClose: onCloseFile,
      tabs: [
        { id: "main", label: "第二会话" },
        { id: "third", label: "第二owner文件", closeable: true },
      ],
    });
  }, [onCloseFile, onSelectSecondFile, secondOwnerOpen, syncWorkspace]);
  return (
    <>
      <output data-testid="active-content">{snapshot.activeTabId ?? ""}</output>
      <button
        onClick={() =>
          dispatch({
            type: "open",
            tab: {
              id: "chat:two",
              label: "第二会话",
              href: "/chats/two",
              kind: "route",
              closeable: true,
            },
          })
        }
      >
        打开第二owner
      </button>
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
      {["chat:one", "chat:two"].map((ownerId) => {
        const tabs = snapshot.tabs.filter(
          (tab) => tab.id === ownerId || tab.ownerTabId === ownerId,
        );
        return tabs.length ? (
          <SceneTabStrip
            key={ownerId}
            ariaLabel={ownerId}
            activeTabId={snapshot.activeTabId ?? ""}
            tabs={tabs.map((tab) => ({
              ...tab,
              label: tab.customLabel ?? tab.label,
            }))}
            onSelect={(tabId) => dispatch({ type: "select", tabId })}
            onClose={(tabId) => dispatch({ type: "close", tabId })}
            onRename={(tabId, label) =>
              dispatch({ type: "rename-tab", tabId, label })
            }
            onCloseOthers={(tabId) =>
              dispatch({
                type: "close-others",
                tabId,
                candidateTabIds: tabs.map((tab) => tab.id),
              })
            }
          />
        ) : null;
      })}
      {snapshot.tabs.some((tab) => tab.id === "settings") ? (
        <>
          <button
            onClick={() => dispatch({ type: "select", tabId: "settings" })}
          >
            返回设置
          </button>
          <button
            onClick={() => dispatch({ type: "close", tabId: "settings" })}
          >
            关闭设置
          </button>
        </>
      ) : null}
    </>
  );
}

function renderTabs(pathname = "/chats/one", navigationState?: unknown) {
  const navigate = vi.fn();
  const onCloseFile = vi.fn();
  const onSelectFile = vi.fn();
  const onSelectSecondFile = vi.fn();
  const view = render(
    <AppSceneWorkspaceProvider
      location={{ pathname, search: "", state: navigationState }}
      navigate={navigate}
    >
      <Tabs
        onCloseFile={onCloseFile}
        onSelectFile={onSelectFile}
        onSelectSecondFile={onSelectSecondFile}
      />
    </AppSceneWorkspaceProvider>,
  );
  return { ...view, navigate, onCloseFile, onSelectFile, onSelectSecondFile };
}

describe("页签批量关闭", () => {
  it("右键重命名只修改目标页签，切换后仍保留名称", async () => {
    renderTabs();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "打开设置" }));
    fireEvent.contextMenu(screen.getByRole("tab", { name: "会话" }));
    expect(screen.getByRole("menuitem", { name: "重命名" })).toBeVisible();
    await user.click(screen.getByRole("menuitem", { name: "重命名" }));
    const input = await screen.findByRole("textbox", { name: "页签名称" });
    await user.clear(input);
    await user.type(input, "偏好配置{Enter}");
    expect(screen.getByRole("tab", { name: "偏好配置" })).toBeVisible();
    await user.click(screen.getByRole("tab", { name: "文件甲" }));
    await user.click(screen.getByRole("tab", { name: "偏好配置" }));
    expect(screen.getByRole("tab", { name: "偏好配置" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

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
    expect(screen.getByRole("button", { name: "返回设置" })).toBeVisible();
    expect(onCloseFile).toHaveBeenCalledExactlyOnceWith("file-two");
    expect(onSelectFile).toHaveBeenCalledExactlyOnceWith("file-one");
    expect(navigate).not.toHaveBeenCalled();
  });
});

it("同一 URL 的新导航激活主会话，并同步工作台回到主页签", async () => {
  const { navigate, onCloseFile, onSelectFile, onSelectSecondFile, rerender } =
    renderTabs();
  const user = userEvent.setup();
  await user.click(screen.getByRole("tab", { name: "文件甲" }));
  expect(screen.getByRole("tab", { name: "文件甲" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  onSelectFile.mockClear();
  rerender(
    <AppSceneWorkspaceProvider
      location={{
        pathname: "/chats/one",
        search: "",
        key: "repeat-navigation",
      }}
      navigate={navigate}
    >
      <Tabs
        onCloseFile={onCloseFile}
        onSelectFile={onSelectFile}
        onSelectSecondFile={onSelectSecondFile}
      />
    </AppSceneWorkspaceProvider>,
  );
  expect(onSelectFile).toHaveBeenCalledExactlyOnceWith("main");
  expect(screen.getByRole("tab", { name: "文件甲" })).toHaveAttribute(
    "aria-selected",
    "false",
  );
  expect(screen.getByRole("tab", { name: "会话" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
});

it("菜单选中文件恢复其owner，关闭owner联动清理原工作台内容", async () => {
  const { onSelectFile, onCloseFile } = renderTabs();
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "打开设置" }));
  await user.click(screen.getByRole("tab", { name: "文件甲" }));
  expect(onSelectFile).toHaveBeenCalledExactlyOnceWith("file-one");
  expect(screen.getByRole("tab", { name: "文件甲" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await user.click(screen.getByRole("button", { name: "关闭 会话" }));
  // 关闭 owner 由其 Scene 卸载释放资源，不额外逐个调用局部关闭。
  expect(onCloseFile).not.toHaveBeenCalled();
  expect(screen.queryAllByRole("tab")).toHaveLength(0);
  expect(screen.getByTestId("active-content")).toHaveTextContent("settings");
});

it("关闭活跃文件的owner时，沿用另一owner工作台的选择与路由协议", async () => {
  const { navigate, onSelectSecondFile, onCloseFile } = renderTabs();
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "打开第二owner" }));
  await user.click(screen.getByRole("tab", { name: "第二owner文件" }));
  await user.click(screen.getByRole("tab", { name: "文件甲" }));
  onSelectSecondFile.mockClear();
  navigate.mockClear();
  await user.click(screen.getByRole("button", { name: "关闭 会话" }));
  expect(onSelectSecondFile).toHaveBeenCalledExactlyOnceWith("third");
  expect(navigate).toHaveBeenCalledExactlyOnceWith("/chats/two");
  expect(onCloseFile).not.toHaveBeenCalled();
  expect(screen.getByRole("tab", { name: "第二owner文件" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
});

it("最后owner关闭不遗留活跃文件身份", async () => {
  renderTabs();
  const user = userEvent.setup();
  await user.click(screen.getByRole("tab", { name: "文件甲" }));
  await user.click(screen.getByRole("button", { name: "关闭 会话" }));
  expect(screen.queryAllByRole("tab")).toHaveLength(0);
  expect(screen.getByTestId("active-content")).toBeEmptyDOMElement();
});

it("关闭非活动owner保留当前设置且不额外选择或导航", async () => {
  const { navigate, onSelectFile, onCloseFile } = renderTabs();
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "打开设置" }));
  await user.click(screen.getByRole("button", { name: "关闭 会话" }));
  expect(screen.getByTestId("active-content")).toHaveTextContent("settings");
  expect(navigate).not.toHaveBeenCalled();
  expect(onSelectFile).not.toHaveBeenCalled();
  expect(onCloseFile).not.toHaveBeenCalled();
});

it("关闭唯一Settings清空active身份，不残留设置或工作台子项", async () => {
  const { onSelectFile, onCloseFile } = renderTabs("/settings/general");
  expect(screen.getByTestId("active-content")).toHaveTextContent("settings");
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "关闭设置" }));
  expect(screen.getByTestId("active-content")).toBeEmptyDOMElement();
  expect(screen.queryAllByRole("tab")).toHaveLength(0);
  expect(screen.queryByRole("button", { name: "返回设置" })).toBeNull();
  expect(onSelectFile).not.toHaveBeenCalled();
  expect(onCloseFile).not.toHaveBeenCalled();
});

it("关闭Settings沿MRU回退已保留聊天，并调用其原primary选择协议", async () => {
  const { onSelectFile } = renderTabs();
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "打开设置" }));
  await user.click(screen.getByRole("tab", { name: "会话" }));
  await user.click(screen.getByRole("button", { name: "返回设置" }));
  onSelectFile.mockClear();
  await user.click(screen.getByRole("button", { name: "关闭设置" }));
  expect(screen.getByTestId("active-content")).toHaveTextContent("chat:one");
  expect(screen.getByRole("tab", { name: "会话" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  expect(onSelectFile).toHaveBeenCalledExactlyOnceWith("main");
});

it("选择与关闭回退各自恢复目标Scene的导航state，不读取当前B的state", async () => {
  const aState = { projectBack: "/wakers/a/projects?search=alpha" };
  const bState = { resourceBack: "/resources?category=skills&search=beta" };
  const { navigate, onCloseFile, onSelectFile, onSelectSecondFile, rerender } =
    renderTabs("/chats/one", aState);
  rerender(
    <AppSceneWorkspaceProvider
      location={{ pathname: "/chats/two", search: "", state: bState }}
      navigate={navigate}
    >
      <Tabs
        onCloseFile={onCloseFile}
        onSelectFile={onSelectFile}
        onSelectSecondFile={onSelectSecondFile}
      />
    </AppSceneWorkspaceProvider>,
  );
  const user = userEvent.setup();
  await user.click(
    within(screen.getByRole("tablist", { name: "chat:one" })).getByRole("tab", {
      name: "会话",
    }),
  );
  expect(navigate).toHaveBeenLastCalledWith("/chats/one", aState);
  await user.click(
    within(screen.getByRole("tablist", { name: "chat:two" })).getByRole("tab", {
      name: "会话",
    }),
  );
  navigate.mockClear();
  await user.click(
    within(screen.getByRole("tablist", { name: "chat:two" })).getByRole(
      "button",
      { name: "关闭 会话" },
    ),
  );
  expect(navigate).toHaveBeenCalledExactlyOnceWith("/chats/one", aState);
});
