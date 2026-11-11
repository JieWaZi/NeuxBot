import assert from "node:assert/strict";
import {
  chmod,
  mkdir,
  mkdtemp,
  readFile,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { copyBloubLicenses } from "./bloub-licenses.mjs";

test("Bloub 许可由实际依赖的公开导出复制，并可替换上次构建产物", async (t) => {
  const root = await mkdtemp(path.join(tmpdir(), "neuxbot-bloub-licenses-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const frontend = path.join(root, "packages/frontend");
  const dependency = path.join(frontend, "node_modules/@neuxbot/bloub");
  const destination = path.join(root, "licenses/bloub");
  await mkdir(path.join(dependency, "legal"), { recursive: true });
  await mkdir(destination, { recursive: true });
  await writeFile(path.join(frontend, "package.json"), '{"private":true}');
  await writeFile(
    path.join(dependency, "package.json"),
    JSON.stringify({
      name: "@neuxbot/bloub",
      exports: {
        "./LICENSE": "./legal/engine.txt",
        "./LICENSE.dsh-bloub-mood": "./legal/expressions.txt",
      },
    }),
  );
  await writeFile(
    path.join(dependency, "legal/engine.txt"),
    "Bloub engine license\n",
  );
  await writeFile(
    path.join(dependency, "legal/expressions.txt"),
    "Expression source license\n",
  );
  for (const name of ["LICENSE", "LICENSE.dsh-bloub-mood"]) {
    await writeFile(
      path.join(destination, name),
      "previous dependency version\n",
    );
    await chmod(path.join(destination, name), 0o444);
  }

  await copyBloubLicenses(root, destination);

  assert.equal(
    await readFile(path.join(destination, "LICENSE"), "utf8"),
    "Bloub engine license\n",
  );
  assert.equal(
    await readFile(path.join(destination, "LICENSE.dsh-bloub-mood"), "utf8"),
    "Expression source license\n",
  );
});
