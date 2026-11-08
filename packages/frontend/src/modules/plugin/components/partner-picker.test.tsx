import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { PartnerPicker } from "./partner-picker";

vi.mock("../api/plugin-queries", () => ({
  getWakerOptions: () => ({
    queryKey: ["picker-owner"],
    queryFn: async () => {
      throw new Error("refresh failed");
    },
  }),
  listWakersOptions: () => ({
    queryKey: ["picker-partners"],
    queryFn: async () => ({ resources: [] }),
  }),
}));
afterEach(cleanup);

it("keeps the selected partner visible when background refresh fails", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  client.setQueryData(["picker-owner"], { id: "w1", name: "已知伙伴" });
  render(
    <QueryClientProvider client={client}>
      <PartnerPicker
        onlyPartner="w1"
        selected={["w1"]}
        selectionMode="single"
        onChange={vi.fn()}
      />
    </QueryClientProvider>,
  );
  await waitFor(() =>
    expect(client.getQueryState(["picker-owner"])?.status).toBe("error"),
  );
  expect(screen.getByRole("radio", { name: "已知伙伴" })).toBeChecked();
  expect(screen.getByRole("button", { name: "重试" })).toBeVisible();
});
