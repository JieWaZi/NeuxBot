import path from "node:path";
import { fileURLToPath } from "node:url";
import { mergeConfig } from "vite";
import { createFrontendViteConfig } from "@neuxbot/frontend/vite";

const root = path.dirname(fileURLToPath(import.meta.url));
const corePort = Number(process.env.ALLY_WEB_CORE_PORT || 8432);
const webPort = Number(process.env.ALLY_WEB_PORT || 1422);
if (
  ![corePort, webPort].every(
    (port) => Number.isInteger(port) && port > 0 && port < 65536,
  )
) {
  throw new Error("Web 与 Core 端口必须位于 1–65535");
}
export default mergeConfig(
  createFrontendViteConfig({ root, port: webPort, base: "/" }),
  {
    server: {
      proxy: {
        "/api": {
          target: `http://127.0.0.1:${corePort}`,
          changeOrigin: true,
          ws: true,
          rewriteWsOrigin: false,
        },
      },
    },
  },
);
