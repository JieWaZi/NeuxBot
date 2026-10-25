import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it } from "vitest";
import "@/shared/i18n";
import { PluginOverview } from "./plugin-overview";

afterEach(cleanup);

it.each([
  {
    brand: "飞书",
    repository: "https://github.com/larksuite/cli",
    version: "v1.0.98",
  },
  {
    brand: "GitHub",
    repository: "https://github.com/github/github-mcp-server",
    version: undefined,
  },
])(
  "shows only upstream information for $brand",
  ({ brand, repository, version }) => {
    render(
      <MemoryRouter>
        <PluginOverview
          pkg={{
            manifest: {
              name: "maintained-plugin",
              author: { name: "NeuxBot contributors" },
              version: "0.1.0",
              repository,
            },
            presentation: { developerName: brand },
            upstream: version
              ? {
                  author: "Lark Suite CLI contributors",
                  tag: version,
                  repository,
                  license: "MIT",
                }
              : undefined,
            servers: [
              { key: "github", url: "https://api.githubcopilot.com/mcp/" },
            ],
          }}
        />
      </MemoryRouter>,
    );
    expect(
      screen.getByText(version ? "Lark Suite CLI contributors" : brand),
    ).toBeVisible();
    if (version) expect(screen.queryByText(brand)).not.toBeInTheDocument();
    if (version) {
      expect(screen.getByText("插件版本")).toBeVisible();
      expect(screen.getByText(version)).toBeVisible();
    } else {
      expect(screen.queryByText("插件版本")).not.toBeInTheDocument();
    }
    expect(screen.queryByText("NeuxBot contributors")).not.toBeInTheDocument();
    expect(screen.queryByText("0.1.0")).not.toBeInTheDocument();
    expect(screen.queryByText("上游版本")).not.toBeInTheDocument();
  },
);
