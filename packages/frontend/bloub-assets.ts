import { createRequire } from "node:module";
import path from "node:path";
import { normalizePath } from "vite";
import type { Plugin } from "vite";

const plushTexture = "@neuxbot/bloub/assets/body-plush.png";

/** HTML 资产不经过包解析器；先解析公开导出，再由 Vite 与纹理模块共用资产。 */
export function bloubTexturePreload(frontendDirectory: string): Plugin {
  const frontendRequire = createRequire(
    path.join(frontendDirectory, "package.json"),
  );
  return {
    name: "neuxbot-bloub-texture-preload",
    transformIndexHtml: {
      order: "pre",
      handler(html, context) {
        if (!html.includes(plushTexture)) return;
        const texture = frontendRequire.resolve(plushTexture);
        const href = context.server
          ? `/@fs/${normalizePath(texture).replace(/^\//, "")}`
          : normalizePath(
              path.relative(path.dirname(context.filename), texture),
            );
        return html.replace(plushTexture, href);
      },
    },
  };
}
