import type { Plugin } from "vite";

/** Native 使用自有 PDF 页面与 Worker，不交付未接入的表单脚本沙箱。 */
export function unusedNativePdfAsset(kind: string, name: string): boolean {
  return (
    kind === "wasm" &&
    (name === "quickjs-eval.js" || name === "quickjs-eval.wasm")
  );
}

/** Electron 的 Chromium 支持 WOFF2；构建前移除 KaTeX 的旧格式回退引用。 */
export function nativeRendererAssets(): Plugin {
  return {
    name: "neuxbot-native-renderer-assets",
    apply: "build",
    enforce: "pre",
    transform(code, id) {
      if (
        !/[/\\]node_modules[/\\]katex[/\\]dist[/\\]katex(?:\.min)?\.css$/.test(
          id,
        )
      )
        return;
      return {
        code: code.replace(
          /\bsrc\s*:\s*([^;}]+)/g,
          (_declaration, sources: string) => {
            const woff2 = sources
              .split(",")
              .filter((source) => /\.woff2["')]/.test(source));
            if (woff2.length === 0)
              throw new Error("KaTeX 字体缺少 Native 交付所需的 WOFF2");
            return `src:${woff2.join(",")}`;
          },
        ),
        map: null,
      };
    },
  };
}
