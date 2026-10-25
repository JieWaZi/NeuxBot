import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { ApiError } from "@/shared/api/errors";
import { KnowledgeLibraryEditor } from "./knowledge-library-editor";
import {
  createKnowledgeEditorController,
  type KnowledgeEditorCommand,
} from "./knowledge-editor-controller";
import { emptyLibrary } from "./knowledge-model";

vi.mock("@/shared/hooks/use-execution-selection", () => ({
  useExecutionSelection: ({ value }: { value: object }) => ({
    selection: {
      runtimeId: "codex",
      accountId: "system",
      model: "model-a",
      permissionLevel: "default",
      ...value,
      reasoning: ("reasoning" in value && value.reasoning) || "low",
    },
    runtimes: [{ id: "codex", name: "Codex" }],
    accounts: [],
    models: [
      { id: "model-a", name: "模型 A" },
      { id: "model-b", name: "模型 B" },
    ],
    reasoningOptions: [
      { id: "low", name: "低" },
      { id: "high", name: "高" },
    ],
    runtime: { id: "codex", models: [] },
    supportedPermissions: new Set(["default"]),
    loading: false,
    valid: true,
    error: null,
    nativeModelsQuery: { isFetching: false },
    thinkingQuery: { isFetching: false },
  }),
}));

const provider = (children: React.ReactNode) => (
  <QueryClientProvider
    client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
  >
    {children}
  </QueryClientProvider>
);

