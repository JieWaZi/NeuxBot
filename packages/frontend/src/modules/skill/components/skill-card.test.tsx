import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { toSkillSummary } from "../model/skill-view";
import { SkillCard } from "./skill-card";

const avatar = vi.hoisted(() => vi.fn());
vi.mock("@/shared/ui/avatar", () => ({
  Avatar: (props: unknown) => {
    avatar(props);
    return <span data-testid="installed-partner-avatar" />;
  },
}));
afterEach(() => {
  cleanup();
  avatar.mockClear();
});

it("shows actual installed partner profiles and offers manage and uninstall", () => {
  const skill = toSkillSummary({
    skill: { id: "writing", name: "writing-skills", source: "git" },
    affectedWakerCount: 6,
    installedPartners: Array.from({ length: 4 }, (_, index) => ({
      id: `partner-${index}`,
      name: `伙伴${index}`,
      avatar: `actual-avatar-${index}`,
    })),
  });
  if (!skill) throw new Error("missing skill fixture");
  const uninstall = vi.fn();
  render(
    <MemoryRouter>
      <SkillCard
        compact
        skill={skill}
        wakerId="w1"
        onEnabledChange={vi.fn()}
        onDelete={uninstall}
      />
    </MemoryRouter>,
  );
  expect(screen.getByText("6 个伙伴使用")).toBeVisible();
  expect(screen.getByText("来自 Git")).toBeVisible();
  expect(screen.getAllByTestId("installed-partner-avatar")).toHaveLength(4);
  expect(avatar.mock.calls.map(([props]) => props.src)).toEqual([
    "actual-avatar-0",
    "actual-avatar-1",
    "actual-avatar-2",
    "actual-avatar-3",
  ]);
  fireEvent.pointerDown(
    screen.getByRole("button", { name: "writing-skills更多操作" }),
    { button: 0, ctrlKey: false },
  );
  expect(screen.getByRole("menuitem", { name: "管理" })).toHaveAttribute(
    "href",
    "/wakers/w1/skills/writing",
  );
  fireEvent.click(screen.getByRole("menuitem", { name: "卸载" }));
  expect(uninstall).toHaveBeenCalledOnce();
});
