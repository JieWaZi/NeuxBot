import { render, cleanup } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { PluginIcon } from "./plugin-icon";

afterEach(cleanup);
it("无品牌图的插件复用分类颜色与图标，未分类统一使用积木", () => {
  const view = render(<PluginIcon category="设计" />);
  expect(view.container.firstElementChild).toHaveClass(
    "bg-resource-category-design",
  );
  expect(view.container.querySelector("svg")).toHaveClass("lucide-swatch-book");
  view.rerender(<PluginIcon />);
  expect(view.container.firstElementChild).toHaveClass(
    "bg-resource-category-tools",
  );
  expect(view.container.querySelector("svg")).toHaveClass("lucide-blocks");
});
it("uses only the supplied package icon for catalog, detail and install sizes", () => {
  const icon = "data:image/png;base64,iVBORw0KGgo=";
  const { container, rerender } = render(<PluginIcon pkg={{ icon }} />);
  expect(container.querySelector("img")).toHaveAttribute("src", icon);
  rerender(<PluginIcon pkg={{ manifest: { name: "no-icon" } }} hero />);
  expect(container.querySelector("img")).not.toBeInTheDocument();
});
it("keeps a single declared icon visible in both themes", () => {
  const icon = "data:image/png;base64,default";
  const { container } = render(
    <PluginIcon pkg={{ presentation: { icons: { default: icon } } }} />,
  );
  expect(container.querySelectorAll("img")).toHaveLength(1);
  expect(container.querySelector("img")).toHaveAttribute("src", icon);
  expect(container.querySelector("img")).not.toHaveClass("dark:hidden");
});
it("uses separately validated theme icons and falls back when only the dark asset exists", () => {
  const light = "data:image/png;base64,light",
    dark = "data:image/png;base64,dark";
  const { container, rerender } = render(
    <PluginIcon pkg={{ presentation: { icons: { default: light, dark } } }} />,
  );
  const images = container.querySelectorAll("img");
  expect(images).toHaveLength(2);
  expect(images[0]).toHaveClass("dark:hidden");
  expect(images[1]).toHaveAttribute("src", dark);
  expect(images[1]).toHaveClass("hidden", "dark:block");
  rerender(<PluginIcon pkg={{ presentation: { icons: { dark } } }} />);
  expect(container.querySelectorAll("img")).toHaveLength(1);
  expect(container.querySelector("img")).not.toHaveClass("dark:hidden");
});
it("uses browse theme icons ahead of the legacy single-icon fallback", () => {
  const light = "data:image/png;base64,light";
  const dark = "data:image/png;base64,dark";
  const { container, rerender } = render(
    <PluginIcon
      icon={light}
      icons={{ default: light, dark }}
      size="navigation"
    />,
  );
  const images = container.querySelectorAll("img");
  expect(images).toHaveLength(2);
  expect(images[0]).toHaveAttribute("src", light);
  expect(images[1]).toHaveAttribute("src", dark);
  expect(images[1]).toHaveClass("dark:block");

  rerender(<PluginIcon icon={light} icons={{ default: light }} />);
  expect(container.querySelectorAll("img")).toHaveLength(1);
  expect(container.querySelector("img")).not.toHaveClass("dark:hidden");
});
