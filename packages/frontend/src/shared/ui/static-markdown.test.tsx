import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { StaticMarkdown } from "./static-markdown";
it("保留 Wiki 相对链接并由宿主导航，不加载远程图片或原始 HTML", async () => {
  const navigate = vi.fn();
  const view = render(
    <StaticMarkdown
      source={
        '# 标题\n\n[内部知识](concept/one.md)\n\n![远程图片](https://example.com/remote.png)\n\n<img src="https://example.com/raw.png" onerror="alert(1)"><script>alert(1)</script>\n\n[危险链接](javascript:alert(1))'
      }
      onLink={navigate}
    />,
  );
  await userEvent.click(await screen.findByRole("link", { name: "内部知识" }));
  expect(navigate).toHaveBeenCalledWith("concept/one.md");
  expect(view.container.querySelector("img,script,iframe")).toBeNull();
  expect(view.container.querySelector('a[href^="javascript:"]')).toBeNull();
});
