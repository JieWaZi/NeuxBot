import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderExcalidraw } from "./chat-excalidraw-render";

beforeEach(() => {
  // SDK 的导出模块初始化时检测 Canvas 过滤能力，SVG 用例无需栅格绘制。
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({
    filter: "none",
  } as CanvasRenderingContext2D);
});

afterEach(() => {
  vi.restoreAllMocks();
});

it("serializes the preview as a valid standalone SVG image", async () => {
  const result = await renderExcalidraw(
    '[{"type":"rectangle","id":"r","x":0,"y":0,"width":100,"height":40}]',
    false,
  );
  const payload = result.image.split(",")[1] ?? "";
  const decoded = new DOMParser().parseFromString(
    decodeURIComponent(payload),
    "image/svg+xml",
  );

  expect(decoded.documentElement.localName).toBe("svg");
  expect(decoded.documentElement.namespaceURI).toBe(
    "http://www.w3.org/2000/svg",
  );
  expect(decoded.documentElement.getAttribute("width")).toBe(
    String(result.width),
  );
  expect(decoded.querySelector("path")).toBeTruthy();
});

it("leaves unsupported fields out of the exported drawing", async () => {
  const { source } = await renderExcalidraw(
    JSON.stringify([
      {
        type: "rectangle",
        id: "r",
        x: 0,
        y: 0,
        width: 100,
        height: 40,
        fillStyle: "solid",
        backgroundColor: "#abc",
        link: "https://example.invalid",
        groupIds: ["unexpected"],
      },
    ]),
    false,
  );

  expect(JSON.parse(source)).toMatchObject({
    elements: [
      {
        id: "r",
        fillStyle: "solid",
        backgroundColor: "#abc",
        link: null,
        groupIds: [],
      },
    ],
  });
});

it("rejects invalid supported settings before handing them to Excalidraw", async () => {
  await expect(
    renderExcalidraw(
      '[{"type":"rectangle","id":"r","x":0,"y":0,"width":100,"height":40,"fillStyle":"unknown"}]',
      false,
    ),
  ).rejects.toThrow(/fillStyle/);
});

it("uses explicit zero dimensions for lines with no points", async () => {
  const { source } = await renderExcalidraw(
    JSON.stringify([
      { type: "line", id: "vertical", x: 0, y: 0, width: 0, height: 60 },
      { type: "line", id: "horizontal", x: 0, y: 0, width: 100, height: 0 },
    ]),
    false,
  );

  expect(JSON.parse(source)).toMatchObject({
    elements: [
      {
        id: "vertical",
        points: [
          [0, 0],
          [0, 60],
        ],
      },
      {
        id: "horizontal",
        points: [
          [0, 0],
          [100, 0],
        ],
      },
    ],
  });
});

it("retains fixed endpoints and reciprocal arrow bindings in the source", async () => {
  const { source } = await renderExcalidraw(
    JSON.stringify([
      { type: "rectangle", id: "start", x: 0, y: 0, width: 100, height: 80 },
      { type: "ellipse", id: "end", x: 200, y: 0, width: 100, height: 80 },
      {
        type: "arrow",
        id: "arrow",
        x: 100,
        y: 20,
        points: [
          [0, 0],
          [100, 40],
        ],
        startBinding: { elementId: "start", fixedPoint: [1, 0.25] },
        endBinding: { elementId: "end", fixedPoint: [0, 0.75] },
      },
    ]),
    false,
  );

  expect(JSON.parse(source)).toMatchObject({
    elements: [
      { id: "start", boundElements: [{ id: "arrow", type: "arrow" }] },
      { id: "end", boundElements: [{ id: "arrow", type: "arrow" }] },
      {
        id: "arrow",
        elbowed: false,
        points: [
          [0, 0],
          [100, 40],
        ],
        startBinding: { elementId: "start", fixedPoint: [1, 0.25] },
        endBinding: { elementId: "end", fixedPoint: [0, 0.75] },
      },
    ],
  });
});
