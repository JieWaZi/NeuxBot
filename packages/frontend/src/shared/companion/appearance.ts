import { SHAPES, SHAPE_BY_ID, type ShapeId } from "@neuxbot/bloub/skins";
import {
  EXPRESSIONS,
  EXPRESSION_BY_ID,
  type ExpressionId,
} from "@neuxbot/bloub/expressions";

export const avatarShapes = SHAPES.filter(({ id }) => id !== "goutte");
export const avatarExpressions = EXPRESSIONS;
export {
  MATERIALS as avatarMaterials,
  PALETTE as avatarColors,
} from "@neuxbot/bloub/materials";
import {
  MATERIALS as avatarMaterials,
  PALETTE as avatarColors,
  DEFAULT_COLOR,
  type Material,
} from "@neuxbot/bloub/materials";
export type AvatarMaterial = Material;
export type CompanionAppearance = {
  material: AvatarMaterial;
  shape: ShapeId;
  expression: ExpressionId;
  color: string;
};
export const defaultCompanionAvatar = "bloub:v1:plush:cercle:neutre:8b5cf6";
const legacy = /^companion:v[12]:[0-9a-f]{32}$/;

function parse(value: string): CompanionAppearance | undefined {
  const [kind, version, material, shape, expression, color, extra] =
    value.split(":");
  const selectedMaterial =
    avatarMaterials.find((item) => item === material) ??
    (["crystal", "metal", "flat", "flora"].includes(material ?? "")
      ? "plush"
      : undefined);
  if (
    kind !== "bloub" ||
    version !== "v1" ||
    extra !== undefined ||
    !selectedMaterial ||
    !shape ||
    !expression ||
    !color ||
    !/^[0-9a-f]{6}$/i.test(color)
  )
    return;
  const body = SHAPE_BY_ID.get(shape);
  const face = EXPRESSION_BY_ID.get(expression);
  if (!body || body.id === "goutte" || !face) return;
  return {
    material: selectedMaterial,
    shape: body.id,
    expression: face.id,
    color: `#${color.toLowerCase()}`,
  };
}

export function encodeCompanionAppearance(config: CompanionAppearance): string {
  const result = `bloub:v1:${config.material}:${config.shape}:${config.expression}:${config.color.replace(/^#/, "").toLowerCase()}`;
  return parse(result) ? result : defaultCompanionAvatar;
}

/** 默认组合只由身份生成；旧标识继续作为稳定身份，不保留旧身体资源。 */
export function companionAppearance(avatar: string): CompanionAppearance {
  const saved = parse(avatar);
  if (saved) return saved;
  let hash = 2166136261;
  for (const character of avatar.slice(0, 256))
    hash = Math.imul(hash ^ character.charCodeAt(0), 16777619);
  const sample = hash >>> 0;
  return {
    material: "plush",
    shape: avatarShapes[sample % avatarShapes.length]?.id ?? "cercle",
    expression:
      avatarExpressions[(sample >>> 8) % avatarExpressions.length]?.id ??
      "neutre",
    color: avatarColors[(sample >>> 16) % avatarColors.length] ?? DEFAULT_COLOR,
  };
}

export function isCompanionAvatar(value: string): boolean {
  return Boolean(parse(value) || legacy.test(value));
}

export function companionAvatar(
  value: string | undefined,
  identity: string,
): string {
  if (value && (parse(value) || legacy.test(value))) return value;
  return encodeCompanionAppearance(
    companionAppearance(`${identity}:${value?.slice(0, 128) ?? ""}`),
  );
}

export function randomCompanionAvatar(
  material: AvatarMaterial = "plush",
): string {
  const bytes = crypto.getRandomValues(new Uint32Array(4));
  return encodeCompanionAppearance({
    material,
    shape: avatarShapes[(bytes[1] ?? 0) % avatarShapes.length]?.id ?? "cercle",
    expression:
      avatarExpressions[(bytes[2] ?? 0) % avatarExpressions.length]?.id ??
      "neutre",
    color: avatarColors[(bytes[3] ?? 0) % avatarColors.length] ?? DEFAULT_COLOR,
  });
}
