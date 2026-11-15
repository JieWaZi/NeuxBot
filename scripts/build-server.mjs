import { execFileSync } from "node:child_process";
import { mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { botCoreLdflags } from "./go-ldflags.mjs";
import "./builtin-skills.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const output = path.join(root, "server/bin/bot-core");
await mkdir(path.dirname(output), { recursive: true });
execFileSync(
  "go",
  [
    "build",
    "-trimpath",
    `-ldflags=${botCoreLdflags()}`,
    "-o",
    output,
    "./cmd/bot-core",
  ],
  {
    cwd: path.join(root, "server"),
    stdio: "inherit",
  },
);
console.log(`已生成 NeuxBot Core: ${output}`);

const cliOutput = path.join(
  path.dirname(output),
  process.platform === "win32" ? "botcli.exe" : "botcli",
);
execFileSync(
  "go",
  [
    "build",
    "-trimpath",
    `-ldflags=${botCoreLdflags()}`,
    "-o",
    cliOutput,
    "./cmd/botcli",
  ],
  { cwd: path.join(root, "server"), stdio: "inherit" },
);
console.log(`已生成软件能力 CLI: ${cliOutput}`);
