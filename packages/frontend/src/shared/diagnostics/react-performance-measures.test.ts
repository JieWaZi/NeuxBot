import {
  performance as nodePerformance,
  PerformanceObserver as NodePerformanceObserver,
} from "node:perf_hooks";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { observeReactPerformanceMeasures } from "./react-performance-measures";

let stop: (() => void) | undefined;

beforeEach(() => {
  vi.stubEnv("DEV", true);
  vi.stubGlobal("performance", nodePerformance);
  vi.stubGlobal("PerformanceObserver", NodePerformanceObserver);
});

afterEach(() => {
  stop?.();
  stop = undefined;
  nodePerformance.clearMeasures();
  nodePerformance.clearMarks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

function componentMeasure(name: string) {
  nodePerformance.measure(name, {
    start: 0,
    end: 1,
    detail: { devtools: { track: "Components ⚛" } },
  });
}

it("持续渲染后释放 React 性能记录，保留业务测量和启动标记", async () => {
  stop = observeReactPerformanceMeasures();
  nodePerformance.mark("ally.startup.entry");
  nodePerformance.measure("business.request", { start: 0, end: 1 });
  for (let batch = 0; batch < 3; batch += 1) {
    for (let index = 0; index < 2_000; index += 1) {
      componentMeasure("\u200bChatMessage");
      nodePerformance.measure("Update", {
        start: 0,
        end: 1,
        detail: { devtools: { track: "Blocking", trackGroup: "Scheduler ⚛" } },
      });
    }
    await vi.waitFor(() => {
      expect(
        nodePerformance.getEntriesByType("measure").map((e) => e.name),
      ).toEqual(["business.request"]);
    });
  }
  expect(nodePerformance.getEntriesByType("mark").map((e) => e.name)).toEqual([
    "ally.startup.entry",
  ]);
});

it("接管已有时间线时也释放安装前的 React 记录", async () => {
  componentMeasure("\u200bOldScene");
  stop = observeReactPerformanceMeasures();
  await vi.waitFor(() => {
    expect(nodePerformance.getEntriesByType("measure")).toHaveLength(0);
  });
});

it("React 与业务测量同名时保留业务记录，不影响其他 React 记录释放", async () => {
  stop = observeReactPerformanceMeasures();
  nodePerformance.measure("Update", {
    start: 0,
    end: 1,
    detail: { source: "business" },
  });
  nodePerformance.measure("Update", {
    start: 0,
    end: 1,
    detail: { devtools: { track: "Blocking", trackGroup: "Scheduler ⚛" } },
  });
  componentMeasure("\u200bChatMessage");
  await vi.waitFor(() => {
    expect(nodePerformance.getEntriesByName("\u200bChatMessage")).toHaveLength(
      0,
    );
  });
  expect(nodePerformance.getEntriesByName("Update", "measure")).toHaveLength(2);
});

it("退出时清理待交付记录并停止观察，后续测量不再被修改", async () => {
  stop = observeReactPerformanceMeasures();
  componentMeasure("\u200bUnmountedScene");
  stop();
  expect(nodePerformance.getEntriesByType("measure")).toHaveLength(0);
  componentMeasure("\u200bAfterStop");
  await new Promise<void>((resolve) => setImmediate(resolve));
  expect(
    nodePerformance.getEntriesByType("measure").map((e) => e.name),
  ).toEqual(["\u200bAfterStop"]);
});

it("生产构建不观察或修改时间线", async () => {
  vi.stubEnv("DEV", false);
  stop = observeReactPerformanceMeasures();
  componentMeasure("\u200bProduction");
  await new Promise<void>((resolve) => setImmediate(resolve));
  expect(nodePerformance.getEntriesByType("measure")).toHaveLength(1);
});

it("宿主没有测量观察能力时正常启动", () => {
  vi.stubGlobal("PerformanceObserver", undefined);
  expect(() => observeReactPerformanceMeasures()()).not.toThrow();
  vi.stubGlobal("PerformanceObserver", { supportedEntryTypes: ["resource"] });
  expect(() => observeReactPerformanceMeasures()()).not.toThrow();
});
