import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { it, expect, vi } from "vitest";
import { useMainConversationSettings } from "./main-conversation-settings";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("@/shared/api/generated/@tanstack/react-query.gen", () => ({
  getWakerOptions: () => ({
    queryKey: ["partner"],
    queryFn: () => ({ name: "Pip" }),
  }),
}));
vi.mock("@/modules/waker", () => ({
  WakerDefaultModelAction: () => null,
  WakerFormDialog: () => null,
  WakerProfileSection: () => <p>Editable instructions</p>,
}));
vi.mock("@/modules/team", () => ({
  TeamFormDialog: () => null,
  getChannelOptions: vi.fn(),
}));

function TestChat() {
  const settings = useMainConversationSettings(
    { subjectType: "waker", subjectId: "pip" },
    true,
  );
  return (
    <>
      {settings.conversationActions}
      <input aria-label="Draft" />
      {settings.conversationPanel}
    </>
  );
}

it("opens inline partner instructions without remounting the chat draft", async () => {
  render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>
        <TestChat />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  const draft = screen.getByRole("textbox", { name: "Draft" });
  const trigger = screen.getByRole("button", { name: "partnerSettings" });
  fireEvent.change(draft, { target: { value: "retained draft" } });
  fireEvent.click(trigger);
  expect(await screen.findByText("Pip")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "profile.title" }));
  expect(screen.getByText("Editable instructions")).toBeInTheDocument();
  fireEvent.keyDown(
    screen.getByRole("complementary", { name: "partnerSettings" }),
    { key: "Escape" },
  );
  expect(screen.queryByText("Editable instructions")).toBeNull();
  expect(trigger).toHaveFocus();
  expect(screen.getByRole("textbox", { name: "Draft" })).toBe(draft);
  expect(draft).toHaveValue("retained draft");
});
