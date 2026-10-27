import path from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync } from "node:child_process";
import { smokeCore } from "./smoke-core.mjs";
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const release = path.join(root, "desktop/release");
const bundle =
  process.platform === "darwin"
    ? path.join(
        release,
        process.arch === "arm64" ? "mac-arm64" : "mac",
        "NeuxBot.app",
      )
    : path.join(
        release,
        process.arch === "arm64" ? "win-arm64-unpacked" : "win-unpacked",
      );
const resources =
  process.platform === "darwin"
    ? path.join(bundle, "Contents/Resources")
    : path.join(bundle, "resources");
const executable =
  process.platform === "darwin"
    ? path.join(bundle, "Contents/MacOS/NeuxBot")
    : path.join(bundle, "NeuxBot.exe");
execFileSync(
  executable,
  [
    path.join(root, "scripts/smoke-desktop.cjs"),
    resources,
    path.join(root, "desktop/dist"),
  ],
  {
    env: { ...process.env, ELECTRON_RUN_AS_NODE: "1" },
    stdio: "inherit",
    timeout: 15000,
  },
);
await smokeCore({
  binary: path.join(
    resources,
    "bin",
    process.platform === "win32" ? "ally-core.exe" : "ally-core",
  ),
  resources,
  executable,
  label: "Packaged Go Core",
});
