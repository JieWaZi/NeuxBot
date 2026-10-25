import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import { i18n } from "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";

afterEach(async () => {
  await i18n.changeLanguage("zh-CN");
});
it("启动只准备导航词，首次直读知识内容后中英文与回返均完整", async () => {
  expect(i18n.t("knowledge:knowledge")).toBe("知识库");
  expect(i18n.getResource("zh-CN", "knowledge", "rawEmpty")).toBeUndefined();
  const { useCopy } = await import("./use-copy");
  function Caption() {
    const t = useCopy();
    return <p>{t("rawEmpty")}</p>;
  }
  const view = render(<Caption />);
  expect(screen.getByText("还没有原始资料")).toBeInTheDocument();
  await i18n.changeLanguage("en-US");
  expect(await screen.findByText("No sources yet")).toBeInTheDocument();
  view.unmount();
  render(<Caption />);
  expect(screen.getByText("No sources yet")).toBeInTheDocument();
});

it("设置骨架可同步展示导航，不提前注册完整编辑器词典", async () => {
  const { KnowledgeLibrarySettingsLoading } =
    await import("./knowledge-library-settings-loading");
  expect(
    i18n.getResource("zh-CN", "knowledge", "libraryIdentifier"),
  ).toBeUndefined();
  render(<KnowledgeLibrarySettingsLoading close={() => {}} />);
  expect(screen.getByRole("navigation", { name: "知识库配置" })).toBeVisible();
  expect(screen.getByRole("button", { name: "基础信息" })).toBeVisible();
  expect(screen.getByRole("button", { name: "保存并关闭" })).toBeDisabled();
  await i18n.changeLanguage("en-US");
  expect(
    screen.getByRole("navigation", { name: "Knowledge base configuration" }),
  ).toBeVisible();
  expect(screen.getByRole("button", { name: "Save and close" })).toBeDisabled();
});

it("顶部图谱入口可切换视图，不提前加载图谱完整词典", async () => {
  const { KnowledgeLibraryActions } =
    await import("./knowledge-library-actions");
  expect(i18n.getResource("zh-CN", "knowledgeGraph", "canvas")).toBeUndefined();
  const change = vi.fn();
  render(
    <TooltipProvider>
      <KnowledgeLibraryActions
        onGraph={change}
        onUpload={() => {}}
        onOrganize={() => {}}
        organizeDisabled
        onSettings={() => {}}
      />
    </TooltipProvider>,
  );
  await userEvent.click(screen.getByRole("button", { name: "图谱" }));
  expect(change).toHaveBeenCalledOnce();
  await i18n.changeLanguage("en-US");
  expect(screen.getByRole("button", { name: "Graph" })).toBeVisible();
});
