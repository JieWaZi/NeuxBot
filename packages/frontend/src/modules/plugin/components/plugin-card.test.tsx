import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { PluginCard } from "./plugin-card";
import type { PluginBrowseEntry } from "../model/plugin-browse";

afterEach(cleanup);
const installed: PluginBrowseEntry = {
  sourceKey: "source:office",
  pluginId: "office",
  versionId: "old",
  catalogKey: "office",
  name: "办公",
  description: "实际安装内容",
  installedCount: 2,
};
function RouteState() {
  return <output>{JSON.stringify(useLocation().state)}</output>;
}
it("opens the actual installed resource and preserves search and partner", () => {
  render(
    <MemoryRouter initialEntries={["/resources?search=办公&partner=w1"]}>
      <PluginCard entry={installed} />
      <RouteState />
    </MemoryRouter>,
  );
  const link = screen.getByRole("link", { name: "查看插件 办公" });
  expect(link).toHaveAttribute("href", "/resources/plugins/office");
  expect(screen.getByText("实际安装内容")).toBeVisible();
  expect(screen.getByText("已安装")).toBeVisible();
  expect(screen.queryByRole("checkbox")).toBeNull();
  expect(screen.queryByRole("button", { name: /更多操作/ })).toBeNull();
  expect(screen.queryByText(/伙伴使用/)).toBeNull();
  fireEvent.click(link);
  expect(screen.getByRole("status")).toHaveTextContent(
    "/resources?search=办公&partner=w1",
  );
});
it("keeps the browse dark icon when the summary also provides a single-icon fallback", () => {
  const light = "data:image/png;base64,light";
  const dark = "data:image/png;base64,dark";
  render(
    <MemoryRouter>
      <PluginCard
        entry={{ ...installed, icon: light, icons: { default: light, dark } }}
      />
    </MemoryRouter>,
  );
  const images = screen.getByRole("article").querySelectorAll("img");
  expect(images).toHaveLength(2);
  expect(images[1]).toHaveAttribute("src", dark);
  expect(images[1]).toHaveClass("dark:block");
});
it("uses a separate text install action and routes uninstalled entries through the catalog", () => {
  const install = vi.fn();
  render(
    <MemoryRouter>
      <PluginCard
        entry={{ ...installed, pluginId: undefined, installedCount: 0 }}
        onInstall={install}
      />
    </MemoryRouter>,
  );
  const link = screen.getByRole("link", { name: "查看插件 办公" });
  const action = screen.getByRole("button", { name: "安装 办公" });
  expect(link).toHaveAttribute("href", "/resources/plugins/catalog/office");
  expect(action).toHaveTextContent("安装");
  expect(action.querySelector("svg")).not.toBeNull();
  expect(link).not.toContainElement(action);
  fireEvent.click(action);
  expect(install).toHaveBeenCalledOnce();
  expect(screen.queryByText("已安装")).toBeNull();
});
it("replaces installed status with the current update action", () => {
  const update = vi.fn();
  render(
    <MemoryRouter>
      <PluginCard
        entry={{ ...installed, update: { available: true } }}
        onUpdate={update}
      />
    </MemoryRouter>,
  );
  expect(screen.queryByText("已安装")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "更新 办公" }));
  expect(update).toHaveBeenCalledOnce();
});
it("keeps the update available when the installed version needs authorization", () => {
  const install = vi.fn();
  const update = vi.fn();
  render(
    <MemoryRouter>
      <PluginCard
        entry={{
          ...installed,
          requiresAuthorization: true,
          update: { available: true },
        }}
        onInstall={install}
        onUpdate={update}
      />
    </MemoryRouter>,
  );
  expect(screen.queryByText("已安装")).toBeNull();
  expect(screen.queryByRole("button", { name: "授权 办公" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "更新 办公" }));
  expect(update).toHaveBeenCalledOnce();
  expect(install).not.toHaveBeenCalled();
});
it.each([
  { wakerId: "w1", canUpdate: true },
  { wakerId: "w2", canUpdate: false },
])(
  "reflects the installation of partner $wakerId",
  ({ wakerId, canUpdate }) => {
    render(
      <MemoryRouter>
        <PluginCard
          detail={{
            plugin: { id: "office", name: "办公" },
            installations: [
              { wakerId: "w1", versionId: "old", enabled: false },
              { wakerId: "w2", versionId: "latest", enabled: true },
            ],
            update: { available: true, wakerIds: ["w1"] },
          }}
          wakerId={wakerId}
          onUpdate={vi.fn()}
        />
      </MemoryRouter>,
    );
    expect(Boolean(screen.queryByRole("button", { name: "更新 办公" }))).toBe(
      canUpdate,
    );
    expect(Boolean(screen.queryByText("已安装"))).toBe(!canUpdate);
  },
);
it("keeps partner frozen-version content and places maintenance behind its detail link", () => {
  render(
    <MemoryRouter>
      <PluginCard
        detail={{
          plugin: { id: "office", name: "办公" },
          resolvedVersionId: "new",
          installations: [{ wakerId: "w1", versionId: "old", enabled: false }],
          versions: [
            { id: "new", package: { manifest: { description: "准备内容" } } },
            {
              id: "old",
              package: { manifest: { description: "伙伴实际内容" } },
            },
          ],
        }}
        wakerId="w1"
      />
    </MemoryRouter>,
  );
  expect(screen.getByText("伙伴实际内容")).toBeVisible();
  expect(screen.queryByText("准备内容")).toBeNull();
  expect(screen.getByRole("link", { name: "查看插件 办公" })).toHaveAttribute(
    "href",
    "/wakers/w1/plugins/office",
  );
  expect(screen.queryByRole("button")).toBeNull();
  expect(screen.queryByRole("switch")).toBeNull();
});
it("does not claim a partner's prepared resource is installed", () => {
  render(
    <MemoryRouter>
      <PluginCard
        detail={{
          plugin: { id: "prepared", name: "待安装" },
          installations: [],
          versions: [
            { id: "v1", package: { manifest: { description: "已准备内容" } } },
          ],
        }}
        wakerId="w1"
      />
    </MemoryRouter>,
  );
  expect(screen.getByRole("link", { name: "查看插件 待安装" })).toHaveAttribute(
    "href",
    "/wakers/w1/plugins/prepared",
  );
  expect(screen.queryByText("已安装")).toBeNull();
  expect(screen.queryByRole("button")).toBeNull();
});
