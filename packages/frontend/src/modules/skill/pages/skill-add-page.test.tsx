import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { uploadFileBytesLimit } from "@neuxbot/platform-contracts/upload-byte-limits";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { SkillAddPage } from "./skill-add-page";

const fixture = vi.hoisted(() => ({
  scan: vi.fn<(input: unknown) => Promise<unknown>>(),
  save: vi.fn<(input: unknown) => Promise<unknown>>(),
  install: vi.fn<(input: unknown) => Promise<unknown>>(),
  createPlugin: vi.fn<(input: unknown) => Promise<unknown>>(),
  selectFiles: vi.fn<() => Promise<string[]>>(),
  stageAttachment: vi.fn<(file: File) => Promise<string>>(),
  discardAttachment: vi.fn<(path: string) => Promise<void>>(),
}));
vi.mock("@/shared/platform", () => ({
  selectFiles: fixture.selectFiles,
  stageAttachment: fixture.stageAttachment,
  discardAttachment: fixture.discardAttachment,
}));
vi.mock("@/modules/waker", () => ({
  WakerDetailLayout: ({ children }: { children: React.ReactNode }) => (
    <main>{children}</main>
  ),
}));
vi.mock("@/shared/api/generated/@tanstack/react-query.gen", () => ({
  scanLibrarySkillImportMutation: () => ({ mutationFn: fixture.scan }),
  importLibrarySkillsMutation: () => ({ mutationFn: fixture.save }),
  installLibrarySkillMutation: () => ({ mutationFn: fixture.install }),
  createPluginFromSkillsMutation: () => ({ mutationFn: fixture.createPlugin }),
}));
vi.mock("@/modules/plugin", () => ({
  invalidatePluginQueries: async () => undefined,
  PluginInstallDialog: () => <div role="dialog" aria-label="安装插件" />,
}));
vi.mock("../api/skill-queries", () => ({
  invalidateVisibleSkillQueries: async () => undefined,
}));

function Destination() {
  const location = useLocation();
  return <p>{location.pathname + location.search}</p>;
}
function renderPage(href = "/resources/skills/new") {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { mutations: { retry: false } } })
      }
    >
      <MemoryRouter initialEntries={[href]}>
        <Routes>
          <Route path="resources/skills/new" element={<SkillAddPage />} />
          <Route
            path="wakers/:wakerId/resources/skills/new"
            element={<SkillAddPage />}
          />
          <Route path="*" element={<Destination />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
async function scanGit() {
  fireEvent.change(screen.getByRole("textbox", { name: "GitHub 地址" }), {
    target: { value: "https://github.com/example/skills.git" },
  });
  fireEvent.click(screen.getByRole("button", { name: "扫描并继续" }));
  await screen.findByText("review");
}
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  fixture.scan.mockResolvedValue({
    token: "scan-1",
    candidates: [
      { id: "candidate-1", name: "review", description: "代码审查" },
      { id: "candidate-2", name: "writing", description: "技术写作" },
    ],
  });
  fixture.save.mockResolvedValue([
    { id: "skill-1", name: "review" },
    { id: "skill-2", name: "writing" },
  ]);
  fixture.install.mockResolvedValue({ success: true });
  fixture.createPlugin.mockReset().mockResolvedValue({
    plugin: { id: "plugin-1" },
    resolvedVersionId: "version-1",
  });
  fixture.selectFiles.mockResolvedValue(["/tmp/skills.zip"]);
  fixture.stageAttachment.mockResolvedValue("upload:dropped-skills");
  fixture.discardAttachment.mockResolvedValue(undefined);
});

