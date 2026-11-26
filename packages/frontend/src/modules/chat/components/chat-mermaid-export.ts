const PNG_SCALE = 2;
const MAX_PNG_DIMENSION = 4096;

export function readMermaidSize(svg: SVGSVGElement | null) {
  const viewBox = svg
    ?.getAttribute("viewBox")
    ?.trim()
    .split(/[\s,]+/)
    .map(Number);
  return { width: viewBox?.[2] ?? 0, height: viewBox?.[3] ?? 0 };
}

export async function createMermaidPNG(markup: string): Promise<Blob> {
  const template = document.createElement("template");
  template.innerHTML = markup;
  const svg = template.content.querySelector("svg");
  const { width, height } = readMermaidSize(svg);
  if (
    !svg ||
    !Number.isFinite(width) ||
    !Number.isFinite(height) ||
    width <= 0 ||
    height <= 0
  ) {
    throw new Error("Invalid Mermaid SVG dimensions");
  }

  // 以完整图形的 viewBox 为准，并限制超大图的光栅化内存。
  const scale = Math.min(
    PNG_SCALE,
    MAX_PNG_DIMENSION / Math.max(width, height),
  );
  const pixelWidth = Math.max(1, Math.round(width * scale));
  const pixelHeight = Math.max(1, Math.round(height * scale));
  svg.setAttribute("width", String(pixelWidth));
  svg.setAttribute("height", String(pixelHeight));
  svg.style.removeProperty("max-width");

  // 独立的 SVG 图片无法从应用根节点继承主题变量。
  const theme = getComputedStyle(document.documentElement);
  const variables = new Set(
    Array.from(markup.matchAll(/\bvar\((--[\w-]+)/g), ([, name]) => name),
  );
  for (const name of variables) {
    if (!name) continue;
    const value = theme.getPropertyValue(name).trim();
    if (value) svg.style.setProperty(name, value);
  }
  const background = theme.getPropertyValue("--background").trim();

  // XML 序列化保留 HTML 标签的命名空间。Data URL 可供画布读取含 foreignObject 的图形。
  const serialized = new XMLSerializer().serializeToString(svg);
  const image = new Image();
  image.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(serialized)}`;
  await image.decode();

  const canvas = document.createElement("canvas");
  canvas.width = pixelWidth;
  canvas.height = pixelHeight;
  const context = canvas.getContext("2d");
  if (!context) throw new Error("Could not create Mermaid export canvas");
  if (background) {
    context.fillStyle = background;
    context.fillRect(0, 0, pixelWidth, pixelHeight);
  }
  context.drawImage(image, 0, 0, pixelWidth, pixelHeight);

  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (blob) resolve(blob);
      else reject(new Error("Could not encode Mermaid PNG"));
    }, "image/png");
  });
}
