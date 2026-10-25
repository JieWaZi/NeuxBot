import path from "node:path";
import { fileURLToPath } from "node:url";
import { readFile, readdir } from "node:fs/promises";

import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";
import type { Plugin } from "vite";
import { graphVendorScope } from "./knowledge-graph-vendor.ts";

const currentDirectory = path.dirname(fileURLToPath(import.meta.url));
const pdfAssetsDirectory = path.join(
  currentDirectory,
  "node_modules/pdfjs-dist",
);
const pdfAssetKinds = ["cmaps", "standard_fonts", "wasm"] as const;

/** Keep PDF.js's version-matched CMaps, fonts and WASM local in dev and builds. */
function pdfAssets(): Plugin {
  return {
    name: "ally-pdfjs-assets",
    configureServer(server) {
      server.middlewares.use("/pdfjs-assets", (request, response, next) => {
        let parts: string[];
        try {
          parts = decodeURIComponent(
            new URL(request.url ?? "", "http://localhost").pathname,
          )
            .split("/")
            .filter(Boolean);
        } catch {
          response.statusCode = 400;
          response.end();
          return;
        }
        const [requestedKind, name] = parts;
        const kind = pdfAssetKinds.find((item) => item === requestedKind);
        if (
          parts.length !== 2 ||
          !kind ||
          !name ||
          !/^[A-Za-z0-9_.+-]+$/.test(name) ||
          name === "." ||
          name === ".."
        ) {
          next();
          return;
        }
        void readFile(path.join(pdfAssetsDirectory, kind, name))
          .then((bytes) => {
            response.setHeader(
              "Content-Type",
              name.endsWith(".wasm")
                ? "application/wasm"
                : "application/octet-stream",
            );
            response.end(bytes);
          })
          .catch(next);
      });
    },
    async generateBundle() {
      for (const kind of pdfAssetKinds) {
        for (const name of await readdir(path.join(pdfAssetsDirectory, kind))) {
          const source = await readFile(
            path.join(pdfAssetsDirectory, kind, name),
          );
          this.emitFile({
            type: "asset",
            fileName: `pdfjs-assets/${kind}/${name}`,
            source,
          });
        }
      }
    },
  };
}

export function createFrontendViteConfig(options: {root: string; port: number; base: string}) { return defineConfig({
  root: options.root,
  publicDir: path.join(currentDirectory, "public"),
  plugins: [react(), tailwindcss(), pdfAssets(), graphVendorScope()],
  resolve: {
    dedupe: ["react", "react-dom", "@tanstack/react-query"],
    alias: {
      "@": path.resolve(currentDirectory, "src"),
    },
  },
  base: options.base,
  build: {
    // 启动预算读取构建器生成的依赖图，涵盖启动时等待的工作台模块。
    manifest: true,
  },
  optimizeDeps: {
    // 只扫描真实 Renderer 入口，避免构建产物或外部软链接成为依赖入口。
    entries: ["index.html"],
  },
  server: {
    host: "127.0.0.1",
    port: options.port,
    fs: { allow: [path.resolve(currentDirectory, "../..")] },
    strictPort: true,
  },
  clearScreen: false,
  test: {
    // jsdom 与真实桌面验收共享资源；可进一步限制文件并发，保留原有超时。
    maxWorkers: process.env.VITEST_MAX_WORKERS || 2,
    environment: "jsdom",
    exclude: ["**/node_modules/**", "electron/**", "release/**"],
    setupFiles: [path.join(currentDirectory, "src/test/setup.ts")],
    css: true,
    testTimeout: 15_000,
    server: {
      deps: {
        inline: [/@lobehub\//],
      },
    },
  },
});
}

export default createFrontendViteConfig({root: currentDirectory, port: 1420, base: "./"});
