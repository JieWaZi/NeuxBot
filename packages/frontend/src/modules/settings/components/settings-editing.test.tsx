import { useState } from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, Link, RouterProvider } from "react-router-dom";
import { afterEach, expect, it } from "vitest";
import "@/shared/i18n";
import {
  SettingsNavigationGuardProvider,
  useSettingsDraftGuard,
  useSettingsNavigationGuard,
} from "./settings-draft-guard";
import {
  SettingsPageFooter,
  SettingsPageFooterHost,
} from "./settings-page-footer";

afterEach(cleanup);
function Editor() {
  const [draft, setDraft] = useState("");
  const [scene, setScene] = useState("设置");
  useSettingsDraftGuard({ dirty: Boolean(draft) });
  const leave = useSettingsNavigationGuard();
  return (
    <>
      <input
        aria-label="草稿"
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
      />
      <Link to="/settings?keysPage=2">密钥下一页</Link>
      <Link to="/other">面包屑离开</Link>
      <button onClick={() => leave(() => setScene("伙伴"))}>切换Scene</button>
      <output>{scene}</output>
    </>
  );
}
function show() {
  const router = createMemoryRouter(
    [
      {
        path: "*",
        element: (
          <SettingsNavigationGuardProvider>
            <Editor />
          </SettingsNavigationGuardProvider>
        ),
      },
    ],
    { initialEntries: ["/other", "/settings"], initialIndex: 1 },
  );
  render(<RouterProvider router={router} />);
  return router;
}
it("dirty面包屑与浏览器后退保留草稿，放弃才继续导航", async () => {
  const router = show();
  const user = userEvent.setup();
  await user.type(screen.getByRole("textbox", { name: "草稿" }), "修改");
  await user.click(screen.getByRole("link", { name: "面包屑离开" }));
  await user.click(await screen.findByRole("button", { name: "继续编辑" }));
  expect(router.state.location.pathname).toBe("/settings");
  expect(screen.getByRole("textbox")).toHaveValue("修改");
  void router.navigate(-1);
  await user.click(await screen.findByRole("button", { name: "放弃更改" }));
  await waitFor(() => expect(router.state.location.pathname).toBe("/other"));
});
it("Scene副作用在确认前不执行，beforeunload保护草稿", async () => {
  show();
  const user = userEvent.setup();
  await user.type(screen.getByRole("textbox"), "修改");
  const event = new Event("beforeunload", { cancelable: true });
  fireEvent(window, event);
  expect(event.defaultPrevented).toBe(true);
  await user.click(screen.getByRole("button", { name: "切换Scene" }));
  expect(screen.getByText("设置", { selector: "output" })).toBeInTheDocument();
  await user.click(await screen.findByRole("button", { name: "放弃更改" }));
  expect(screen.getByText("伙伴", { selector: "output" })).toBeInTheDocument();
});
it("保存条portal到正文滚动区的兄弟host，无宿主时可独立渲染", () => {
  const view = render(
    <SettingsPageFooterHost>
      <div data-testid="scroll">
        <SettingsPageFooter>
          <button>保存</button>
        </SettingsPageFooter>
      </div>
    </SettingsPageFooterHost>,
  );
  const footer = screen.getByRole("contentinfo");
  expect(screen.getByTestId("scroll")).not.toContainElement(footer);
  expect(footer.parentElement).toHaveAttribute(
    "data-slot",
    "settings-page-footer-host",
  );
  view.unmount();
  render(<SettingsPageFooter>独立保存</SettingsPageFooter>);
  expect(screen.getByRole("contentinfo")).toHaveTextContent("独立保存");
});

it("同一编辑作用域的密钥分页不询问，保留dirty草稿", async () => {
  const router = show();
  const user = userEvent.setup();
  await user.type(screen.getByRole("textbox"), "修改");
  await user.click(screen.getByRole("link", { name: "密钥下一页" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(router.state.location.search).toBe("?keysPage=2");
  expect(screen.getByRole("textbox")).toHaveValue("修改");
});
