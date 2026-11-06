import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";
import {
  AppNavigationHistoryControls,
  useAppNavigationHistory,
} from "./app-titlebar";
import type { ChatSceneOwner } from "@/shared/workspace/app-scene-model";

function Harness() {
  const [owner, setOwner] = useState<ChatSceneOwner | undefined>({
    kind: "waker",
    id: "one",
  });
  const [detail, setDetail] = useState(false);
  const history = useAppNavigationHistory({
    owner,
    available: () => true,
    onSelect: setOwner,
    onPageBack: detail ? () => setDetail(false) : undefined,
  });
  return (
    <TooltipProvider>
      <AppNavigationHistoryControls {...history} />
      <output>{detail ? "详情" : (owner?.id ?? "覆盖页")}</output>
      <button onClick={() => setOwner({ kind: "waker", id: "two" })}>
        另一伙伴
      </button>
      <button onClick={() => setOwner(undefined)}>覆盖</button>
      <button onClick={() => setDetail(true)}>设置详情</button>
    </TooltipProvider>
  );
}
describe("侧栏实体历史", () => {
  it("覆盖页不写入历史，后退恢复实体，前进到伙伴", async () => {
    render(<Harness />);
    const user = userEvent.setup();
    expect(screen.getByRole("button", { name: "后退" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "另一伙伴" }));
    await user.click(screen.getByRole("button", { name: "覆盖" }));
    await user.click(screen.getByRole("button", { name: "后退" }));
    expect(screen.getByRole("status")).toHaveTextContent("one");
    await user.click(screen.getByRole("button", { name: "前进" }));
    expect(screen.getByRole("status")).toHaveTextContent("two");
  });
  it("设置详情 Back 优先父级且不移动实体游标", async () => {
    render(<Harness />);
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "另一伙伴" }));
    await user.click(screen.getByRole("button", { name: "设置详情" }));
    await user.click(screen.getByRole("button", { name: "后退" }));
    expect(screen.getByRole("status")).toHaveTextContent("two");
    expect(screen.getByRole("button", { name: "前进" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "后退" }));
    expect(screen.getByRole("status")).toHaveTextContent("one");
  });
});
