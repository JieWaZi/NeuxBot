import type {
  WorkspaceFileOpenRequest,
  WorkspaceFileOpenTarget,
} from "@neuxbot/platform-contracts/workspace-file-editors";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { WorkspaceFileOpenActions } from "./workspace-file-open-actions";

const { listWorkspaceFileOpenTargets, openWorkspaceFileTarget, theme } =
  vi.hoisted(() => ({
    theme: { resolvedTheme: "light" },
    listWorkspaceFileOpenTargets:
      vi.fn<() => Promise<WorkspaceFileOpenTarget[]>>(),
    openWorkspaceFileTarget:
      vi.fn<(input: WorkspaceFileOpenRequest) => Promise<boolean>>(),
  }));
vi.mock("@/shared/platform", () => ({
  listWorkspaceFileOpenTargets,
  openWorkspaceFileTarget,
}));

vi.mock("@/shared/theme", () => ({
  useTheme: () => theme,
}));

beforeEach(() => {
  theme.resolvedTheme = "light";
  vi.spyOn(navigator, "platform", "get").mockReturnValue("Linux x86_64");
  const storage = new Map<string, string>();
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
  });
  listWorkspaceFileOpenTargets.mockReset();
  openWorkspaceFileTarget.mockReset().mockResolvedValue(true);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function renderOpenActions(hasFile = true) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <WorkspaceFileOpenActions
          active
          chatId="chat-42"
          root={hasFile ? "/workspace/repo" : undefined}
          relativePath={hasFile ? "src/main.ts" : undefined}
          line={12}
        />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

it("选择编辑器更新分组按钮，点击主按钮才打开当前文件", async () => {
  listWorkspaceFileOpenTargets.mockResolvedValue([
    "vscode",
    "zed",
    "file-manager",
  ]);
  const user = userEvent.setup();
  const { unmount } = renderOpenActions();
  const openVSCode = await screen.findByRole("button", {
    name: "在 VS Code 中打开",
  });
  const initialIcon = openVSCode.querySelector("img")?.getAttribute("src");
  const choose = screen.getByRole("button", { name: "选择外部编辑器" });
  for (const button of [openVSCode, choose]) {
    expect(button).toHaveClass("border-button-outline");
    expect(button).not.toHaveClass("bg-button-fill");
  }

  await user.click(choose);
  const menu = screen.getByRole("menu");
  expect(
    within(menu).getByRole("menuitemradio", { name: "VS Code", checked: true }),
  ).toBeVisible();
  await user.click(within(menu).getByRole("menuitemradio", { name: "Zed" }));
  const openZed = screen.getByRole("button", { name: "在 Zed 中打开" });
  expect(openZed.querySelector("img")?.getAttribute("src")).not.toBe(
    initialIcon,
  );
  expect(openWorkspaceFileTarget).not.toHaveBeenCalled();
  expect(within(screen.getByRole("group")).getAllByRole("button")).toHaveLength(
    2,
  );

  unmount();
  renderOpenActions();
  await user.click(
    await screen.findByRole("button", { name: "在 Zed 中打开" }),
  );
  expect(openWorkspaceFileTarget).toHaveBeenCalledExactlyOnceWith({
    chatId: "chat-42",
    root: "/workspace/repo",
    relativePath: "src/main.ts",
    target: "zed",
    line: 12,
  });
});

it("路径留空时可以选择外部编辑器，选中文件后才可打开", async () => {
  listWorkspaceFileOpenTargets.mockResolvedValue(["vscode", "zed"]);
  const user = userEvent.setup();
  renderOpenActions(false);

  const open = await screen.findByRole("button", { name: "在 VS Code 中打开" });
  expect(open).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "选择外部编辑器" }));
  await user.click(screen.getByRole("menuitemradio", { name: "Zed" }));
  expect(screen.getByRole("button", { name: "在 Zed 中打开" })).toBeDisabled();
  expect(openWorkspaceFileTarget).not.toHaveBeenCalled();
});