describe("SkillAddPage", () => {
  it("uses the plugin name limit in Unicode characters and blocks an oversized name before submitting", async () => {
    renderPage();
    await scanGit();
    fireEvent.click(screen.getByRole("button", { name: "添加为插件" }));
    const name = screen.getByRole("textbox", { name: "插件名称" });
    fireEvent.change(name, { target: { value: "😀".repeat(120) } });
    expect(screen.getByRole("button", { name: "创建插件" })).toBeEnabled();
    fireEvent.change(name, { target: { value: "😀".repeat(121) } });
    expect(screen.getByRole("button", { name: "创建插件" })).toBeDisabled();
    expect(name).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByRole("alert")).toHaveTextContent(
      "插件名称不能超过 120 个字符。",
    );
    expect(fixture.createPlugin).not.toHaveBeenCalled();
  });

  it("offers plugin creation only for multiple selected skills and preserves the same selection and draft when cancelled", async () => {
    renderPage();
    expect(
      screen.queryByRole("button", { name: "添加为插件" }),
    ).not.toBeInTheDocument();
    await scanGit();
    fireEvent.click(screen.getByRole("checkbox", { name: "选择 writing" }));
    expect(
      screen.queryByRole("button", { name: "添加为插件" }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("checkbox", { name: "选择 writing" }));
    fireEvent.click(screen.getByRole("button", { name: "添加为插件" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("radiogroup", { name: "技能来源" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "创建插件" })).toBeDisabled();
    fireEvent.change(screen.getByRole("textbox", { name: "插件名称" }), {
      target: { value: "研发工具箱" },
    });
    fireEvent.change(screen.getByRole("textbox", { name: "说明（可选）" }), {
      target: { value: "代码审查与写作" },
    });
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(screen.getByRole("textbox", { name: "GitHub 地址" })).toHaveValue(
      "https://github.com/example/skills.git",
    );
    expect(screen.getByRole("checkbox", { name: "选择 review" })).toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: "选择 writing" }),
    ).toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: "添加为插件" }));
    expect(screen.getByRole("textbox", { name: "插件名称" })).toHaveValue(
      "研发工具箱",
    );
    expect(screen.getByRole("textbox", { name: "说明（可选）" })).toHaveValue(
      "代码审查与写作",
    );
    expect(fixture.scan).toHaveBeenCalledOnce();
    expect(fixture.createPlugin).not.toHaveBeenCalled();
    expect(fixture.save).not.toHaveBeenCalled();
  });

  // jsdom 验证滚动与底栏的结构边界；真实遮挡和窄窗口另做平台验收。
  it.each(["git", "zip"])(
    "%s 添加技能的底栏位于正文之外，长列表选择、重置和提交仍可操作",
    async (source) => {
      fixture.scan.mockResolvedValueOnce({
        token: "long-scan",
        candidates: Array.from({ length: 15 }, (_, index) => ({
          id: `candidate-${index}`,
          name: `skill-${index}`,
          description: `说明 ${index}`,
        })),
      });
      const { container } = renderPage(
        `/resources/skills/new?source=${source}`,
      );
      expect(screen.queryByLabelText("添加状态")).not.toBeInTheDocument();
      const prepareSource = async () => {
        if (source === "zip") {
          fireEvent.click(
            screen.getByRole("button", { name: "选择 ZIP 压缩包" }),
          );
          await screen.findByText("skills.zip");
        } else {
          fireEvent.change(
            screen.getByRole("textbox", { name: "GitHub 地址" }),
            {
              target: { value: "https://github.com/example/skills.git" },
            },
          );
        }
      };
      await prepareSource();
      const surface = container.querySelector(
        '[data-slot="resource-page-surface"]',
      );
      const viewport = surface?.querySelector(
        '[data-slot="resource-page-viewport"]',
      );
      const footerHost = surface?.querySelector(
        '[data-slot="resource-page-footer-host"]',
      );
      const form = viewport?.querySelector("form");
      const footer = screen.getByLabelText("添加状态");
      expect(footer.parentElement).toBe(footerHost);
      expect(footerHost?.parentElement).toBe(surface);
      expect(viewport?.contains(footer)).toBe(false);
      expect(footer).not.toHaveClass("sticky", "fixed", "bottom-0");
      expect(
        screen.getByRole("button", { name: "扫描并继续" }),
      ).toHaveAttribute("form", form?.id);
      fireEvent.click(screen.getByRole("button", { name: "重置" }));
      expect(screen.queryByLabelText("添加状态")).not.toBeInTheDocument();
      expect(fixture.scan).not.toHaveBeenCalled();
      await prepareSource();
      fireEvent.click(screen.getByRole("button", { name: "扫描并继续" }));
      await screen.findByRole("checkbox", { name: "选择 skill-14" });
      const candidates = screen.getByRole("region", { name: "识别到的技能" });
      expect(viewport?.contains(candidates)).toBe(true);
      expect(footerHost?.contains(candidates)).toBe(false);
      expect(within(candidates).getAllByRole("checkbox")).toHaveLength(15);
      expect(screen.getByLabelText("添加状态").parentElement).toBe(footerHost);
      fireEvent.click(screen.getByRole("checkbox", { name: "选择 skill-14" }));
      const submit = screen.getByRole("button", { name: "添加 14 个技能" });
      expect(submit).toHaveAttribute("form", form?.id);
      fireEvent.click(submit);
      await screen.findByText("/resources?category=skills");
      expect(fixture.scan).toHaveBeenCalledOnce();
      expect(fixture.save).toHaveBeenCalledOnce();
      expect(fixture.save.mock.calls[0]?.[0]).toMatchObject({
        body: {
          token: "long-scan",
          candidates: Array.from({ length: 14 }, (_, index) => ({
            id: `candidate-${index}`,
            version: "1.0.0",
            description: `说明 ${index}`,
          })),
        },
      });
    },
  );

  it("scans and adds selected resources, returning to the original category and search", async () => {
    const back =
      "/resources?category=skills&skillCategory=%E5%BC%80%E5%8F%91&search=review";
    renderPage(
      `/resources/skills/new?${new URLSearchParams({ returnTo: back })}`,
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
    expect(
      screen.getByRole("heading", { level: 1, name: "添加技能" }),
    ).toBeVisible();
    const source = screen.getByRole("radiogroup", { name: "技能来源" });
    expect(
      screen.getAllByRole("radiogroup", { name: "技能来源" }),
    ).toHaveLength(1);
    expect(source.closest("header")).toContainElement(
      screen.getByRole("heading", { level: 1, name: "添加技能" }),
    );
    expect(source.closest("form")).toBeNull();
    expect(
      within(screen.getByRole("navigation", { name: "技能" })).getByText(
        "添加技能",
      ),
    ).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: "技能" })).toHaveAttribute(
      "href",
      back,
    );
    expect(
      screen.queryByRole("button", { name: "扫描并继续" }),
    ).not.toBeInTheDocument();
    await scanGit();
    expect(fixture.scan).toHaveBeenCalledWith(
      expect.objectContaining({
        body: {
          kind: "git",
          location: "https://github.com/example/skills.git",
        },
      }),
      expect.anything(),
    );
    fireEvent.click(screen.getByRole("checkbox", { name: "选择 writing" }));
    fireEvent.click(screen.getByRole("button", { name: "添加 1 个技能" }));
    await screen.findByText(back);
    expect(fixture.save).toHaveBeenCalledWith(
      expect.objectContaining({
        body: {
          token: "scan-1",
          candidates: [
            { id: "candidate-1", version: "1.0.0", description: "代码审查" },
          ],
        },
      }),
      expect.anything(),
    );
    expect(fixture.install).not.toHaveBeenCalled();
  });

  it("keeps added library resources when partner installation fails and retries only failed installs", async () => {
    fixture.install.mockRejectedValueOnce(new Error("temporarily unavailable"));
    renderPage("/wakers/waker-1/resources/skills/new");
    await scanGit();
    fireEvent.click(screen.getByRole("button", { name: "添加 2 个技能" }));
    await screen.findByRole("button", { name: "重试安装到伙伴" });
    expect(
      screen.getByText("技能已保存在技能库，以下技能尚未安装到当前伙伴。"),
    ).toBeVisible();
    expect(screen.getByRole("radio", { name: "GitHub" })).toBeDisabled();
    expect(screen.getByRole("radio", { name: "ZIP" })).toBeDisabled();
    expect(screen.getByRole("link", { name: "review" })).toHaveAttribute(
      "href",
      "/resources/skills/skill-1",
    );
    expect(
      screen.queryByRole("link", { name: "writing" }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "重试安装到伙伴" }));
    await screen.findByText("/wakers/waker-1/resources?category=skills");
    expect(fixture.save).toHaveBeenCalledTimes(1);
    expect(fixture.install).toHaveBeenCalledTimes(3);
    expect(fixture.install.mock.calls[2]?.[0]).toMatchObject({
      path: { skillId: "skill-1" },
      body: { wakerIds: ["waker-1"] },
    });
  });

  it("uses the ZIP upload path and preserves candidates on a failed add", async () => {
    fixture.save.mockRejectedValueOnce(new Error("temporary failure"));
    renderPage();
    fireEvent.click(screen.getByRole("radio", { name: "ZIP" }));
    fireEvent.click(screen.getByRole("button", { name: "选择 ZIP 压缩包" }));
    await screen.findByText("skills.zip");
    fireEvent.click(screen.getByRole("button", { name: "扫描并继续" }));
    await screen.findByText("review");
    expect(fixture.scan.mock.calls[0]?.[0]).toMatchObject({
      body: { kind: "zip", location: "/tmp/skills.zip" },
    });
    fireEvent.click(screen.getByRole("button", { name: "添加 2 个技能" }));
    await screen.findByRole("alert");
    expect(screen.getByRole("checkbox", { name: "选择 review" })).toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: "添加 2 个技能" }));
    await screen.findByText("/resources?category=skills");
    expect(fixture.save).toHaveBeenCalledTimes(2);
  });

  it("retries a failed scan and offers a different source when no skills are found", async () => {
    fixture.scan.mockRejectedValueOnce(new Error("unavailable"));
    fixture.scan.mockResolvedValueOnce({ token: "empty-scan", candidates: [] });
    renderPage();
    fireEvent.change(screen.getByRole("textbox", { name: "GitHub 地址" }), {
      target: { value: "https://github.com/example/empty.git" },
    });
    fireEvent.click(screen.getByRole("button", { name: "扫描并继续" }));
    await screen.findByRole("alert");
    expect(screen.getByRole("textbox", { name: "GitHub 地址" })).toHaveValue(
      "https://github.com/example/empty.git",
    );
    fireEvent.click(screen.getByRole("button", { name: "扫描并继续" }));
    await screen.findByText("没有发现技能");
    expect(
      screen.getByRole("button", { name: "添加 0 个技能" }),
    ).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "更换来源" }));
    expect(screen.getByRole("textbox", { name: "GitHub 地址" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "扫描并继续" })).toBeEnabled();
  });

  it("rejects a return address outside the resource list", () => {
    renderPage("/resources/skills/new?returnTo=https://example.com/unsafe");
    expect(screen.getByRole("link", { name: "技能" })).toHaveAttribute(
      "href",
      "/resources?category=skills",
    );
    fireEvent.click(screen.getByRole("button", { name: "返回技能列表" }));
    expect(screen.getByText("/resources?category=skills")).toBeVisible();
  });

  it.each([
    [
      "/resources/skills/new",
      "/resources?category=skills&skillCategory=development&search=review",
    ],
    [
      "/wakers/waker-1/resources/skills/new",
      "/wakers/waker-1/resources?category=skills&skillCategory=writing&search=draft",
    ],
  ])(
    "returns from the shared page header at %s with the list context intact",
    (path, back) => {
      renderPage(`${path}?${new URLSearchParams({ returnTo: back })}`);
      fireEvent.click(screen.getByRole("button", { name: "返回技能列表" }));
      expect(screen.getByText(back)).toBeVisible();
      expect(fixture.scan).not.toHaveBeenCalled();
      expect(fixture.save).not.toHaveBeenCalled();
    },
  );

  it("keeps the detected-skills surface visible before scanning, while scanning, and after an empty result", async () => {
    let finish: ((value: unknown) => void) | undefined;
    fixture.scan.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    renderPage();
    const source = screen.getByRole("radiogroup", { name: "技能来源" });
    expect(source).toHaveClass("w-fit");
    expect(source).not.toHaveClass("w-full");
    const region = screen.getByRole("region", { name: "识别到的技能" });
    expect(within(region).getByText("等待扫描")).toBeVisible();
    fireEvent.change(screen.getByRole("textbox", { name: "GitHub 地址" }), {
      target: { value: "https://github.com/example/empty.git" },
    });
    fireEvent.click(screen.getByRole("button", { name: "扫描并继续" }));
    await waitFor(() => expect(region).toHaveAttribute("aria-busy", "true"));
    expect(
      within(region).getByRole("status", { name: "正在扫描…" }),
    ).toHaveAttribute("aria-busy", "true");
    expect(screen.getByRole("radio", { name: "GitHub" })).toBeDisabled();
    expect(screen.getByRole("radio", { name: "ZIP" })).toBeDisabled();
    await act(async () => finish?.({ token: "empty", candidates: [] }));
    expect(region).toHaveAttribute("aria-busy", "false");
    expect(within(region).getByText("没有发现技能")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "添加 0 个技能" }),
    ).toBeDisabled();
  });

  it("stages a browser ZIP drop before scanning its upload reference", async () => {
    renderPage("/resources/skills/new?source=zip");
    const file = new File(["zip-bytes"], "browser-skills.zip", {
      type: "application/zip",
    });
    const target = screen.getByRole("button", { name: "选择 ZIP 压缩包" });
    fireEvent.dragOver(target, {
      dataTransfer: { types: ["Files"], dropEffect: "none" },
    });
    fireEvent.drop(target, { dataTransfer: { files: [file] } });
    expect(await screen.findByText("browser-skills.zip")).toBeVisible();
    expect(fixture.stageAttachment).toHaveBeenCalledWith(file);
    expect(fixture.selectFiles).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "扫描并继续" }));
    await screen.findByText("review");
    expect(fixture.scan.mock.calls[0]?.[0]).toMatchObject({
      body: { kind: "zip", location: "upload:dropped-skills" },
    });
    cleanup();
    expect(fixture.discardAttachment).not.toHaveBeenCalled();
  });

  it("requires a fresh ZIP after changing a source whose upload was consumed by scanning", async () => {
    fixture.selectFiles.mockResolvedValueOnce(["upload:skills-zip"]);
    renderPage("/resources/skills/new?source=zip");
    fireEvent.click(screen.getByRole("button", { name: "选择 ZIP 压缩包" }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "扫描并继续" })).toBeEnabled(),
    );
    fireEvent.click(screen.getByRole("button", { name: "扫描并继续" }));
    await screen.findByText("review");
    fireEvent.click(screen.getByRole("button", { name: "更换来源" }));
    expect(
      screen.queryByRole("button", { name: "扫描并继续" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "选择 ZIP 压缩包" }),
    ).toBeEnabled();
    expect(fixture.scan).toHaveBeenCalledOnce();
  });

  it.each(["format", "size", "multiple"])(
    "rejects an invalid ZIP drop (%s) before staging bytes",
    async (reason) => {
      renderPage("/resources/skills/new?source=zip");
      const file = new File(
        ["bytes"],
        reason === "format" ? "skills.txt" : "skills.zip",
      );
      if (reason === "size")
        Object.defineProperty(file, "size", {
          value: uploadFileBytesLimit + 1,
        });
      fireEvent.drop(screen.getByRole("button", { name: "选择 ZIP 压缩包" }), {
        dataTransfer: { files: reason === "multiple" ? [file, file] : [file] },
      });
      expect(await screen.findByRole("alert")).toHaveTextContent(
        reason === "format"
          ? "请选择 ZIP 格式的压缩包。"
          : reason === "size"
            ? "上传的压缩包不能超过 64 MiB。"
            : "请一次选择一个 ZIP 压缩包。",
      );
      expect(fixture.stageAttachment).not.toHaveBeenCalled();
      expect(fixture.scan).not.toHaveBeenCalled();
      expect(screen.getByRole("button", { name: "扫描并继续" })).toBeDisabled();
    },
  );

  it("releases a browser upload that finishes after leaving the page", async () => {
    let finish: ((path: string) => void) | undefined;
    fixture.stageAttachment.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const view = renderPage("/resources/skills/new?source=zip");
    const file = new File(["bytes"], "skills.zip");
    fireEvent.drop(screen.getByRole("button", { name: "选择 ZIP 压缩包" }), {
      dataTransfer: { files: [file] },
    });
    expect(await screen.findByText("正在上传压缩包…")).toBeVisible();
    expect(screen.getByRole("radio", { name: "GitHub" })).toBeDisabled();
    view.unmount();
    await act(async () => finish?.("upload:late-file"));
    expect(fixture.discardAttachment).toHaveBeenCalledWith("upload:late-file");
    expect(fixture.scan).not.toHaveBeenCalled();
  });

  it("releases an unscanned ZIP upload when switching its header source back to GitHub", async () => {
    renderPage("/resources/skills/new?source=zip");
    fireEvent.drop(screen.getByRole("button", { name: "选择 ZIP 压缩包" }), {
      dataTransfer: { files: [new File(["bytes"], "skills.zip")] },
    });
    await screen.findByText("skills.zip");
    fireEvent.click(screen.getByRole("radio", { name: "GitHub" }));
    expect(screen.getByRole("textbox", { name: "GitHub 地址" })).toHaveValue(
      "",
    );
    expect(fixture.discardAttachment).toHaveBeenCalledExactlyOnceWith(
      "upload:dropped-skills",
    );
    expect(fixture.scan).not.toHaveBeenCalled();
    cleanup();
    expect(fixture.discardAttachment).toHaveBeenCalledTimes(1);
  });
});
