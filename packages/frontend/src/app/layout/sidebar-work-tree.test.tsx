import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { SidebarWorkTree } from "./sidebar-work-tree";

const { activity } = vi.hoisted(() => ({
  activity: {
    data: [
      {
        ticketId: "child",
        parentTicketId: "root",
        title: "Review",
        state: "running",
        workChatId: "private" as string | undefined,
        workSubjectType: "waker",
        workSubjectId: "recipient",
        received: true,
      },
      {
        ticketId: "root",
        title: "Release",
        state: "active",
        sourceSubjectType: "team",
        sourceSubjectId: "team",
        sourceMessageId: "origin",
      },
    ],
  },
}));
vi.mock("@/modules/chat/api/work-ticket-activity", () => ({
  useWorkTicketActivity: () => activity,
}));

const tree = (search = "") => (
  <MemoryRouter>
    <SidebarWorkTree id="scope" kind="team" search={search} />
  </MemoryRouter>
);

describe("sidebar delegated work", () => {
  it("retains the same visible row after completion, ordered under its parent", () => {
    const { rerender } = render(tree());
    expect(screen.queryByText("Review")).toBeNull();
    fireEvent.click(screen.getByRole("button"));
    const links = screen.getAllByRole("link");
    expect(
      links.map(
        (link) => link.textContent?.split(/Run|In progress|进行|运行/)[0],
      ),
    ).toHaveLength(2);
    expect(links[0]).toHaveAttribute(
      "href",
      "/chat/main/team/team?message=origin",
    );
    expect(links[1]).toHaveAttribute(
      "href",
      "/chat/main/waker/recipient?source=private",
    );
    activity.data[0]!.state = "completed";
    rerender(tree());
    expect(screen.getAllByRole("link")[1]).toBe(links[1]);
  });

  it("search presents work directly without revealing the source of a received task", () => {
    const record = activity.data[0]!;
    const before = record.workChatId;
    record.workChatId = undefined;
    render(tree("review"));
    expect(screen.getByText("Review")).toBeVisible();
    expect(screen.queryByText("Release")).toBeNull();
    expect(screen.queryByRole("link")).toBeNull();
    record.workChatId = before;
  });
});