it("已选编辑器未被检测到时只提供文件管理器", async () => {
  localStorage.setItem("bot.workspace-file-open-target", "cursor");
  listWorkspaceFileOpenTargets.mockResolvedValue(["file-manager"]);
  const user = userEvent.setup();
  renderOpenActions();

  const reveal = await screen.findByRole("button", {
    name: "在文件管理器中显示",
  });
  await user.click(screen.getByRole("button", { name: "选择外部编辑器" }));
  const options = within(screen.getByRole("menu")).getAllByRole(
    "menuitemradio",
  );
  expect(options).toHaveLength(1);
  expect(options[0]).toHaveTextContent("文件管理器");
  expect(options[0]?.querySelector("img")).toBeNull();
  expect(options[0]).toHaveAttribute("aria-checked", "true");
  await user.keyboard("{Escape}");
  await user.click(reveal);
  expect(openWorkspaceFileTarget).toHaveBeenCalledExactlyOnceWith({
    chatId: "chat-42",
    root: "/workspace/repo",
    relativePath: "src/main.ts",
    target: "file-manager",
    line: 12,
  });
});

it.each([
  ["MacIntel", "Finder", "finder"],
  ["Win32", "文件资源管理器", "file-explorer"],
])("%s 使用系统文件管理器的名称与图标", async (platform, name, icon) => {
  vi.spyOn(navigator, "platform", "get").mockReturnValue(platform);
  listWorkspaceFileOpenTargets.mockResolvedValue(["file-manager"]);
  const user = userEvent.setup();
  renderOpenActions();

  const reveal = await screen.findByRole("button", {
    name: `在${name}中显示`,
  });
  const asset = new RegExp(`/editor-logos/${icon}\\.svg(?:\\?no-inline)?$`);
  expect(reveal.querySelector("img")?.getAttribute("src")).toMatch(asset);
  await user.click(screen.getByRole("button", { name: "选择外部编辑器" }));
  const manager = within(screen.getByRole("menu")).getByRole("menuitemradio", {
    name,
    checked: true,
  });
  expect(manager.querySelector("img")?.getAttribute("src")).toMatch(asset);
  await user.keyboard("{Escape}");
  await user.click(reveal);
  expect(openWorkspaceFileTarget).toHaveBeenCalledExactlyOnceWith({
    chatId: "chat-42",
    root: "/workspace/repo",
    relativePath: "src/main.ts",
    target: "file-manager",
    line: 12,
  });
});

it("主按钮和菜单使用各编辑器的品牌标志", async () => {
  listWorkspaceFileOpenTargets.mockResolvedValue([
    "vscode",
    "vscode-insiders",
    "vscodium",
    "cursor",
    "zed",
  ]);
  const user = userEvent.setup();
  renderOpenActions();

  const main = await screen.findByRole("button", { name: "在 VS Code 中打开" });
  expect(main.querySelector("img")).toHaveAttribute(
    "src",
    expect.stringMatching(/\/vscode\.svg(?:\?no-inline)?$/),
  );
  await user.click(screen.getByRole("button", { name: "选择外部编辑器" }));
  const menu = within(screen.getByRole("menu"));
  const brands = [
    ["VS Code", "vscode"],
    ["VS Code Insiders", "vscode"],
    ["VSCodium", "vscodium"],
    ["Cursor", "cursor"],
    ["Zed", "zed"],
  ];
  for (const [name, file] of brands) {
    const logo = menu.getByRole("menuitemradio", { name }).querySelector("img");
    expect(logo?.getAttribute("src")).toMatch(
      new RegExp(`/editor-logos/${file}\\.svg(?:\\?no-inline)?$`),
    );
    expect(logo).toHaveAttribute("alt", "");
  }
});

it("暗色主题选用对应的本地 Cursor 与 Zed 标志", async () => {
  theme.resolvedTheme = "dark";
  listWorkspaceFileOpenTargets.mockResolvedValue(["cursor", "zed"]);
  const user = userEvent.setup();
  renderOpenActions();

  const main = await screen.findByRole("button", { name: "在 Cursor 中打开" });
  expect(main.querySelector("img")?.getAttribute("src")).toMatch(
    /\/cursor-dark\.svg(?:\?no-inline)?$/,
  );
  await user.click(screen.getByRole("button", { name: "选择外部编辑器" }));
  const zed = within(screen.getByRole("menu")).getByRole("menuitemradio", {
    name: "Zed",
  });
  expect(zed.querySelector("img")?.getAttribute("src")).toMatch(
    /\/zed-dark\.svg(?:\?no-inline)?$/,
  );
});
