import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, expect, it } from "vitest";
import "@/shared/i18n";
import { PluginOverview } from "./plugin-overview";

afterEach(cleanup);
function Destination() {
  const location = useLocation();
  return (
    <output aria-label="技能路由">
      {location.pathname}
      {location.search}
    </output>
  );
}
it.each(["catalog", "installed"])(
  "navigates to the fixed skill page from %s without a dialog",
  (mode) => {
    const sourceCommit = "a".repeat(40),
      archiveSHA256 = "b".repeat(64);
    render(
      <MemoryRouter initialEntries={["/resources/plugins/p?version=fixed-v"]}>
        <Destination />
        <PluginOverview
          pkg={{
            manifest: { name: "github" },
            skills: [
              {
                key: "skills/review",
                name: "review",
                description: "Review changes",
              },
            ],
            servers: [
              {
                key: "github",
                url: "https://api.githubcopilot.com/mcp?token=hidden",
              },
            ],
          }}
          skillContext={
            mode === "catalog"
              ? { catalogKey: "github", sourceCommit, archiveSHA256 }
              : { pluginId: "p", versionId: "fixed-v" }
          }
        />
      </MemoryRouter>,
    );
    expect(
      screen.getAllByRole("button", { name: "查看技能 review" }),
    ).toHaveLength(1);
    expect(screen.getAllByRole("heading", { name: "github" })).toHaveLength(1);
    expect(
      screen.queryByText(/hidden|Codex|Claude Code|streamable-http/),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "查看技能 review" }));
    expect(screen.getByLabelText("技能路由")).toHaveTextContent(
      mode === "catalog"
        ? `/resources/plugins/catalog/github/skills/skills%2Freview?sourceCommit=${sourceCommit}&archiveSHA256=${archiveSHA256}`
        : "/resources/plugins/p/skills/skills%2Freview?version=fixed-v",
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  },
);
it("hides empty Skill and MCP sections", () => {
  render(
    <MemoryRouter>
      <PluginOverview
        pkg={{ manifest: { name: "empty" }, skills: [], servers: [] }}
      />
    </MemoryRouter>,
  );
  expect(
    screen.queryByRole("heading", { name: /技能|连接器/ }),
  ).not.toBeInTheDocument();
});
