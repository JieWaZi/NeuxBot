import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
if (process.platform !== "darwin") throw new Error("本地应用签名只支持 macOS");
const bundle = path.join(
  root,
  "desktop/release",
  process.arch === "arm64" ? "mac-arm64" : "mac",
  "NeuxBot.app",
);
execFileSync(
  "/usr/bin/codesign",
  ["--force", "--deep", "--sign", "-", bundle],
  { stdio: "inherit" },
);
execFileSync("/usr/bin/codesign", ["--verify", "--deep", "--strict", bundle], {
  stdio: "inherit",
});
