import type { ExpressionId } from "@neuxbot/bloub/expressions";
import type { ShapeId } from "@neuxbot/bloub/skins";

/** 按用户选定的表情顺序提供完整头像，供选择器与新身份共用。 */
export const avatarPresets = [
  { shape: "cercle", expression: "neutre" },
  { shape: "galet", expression: "attentif" },
  { shape: "squircle", expression: "hilare" },
  { shape: "capsule", expression: "curieux" },
  { shape: "triangle", expression: "timide" },
  { shape: "hexagone", expression: "blase" },
  { shape: "nuage", expression: "gene" },
  { shape: "carre", expression: "reveur" },
  { shape: "losange", expression: "meditatif" },
  { shape: "pentagone", expression: "confus" },
] as const satisfies readonly { shape: ShapeId; expression: ExpressionId }[];
