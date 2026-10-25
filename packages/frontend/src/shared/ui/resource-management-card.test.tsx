import { render, cleanup, screen } from "@testing-library/react";
import { it, expect, vi, afterEach } from "vitest";
import "@/shared/i18n";
import { ResourcePartnerUsage } from "./resource-management-card";

const avatar = vi.hoisted(() => vi.fn());
vi.mock("./avatar", () => ({
  Avatar: (props: unknown) => {
    avatar(props);
    return <span data-testid="actual-avatar" />;
  },
}));
afterEach(() => {
  cleanup();
  avatar.mockClear();
});
it("passes the real partner avatar into the static renderer", () => {
  render(
    <ResourcePartnerUsage
      count={1}
      partners={[
        {
          id: "w1",
          name: "伙伴",
          avatar: "companion:v2:0123456789abcdef0123456789abcdef",
        },
      ]}
    />,
  );
  expect(avatar.mock.calls[0]?.[0]).toMatchObject({
    src: "companion:v2:0123456789abcdef0123456789abcdef",
    name: "伙伴",
    animated: false,
    interactive: false,
  });
});
it("does not synthesize a companion when the response has no avatar", () => {
  render(
    <ResourcePartnerUsage count={1} partners={[{ id: "w1", name: "伙伴" }]} />,
  );
  expect(avatar).not.toHaveBeenCalled();
  expect(screen.getByText("1 个伙伴使用")).toBeVisible();
});

it("overlaps at most four actual avatars while retaining the full usage count", () => {
  render(
    <ResourcePartnerUsage
      count={6}
      partners={Array.from({ length: 6 }, (_, index) => ({
        id: `w${index}`,
        name: `伙伴${index}`,
        avatar: `avatar-${index}`,
      }))}
    />,
  );
  expect(screen.getAllByTestId("actual-avatar")).toHaveLength(4);
  expect(screen.getByText("6 个伙伴使用")).toBeVisible();
  expect(
    screen.getAllByTestId("actual-avatar")[0]?.parentElement?.parentElement,
  ).toHaveClass("-space-x-2");
});
