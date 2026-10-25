import { render, screen, cleanup, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { it, expect, vi, afterEach } from "vitest";
import "@/shared/i18n";
import { PluginCard } from "./plugin-card";
import type { PluginCatalogEntry, PluginDetail } from "../model/plugin-view";

afterEach(cleanup);
it("keeps a catalog name localized after preparation saves its English default", () => {
  render(
    <MemoryRouter>
      <PluginCard
        catalog={{
          key: "feishu-office",
          package: {
            manifest: { name: "feishu-office" },
            presentation: {
              displayName: "feishu-office",
              displayNames: { en: "Feishu Office", "zh-CN": "飞书办公" },
              localizedDescriptions: { "zh-CN": "协助办公" },
            },
          },
        }}
        detail={{ plugin: { id: "p", name: "Feishu Office" } }}
        onInstall={vi.fn()}
      />
    </MemoryRouter>,
  );
  expect(screen.getByRole("button", { name: "安装 飞书办公" })).toBeVisible();
  expect(screen.queryByText("Feishu Office")).not.toBeInTheDocument();
  expect(screen.getByText("协助办公")).toBeVisible();
});
it("keeps the saved card focused on description, partner usage and management", () => {
  render(
    <MemoryRouter>
      <PluginCard
        detail={{
          plugin: { id: "p", name: "飞书办公" },
          account: { available: true },
          installations: [{ wakerId: "w1", versionId: "v1" }],
          versions: [
            {
              id: "v1",
              version: "1.0.98",
              package: {
                manifest: {
                  name: "feishu",
                  author: { name: "Developer" },
                },
              },
            },
          ],
        }}
        onInstall={vi.fn()}
      />
    </MemoryRouter>,
  );
  expect(screen.getByText("1 个伙伴使用")).toBeVisible();
  const more = screen.getByRole("button", { name: "飞书办公更多操作" });
  fireEvent.pointerDown(more, { button: 0, ctrlKey: false });
  expect(screen.getByRole("menuitem", { name: "查看详情" })).toHaveAttribute(
    "href",
    "/resources/plugins/p",
  );
  expect(screen.queryByText("已授权")).not.toBeInTheDocument();
  expect(screen.queryByText("Developer")).not.toBeInTheDocument();
  expect(screen.queryByText(/1.0.98/)).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: /安装/ }),
  ).not.toBeInTheDocument();
});

it.each([
  [false, false, "安装 飞书办公"],
  [true, false, "授权 飞书办公"],
  [true, true, "安装 飞书办公"],
])(
  "catalog action follows actual installation and shared authorization (%s, %s)",
  (installed, available, label) => {
    const pkg: NonNullable<PluginCatalogEntry["package"]> = {
      manifest: { name: "office" },
      presentation: { displayName: "飞书办公" },
    };
    Object.assign(pkg.manifest ?? {}, {
      extensions: { "com.ally": { provider: "feishu" } },
    });
    const detail: PluginDetail = {
      installations: installed
        ? [{ wakerId: "already-installed-partner" }]
        : [],
      account: { available, checkedAt: 123 },
      versions: [{ package: pkg }],
    };
    render(
      <MemoryRouter>
        <PluginCard
          catalog={{ key: "office", package: pkg }}
          detail={detail}
          onInstall={vi.fn()}
        />
      </MemoryRouter>,
    );
    expect(screen.getByRole("button", { name: label })).toBeVisible();
    if (available || !installed)
      expect(screen.queryByText("授权")).not.toBeInTheDocument();
  },
);

it("shows a separate update action beside the disabled partner switch and retains installed content", () => {
  const onUpdate = vi.fn();
  render(
    <MemoryRouter>
      <PluginCard
        detail={{
          plugin: { id: "p", name: "办公" },
          resolvedVersionId: "old",
          versions: [
            {
              id: "prepared",
              package: { manifest: { description: "尚未安装的新内容" } },
            },
            {
              id: "old",
              package: { manifest: { description: "实际安装的内容" } },
            },
          ],
          installations: [{ wakerId: "w1", versionId: "old", enabled: false }],
          update: { available: true, wakerIds: ["w1"] },
        }}
        wakerId="w1"
        onInstall={vi.fn()}
        onToggle={vi.fn()}
        onUpdate={onUpdate}
      />
    </MemoryRouter>,
  );
  expect(screen.getByText("实际安装的内容")).toBeVisible();
  expect(screen.queryByText("尚未安装的新内容")).not.toBeInTheDocument();
  expect(screen.getByRole("switch")).not.toBeChecked();
  fireEvent.click(screen.getByRole("button", { name: "可更新" }));
  expect(onUpdate).toHaveBeenCalledOnce();
});

it("uses the resolved installed version for a global card instead of a prepared version", () => {
  render(
    <MemoryRouter>
      <PluginCard
        detail={{
          plugin: { id: "p", name: "办公" },
          resolvedVersionId: "old",
          versions: [
            {
              id: "prepared",
              package: { manifest: { description: "准备内容" } },
            },
            { id: "old", package: { manifest: { description: "已安装内容" } } },
          ],
        }}
        onInstall={vi.fn()}
      />
    </MemoryRouter>,
  );
  expect(screen.getByText("已安装内容")).toBeVisible();
});
