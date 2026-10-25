import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, it, expect, vi } from "vitest";
import "@/shared/i18n";
import { SkillImportDialog } from "./skill-import-dialog";
const fixture = vi.hoisted(() => ({
  scan: vi.fn<(input: unknown) => Promise<unknown>>().mockResolvedValue({
    token: "scan-1",
    candidates: [{ id: "skill-1", name: "review", description: "说明" }],
  }),
  save: vi.fn<(input: unknown) => Promise<unknown>>().mockResolvedValue([]),
}));
vi.mock("@/shared/platform", () => ({
  selectFiles: async () => ["/tmp/skills.zip"],
}));
vi.mock("../api/skill-queries", () => ({
  scanWakerSkillImportMutation: () => ({ mutationFn: fixture.scan }),
  importWakerSkillMutation: () => ({ mutationFn: fixture.save }),
}));
afterEach(cleanup);
describe("global skill import", () => {
  it("scans and saves a resource without requiring a partner", async () => {
    const imported = vi.fn();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter>
          <SkillImportDialog
            open
            wakerId=""
            onImported={imported}
            onOpenChange={vi.fn()}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    const input = screen.getByRole("textbox");
    fireEvent.change(input, {
      target: { value: "https://example.com/skills.git" },
    });
    const scan = screen.getByRole("button", { name: /扫描/ });
    expect(scan).toBeEnabled();
    fireEvent.click(scan);
    await screen.findByText("review");
    expect(fixture.scan.mock.calls[0]?.[0]).toMatchObject({
      path: { wakerId: "" },
      body: { location: "https://example.com/skills.git" },
    });
    fireEvent.click(
      screen.getByRole("button", {
        name: /安装所选|导入所选|安装选中|导入选中/,
      }),
    );
    await waitFor(() => expect(imported).toHaveBeenCalled());
    expect(fixture.save.mock.calls[0]?.[0]).toMatchObject({
      path: { wakerId: "" },
      body: { token: "scan-1" },
    });
  });
});
