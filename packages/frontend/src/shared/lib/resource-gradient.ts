// Adapted from OpenBot v0.62.0 packages/brand/src/article-gradient.ts.
// Source and license: THIRD_PARTY_NOTICES.md.
// NeuxBot 的浅暗调色板由共享主题层维护；CSS 直接引用，WebGL 按当前主题解析。
// 颜色分组、标题哈希与所有 mesh 参数保持源端算法，主题只改变色值。

const LILAC = "var(--resource-artwork-lilac)";
const EMBER = "var(--resource-artwork-ember)";
const MINT = "var(--resource-artwork-mint)";
const ACCENT = "var(--resource-artwork-blue)";
const SKY = "var(--resource-artwork-sky)";
const INDIGO = "var(--resource-artwork-indigo)";
const CYAN = "var(--resource-artwork-cyan)";
const TERRACOTTA = "var(--resource-artwork-terracotta)";
const CORAL = "var(--resource-artwork-coral)";
const MAGENTA = "var(--resource-artwork-magenta)";
const DIM = "var(--resource-artwork-dim)";
const CANVAS = "var(--resource-artwork-canvas)";

// Neighbouring anchors give each family depth before it fades into the canvas.
const VIOLET_DEEP = "var(--resource-artwork-violet-deep)";
const RED_DEEP = "var(--resource-artwork-red-deep)";
const GREEN_DEEP = "var(--resource-artwork-green-deep)";
const BLUE_DEEP = "var(--resource-artwork-blue-deep)";
const PLUM_DEEP = "var(--resource-artwork-plum-deep)";
const SILVER = "var(--resource-artwork-silver)";

// Twelve families, each one hue band rather than a spread of the whole wheel: a
// dominant colour, two neighbours, and an anchor. A card is meant to read as
// one colour seen from several sides, which is also what keeps a row of them
// looking like one system rather than a swatch page. Every family ends on the
// canvas colour, and that is what makes a card fade into the page at its edges
// instead of sitting on it as a rectangle.
const FAMILIES = [
  // Lilac
  [LILAC, VIOLET_DEEP, INDIGO, ACCENT, CANVAS],
  // Ember
  [EMBER, RED_DEEP, CORAL, TERRACOTTA, CANVAS],
  // Mint
  [MINT, GREEN_DEEP, CYAN, ACCENT, CANVAS],
  // Deep water
  [ACCENT, BLUE_DEEP, SKY, INDIGO, CANVAS],
  // Sunset
  [EMBER, LILAC, PLUM_DEEP, MAGENTA, CANVAS],
  // Graphite
  [SILVER, DIM, LILAC, SKY, CANVAS],
  // Terracotta
  [TERRACOTTA, EMBER, PLUM_DEEP, VIOLET_DEEP, CANVAS],
  // Lagoon
  [CYAN, ACCENT, MINT, SKY, CANVAS],
  // Magenta
  [MAGENTA, PLUM_DEEP, LILAC, VIOLET_DEEP, CANVAS],
  // Indigo
  [INDIGO, VIOLET_DEEP, SKY, LILAC, CANVAS],
  // Rosewood
  [CORAL, MAGENTA, INDIGO, LILAC, CANVAS],
  // Forest
  [GREEN_DEEP, MINT, CYAN, BLUE_DEEP, CANVAS],
] as const;

export interface ArticleGradient {
  /** Theme colour references resolved to shader colours when mounted. */
  colors: readonly string[];
  /** Organic noise distortion, 0 to 1. */
  distortion: number;
  /** Vortex distortion, 0 to 1. */
  swirl: number;
  /** Grain worked into the shape edges, 0 to 1. */
  grainMixer: number;
  /** Degrees, 0 to 360. */
  rotation: number;
  /**
   * Milliseconds into the animation. With `speed: 0` this is the whole of the
   * shader's time input, so it is what makes a still render reproducible.
   */
  frame: number;
}

// FNV-1a. Chosen because it is eight lines and has no dependencies, not for any
// cryptographic property: the only requirement is that it never changes.
function hashTitle(title: string): number {
  let hash = 0x811c9dc5;
  for (let index = 0; index < title.length; index += 1) {
    hash ^= title.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash;
}

// Successive independent-ish draws from one hash. Mixing between reads keeps the
// low bits of the original hash from driving every parameter at once.
function createDraw(seed: number): (range: number) => number {
  let state = seed;
  return (range) => {
    state ^= state << 13;
    state >>>= 0;
    state ^= state >> 17;
    state ^= state << 5;
    state >>>= 0;
    return state % range;
  };
}

export function articleGradient(title: string): ArticleGradient {
  const hash = hashTitle(title);
  const draw = createDraw(hash || 1);
  const family = FAMILIES[hash % FAMILIES.length] ?? FAMILIES[0];

  return {
    colors: [...family],
    // Kept well short of 1: past roughly 0.8 the colour spots smear into mud and
    // every article starts to look like the same brown cloud.
    distortion: round(0.55 + draw(26) / 100),
    swirl: round(0.08 + draw(22) / 100),
    grainMixer: round(0.1 + draw(16) / 100),
    rotation: draw(360),
    frame: draw(40_000),
  };
}

function round(value: number): number {
  return Math.round(value * 100) / 100;
}

/**
 * The mesh gradient's uniforms. Takes the colour converter as an argument rather
 * than importing it, so that this module — which the sitemap route and the feed
 * also pull in — never drags the shader library into the Worker bundle. The
 * browser component and the build-time generator both call this, which is what
 * makes the live shader and the baked image the same picture.
 */
export function articleGradientUniforms(
  gradient: ArticleGradient,
  toShaderColor: (hex: string) => number[],
): Record<string, number | number[] | number[][]> {
  return {
    u_colors: gradient.colors.map(toShaderColor),
    u_colorsCount: gradient.colors.length,
    u_distortion: gradient.distortion,
    u_swirl: gradient.swirl,
    u_grainMixer: gradient.grainMixer,
    u_grainOverlay: 0,
    // `worldWidth`/`worldHeight` of 0 means there is no fixed world to fit, so
    // `u_fit` has nothing to act on and scale alone decides the framing.
    u_fit: 0,
    u_scale: 1,
    u_rotation: gradient.rotation,
    u_originX: 0.5,
    u_originY: 0.5,
    u_offsetX: 0,
    u_offsetY: 0,
    u_worldWidth: 0,
    u_worldHeight: 0,
  };
}

/**
 * A plain CSS approximation of the same colours, used as the layer underneath the
 * generated image. It renders during SSR, covers the moment before the PNG
 * decodes, and is what the card falls back to if the image is ever missing — so a
 * failed generation degrades to a duller card rather than an empty grey box.
 */
export function articleGradientCss(gradient: ArticleGradient): string {
  const [first, second, third, fourth] = gradient.colors;
  // The last entry of a family is always the page colour, so it is the base the
  // spots sit on rather than a spot of its own.
  const base = gradient.colors[gradient.colors.length - 1];
  return [
    `radial-gradient(circle at 22% 26%, ${first} 0%, transparent 55%)`,
    `radial-gradient(circle at 78% 22%, ${second} 0%, transparent 52%)`,
    `radial-gradient(circle at 32% 82%, ${third} 0%, transparent 58%)`,
    `radial-gradient(circle at 84% 76%, ${fourth} 0%, transparent 54%)`,
    `linear-gradient(135deg, ${second} 0%, ${base} 100%)`,
  ].join(", ");
}
