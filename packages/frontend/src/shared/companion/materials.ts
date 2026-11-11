export { TEXTURES as materialTextures } from "@neuxbot/bloub/materials";
import type { Material } from "@neuxbot/bloub/materials";
import type { ShapeId } from "@neuxbot/bloub/skins";
import type { SvgDrawing } from "@neuxbot/bloub/drawing";

/** 材质共用面部坐标；视觉体积补偿只作用于身体素材。 */
export function materialViewBox(): string {
  return "-158 -158 316 316";
}

type Bounds = readonly [number, number, number, number];

// 当前 362px 图格中 alpha > 100 的可见边界（含端点），不计稀疏微尘。
// 与图集一起校准：粒子匹配毛绒的主体体积和中心，不再用单个倍率放大整张脸。
const bodyBounds: Partial<
  Record<ShapeId, { plush: Bounds; particles: Bounds }>
> = {
  cercle: { plush: [52, 52, 301, 304], particles: [66, 66, 294, 295] },
  galet: { plush: [43, 74, 296, 291], particles: [65, 82, 293, 283] },
  squircle: { plush: [41, 61, 287, 299], particles: [70, 70, 290, 291] },
  capsule: { plush: [32, 98, 296, 263], particles: [62, 109, 299, 252] },
  triangle: { plush: [45, 27, 305, 264], particles: [66, 52, 297, 265] },
  hexagone: { plush: [39, 38, 306, 280], particles: [62, 73, 298, 288] },
  nuage: { plush: [35, 55, 293, 277], particles: [69, 91, 290, 283] },
  carre: { plush: [40, 37, 289, 282], particles: [75, 75, 286, 286] },
  losange: { plush: [35, 16, 313, 299], particles: [50, 50, 310, 310] },
  pentagone: { plush: [37, 23, 306, 282], particles: [62, 57, 298, 287] },
};

/** 在原生形变之前校准图格，保持眼睛、眨眼和动作坐标不受材质影响。 */
export function alignMaterialDrawing(
  drawing: SvgDrawing[],
  material: Material,
  shape: ShapeId,
): SvgDrawing[] {
  const bounds = bodyBounds[shape];
  if (material !== "particles" || !bounds) return drawing;
  const { plush: target, particles: source } = bounds;
  const sx = (target[2] - target[0] + 1) / (source[2] - source[0] + 1);
  const sy = (target[3] - target[1] + 1) / (source[3] - source[1] + 1);
  const center = (min: number, max: number) =>
    (((min + max + 1) / 2 - 181) * 316) / 362;
  const x = center(target[0], target[2]) - center(source[0], source[2]) * sx;
  const y = center(target[1], target[3]) - center(source[1], source[3]) * sy;
  const align = (node: SvgDrawing): SvgDrawing => ({
    ...node,
    attributes:
      node.attributes["data-body-sprite"] !== undefined
        ? {
            ...node.attributes,
            x: -158 * sx + x,
            y: -158 * sy + y,
            width: 316 * sx,
            height: 316 * sy,
          }
        : node.attributes["data-particle-layer"] !== undefined
          ? {
              ...node.attributes,
              transform: `translate(${x},${y}) scale(${sx},${sy})`,
            }
          : node.attributes,
    children: node.children.map(align),
  });
  return drawing.map(align);
}
