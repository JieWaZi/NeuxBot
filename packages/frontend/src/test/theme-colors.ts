import themeStyles from "@/shared/styles/theme.css?raw";

type Color = { r: number; g: number; b: number; a: number };

function declarations(source: string) {
  return Object.fromEntries(
    [...source.matchAll(/(--[\w-]+):\s*([^;]+);/g)].map((match) => [
      match[1],
      match[2]?.trim() ?? "",
    ]),
  );
}

export function themeTokens(theme: "light" | "dark") {
  const lightStart = themeStyles.indexOf(':root,\n[data-theme="light"] {');
  const darkStart = themeStyles.indexOf('[data-theme="dark"] {', lightStart);
  const aliasesStart = themeStyles.indexOf("/* 组件层语义接口");
  const darkOverridesStart = themeStyles.lastIndexOf('[data-theme="dark"] {');
  const references = declarations(themeStyles.slice(0, lightStart));
  const aliases = declarations(
    themeStyles.slice(aliasesStart, darkOverridesStart),
  );
  const themeValues = declarations(
    theme === "light"
      ? themeStyles.slice(lightStart, darkStart)
      : themeStyles.slice(darkStart, aliasesStart),
  );
  const overrides =
    theme === "dark" ? declarations(themeStyles.slice(darkOverridesStart)) : {};
  return { ...references, ...themeValues, ...aliases, ...overrides };
}

function mix(first: Color, second: Color, firstWeight: number): Color {
  const secondWeight = 1 - firstWeight;
  return {
    r: first.r * firstWeight + second.r * secondWeight,
    g: first.g * firstWeight + second.g * secondWeight,
    b: first.b * firstWeight + second.b * secondWeight,
    a: first.a * firstWeight + second.a * secondWeight,
  };
}

export function resolveColor(
  token: string,
  tokens: Record<string, string>,
  resolving = new Set<string>(),
): Color {
  if (resolving.has(token)) throw new Error(`颜色 Token 循环引用：${token}`);
  const value = tokens[token]?.replace(/\s+/g, " ").trim();
  if (!value) throw new Error(`颜色 Token 未定义：${token}`);
  const nextResolving = new Set(resolving).add(token);
  if (value === "transparent") return { r: 0, g: 0, b: 0, a: 0 };
  const variable = /^var\((--[\w-]+)\)$/.exec(value);
  if (variable?.[1]) return resolveColor(variable[1], tokens, nextResolving);
  const hex = /^#([\da-f]{6})$/i.exec(value)?.[1];
  if (hex) {
    return {
      r: Number.parseInt(hex.slice(0, 2), 16),
      g: Number.parseInt(hex.slice(2, 4), 16),
      b: Number.parseInt(hex.slice(4, 6), 16),
      a: 1,
    };
  }
  const rgba = /^rgba?\((\d+), (\d+), (\d+)(?:, ([\d.]+))?\)$/.exec(value);
  if (rgba) {
    return {
      r: Number(rgba[1]),
      g: Number(rgba[2]),
      b: Number(rgba[3]),
      a: rgba[4] === undefined ? 1 : Number(rgba[4]),
    };
  }
  const oklch = /^oklch\(([\d.]+)% ([\d.]+) ([\d.]+)\)$/.exec(value);
  if (oklch) {
    // 主题绿采用 OKLCH，转为 sRGB 后参与同一套文字对比度核验。
    const lightness = Number(oklch[1]) / 100;
    const chroma = Number(oklch[2]);
    const hue = (Number(oklch[3]) * Math.PI) / 180;
    const a = chroma * Math.cos(hue);
    const b = chroma * Math.sin(hue);
    const l = (lightness + 0.3963377774 * a + 0.2158037573 * b) ** 3;
    const m = (lightness - 0.1055613458 * a - 0.0638541728 * b) ** 3;
    const s = (lightness - 0.0894841775 * a - 1.291485548 * b) ** 3;
    const encode = (linear: number) =>
      Math.max(
        0,
        Math.min(
          1,
          linear <= 0.0031308
            ? 12.92 * linear
            : 1.055 * linear ** (1 / 2.4) - 0.055,
        ),
      ) * 255;
    return {
      r: encode(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s),
      g: encode(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s),
      b: encode(-0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s),
      a: 1,
    };
  }
  const transparentMix =
    /^color-mix\(\s*in srgb, var\((--[\w-]+)\) ([\d.]+)%, transparent\s*\)$/.exec(
      value,
    );
  if (transparentMix?.[1] && transparentMix[2]) {
    const color = resolveColor(transparentMix[1], tokens, nextResolving);
    return { ...color, a: (color.a * Number(transparentMix[2])) / 100 };
  }
  const colorMix =
    /^color-mix\(\s*in srgb, var\((--[\w-]+)\) ([\d.]+)%, var\((--[\w-]+)\)\s*\)$/.exec(
      value,
    );
  if (colorMix?.[1] && colorMix[2] && colorMix[3]) {
    return mix(
      resolveColor(colorMix[1], tokens, nextResolving),
      resolveColor(colorMix[3], tokens, nextResolving),
      Number(colorMix[2]) / 100,
    );
  }
  throw new Error(`暂不支持的颜色表达式：${token} = ${value}`);
}

function composite(foreground: Color, background: Color): Color {
  const alpha = foreground.a + background.a * (1 - foreground.a);
  return {
    r:
      (foreground.r * foreground.a +
        background.r * background.a * (1 - foreground.a)) /
      alpha,
    g:
      (foreground.g * foreground.a +
        background.g * background.a * (1 - foreground.a)) /
      alpha,
    b:
      (foreground.b * foreground.a +
        background.b * background.a * (1 - foreground.a)) /
      alpha,
    a: alpha,
  };
}

function luminance(color: Color) {
  return [color.r, color.g, color.b]
    .map((channel) => channel / 255)
    .map((channel) =>
      channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4,
    )
    .reduce(
      (total, channel, index) =>
        total + channel * ([0.2126, 0.7152, 0.0722][index] ?? 0),
      0,
    );
}

export function contrast(
  foregroundToken: string,
  backgroundToken: string,
  tokens: Record<string, string>,
  surfaceToken = "--surface-scene",
) {
  const scene = resolveColor(surfaceToken, tokens);
  const background = composite(resolveColor(backgroundToken, tokens), scene);
  const foreground = composite(
    resolveColor(foregroundToken, tokens),
    background,
  );
  const foregroundLuminance = luminance(foreground);
  const backgroundLuminance = luminance(background);
  return (
    (Math.max(foregroundLuminance, backgroundLuminance) + 0.05) /
    (Math.min(foregroundLuminance, backgroundLuminance) + 0.05)
  );
}
