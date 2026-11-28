import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useWorkTicketActivity } from "./work-ticket-activity";

const mocks = vi.hoisted(() => ({
  request: vi.fn(),
  events: vi.fn(),
}));

vi.mock("@/shared/api/generated/@tanstack/react-query.gen", () => ({
  listWorkTicketActivityOptions: (options: {
    query: { subjectType: string; subjectId: string };
  }) => ({
    queryKey: ["ticket-activity", options.query],
    queryFn: () => mocks.request(options.query),
  }),
}));
vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: mocks.events,
}));

function renderActivity(subjectType: "waker" | "team", subjectId?: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderHook(() => useWorkTicketActivity(subjectType, subjectId), {
    wrapper: ({ children }: PropsWithChildren) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  });
}

afterEach(() => vi.clearAllMocks());

describe("work ticket activity", () => {
  it("uses the exact selected subject for the request and shared live refresh", async () => {
    mocks.request.mockResolvedValue([{ ticketId: "child" }]);
    const { result } = renderActivity("team", "team-1");
    await waitFor(() =>
      expect(result.current.data).toEqual([{ ticketId: "child" }]),
    );

    expect(mocks.request).toHaveBeenCalledWith({
      subjectType: "team",
      subjectId: "team-1",
    });
    expect(mocks.events).toHaveBeenCalledWith(
      expect.objectContaining({
        topic: "main-conversations",
        resource: { kind: "team", id: "team-1" },
        eventTypes: ["main-conversations.changed"],
        enabled: true,
      }),
    );
  });

  it("does not fetch or subscribe without a selected subject", () => {
    renderActivity("waker");
    expect(mocks.request).not.toHaveBeenCalled();
    expect(mocks.events).toHaveBeenLastCalledWith(
      expect.objectContaining({ enabled: false }),
    );
  });
});
