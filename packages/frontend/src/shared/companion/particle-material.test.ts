import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { BotEngine } from "@neuxbot/bloub/engine";
import { EXPRESSION_BY_ID } from "@neuxbot/bloub/expressions";
import { SHAPES, SHAPE_BY_ID } from "@neuxbot/bloub/skins";
import { avatarDrawing, type SvgDrawing } from "@neuxbot/bloub/drawing";
import { POSES, SEQUENCE } from "@neuxbot/bloub/states";

function flatten(nodes: SvgDrawing[]): SvgDrawing[] {
  return nodes.flatMap((node) => [node, ...flatten(node.children)]);
}

function drawing(time = 0, eyes = true) {
  const frame = new BotEngine(
    100,
    "idle",
    SHAPE_BY_ID.get("cercle")?.radii,
    EXPRESSION_BY_ID.get("attentif"),
  ).sample(1.2);
  return avatarDrawing(
    eyes ? frame : { ...frame, eyes: [] },
    "particles",
    "#8b5cf6",
    "particles",
    "/particles.png",
    [],
    [],
    undefined,
    "cercle",
    time,
  );
}

function grains(nodes: SvgDrawing[]) {
  return flatten(nodes).filter(
    (node) => node.attributes["data-material-particle"] !== undefined,
  );
}

