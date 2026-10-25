import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it, vi } from "vitest";
import {
  createCompanion,
  companionReactions,
} from "../../../../../vendor/companion/renderer";
import styles from "../../../../../vendor/companion/styles.json";
import backendConstants from "../../../../../server/constants/common.go?raw";
import {
  companionAppearance,
  companionAvatar,
  nextCompanionAvatar,
} from "./appearance";

const names = Object.keys(styles) as (keyof typeof styles)[];
describe("静态身体风格", () => {
  it("内置 NeuxBot 固定毛绒，所有目录身份都直接还原对应风格", () => {
    const builtinAvatar = backendConstants.match(
      /PersonalAssistantAvatar = "([^"]+)"/,
    )?.[1];
    expect(builtinAvatar).toBe(styles.plush.avatar);
    expect(companionAppearance(builtinAvatar ?? "").style).toBe("plush");
    for (const style of names) {
      expect(companionAvatar(styles[style].avatar, "改名")).toBe(
        styles[style].avatar,
      );
      expect(companionAppearance(styles[style].avatar).style).toBe(style);
    }
  });

  it("从任意已保存起点顺序浏览全部风格，一轮内无重复，保存后继续且最后回到起点", () => {
    for (const start of [
      ...names.map((style) => styles[style].avatar),
      "companion:v2:0123456789abcdef0123456789abcdef",
    ]) {
      const initialStyle = companionAppearance(start).style;
      const index = names.indexOf(initialStyle);
      const seen = new Set([initialStyle]);
      let avatar = start;
      for (let offset = 1; offset <= names.length; offset++) {
        avatar = nextCompanionAvatar(avatar);
        const style = companionAppearance(avatar).style;
        expect(style).toBe(names[(index + offset) % names.length]);
        if (offset < names.length) {
          expect(seen.has(style)).toBe(false);
          seen.add(style);
        } else expect(style).toBe(initialStyle);
        avatar = companionAvatar(JSON.parse(JSON.stringify(avatar)), "改名");
      }
      expect(seen.size).toBe(names.length);
    }
  });

  it("全部风格均可随机取到，保存与改名后稳定，无装饰或独立肤色", () => {
    const seen = new Set<string>();
    for (let index = 0; index < 1000; index++) {
      const avatar = `companion:v2:${index.toString(16).padStart(32, "0")}`;
      const appearance = companionAppearance(avatar);
      seen.add(appearance.style);
      expect(companionAvatar(avatar, "改名")).toBe(avatar);
      expect(companionAppearance(avatar)).toEqual(appearance);
      expect(appearance).not.toHaveProperty("accessories");
      expect(appearance).not.toHaveProperty("color");
      expect(appearance).not.toHaveProperty("palette");
      expect(appearance.glow).toBe(styles[appearance.style].glow);
    }
    expect([...seen].sort()).toEqual([...names].sort());
  });

  it.each(names)(
    "%s 使用完整图片与自身动效配色，所有状态不改变图片与肢体",
    (style) => {
      const clock = vi.spyOn(performance, "now").mockReturnValue(1000);
      const host = document.createElement("div");
      const engine = createCompanion(host, {
        character: "ally",
        emotion: "02",
        style,
        autostart: false,
        lite: false,
        idle: false,
      });
      try {
        const image = host.querySelector("image");
        expect(host.querySelectorAll("image")).toHaveLength(1);
        expect(image?.getAttribute("href")).toContain(`${style}-256.png`);
        for (const [tone, color] of Object.entries(styles[style].effects)) {
          expect(
            host
              .querySelector("svg")
              ?.style.getPropertyValue(`--companion-effect-${tone}`),
          ).toBe(color);
        }
        expect(
          [...host.querySelectorAll('[id$="eye"] stop')].map((stop) =>
            stop.getAttribute("stop-color"),
          ),
        ).toEqual(styles[style].eyes);
        expect(engine.ball.setResolution(512)).toBe(true);
        expect(image?.getAttribute("href")).toContain(`${style}-512.png`);
        const imageMarkup = image?.outerHTML;
        for (const reaction of companionReactions) {
          engine.ball.cancelAction();
          engine.setEmotion(reaction.emotion);
          if (reaction.action)
            expect(engine.ball.perform(reaction.action)).toBe(true);
          for (const time of [1200, 1550, 2100]) {
            clock.mockReturnValue(time);
            engine.renderStatic();
            expect(image?.outerHTML).toBe(imageMarkup);
            expect(host.innerHTML).not.toContain("NaN");
          }
        }
        expect(
          host.querySelector(
            ".ally-left-arm, .ally-right-arm, .ally-left-foot, .ally-right-foot, .ally-clothing-layer, .ally-accessory-head, .ally-accessory-eyewear, feColorMatrix",
          ),
        ).toBeNull();
        expect(host.querySelectorAll(".ally-eye")).toHaveLength(2);
      } finally {
        engine.destroy();
        clock.mockRestore();
      }
    },
  );

  it.each(names)("%s 的两档图片为带透明通道的 256 / 512 PNG", (style) => {
    for (const size of [256, 512]) {
      const data = readFileSync(
        resolve(
          process.cwd(),
          `../../vendor/companion/assets/bodies/${style}-${size}.png`,
        ),
      );
      expect(data.readUInt32BE(16)).toBe(size);
      expect(data.readUInt32BE(20)).toBe(size);
      expect(data[25]).toBe(6);
      expect(data.length).toBeLessThan(size === 256 ? 250_000 : 850_000);
    }
  });
});
