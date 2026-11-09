import { expect, it } from "vitest";
import { connectionArcs } from "@neuxbot/bloub/effects";

it("连接环在固定姿态和持续连接时始终可见，不随一次性入场结束而消失", () => {
  for (const time of [0, 1.4, 5, 30]) {
    const arcs = connectionArcs(time);
    expect(arcs).toHaveLength(3);
    expect(arcs.every((arc) => arc.opacity > 0 && arc.front.length > 0)).toBe(
      true,
    );
  }
  expect(connectionArcs(5)).not.toEqual(connectionArcs(5.2));
});
