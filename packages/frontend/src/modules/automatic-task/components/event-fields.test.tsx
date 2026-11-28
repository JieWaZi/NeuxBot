import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import "@/shared/i18n";
import { listAutomaticEventSourcesOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { defaultEventConfiguration, EventFields } from "./event-fields";

it("按来源字段声明保存同一 PR 的跟进选项", () => {
  const query = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity } },
  });
  query.setQueryData(listAutomaticEventSourcesOptions().queryKey, {
    resources: [
      {
        id: "github_pull_requests",
        mode: "poll",
        fields: ["trackDiscussion"],
        kinds: ["pull_opened", "pull_reviewed", "checks_passed"],
      },
    ],
  });
  const change = vi.fn();
  const fields = (canTrackDiscussion: boolean) => (
    <QueryClientProvider client={query}>
      <EventFields
        value={defaultEventConfiguration}
        onChange={change}
        canTrackDiscussion={canTrackDiscussion}
      />
    </QueryClientProvider>
  );
  const { rerender } = render(fields(true));
  fireEvent.click(screen.getByRole("checkbox", { name: "同一 PR 持续跟进" }));
  expect(change).toHaveBeenCalledWith({
    ...defaultEventConfiguration,
    trackDiscussion: true,
  });
  rerender(fields(false));
  expect(
    screen.queryByRole("checkbox", { name: "同一 PR 持续跟进" }),
  ).not.toBeInTheDocument();
});
