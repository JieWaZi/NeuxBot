import { spawn } from "node:child_process";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { buildElectron, watchElectron } from "./build-electron.mjs";

const desktop = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../desktop",
);
const require = createRequire(path.join(desktop, "package.json"));
const electron = require("electron");
const viteCLI = path.join(
  path.dirname(require.resolve("vite/package.json")),
  "bin/vite.js",
);

await buildElectron();

const vite = spawn(process.execPath, [viteCLI], {
  cwd: desktop,
  stdio: "inherit",
});

let app;
let appGeneration = 0;
let stopped = false;
/** @type {import("esbuild").BuildContext | undefined} */
let watchCtx;

const stop = async () => {
  if (stopped) return;
  stopped = true;
  vite.kill("SIGTERM");
  app?.kill("SIGTERM");
  await watchCtx?.dispose();
};

const startElectron = () => {
  if (stopped) return;
  app?.kill("SIGTERM");
  const generation = ++appGeneration;
  app = spawn(electron, ["."], {
    cwd: desktop,
    stdio: "inherit",
    env: { ...process.env, NEUXBOT_RENDERER_URL: "http://127.0.0.1:1420" },
  });
  app.on("exit", (code) => {
    if (generation !== appGeneration || stopped) return;
    void stop().then(() => {
      process.exitCode = code ?? 1;
      process.exit(process.exitCode);
    });
  });
};

process.on("SIGINT", () => {
  void stop();
});
process.on("SIGTERM", () => {
  void stop();
});
vite.on("exit", () => {
  void stop();
});

try {
  for (let attempt = 0; attempt < 100; attempt++) {
    if (stopped) throw new Error("Development server stopped");
    if (
      await fetch("http://127.0.0.1:1420", { signal: AbortSignal.timeout(500) })
        .then((r) => r.ok)
        .catch(() => false)
    )
      break;
    if (attempt === 99)
      throw new Error("Development server did not become ready");
    await new Promise((resolve) => setTimeout(resolve, 200));
  }

  startElectron();

  watchCtx = await watchElectron(() => {
    console.log("[electron] main/preload rebuilt, restarting app...");
    startElectron();
  });
} catch (error) {
  await stop();
  throw error;
}
