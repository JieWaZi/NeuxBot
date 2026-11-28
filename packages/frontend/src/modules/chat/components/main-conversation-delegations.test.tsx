import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import {
  delegatedWorkForMessage,
  MainConversationDelegations,
} from "./main-conversation-delegations";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

describe("delegated work timeline", () => {
  it("shows only outgoing child tickets attached to the exact origin message", () => {
    const child = {
      ticketId: "child",
      parentTicketId: "root",
      sourceMessageId: "origin",
    };
    expect(
      delegatedWorkForMessage(
        [
          child,
          { ...child, ticketId: "received", received: true },
          { ticketId: "root", sourceMessageId: "origin" },
          { ...child, ticketId: "other", sourceMessageId: "different" },
        ],
        "origin",
      ),
    ).toEqual([child]);
  });
});

it("keeps a completed receipt at the same node and links only approved work sources", () => {
  const item = {
    ticketId: "a",
    title: "Compare release notes",
    state: "active",
    workChatId: "b-work",
    workSubjectType: "waker",
    workSubjectId: "b",
  };
  const unavailable = {
    ticketId: "private",
    title: "Private metadata",
    state: "queued",
    workChatId: "not-authorized",
  };
  const tree = (state: string) => (
    <MemoryRouter>
      <MainConversationDelegations
        entries={[{ ...item, state }, unavailable]}
      />
    </MemoryRouter>
  );
  const { rerender } = render(tree("active"));
  const link = screen.getByRole("link", { name: /Compare release notes/ });
  expect(link).toHaveAttribute("href", "/chat/main/waker/b?source=b-work");
  expect(screen.queryByRole("link", { name: /Private metadata/ })).toBeNull();
  rerender(tree("completed"));
  expect(screen.getByRole("link", { name: /Compare release notes/ })).toBe(
    link,
  );
  expect(link).toHaveTextContent("delegationCompleted");
});
