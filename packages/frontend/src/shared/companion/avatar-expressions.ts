import {
  EXPRESSIONS,
  type BotExpression,
  type ExpressionId,
} from "@neuxbot/bloub/expressions";

type EyeSizes = readonly [readonly [number, number], readonly [number, number]];

/** 仅校准失衡的眼型；保留原生视线、倾斜、眼距和闭合程度。 */
const sizes: Partial<Record<ExpressionId, EyeSizes>> = {
  neutre: [
    [0.21, 0.412],
    [0.21, 0.412],
  ],
  surpris: [
    [0.32, 0.4],
    [0.32, 0.4],
  ],
  excite: [
    [0.32, 0.48],
    [0.32, 0.48],
  ],
  effraye: [
    [0.32, 0.48],
    [0.32, 0.48],
  ],
  hilare: [
    [0.34, 0.16],
    [0.34, 0.16],
  ],
  colere: [
    [0.34, 0.17],
    [0.34, 0.17],
  ],
  fier: [
    [0.3, 0.17],
    [0.3, 0.17],
  ],
  timide: [
    [0.21, 0.32],
    [0.21, 0.32],
  ],
  blase: [
    [0.3, 0.15],
    [0.3, 0.15],
  ],
  complice: [
    [0.28, 0.1],
    [0.21, 0.4],
  ],
  determine: [
    [0.21, 0.34],
    [0.21, 0.34],
  ],
  reveur: [
    [0.18, 0.2],
    [0.18, 0.2],
  ],
  inquiet: [
    [0.21, 0.32],
    [0.21, 0.32],
  ],
  espiegle: [
    [0.32, 0.13],
    [0.21, 0.36],
  ],
  meditatif: [
    [0.28, 0.14],
    [0.28, 0.14],
  ],
  ebloui: [
    [0.17, 0.32],
    [0.17, 0.32],
  ],
  gene: [
    [0.32, 0.14],
    [0.22, 0.14],
  ],
};

export const avatarExpressions = EXPRESSIONS.map<BotExpression>((face) => {
  const dimensions = sizes[face.id];
  if (!dimensions) return face;
  const resize = (index: 0 | 1) => ({
    ...face.eyes[index],
    w: dimensions[index][0],
    h: dimensions[index][1],
  });
  return { ...face, eyes: [resize(0), resize(1)] };
});

export const avatarExpressionById = new Map<string, BotExpression>(
  avatarExpressions.map((face) => [face.id, face]),
);
