import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { WorkspaceFileTypeIcon } from "./workspace-file-type-icon";

describe("工作区文件图标", () => {
  it.each([
    ["REPORT.MD", "markdown"],
    ["src/app.tsx", "react_ts"],
    ["C:\\repo\\package.json", "nodejs"],
    ["tsconfig.app.json", "tsconfig"],
    [".env.local", "tune"],
    ["Dockerfile.prod", "docker"],
    ["photo.jpeg", "image"],
    ["archive.tar.gz", "zip"],
    ["unknown.xyz", "file"],
    ["README", "file"],
  ])("%s 使用对应的本地图标", (name, expected) => {
    const { container } = render(
      <WorkspaceFileTypeIcon name={name} isDirectory={false} />,
    );
    expect(container.querySelector("img")?.getAttribute("src")).toContain(
      `/material-icon-theme/${expected}.svg`,
    );
    expect(screen.queryByRole("img")).toBeNull();
  });

  it("展开文件夹时更新图标并保持行内尺寸", () => {
    const { container, rerender } = render(
      <WorkspaceFileTypeIcon name="src" isDirectory size="1em" />,
    );
    expect(container.querySelector("img")?.getAttribute("src")).toContain(
      "/material-icon-theme/folder.svg",
    );
    rerender(
      <WorkspaceFileTypeIcon name="src" isDirectory isOpen size="1em" />,
    );
    expect(container.querySelector("img")?.getAttribute("src")).toContain(
      "/material-icon-theme/folder-open.svg",
    );
    expect(container.querySelector("span")?.style.width).toBe("1em");
    expect(container.querySelector("span")?.style.height).toBe("1em");
  });
});
