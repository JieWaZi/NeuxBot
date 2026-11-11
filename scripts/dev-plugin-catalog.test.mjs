import test from "node:test";
import assert from "node:assert/strict";
import { execFile, spawn } from "node:child_process";
import { once } from "node:events";
import { promisify } from "node:util";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { serveDevelopmentCatalog } from "./dev-plugin-catalog.mjs";

const execute = promisify(execFile);
const launcher = fileURLToPath(
  new URL("./dev-plugin-catalog.mjs", import.meta.url),
);

test("local plugin launcher requires an explicit repository", async () => {
  await assert.rejects(execute(process.execPath, [launcher]), /--plugins/);
});

test("local catalog serves fixed artifacts and closes with its child", async (t) => {
  const repository = await mkdtemp(path.join(tmpdir(), "neux-local-catalog-"));
  t.after(() => rm(repository, { recursive: true, force: true }));
  await mkdir(path.join(repository, "scripts"));
  await writeFile(
    path.join(repository, "scripts/build-review-snapshot.mjs"),
    `import {mkdir, writeFile} from 'node:fs/promises';
     import path from 'node:path';
     const out=process.argv[3]; await mkdir(out,{recursive:true});
     await writeFile(path.join(out,'demo.zip'),'fixed archive');
     await writeFile(path.join(out,'demo.details.json'),'fixed details');
     await writeFile(path.join(out,'catalog.json'),JSON.stringify({schemaVersion:1,entries:[{key:'demo',archiveSHA256:'unchanged-archive-digest',detailsSHA256:'unchanged-details-digest',archiveURL:'https://example.invalid/demo.zip',detailsURL:'https://example.invalid/demo.details.json'}]}));`,
  );
  const resultPath = path.join(repository, "result.json");
  const probe = path.join(repository, "probe.mjs");
  await writeFile(
    probe,
    `import {readFile,writeFile} from 'node:fs/promises';
     import path from 'node:path';
     const directory=process.env.ALLY_PLUGIN_CATALOG_URL;
     const index=JSON.parse(await readFile(path.join(directory,'catalog.json'),'utf8'));
     const entry=index.entries[0];
     const archive=await (await fetch(entry.archiveURL)).text();
     const details=await (await fetch(entry.detailsURL)).text();
     const blocked=(await fetch(new URL('/../package.json',entry.archiveURL))).status;
     await writeFile(process.argv[2],JSON.stringify({directory,entry,archive,details,blocked}));`,
  );
  await execute(process.execPath, [
    launcher,
    "--plugins",
    repository,
    "--",
    process.execPath,
    probe,
    resultPath,
  ]);
  const result = JSON.parse(await readFile(resultPath, "utf8"));
  assert.equal(
    result.directory,
    path.join(repository, ".cache/development/catalog"),
  );
  assert.equal(result.archive, "fixed archive");
  assert.equal(result.details, "fixed details");
  assert.equal(result.blocked, 404);
  assert.equal(result.entry.archiveSHA256, "unchanged-archive-digest");
  assert.equal(result.entry.detailsSHA256, "unchanged-details-digest");
  assert.equal(new URL(result.entry.archiveURL).hostname, "127.0.0.1");
  await assert.rejects(fetch(result.entry.archiveURL));

  const serverOnly = spawn(process.execPath, [
    launcher,
    "--plugins",
    repository,
    "--serve-only",
  ]);
  t.after(() => serverOnly.kill("SIGTERM"));
  await new Promise((resolve, reject) => {
    serverOnly.once("error", reject);
    serverOnly.stdout.on("data", (data) => {
      if (data.toString().includes("Local development catalog:")) resolve();
    });
    serverOnly.once("exit", () =>
      reject(new Error("catalog server exited before readiness")),
    );
  });
  const restarted = JSON.parse(
    await readFile(path.join(result.directory, "catalog.json"), "utf8"),
  );
  assert.equal(
    await (await fetch(restarted.entries[0].archiveURL)).text(),
    "fixed archive",
  );
  const exited = once(serverOnly, "exit");
  serverOnly.kill("SIGTERM");
  assert.equal((await exited)[0], 0);
  await assert.rejects(fetch(restarted.entries[0].archiveURL));
});

test("local hosting rejects the old published catalog schema", async (t) => {
  const directory = await mkdtemp(path.join(tmpdir(), "neux-old-catalog-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  await writeFile(
    path.join(directory, "catalog.json"),
    JSON.stringify({ schemaVersion: 2, entries: [] }),
  );
  await assert.rejects(serveDevelopmentCatalog(directory), /schemaVersion 1/);
});
