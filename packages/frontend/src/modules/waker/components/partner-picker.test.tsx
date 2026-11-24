import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { PartnerPicker } from "./partner-picker";

vi.mock("../api/waker-queries", () => ({
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

it("browses partners with arrow keys and confirms the focused partner explicitly", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  client.setQueryData(["picker-partners"], {
    resources: [
      { id: "w1", name: "第一位" },
      { id: "w2", name: "第二位" },
    ],
  });
  const onChange = vi.fn();
  render(
    <QueryClientProvider client={client}>
      <PartnerPicker
        variant="select"
        selectionMode="single"
        triggerProps={{ appearance: "settings" }}
        label="选择伙伴"
        selected={["w1"]}
        onChange={onChange}
      />
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "选择伙伴" }));
  const first = await screen.findByRole("radio", { name: "第一位" });
  const second = screen.getByRole("radio", { name: "第二位" });
  first.focus();
  fireEvent.keyDown(first, { key: "ArrowDown" });
  await waitFor(() => expect(second).toHaveFocus());
  fireEvent.keyUp(second, { key: "ArrowDown" });
  expect(onChange).not.toHaveBeenCalled();
  expect(first).toBeChecked();
  fireEvent.keyDown(second, { key: "Enter" });
  expect(onChange).toHaveBeenCalledExactlyOnceWith(["w2"]);
  await waitFor(() => expect(second).not.toBeInTheDocument());
});
