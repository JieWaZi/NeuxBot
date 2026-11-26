import { z } from "zod";
import type { convertToExcalidrawElements } from "@excalidraw/excalidraw";
import type {
  FixedPointBinding,
  PointBinding,
} from "@excalidraw/excalidraw/element/types";

type Skeleton = NonNullable<
  Parameters<typeof convertToExcalidrawElements>[0]
>[number];

const coordinate = z.number().min(-16_000).max(16_000);
const color = z
  .string()
  .regex(/^(?:#[\da-fA-F]{3,4}|#[\da-fA-F]{6}|#[\da-fA-F]{8}|transparent)$/);
const id = z.string().regex(/^[a-zA-Z0-9_.-]{1,80}$/);
const text = z.string().max(4096);
const fontSize = z.number().positive().max(256);
const textAlign = z.enum(["left", "center", "right"]);
const arrowhead = z
  .enum([
    "arrow",
    "bar",
    "dot",
    "triangle",
    "triangle_outline",
    "diamond",
    "diamond_outline",
    "circle",
    "circle_outline",
    "crowfoot_one",
    "crowfoot_many",
    "crowfoot_one_or_many",
  ])
  .nullable();
const ratio = z.number().min(0).max(1);
const binding = z.object({
  elementId: id,
  fixedPoint: z.tuple([ratio, ratio]).optional(),
});
const label = z.object({
  text,
  fontSize: fontSize.optional(),
  strokeColor: color.optional(),
  textAlign: textAlign.optional(),
  verticalAlign: z.enum(["top", "middle", "bottom"]).optional(),
});
const element = z.object({
  type: z.enum(["rectangle", "ellipse", "diamond", "arrow", "line", "text"]),
  id,
  x: coordinate,
  y: coordinate,
  width: coordinate.optional(),
  height: coordinate.optional(),
  angle: coordinate.optional(),
  strokeColor: color.optional(),
  backgroundColor: color.optional(),
  fillStyle: z.enum(["solid", "hachure", "cross-hatch", "zigzag"]).optional(),
  strokeStyle: z.enum(["solid", "dashed", "dotted"]).optional(),
  strokeWidth: z.number().min(0).max(64).optional(),
  roughness: z.number().min(0).max(4).optional(),
  opacity: z.number().min(0).max(100).optional(),
  roundness: z
    .object({
      type: z.literal([1, 2, 3]),
      value: z.number().min(0).max(1024).optional(),
    })
    .optional(),
  text: text.optional(),
  fontSize: fontSize.optional(),
  textAlign: textAlign.optional(),
  label: label.optional(),
  points: z
    .array(z.tuple([coordinate, coordinate]))
    .max(128)
    .optional(),
  startArrowhead: arrowhead.optional(),
  endArrowhead: arrowhead.optional(),
  startBinding: binding.optional(),
  endBinding: binding.optional(),
});
const scene = z.array(element).min(1).max(300);

function preserveFixedPoint(
  current: PointBinding | null,
  projected: z.infer<typeof binding> | undefined,
): PointBinding | FixedPointBinding | null {
  if (
    current &&
    projected?.fixedPoint &&
    current.elementId === projected.elementId
  ) {
    return { ...current, fixedPoint: projected.fixedPoint };
  }
  return current;
}

export async function renderExcalidraw(encoded: string, dark: boolean) {
  const parsed: unknown = JSON.parse(encoded);
  const input = scene.parse(parsed);
  const {
    convertToExcalidrawElements,
    exportToBlob,
    exportToSvg,
    FONT_FAMILY,
    serializeAsJSON,
  } = await import("@excalidraw/excalidraw");
  const byId = new Map(input.map((item) => [item.id, item]));
  const bindingTarget = (projected: z.infer<typeof binding> | undefined) => {
    const target = projected && byId.get(projected.elementId);
    if (
      target?.type === "rectangle" ||
      target?.type === "ellipse" ||
      target?.type === "diamond"
    ) {
      return { id: target.id };
    }
  };
  // 预览与下载使用相同的系统字体度量，桌面离线时无需远端字体。
  const fontFamily = FONT_FAMILY.Helvetica;
  const skeleton = input.map(
    ({
      type,
      text,
      fontSize,
      textAlign,
      label,
      points,
      startArrowhead,
      endArrowhead,
      startBinding,
      endBinding,
      ...style
    }): Skeleton => {
      const boundLabel = label && { ...label, fontFamily };
      switch (type) {
        case "text":
          return {
            ...style,
            type,
            text: text ?? "",
            fontFamily,
            fontSize,
            textAlign,
          };
        case "line":
        case "arrow": {
          // SDK 用逻辑或默认宽度，显式零跨度需要直接构造端点。
          const linePoints: [number, number][] = points?.length
            ? points
            : [
                [0, 0],
                [style.width ?? 100, style.height ?? 0],
              ];
          if (type === "line") return { ...style, type, points: linePoints };
          return {
            ...style,
            type,
            points: linePoints,
            label: boundLabel,
            startArrowhead,
            endArrowhead,
            start: bindingTarget(startBinding),
            end: bindingTarget(endBinding),
          };
        }
        default:
          return { ...style, type, label: boundLabel };
      }
    },
  );
  const elements = convertToExcalidrawElements(skeleton, {
    regenerateIds: false,
  }).map((item) => {
    if (item.type !== "arrow") return item;
    const projected = byId.get(item.id);
    const startBinding = preserveFixedPoint(
      item.startBinding,
      projected?.startBinding,
    );
    const endBinding = preserveFixedPoint(
      item.endBinding,
      projected?.endBinding,
    );
    if (startBinding === item.startBinding && endBinding === item.endBinding)
      return item;
    // 保留 SDK 生成的双向绑定以及工具指定的连接位置和端点。
    return {
      ...item,
      points: projected?.points?.length ? projected.points : item.points,
      startBinding,
      endBinding,
    };
  });
  const appState = { viewBackgroundColor: "#ffffff", exportBackground: true };
  const files = {};
  const exportOptions = { elements, appState, files, exportPadding: 16 };
  const svg: SVGSVGElement = await exportToSvg({
    ...exportOptions,
    appState: {
      ...appState,
      exportBackground: false,
      exportWithDarkMode: dark,
    },
    skipInliningFonts: true,
  });
  const width = Number(svg.getAttribute("width"));
  const height = Number(svg.getAttribute("height"));
  if (
    !Number.isFinite(width) ||
    !Number.isFinite(height) ||
    width <= 0 ||
    height <= 0
  ) {
    throw new Error("Excalidraw export is missing positive dimensions");
  }
  // 用原生 SVG 命名空间序列化，避免 SDK 添加的普通 xmlns 属性造成重复声明。
  svg.removeAttribute("xmlns");
  const svgText = new XMLSerializer().serializeToString(svg);

  return {
    image: `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svgText)}`,
    width,
    height,
    source: serializeAsJSON(elements, appState, files, "local"),
    createPNG: (): Promise<Blob> =>
      exportToBlob({
        ...exportOptions,
        mimeType: "image/png",
        getDimensions: (width: number, height: number) => {
          const scale = Math.min(2, 4096 / Math.max(width, height));
          return {
            width: Math.max(1, Math.ceil(width * scale)),
            height: Math.max(1, Math.ceil(height * scale)),
            scale,
          };
        },
      }),
  };
}