it.each(["create", "edit"] as const)(
  "%s通过同一编辑器保存相同字段与模型配置",
  async (mode) => {
    const user = userEvent.setup();
    const initial =
      mode === "edit" ? emptyLibrary("kb", "原名称", "general") : undefined;
    const controller = createKnowledgeEditorController(initial, {
      runtimeId: "codex",
      model: "model-a",
    });
    const save = vi
      .fn<(command: KnowledgeEditorCommand) => Promise<void>>()
      .mockResolvedValue(undefined);
    render(
      provider(
        <KnowledgeLibraryEditor
          opened
          mode={mode}
          controller={controller}
          close={vi.fn()}
          save={save}
          activity={<p>真实活动接口容器</p>}
        />,
      ),
    );
    const name = screen.getByRole("textbox", { name: "名称" });
    await user.clear(name);
    await user.type(name, "修改后的名称");
    await user.click(screen.getByRole("button", { name: "模型配置" }));
    fireEvent.keyDown(screen.getByRole("combobox", { name: "模型" }), {
      key: "ArrowDown",
    });
    await user.click(screen.getByRole("option", { name: "模型 B" }));
    expect(
      screen.getByRole("combobox", { name: "推理强度" }),
    ).toHaveTextContent("低");
    fireEvent.keyDown(screen.getByRole("combobox", { name: "推理强度" }), {
      key: "ArrowDown",
    });
    expect(
      screen.queryByRole("option", { name: "默认推理强度" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("option", { name: "高" }));
    await user.click(screen.getByRole("button", { name: "分块设置" }));
    const chunk = screen.getByRole("slider", { name: "分块大小" });
    chunk.focus();
    await user.keyboard("{End}");
    if (mode === "edit") {
      await user.click(screen.getByRole("button", { name: "活动记录" }));
      expect(screen.getByText("真实活动接口容器")).toBeVisible();
      const footer = screen.getByRole("dialog").querySelector("footer");
      if (!footer) throw new Error("缺少固定底栏");
      expect(
        within(footer).queryByRole("button", { name: "保存并关闭" }),
      ).not.toBeInTheDocument();
    }
    await user.click(screen.getByRole("button", { name: "基础信息" }));
    expect(screen.getByRole("textbox", { name: "名称" })).toHaveValue(
      "修改后的名称",
    );
    await user.click(
      screen.getByRole("button", {
        name: mode === "create" ? "创建知识库" : "保存并关闭",
      }),
    );
    await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
    expect(save.mock.calls[0]?.[0]).toMatchObject({
      draft: {
        library: { name: "修改后的名称", chunkingConfig: { chunk_size: 4000 } },
        execution: { model: "model-b", reasoning: "high" },
      },
    });
  },
);

it("编辑hydrate保留0与false，切Wiki-only不套新建预设，ID可复制且不展示Wiki语言设置", async () => {
  const user = userEvent.setup();
  const library = emptyLibrary("kb-real-id", "编辑库", "general");
  library.language = "en-US";
  library.indexing.keywordEnabled = true;
  library.chunkingConfig = {
    ...library.chunkingConfig,
    chunk_size: 1337,
    chunk_overlap: 0,
    enable_parent_child: false,
  };
  const controller = createKnowledgeEditorController(library);
  render(
    provider(
      <KnowledgeLibraryEditor
        opened
        mode="edit"
        controller={controller}
        close={vi.fn()}
        save={vi.fn()}
      />,
    ),
  );
  expect(
    screen.queryByRole("combobox", { name: "Wiki 编写语言" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("textbox", { name: "知识库 ID" }),
  ).not.toBeInTheDocument();
  expect(screen.getByLabelText("知识库 ID")).toHaveTextContent("kb-real-id");
  await user.click(screen.getByRole("button", { name: "复制知识库 ID" }));
  expect(await navigator.clipboard.readText()).toBe("kb-real-id");
  await user.click(screen.getByRole("checkbox", { name: "RAG 检索" }));
  await user.click(screen.getByRole("button", { name: "分块设置" }));
  expect(screen.getByRole("slider", { name: "分块大小" })).toHaveAttribute(
    "aria-valuenow",
    "1337",
  );
  expect(screen.getByRole("slider", { name: "重叠大小" })).toHaveAttribute(
    "aria-valuenow",
    "0",
  );
  expect(screen.getByRole("switch", { name: "父子分块" })).not.toBeChecked();
  expect(controller.getSnapshot().draft.library.language).toBe("en-US");
});

it("未知保存锁住原payload但允许关闭，重开同controller以原身份核验", async () => {
  const user = userEvent.setup();
  const controller = createKnowledgeEditorController();
  const close = vi.fn();
  const save = vi
    .fn()
    .mockRejectedValueOnce(new ApiError("host保存待补齐", "PARTIAL", 500))
    .mockResolvedValueOnce(undefined);
  const view = () =>
    provider(
      <KnowledgeLibraryEditor
        opened
        mode="create"
        controller={controller}
        close={close}
        save={save}
      />,
    );
  const rendered = render(view());
  await user.type(
    screen.getByRole("textbox", { name: "名称" }),
    "不可重复创建",
  );
  await user.click(screen.getByRole("button", { name: "创建知识库" }));
  await waitFor(() =>
    expect(screen.getByRole("textbox", { name: "名称" })).toBeDisabled(),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "取消" }));
  expect(close).toHaveBeenCalledOnce();
  rendered.unmount();
  render(view());
  expect(screen.getByRole("textbox", { name: "名称" })).toHaveValue(
    "不可重复创建",
  );
  await user.click(screen.getByRole("button", { name: "创建知识库" }));
  await waitFor(() => expect(save).toHaveBeenCalledTimes(2));
  expect(save.mock.calls[1]?.[0]).toEqual(save.mock.calls[0]?.[0]);
});

it("索引使用RAG与Wiki两张卡，保留旧关键词单开值并至少保留一种能力", async () => {
  const user = userEvent.setup();
  const legacy = emptyLibrary("legacy", "旧库", "general");
  legacy.indexing.keywordEnabled = true;
  const controller = createKnowledgeEditorController(legacy);
  render(
    provider(
      <KnowledgeLibraryEditor
        opened
        mode="edit"
        controller={controller}
        close={vi.fn()}
        save={vi.fn()}
      />,
    ),
  );
  expect(
    screen.queryByRole("radiogroup", { name: "知识类型" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("switch", { name: "关键词检索" }),
  ).not.toBeInTheDocument();
  const rag = screen.getByRole("checkbox", { name: "RAG 检索" });
  const wiki = screen.getByRole("checkbox", { name: "Wiki 知识库" });
  expect(rag).toBeChecked();
  expect(controller.getSnapshot().draft.library.indexing).toMatchObject({
    keywordEnabled: true,
    vectorEnabled: false,
  });
  await user.click(wiki);
  expect(wiki).not.toBeChecked();
  await user.click(rag);
  expect(rag).toBeChecked();
  await user.click(wiki);
  await user.click(rag);
  expect(controller.getSnapshot().draft.library.indexing).toEqual({
    wikiEnabled: true,
    vectorEnabled: false,
    keywordEnabled: false,
  });
  await user.click(rag);
  expect(controller.getSnapshot().draft.library.indexing).toEqual({
    wikiEnabled: true,
    vectorEnabled: true,
    keywordEnabled: true,
  });
});

vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
);

it("开启RAG后标注Embedding必填，并阻止空配置提交", async () => {
  const user = userEvent.setup();
  const controller = createKnowledgeEditorController();
  const save = vi.fn().mockResolvedValue(undefined);
  render(
    provider(
      <KnowledgeLibraryEditor
        opened
        mode="create"
        controller={controller}
        close={vi.fn()}
        save={save}
      />,
    ),
  );
  const name = screen.getByRole("textbox", { name: "名称" });
  expect(name).toBeRequired();
  await user.type(name, "RAG 知识库");
  await user.click(screen.getByRole("checkbox", { name: "RAG 检索" }));
  await user.click(screen.getByRole("button", { name: "创建知识库" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(/Embedding/);
  expect(screen.getByRole("textbox", { name: "服务 URL" })).toBeRequired();
  expect(
    screen.getByRole("textbox", { name: "Embedding 模型" }),
  ).toBeRequired();
  expect(screen.getByLabelText("API Key")).toBeRequired();
  expect(save).not.toHaveBeenCalled();
  await user.type(
    screen.getByRole("textbox", { name: "服务 URL" }),
    "https://embedding.example/v1",
  );
  await user.type(
    screen.getByRole("textbox", { name: "Embedding 模型" }),
    "embed-v1",
  );
  await user.type(screen.getByLabelText("API Key"), "test-key");
  await user.click(screen.getByRole("button", { name: "创建知识库" }));
  await waitFor(() => expect(save).toHaveBeenCalledOnce());
});

it("模型分区直接内嵌本库Embedding草稿，不打开记忆共享设置", async () => {
  const user = userEvent.setup();
  const controller = createKnowledgeEditorController();
  render(
    provider(
      <KnowledgeLibraryEditor
        opened
        mode="create"
        controller={controller}
        close={vi.fn()}
        save={vi.fn()}
      />,
    ),
  );
  await user.click(screen.getByRole("button", { name: "模型配置" }));
  await user.type(
    screen.getByRole("textbox", { name: "服务 URL" }),
    "https://kb-only.example/v1",
  );
  await user.type(
    screen.getByRole("textbox", { name: "Embedding 模型" }),
    "kb-embed",
  );
  await user.type(screen.getByLabelText("API Key"), "kb-secret");
  expect(screen.getByRole("button", { name: "测试连接" })).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "配置共享 Embedding" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText(/影响记忆/)).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "基础信息" }));
  await user.click(screen.getByRole("button", { name: "模型配置" }));
  expect(screen.getByRole("textbox", { name: "服务 URL" })).toHaveValue(
    "https://kb-only.example/v1",
  );
});
