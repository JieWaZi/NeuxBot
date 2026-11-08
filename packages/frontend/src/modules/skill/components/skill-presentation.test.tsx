import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { SkillPresentation } from "./skill-presentation";

const state = vi.hoisted(() => ({
  installed: true,
  enabled: true,
  version: "v1",
  packageStatus: "available" as string | undefined,
}));
vi.mock("@/modules/plugin", () => ({
  PartnerPicker: ({ onChange }: { onChange: (ids: string[]) => void }) => (
    <button onClick={() => onChange(["w1"])}>选择伙伴一</button>
  ),
}));
vi.mock("@/shared/workspace/app-scene-workspace", () => ({
  useRouteSceneActive: () => true,
}));
vi.mock("@/shared/api/generated/@tanstack/react-query.gen", () => ({
  listWakerSkillsOptions: () => ({
    queryKey: ["try-skills"],
    queryFn: async () =>
      state.installed
        ? [
            {
              enabled: state.enabled,
              skill: { id: "s1" },
              currentVersion: { id: state.version },
            },
          ]
        : [],
  }),
  getPluginOptions: () => ({
    queryKey: ["try-plugin"],
    queryFn: async () => ({
      packageStatus: state.packageStatus,
      installations: state.installed
        ? [{ wakerId: "w1", enabled: state.enabled, versionId: state.version }]
        : [],
    }),
  }),
}));
function RetainedScene({ children }: { children: ReactNode }) {
  const location = useLocation();
  return (
    <>
      <div
        data-testid="retained-skill-page"
        hidden={location.pathname === "/chat"}
      >
        {children}
      </div>
      <Routes>
        <Route path="/chat" element={<div>聊天草稿页</div>} />
        <Route path="*" element={null} />
      </Routes>
      <Location />
    </>
  );
}

function Location() {
  const location = useLocation();
  return (
    <output data-testid="destination">
      {JSON.stringify({
        path: location.pathname + location.search,
        state: location.state,
      })}
    </output>
  );
}
function mount(props: Parameters<typeof SkillPresentation>[0]) {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter initialEntries={["/resources/skills/s1"]}>
        <RetainedScene>
          <SkillPresentation {...props} />
        </RetainedScene>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
afterEach(() => {
  cleanup();
  state.installed = true;
  state.enabled = true;
  state.version = "v1";
  state.packageStatus = "available";
});
it("always shows the fallback TryCard and prepares a referenced skill draft for the current usable partner", () => {
  mount({
    name: "代码审查",
    context: { kind: "skill", skillId: "s1", versionId: "v1" },
    wakerId: "w1",
    usable: true,
  });
  fireEvent.click(screen.getByRole("button", { name: "帮我使用这个技能。" }));
  const result = JSON.parse(
    screen.getByTestId("destination").textContent ?? "{}",
  );
  expect(result.path).toMatch(/^\/chat\?draftId=.+&wakerId=w1$/);
  expect(result.state.chatDraftIntent).toMatchObject({
    wakerId: "w1",
    skillId: "s1",
    text: "帮我使用这个技能。",
  });
  expect(result.state.chatDraftIntent.pluginId).toBeUndefined();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
it("requires an explicit partner and verifies its enabled installed version before carrying a skill reference", async () => {
  mount({
    name: "代码审查",
    context: { kind: "skill", skillId: "s1", versionId: "v1" },
  });
  fireEvent.click(screen.getByRole("button", { name: "帮我使用这个技能。" }));
  expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "选择伙伴一" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "继续填写草稿" }));
  expect(
    JSON.parse(screen.getByTestId("destination").textContent ?? "{}").state
      .chatDraftIntent.skillId,
  ).toBe("s1");
});
it.each(["missing", "disabled", "different version"])(
  "does not offer a false skill reference when installation is %s",
  async (reason) => {
    state.installed = reason !== "missing";
    state.enabled = reason !== "disabled";
    state.version = reason === "different version" ? "old" : "v1";
    mount({
      name: "代码审查",
      context: { kind: "skill", skillId: "s1", versionId: "v1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "帮我使用这个技能。" }));
    fireEvent.click(screen.getByRole("button", { name: "选择伙伴一" }));
    expect(
      await screen.findByText("需要已安装并启用当前版本的伙伴。"),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeDisabled();
    expect(
      JSON.parse(screen.getByTestId("destination").textContent ?? "{}").state,
    ).toBeNull();
  },
);
it("keeps a catalog preview TryCard visible with a disabled arrow and an honest reason", () => {
  mount({ name: "目录技能", context: { kind: "preview" } });
  expect(
    screen.getByRole("button", { name: "帮我使用这个技能。" }),
  ).toBeDisabled();
  expect(
    screen.getByRole("button", { name: "帮我使用这个技能。" }),
  ).toHaveAttribute("title", "需要已安装并启用当前版本的伙伴。");
  expect(
    screen.queryByText("需要已安装并启用当前版本的伙伴。"),
  ).not.toBeInTheDocument();
});
it("references the containing plugin only after checking the same installed snapshot", async () => {
  mount({
    name: "插件技能",
    context: { kind: "plugin", pluginId: "p1", versionId: "v1" },
  });
  fireEvent.click(screen.getByRole("button", { name: "帮我使用这个技能。" }));
  fireEvent.click(screen.getByRole("button", { name: "选择伙伴一" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "继续填写草稿" }));
  const intent = JSON.parse(
    screen.getByTestId("destination").textContent ?? "{}",
  ).state.chatDraftIntent;
  expect(intent.pluginId).toBe("p1");
  expect(intent.skillId).toBeUndefined();
});

it.each([undefined, "unsupported", "unknown"])(
  "does not prepare a plugin reference when package availability is %s",
  async (status) => {
    state.packageStatus = status;
    mount({
      name: "插件技能",
      context: { kind: "plugin", pluginId: "p1", versionId: "v1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "帮我使用这个技能。" }));
    fireEvent.click(screen.getByRole("button", { name: "选择伙伴一" }));
    expect(
      await screen.findByText("需要已安装并启用当前版本的伙伴。"),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeDisabled();
    expect(
      JSON.parse(screen.getByTestId("destination").textContent ?? "{}").state,
    ).toBeNull();
  },
);

it("closes its partner dialog when navigation hides the retained scene", async () => {
  mount({
    name: "代码审查",
    context: { kind: "skill", skillId: "s1", versionId: "v1" },
  });
  fireEvent.click(screen.getByRole("button", { name: "帮我使用这个技能。" }));
  fireEvent.click(screen.getByRole("button", { name: "选择伙伴一" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "继续填写草稿" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "继续填写草稿" }));
  expect(screen.getByText("聊天草稿页")).toBeVisible();
  expect(screen.getByTestId("retained-skill-page")).toBeInTheDocument();
  expect(screen.getByTestId("retained-skill-page")).not.toBeVisible();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
