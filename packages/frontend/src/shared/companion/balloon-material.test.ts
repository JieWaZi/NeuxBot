import { readFileSync } from "node:fs";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { SHAPES } from "@neuxbot/bloub/skins";
import { bakeBalloonMaterial } from "@neuxbot/bloub/balloon";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

beforeEach(() => {
  vi.stubGlobal(
    "Image",
    class {
      naturalWidth = 8;
      naturalHeight = 8;
      crossOrigin = "";
      onload: (() => void) | null = null;
      set src(_value: string) {
        queueMicrotask(() => this.onload?.());
      }
    },
  );
});

function graphics() {
  const calls = new Map<string, ReturnType<typeof vi.fn>>();
  const loseContext = vi.fn();
  const gl = new Proxy(
    {},
    {
      get(_target, property: string) {
        if (/^[A-Z_0-9]+$/.test(property))
          return property === "NO_ERROR" ? 0 : 1;
        let method = calls.get(property);
        if (!method) {
          method = vi.fn((...args: unknown[]) => {
            if (property === "getExtension") return { loseContext };
            if (
              property === "getShaderParameter" ||
              property === "getProgramParameter"
            )
              return true;
            if (property === "getError") return 0;
            if (property === "isContextLost") return false;
            if (property === "getUniformLocation") return args[1];
            if (property.startsWith("create")) return {};
          });
          calls.set(property, method);
        }
        return method;
      },
    },
  );
  const pixels = new Uint8ClampedArray(8 * 8 * 4);
  for (let y = 2; y < 6; y++)
    for (let x = 2; x < 6; x++) pixels[(y * 8 + x) * 4 + 3] = 255;
  const drawImage = vi.fn();
  const getImageData = vi.fn(() => ({ data: pixels }));
  const context = { drawImage, getImageData };
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockImplementation(
    (kind) => (kind === "webgl2" ? gl : context) as unknown as RenderingContext,
  );
  return { calls, loseContext, drawImage, getImageData };
}

it("软胶遮罩包含十种原生形状，与其他材质使用相同图格且不嵌入远程资源", () => {
  const svg = readFileSync(
    new URL(import.meta.resolve("@neuxbot/bloub/assets/body-balloon.svg")),
    "utf8",
  );
  expect(svg).toContain('width="1448" height="1086" viewBox="0 0 1264 948"');
  expect(
    [...svg.matchAll(/id="shape-([^"]+)"/g)].map((match) => match[1]),
  ).toEqual(
    SHAPES.filter((shape) => shape.id !== "goutte").map((shape) => shape.id),
  );
  expect(svg).not.toMatch(
    /<(?:image|script|foreignObject)\b|(?:href|filter)=|NaN|Infinity/,
  );
});

it("复用开屏的 balloon 着色，快照后立即释放 GPU，再异步编码透明纹理", async () => {
  const { calls, loseContext, drawImage } = graphics();
  let finish!: (blob: Blob | null) => void;
  vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation(
    (callback) => {
      finish = callback;
    },
  );
  const texture = bakeBalloonMaterial("/body-balloon.svg", "#22c55e");
  await vi.waitFor(() => expect(loseContext).toHaveBeenCalledOnce());
  expect(calls.get("uniform1f")).toHaveBeenCalledWith("uMaterial", 0);
  expect(calls.get("uniform3f")).toHaveBeenCalledWith(
    "uColor",
    34 / 255,
    197 / 255,
    94 / 255,
  );
  expect(calls.get("drawArrays")).toHaveBeenCalledOnce();
  expect(drawImage).toHaveBeenCalledTimes(2);
  expect(calls.get("deleteTexture")).toHaveBeenCalledTimes(3);
  expect(calls.get("deleteBuffer")).toHaveBeenCalledOnce();
  expect(calls.get("deleteProgram")).toHaveBeenCalledOnce();
  finish(new Blob(["encoded-png"], { type: "image/png" }));
  await expect(texture).resolves.toMatch(/^data:image\/png;base64,/);
});

it("烘焙中途失败也释放已分配资源，不保留头像专属上下文", async () => {
  const { calls, loseContext } = graphics();
  vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation(() => {
    throw new Error("encode failed");
  });
  await expect(
    bakeBalloonMaterial("/body-balloon.svg", "#38bdf8"),
  ).rejects.toThrow("encode failed");
  expect(calls.get("deleteTexture")).toHaveBeenCalledTimes(3);
  expect(calls.get("deleteBuffer")).toHaveBeenCalledOnce();
  expect(calls.get("deleteProgram")).toHaveBeenCalledOnce();
  expect(loseContext).toHaveBeenCalledOnce();
});

it("同一轮廓换色共用已求解的高度场，只重新烘焙颜色与高光", async () => {
  const { calls, loseContext, getImageData } = graphics();
  vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation(
    (callback) => callback(new Blob(["png"], { type: "image/png" })),
  );
  await bakeBalloonMaterial("/shared-balloon-mask.svg", "#22c55e");
  await bakeBalloonMaterial("/shared-balloon-mask.svg", "#8b5cf6");
  expect(getImageData).toHaveBeenCalledOnce();
  expect(calls.get("drawArrays")).toHaveBeenCalledTimes(2);
  expect(loseContext).toHaveBeenCalledTimes(2);
});
