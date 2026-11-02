import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import {
  mkdir,
  mkdtemp,
  readdir,
  readFile,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";
import { loadModule } from "./load-module.mjs";

const { ApplicationUpdate } = loadModule("main/application-update.ts");
const api = "https://api.github.com/repos/JieWaZi/NeuxBot/releases/latest";
const bytes = Buffer.from("NeuxBot installer fixture");
const digest = createHash("sha256").update(bytes).digest("hex");
const names = [
  "NeuxBot-mac-arm64.dmg",
  "NeuxBot-mac-x64.dmg",
  "NeuxBot-windows-x64.exe",
];

function release(version = "0.2.0") {
  return {
    tag_name: `v${version}`,
    draft: false,
    prerelease: false,
    body: "改进伙伴工作台。",
    published_at: "2026-10-16T00:00:00Z",
    assets: names.map((name) => ({
      name,
      state: "uploaded",
      size: bytes.length,
      digest: `sha256:${digest}`,
    })),
  };
}

async function fixture(t, options = {}) {
  const directory = await mkdtemp(path.join(tmpdir(), "neuxally-update-"));
  const calls = [];
  const opened = [];
  const updater = new ApplicationUpdate({
    version: "0.1.0",
    packaged: true,
    platform: "darwin",
    arch: "arm64",
    cacheDirectory: directory,
    fetch: async (url) => {
      calls.push(url);
      return url === api ? Response.json(release()) : new Response(bytes);
    },
    openInstaller: async (file) => {
      opened.push(file);
      return "";
    },
    ...options,
  });
  t.after(async () => {
    await updater.dispose();
    await rm(directory, { recursive: true, force: true });
  });
  return { updater, directory, calls, opened };
}

test("按数字比较正式版本，忽略相同或较低版本并拒绝异常发行信息", async (t) => {
  for (const [version, expected] of [
    ["0.1.0", "current"],
    ["0.0.9", "current"],
    ["0.10.0", "available"],
    ["0.2.0-beta.1", "error"],
    ["../../1.0.0", "error"],
  ]) {
    const { updater } = await fixture(t, {
      fetch: async () => Response.json(release(version)),
    });
    assert.equal((await updater.check()).status, expected, version);
  }
  for (const field of ["draft", "prerelease"]) {
    const data = release();
    data[field] = true;
    const { updater } = await fixture(t, {
      fetch: async () => Response.json(data),
    });
    assert.equal((await updater.check()).error, "invalid-release");
  }
});

test("无公开发行、限流和网络故障分别表达，不伪装成已是最新", async (t) => {
  for (const [status, expected] of [
    [404, "no-release"],
    [403, "error"],
    [429, "error"],
    [500, "error"],
  ]) {
    const { updater } = await fixture(t, {
      fetch: async () => new Response(null, { status }),
    });
    const state = await updater.check();
    assert.equal(state.status, expected);
    if ([403, 429].includes(status)) assert.equal(state.error, "rate-limited");
  }
  const { updater } = await fixture(t, {
    fetch: async () => {
      throw new Error("offline");
    },
  });
  assert.equal((await updater.check()).error, "network");
});

test("只选择已支持的平台安装包，开发模式可检查但不下载", async (t) => {
  for (const [platform, arch, name] of [
    ["darwin", "arm64", names[0]],
    ["darwin", "x64", names[1]],
    ["win32", "x64", names[2]],
  ]) {
    const { updater, calls, opened } = await fixture(t, { platform, arch });
    assert.equal((await updater.check()).status, "available");
    assert.equal((await updater.download()).status, "downloaded");
    assert.ok(calls.at(-1).endsWith(`/${name}`));
    await updater.openInstaller();
    assert.equal(path.basename(opened[0]), name);
  }
  const { updater: devUpdater, calls: devCalls } = await fixture(t, {
    packaged: false,
  });
  assert.equal((await devUpdater.check()).status, "available");
  assert.equal(devCalls.length, 1);
  assert.equal((await devUpdater.download()).status, "available");
  assert.equal(devCalls.length, 1);
  await devUpdater.openInstaller();
  assert.equal(devCalls.length, 1);
  for (const options of [
    { platform: "linux" },
    { platform: "win32", arch: "arm64" },
  ]) {
    const { updater, calls } = await fixture(t, options);
    assert.equal((await updater.check()).status, "unavailable");
    await updater.download();
    assert.equal(calls.length, 0);
  }
});

test("缺少架构安装包或校验来源时阻止下载", async (t) => {
  const missing = release();
  missing.assets = [];
  const first = await fixture(t, { fetch: async () => Response.json(missing) });
  assert.equal((await first.updater.check()).error, "missing-asset");
  const unchecked = release();
  unchecked.assets = unchecked.assets.map((asset) => ({
    ...asset,
    digest: null,
  }));
  const second = await fixture(t, {
    fetch: async () => Response.json(unchecked),
  });
  assert.equal((await second.updater.check()).error, "checksum");
});

test("重复检查与下载复用同一操作，多块字节完整写入且进度单调递增", async (t) => {
  let downloads = 0;
  const states = [];
  const { updater, directory, opened } = await fixture(t, {
    fetch: async (url) => {
      if (url === api) return Response.json(release());
      downloads++;
      return new Response(
        new ReadableStream({
          start(controller) {
            controller.enqueue(bytes.subarray(0, 5));
            controller.enqueue(bytes.subarray(5));
            controller.close();
          },
        }),
      );
    },
  });
  const stop = updater.subscribe((state) => states.push(state));
  const checking = updater.check();
  assert.equal(updater.check(), checking);
  await checking;
  const downloading = updater.download();
  assert.equal(updater.download(), downloading);
  assert.equal(updater.check(), downloading);
  const state = await downloading;
  assert.equal(state.status, "downloaded");
  assert.equal(state.currentVersion, "0.1.0");
  assert.equal(downloads, 1);
  assert.equal(state.downloadedBytes, bytes.length);
  assert.deepEqual(
    await readFile(path.join(directory, "v0.2.0", names[0])),
    bytes,
  );
  assert.deepEqual(await readdir(path.join(directory, "v0.2.0")), [names[0]]);
  assert.equal(opened.length, 0);
  await updater.openInstaller();
  assert.equal(opened.length, 1);
  for (let index = 1; index < states.length; index++)
    assert.ok(states[index].revision > states[index - 1].revision);
  stop();
});

test("错误校验和、截断或超长下载均清理临时文件，之后可以重新下载", async (t) => {
  for (const corrupt of [
    Buffer.alloc(bytes.length),
    bytes.subarray(1),
    Buffer.concat([bytes, bytes]),
  ]) {
    let broken = true;
    const { updater, directory, opened } = await fixture(t, {
      fetch: async (url) =>
        url === api
          ? Response.json(release())
          : new Response(broken ? corrupt : bytes),
    });
    await updater.check();
    assert.equal((await updater.download()).error, "checksum");
    assert.deepEqual(await readdir(path.join(directory, "v0.2.0")), []);
    await updater.openInstaller();
    assert.equal(opened.length, 0);
    broken = false;
    assert.equal((await updater.download()).status, "downloaded");
  }
});

test("旧发行可使用 SHA256SUMS，缓存复用和安装前再次校验都有效", async (t) => {
  const data = release();
  data.assets = data.assets.map((asset) => ({ ...asset, digest: null }));
  data.assets.push({ name: "SHA256SUMS", state: "uploaded", size: 200 });
  const calls = [];
  const fetch = async (url) => {
    calls.push(url);
    if (url === api) return Response.json(data);
    if (url.endsWith("SHA256SUMS"))
      return new Response(`${digest}  ${names[0]}\n`);
    return new Response(bytes);
  };
  const { updater, directory, opened } = await fixture(t, { fetch });
  await updater.check();
  await updater.download();
  await updater.openInstaller();
  assert.equal(opened.length, 1);

  const cached = await fixture(t, { fetch, cacheDirectory: directory });
  await cached.updater.check();
  const before = calls.length;
  assert.equal((await cached.updater.download()).status, "downloaded");
  assert.equal(
    calls.length,
    before + 1,
    "只读取校验文件，不重复下载已校验缓存",
  );
  await cached.updater.openInstaller();
  assert.equal(cached.opened.length, 1);
  await writeFile(opened[0], "tampered");
  assert.equal((await updater.openInstaller()).error, "checksum");
  assert.equal(opened.length, 1);
  assert.equal((await updater.download()).status, "downloaded");
});

test("下载重定向仅允许 GitHub HTTPS 发行资产，不接受本地或任意站点", async (t) => {
  for (const location of [
    "http://127.0.0.1:8432/",
    "https://attacker.example/installer.exe",
    "file:///tmp/installer",
    "https://github.com/other/repo/releases/download/v1/a.exe",
  ]) {
    const calls = [];
    const { updater } = await fixture(t, {
      fetch: async (url) => {
        calls.push(url);
        return url === api
          ? Response.json(release())
          : new Response(null, { status: 302, headers: { location } });
      },
    });
    await updater.check();
    assert.equal((await updater.download()).error, "invalid-release");
    assert.equal(calls.length, 2);
  }
  const { updater } = await fixture(t, {
    fetch: async (url) => {
      if (url === api) return Response.json(release());
      return url.startsWith("https://github.com/")
        ? new Response(null, {
            status: 302,
            headers: {
              location:
                "https://release-assets.githubusercontent.com/example?token=temporary",
            },
          })
        : new Response(bytes);
    },
  });
  await updater.check();
  assert.equal((await updater.download()).status, "downloaded");
});

test("退出取消外部请求，释放订阅且不再发布迟到状态", async (t) => {
  let signal;
  const { updater } = await fixture(t, {
    fetch: async (_url, options) => {
      signal = options.signal;
      return new Promise((_resolve, reject) =>
        signal.addEventListener("abort", () => reject(signal.reason), {
          once: true,
        }),
      );
    },
  });
  const states = [];
  updater.subscribe((state) => states.push(state));
  const pending = updater.check();
  assert.equal(signal.aborted, false);
  await updater.dispose();
  assert.equal(signal.aborted, true);
  await pending;
  assert.equal(states.length, 1);
});

test("下载中退出中断文件写入并清理未校验的临时安装包", async (t) => {
  let chunkWritten;
  const ready = new Promise((resolve) => {
    chunkWritten = resolve;
  });
  const { updater, directory } = await fixture(t, {
    fetch: async (url, options) => {
      if (url === api) return Response.json(release());
      return new Response(
        new ReadableStream({
          start(controller) {
            controller.enqueue(bytes.subarray(0, 5));
            options.signal.addEventListener(
              "abort",
              () => controller.error(options.signal.reason),
              { once: true },
            );
          },
        }),
      );
    },
  });
  await updater.check();
  updater.subscribe((state) => {
    if (state.downloadedBytes > 0) chunkWritten();
  });
  const pending = updater.download();
  await ready;
  await updater.dispose();
  await pending;
  assert.deepEqual(await readdir(path.join(directory, "v0.2.0")), []);
});

test("已失败的流拒绝取消时仍关闭文件句柄并释放读取器", async (t) => {
  const fs = await import("node:fs/promises");
  let closed = 0;
  let released = 0;
  let reads = 0;
  const { ApplicationUpdate: UpdateWithHandles } = loadModule(
    "main/application-update.ts",
    {
      "node:fs/promises": {
        ...fs,
        open: async (...args) => {
          const file = await fs.open(...args);
          return {
            writeFile: (value) => file.writeFile(value),
            close: async () => {
              closed++;
              await file.close();
            },
          };
        },
      },
    },
  );
  const directory = await mkdtemp(
    path.join(tmpdir(), "neuxally-update-handles-"),
  );
  const updater = new UpdateWithHandles({
    version: "0.1.0",
    packaged: true,
    platform: "darwin",
    arch: "arm64",
    cacheDirectory: directory,
    fetch: async (url) =>
      url === api
        ? Response.json(release())
        : {
            ok: true,
            body: {
              getReader: () => ({
                read: async () => {
                  if (reads++ === 0)
                    return { done: false, value: bytes.subarray(0, 5) };
                  throw new Error("transfer interrupted");
                },
                cancel: async () => {
                  throw new Error("stream already errored");
                },
                releaseLock: () => released++,
              }),
            },
          },
    openInstaller: async () => "",
  });
  t.after(async () => {
    await updater.dispose();
    await rm(directory, { recursive: true, force: true });
  });
  await updater.check();
  assert.equal((await updater.download()).status, "error");
  assert.equal(closed, 1);
  assert.equal(released, 1);
  assert.deepEqual(await readdir(path.join(directory, "v0.2.0")), []);
});

test("已下载更新仍可重新检查，相同候选保留缓存，新版取代旧安装包入口", async (t) => {
  let version = "0.2.0";
  let metadataCalls = 0;
  const { updater, opened } = await fixture(t, {
    fetch: async (url) => {
      if (url === api) {
        metadataCalls++;
        return Response.json(release(version));
      }
      return new Response(bytes);
    },
  });
  await updater.check();
  await updater.download();
  assert.equal((await updater.check()).status, "downloaded");
  assert.equal(metadataCalls, 2);
  version = "0.3.0";
  const state = await updater.check();
  assert.equal(metadataCalls, 3);
  assert.equal(state.status, "available");
  assert.equal(state.release.version, "0.3.0");
  assert.equal(state.downloadedBytes, 0);
  await updater.openInstaller();
  assert.equal(opened.length, 0);
  await updater.download();
  await updater.openInstaller();
  assert.equal(opened.length, 1);
  assert.ok(opened[0].includes(`${path.sep}v0.3.0${path.sep}`));
});

test("下载后发现发行撤回或不再高于当前版本时废止旧安装入口", async (t) => {
  for (const status of [404, 200]) {
    let withdrawn = false;
    const { updater, opened } = await fixture(t, {
      fetch: async (url) => {
        if (url !== api) return new Response(bytes);
        if (!withdrawn) return Response.json(release());
        return status === 404
          ? new Response(null, { status })
          : Response.json(release("0.1.0"));
      },
    });
    await updater.check();
    await updater.download();
    withdrawn = true;
    const state = await updater.check();
    assert.equal(state.status, status === 404 ? "no-release" : "current");
    assert.equal(state.release, undefined);
    assert.equal(state.downloadedBytes, 0);
    await updater.openInstaller();
    assert.equal(opened.length, 0);
  }
});

test("已有安装包时检查失败仍可安装缓存并继续重试检查", async (t) => {
  let broken = false;
  let version = "0.2.0";
  const { updater, opened } = await fixture(t, {
    fetch: async (url) => {
      if (url !== api) return new Response(bytes);
      if (broken) throw new Error("offline");
      return Response.json(release(version));
    },
  });
  await updater.check();
  await updater.download();
  broken = true;
  const state = await updater.check();
  assert.equal(state.status, "downloaded");
  assert.equal(state.error, "network");
  await updater.openInstaller();
  assert.equal(opened.length, 1);
  broken = false;
  version = "0.3.0";
  assert.equal((await updater.check()).release.version, "0.3.0");
});

test("元数据、校验文件与安装包的失败 HTTP 响应均释放响应体", async (t) => {
  for (const [stage, status] of [
    ["metadata", 403],
    ["metadata", 404],
    ["metadata", 500],
    ["checksums", 500],
    ["installer", 500],
  ]) {
    let cancelled = 0;
    const data = release();
    if (stage === "checksums") {
      data.assets = data.assets.map((asset) => ({ ...asset, digest: null }));
      data.assets.push({ name: "SHA256SUMS", state: "uploaded", size: 200 });
    }
    const { updater } = await fixture(t, {
      fetch: async (url) => {
        if (url === api && stage !== "metadata") return Response.json(data);
        return new Response(new ReadableStream({ cancel: () => cancelled++ }), {
          status,
        });
      },
    });
    await updater.check();
    if (stage !== "metadata") await updater.download();
    assert.equal(cancelled, 1, `${stage}/${status}`);
  }
});

test("取得安装包响应后文件创建失败仍取消并释放读取器", async (t) => {
  let cancelled = 0;
  let body;
  const { updater, directory, opened } = await fixture(t, {
    fetch: async (url) => {
      if (url === api) return Response.json(release());
      // 在请求返回前制造真实的文件创建冲突，走 open 失败分支。
      await mkdir(path.join(directory, "v0.2.0", `${names[0]}.part`));
      body = new ReadableStream({ cancel: () => cancelled++ });
      return new Response(body);
    },
  });
  await updater.check();
  assert.equal((await updater.download()).status, "error");
  assert.equal(cancelled, 1);
  assert.equal(body.locked, false);
  assert.equal(opened.length, 0);
});

test("新安装包校验成功后移除旧版本缓存，其他目录保留", async (t) => {
  const { updater, directory } = await fixture(t);
  for (const name of ["v0.1.0", "other"]) {
    await mkdir(path.join(directory, name));
    await writeFile(path.join(directory, name, "keep-or-remove"), "fixture");
  }
  await updater.check();
  assert.equal((await updater.download()).status, "downloaded");
  assert.deepEqual((await readdir(directory)).sort(), ["other", "v0.2.0"]);
});
