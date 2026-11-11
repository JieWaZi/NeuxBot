import { expect, it } from "vitest";
import { companionPlayback } from "./playback";

it("准备沿用思考，不再单独播放连接动画", () => {
  for (const time of [0, 0.6, 2.5, 30])
    expect(companionPlayback("connecting", time)).toEqual({
      state: "thinking",
      local: time,
      block: "steady",
    });
});
