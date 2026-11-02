import path from "node:path";
import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

const directory = path.dirname(fileURLToPath(import.meta.url));
const frontend = path.resolve(directory, "../..");
const baseline =
  process.env.NEUX_MARKDOWN_AB_BASELINE ?? "/private/tmp/neux-chat-ab-baseline";
const source =
  process.env.NEUX_MARKDOWN_AB_VARIANT === "baseline"
    ? path.join(baseline, "packages/frontend/src")
    : path.join(frontend, "src");

// A separate process selects the whole original source graph, including its
// original relative adapters. Neither renderer nor highlighter is substituted.
export default defineConfig({
  root: frontend,
  plugins: [react()],
  resolve: {
    alias: { "@": source },
    dedupe: [
      "react",
      "react-dom",
      "@tanstack/react-query",
      "@assistant-ui/react",
    ],
  },
  test: {
    environment: "jsdom",
    include: ["src/test/performance/markdown-ab.test.tsx"],
    maxWorkers: 1,
    fileParallelism: false,
    testTimeout: 180_000,
    css: false,
    server: { deps: { inline: [/@lobehub\//] } },
  },
});
