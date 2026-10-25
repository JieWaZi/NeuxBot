import { buildSync } from "esbuild";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
const require = createRequire(import.meta.url);
export function loadModule(entry, overrides = {}) {
  const source = buildSync({
    entryPoints: [fileURLToPath(new URL(entry, import.meta.url))],
    bundle: true,
    platform: "node",
    format: "cjs",
    write: false,
    external: ["node-pty", "@xterm/*"],
  }).outputFiles[0].text;
  const module = { exports: {} };
  new Function("require", "module", "exports", source)(
    (name) => (name in overrides ? overrides[name] : require(name)),
    module,
    module.exports,
  );
  return module.exports;
}
