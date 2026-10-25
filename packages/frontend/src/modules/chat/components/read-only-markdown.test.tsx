import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import "@/shared/i18n";
import { openExternalURL } from "@/shared/platform";
import type * as Desktop from "@/shared/platform";
import { ReadOnlyMarkdown } from "./read-only-markdown";

vi.mock("@/shared/platform", async (importOriginal) => ({
  ...(await importOriginal<typeof Desktop>()),
  openExternalURL: vi.fn().mockResolvedValue(undefined),
}));

it("只读工作流Markdown通过共享桌面外链入口打开网页，文件路径不成为链接", async () => {
  const user = userEvent.setup();
  render(
    <ReadOnlyMarkdown source="[参考报告](https://example.com/report) 与 [本机路径](/tmp/report.txt)" />,
  );
  await user.click(await screen.findByRole("link", { name: "参考报告" }));
  expect(openExternalURL).toHaveBeenCalledWith("https://example.com/report");
  expect(
    screen.queryByRole("link", { name: "本机路径" }),
  ).not.toBeInTheDocument();
});
