import { fileURLToPath } from "node:url";
import { createFrontendViteConfig } from "@neuxbot/frontend/vite";

export default createFrontendViteConfig({
  root: fileURLToPath(new URL(".", import.meta.url)),
  port: 1420,
  base: "./",
});
