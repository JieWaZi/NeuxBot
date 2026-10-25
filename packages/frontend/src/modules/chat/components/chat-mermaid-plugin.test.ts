import { afterEach, expect, it, vi } from "vitest";

afterEach(() => {
  vi.doUnmock("@streamdown/mermaid");
  vi.resetModules();
});

it("普通 Markdown 不加载绘图引擎，实际绘图采用最新初始化配置", async () => {
  const render = vi.fn(async () => ({ svg: "<svg></svg>" }));
  const getMermaid = vi.fn(() => ({ render }));
  const load = vi.fn(() => ({ mermaid: { getMermaid } }));
  vi.doMock("@streamdown/mermaid", load);
  const { mermaid } = await import("./chat-mermaid-plugin");
  const diagram = mermaid.getMermaid({ theme: "default" });
  diagram.initialize({ theme: "dark" });
  expect(load).not.toHaveBeenCalled();

  await expect(
    diagram.render("diagram-1", "flowchart TD; A-->B"),
  ).resolves.toEqual({
    svg: "<svg></svg>",
  });
  expect(load).toHaveBeenCalledOnce();
  expect(getMermaid).toHaveBeenCalledWith({ theme: "dark" });
  expect(render).toHaveBeenCalledWith("diagram-1", "flowchart TD; A-->B");
});

it("绘图引擎加载失败交给现有图表错误界面处理", async () => {
  const failure = new Error("绘图引擎加载失败");
  vi.doMock("@streamdown/mermaid", () => {
    throw failure;
  });
  const { mermaid } = await import("./chat-mermaid-plugin");
  await expect(
    mermaid.getMermaid().render("diagram-1", "flowchart TD"),
  ).rejects.toMatchObject({ cause: failure });
});