describe("粒子身体的体积与面部遮挡", () => {
  it("原生图集的细点填满中心，左上光照强于右下，保持全部通用形状与透明空格", () => {
    const svg = readFileSync(
      new URL(import.meta.resolve("@neuxbot/bloub/assets/body-particles.svg")),
      "utf8",
    );
    expect(svg).toContain('width="1448" height="1086" viewBox="0 0 1264 948"');
    expect(
      [...svg.matchAll(/<g id="shape-([^"]+)"/g)].map((match) => match[1]),
    ).toEqual(
      SHAPES.filter((shape) => shape.id !== "goutte").map((shape) => shape.id),
    );
    expect(svg).not.toMatch(/<(?:rect|image|circle|filter)\b/);
    const circle = svg.match(/<g id="shape-cercle"[^>]*>(.*?)<\/g>/)?.[1] ?? "";
    let core = 0,
      edge = 0,
      bright = 0,
      brightWeight = 0,
      dark = 0,
      darkWeight = 0;
    for (const path of circle.matchAll(
      /<path d="([^"]+)" stroke="#([0-9a-f]{6})" stroke-opacity="([^"]+)" stroke-width="([^"]+)"\/>/g,
    )) {
      const gray = parseInt(path[2]?.slice(0, 2) ?? "0", 16);
      const alpha = Number(path[3]);
      expect((Number(path[4]) * 128) / 316).toBeLessThan(1);
      expect(alpha).toBeGreaterThan(0);
      expect(alpha).toBeLessThanOrEqual(1);
      for (const point of path[1]?.matchAll(/M(-?[\d.]+) (-?[\d.]+)h\.01/g) ??
        []) {
        const x = Number(point[1]),
          y = Number(point[2]),
          radius = Math.hypot(x, y);
        if (radius < 20) core++;
        if (radius > 90 && radius <= 100) edge++;
        if (x < -15 && y < -15) {
          bright += gray * alpha;
          brightWeight += alpha;
        }
        if (x > 15 && y > 15) {
          dark += gray * alpha;
          darkWeight += alpha;
        }
      }
    }
    // 等面积密度比较能拦住“只剩轮廓一圈”和眼区挖空，且不绑定微点数量。
    expect(core / 20 ** 2).toBeGreaterThan((edge / (100 ** 2 - 90 ** 2)) * 1.4);
    expect(bright / brightWeight - dark / darkWeight).toBeGreaterThan(20);
  });

  it("运行时只叠细光尘，128px呈现的单点直径不超过1px，主体不降透明度", () => {
    const nodes = flatten(drawing());
    const points = grains(drawing());
    expect(points.length).toBeGreaterThan(0);
    expect(
      Math.max(
        ...points.map((point) => (Number(point.attributes.r) * 2 * 128) / 316),
      ),
    ).toBeLessThanOrEqual(1);
    const sprite = nodes.find(
      (node) => node.attributes["data-body-sprite"] === "cercle",
    );
    expect(sprite?.attributes.opacity ?? 1).toBe(1);
    expect(nodes.filter((node) => node.tag === "filter")).toHaveLength(1);
  });

  it("十种通用轮廓均连续采样完整材质图格，不切回旧专属外形", () => {
    for (const [index, shape] of SHAPES.filter(
      (item) => item.id !== "goutte",
    ).entries()) {
      const frame = new BotEngine(100, "idle", shape.radii).sample(1.4);
      const nodes = flatten(
        avatarDrawing(
          frame,
          "particles",
          "#f97316",
          shape.id,
          "/body.svg",
          [],
          [],
          undefined,
          shape.id,
          1.4,
        ),
      );
      const sprite = nodes.find(
        (node) => node.attributes["data-body-sprite"] === shape.id,
      );
      expect(sprite?.attributes.viewBox).toBe(
        `${(index % 4) * 362} ${Math.floor(index / 4) * 362} 362 362`,
      );
      expect(nodes.find((node) => node.tag === "image")?.attributes.href).toBe(
        "/body.svg",
      );
      expect(JSON.stringify(nodes)).not.toMatch(/NaN|Infinity/);
    }
  });

  it("加眼睛只遮住胶囊本身，不删去眼睛周围或面前的粒子", () => {
    const nodes = drawing();
    const body = nodes.find(
      (node) => node.attributes["data-body-material"] === "particles",
    );
    expect(grains(nodes)).toEqual(grains(drawing(0, false)));
    const firstEye = body?.children.findIndex(
      (node) => node.attributes["data-eye"] !== undefined,
    );
    const lastParticleLayer = body?.children.findLastIndex(
      (node) => node.attributes["data-particle-layer"] !== undefined,
    );
    expect(firstEye).toBeGreaterThan(lastParticleLayer ?? -1);
    expect(flatten(nodes).some((node) => node.tag === "mask")).toBe(false);

    // 面部覆盖这片中央区域，错误的眼部排除会让其缺少粒子。
    expect(
      grains(nodes).filter(
        (node) =>
          Math.abs(Number(node.attributes.cx)) < 50 &&
          Math.abs(Number(node.attributes.cy)) < 40,
      ).length,
    ).toBeGreaterThan(0);
  });

  it("从调用方已有时钟取样：同一时间可重放，前后景随时间有不同的位移", () => {
    expect(drawing(3)).toEqual(drawing(3));
    const before = grains(drawing(0));
    const after = grains(drawing(3));
    expect(after.map((node) => node.attributes.cx)).not.toEqual(
      before.map((node) => node.attributes.cx),
    );
    expect(after.map((node) => node.attributes.opacity)).not.toEqual(
      before.map((node) => node.attributes.opacity),
    );
  });

  it("粒子随原生形变、透明度和通知缺口绘制，毛绒不增加粒子层", () => {
    for (const state of SEQUENCE) {
      const frame = new BotEngine(100, state).sample(POSES[state]);
      for (const material of ["particles", "plush"] as const) {
        const nodes = avatarDrawing(
          frame,
          material,
          "#38bdf8",
          state,
          "/body.png",
          [],
          [],
        );
        const body = nodes.find(
          (node) => node.attributes["data-body-material"] === material,
        );
        expect(body?.attributes.opacity).toBe(frame.bodyAlpha);
        expect(body?.attributes.mask).toBe(
          frame.notch ? `url(#${state}-notification-cut)` : undefined,
        );
        expect(grains(nodes).length > 0).toBe(material === "particles");
        expect(JSON.stringify(nodes)).not.toMatch(/NaN|Infinity/);
      }
    }
  });
});
