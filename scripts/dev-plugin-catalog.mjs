import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(fileURLToPath(new URL("..", import.meta.url)));

function run(command, args, options) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, options);
    child.once("error", reject);
    child.once("exit", (code, signal) => {
      if (code === 0) resolve();
      else reject(new Error(`${command} exited with ${signal ?? code}`));
    });
  });
}

// 本地镜像使用稳定目录身份；只重写下载地址，不冒充正式发布源或改动包摘要。
export async function serveDevelopmentCatalog(directory) {
  const filename = path.join(directory, "catalog.json");
  const catalog = JSON.parse(await readFile(filename, "utf8"));
  if (catalog.schemaVersion !== 1 || !Array.isArray(catalog.entries)) {
    throw new Error("Local plugin catalog must use schemaVersion 1");
  }
  const files = new Set(["catalog.json"]);
  for (const entry of catalog.entries) {
    if (!/^[a-z0-9-]+$/.test(entry.key)) {
      throw new Error("Local plugin catalog contains an invalid entry key");
    }
    files.add(`${entry.key}.zip`);
    files.add(`${entry.key}.details.json`);
  }
  const server = createServer(async (request, response) => {
    const name = request.url?.slice(1);
    if (request.method !== "GET" || !files.has(name)) {
      response.writeHead(404).end();
      return;
    }
    try {
      response.end(await readFile(path.join(directory, name)));
    } catch {
      response.writeHead(404).end();
    }
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const close = () => new Promise((resolve) => server.close(resolve));
  try {
    const origin = `http://127.0.0.1:${server.address().port}`;
    for (const entry of catalog.entries) {
      entry.archiveURL = `${origin}/${entry.key}.zip`;
      entry.detailsURL = `${origin}/${entry.key}.details.json`;
    }
    await writeFile(filename, `${JSON.stringify(catalog, null, 2)}\n`);
    return { close, origin };
  } catch (error) {
    await close();
    throw error;
  }
}

export async function runPluginDevelopment(args) {
  let repository;
  let output;
  let serveOnly = false;
  let command = ["pnpm", "dev:native"];
  for (let index = 0; index < args.length; index++) {
    if (args[index] === "--serve-only") {
      serveOnly = true;
      continue;
    }
    if (args[index] === "--") {
      command = args.slice(index + 1);
      break;
    }
    const option = args[index];
    const value = args[++index];
    if (!value || value.startsWith("--")) {
      throw new Error(`Missing value for ${option}`);
    }
    if (option === "--plugins") repository = path.resolve(value);
    else if (option === "--out") output = path.resolve(value);
    else throw new Error(`Unknown option: ${option}`);
  }
  if (!repository || !command.length) {
    throw new Error(
      "Usage: pnpm dev:native:plugins --plugins <repository> [--out <directory>] [--serve-only | -- command...]",
    );
  }
  const directory =
    output ?? path.join(repository, ".cache/development/catalog");
  await run(
    process.execPath,
    [
      path.join(repository, "scripts/build-review-snapshot.mjs"),
      "--out",
      directory,
    ],
    { cwd: repository, stdio: "inherit" },
  );
  const catalog = await serveDevelopmentCatalog(directory);
  console.log(`[plugins] Local development catalog: ${directory}`);
  if (serveOnly) {
    let stop;
    try {
      await new Promise((resolve) => {
        stop = resolve;
        process.once("SIGINT", stop);
        process.once("SIGTERM", stop);
      });
      return 0;
    } finally {
      process.off("SIGINT", stop);
      process.off("SIGTERM", stop);
      await catalog.close();
    }
  }
  const child = spawn(command[0], command.slice(1), {
    cwd: root,
    stdio: "inherit",
    env: { ...process.env, ALLY_PLUGIN_CATALOG_URL: directory },
  });
  const interrupt = () => child.kill("SIGINT");
  const terminate = () => child.kill("SIGTERM");
  process.on("SIGINT", interrupt);
  process.on("SIGTERM", terminate);
  try {
    return await new Promise((resolve, reject) => {
      child.once("error", reject);
      child.once("exit", (code, signal) => resolve(code ?? (signal ? 1 : 0)));
    });
  } finally {
    process.off("SIGINT", interrupt);
    process.off("SIGTERM", terminate);
    await catalog.close();
  }
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  process.exitCode = await runPluginDevelopment(process.argv.slice(2));
}
