import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";

import {
  EntityActivityBadges,
  EntityActivitySummary,
  entityActivityCounts,
} from "./entity-activity-summary";
import type { NavigationEntity } from "./entity-navigation";

describe("EntityActivitySummary", () => {
  it("按运行与待处理数量渲染角标", () => {
    render(
      <TooltipProvider>
        <EntityActivitySummary counts={{ runningCount: 4, waitingCount: 1 }} />
      </TooltipProvider>,
    );

    expect(screen.getByText("4")).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();
    expect(
      screen.getByRole("status", {
        name: "4 个会话正在运行 · 1 个会话需要处理",
      }),
    ).toBeInTheDocument();
  });

  it("无活动时不渲染角标", () => {
    const { container } = render(
      <EntityActivityBadges counts={{ runningCount: 0, waitingCount: 0 }} />,
    );

    expect(container).toBeEmptyDOMElement();
  });

  it("从导航实体读取活动计数", () => {
    const item: NavigationEntity = {
      id: "waker-1",
      key: "waker:waker-1",
      kind: "waker",
      waker: {
        id: "waker-1",
        name: "伙伴",
        runningCount: 2,
        waitingCount: 0,
      },
    };

    expect(entityActivityCounts(item)).toEqual({
      runningCount: 2,
      waitingCount: 0,
    });
  });
});
