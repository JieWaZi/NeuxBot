import { execFileSync } from "node:child_process";
import { mkdtemp, readFile, rm, copyFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

// 只在构建阶段编译固定官方源码；底层钥匙串统一隔离，包含官方迁移入口。
export async function buildGitHubProvider({
  archive,
  provider,
  target,
  sourceRoot,
  offline,
}) {
  const temporary = await mkdtemp(path.join(tmpdir(), "neuxbot-github-build-"));
  try {
    execFileSync(
      "tar",
      ["-xzf", archive, "--strip-components=1", "-C", temporary],
      { stdio: "pipe" },
    );
    const patch = path.join(sourceRoot, provider.build.patch);
    execFileSync("git", ["apply", "--unidiff-zero", "--check", patch], {
      cwd: temporary,
      stdio: "pipe",
    });
    execFileSync("git", ["apply", "--unidiff-zero", patch], {
      cwd: temporary,
      stdio: "pipe",
    });
    await copyFile(
      path.join(sourceRoot, provider.build.verification),
      path.join(temporary, "internal/config/neuxally_isolation_test.go"),
    );
    const environment = {
      ...process.env,
      GOTOOLCHAIN: provider.build.toolchain,
      GOWORK: "off",
      GOFLAGS: "",
      CGO_ENABLED: "0",
      GOOS: process.platform === "win32" ? "windows" : process.platform,
      GOARCH: process.arch === "x64" ? "amd64" : process.arch,
      ...(offline ? { GOPROXY: "off", GOSUMDB: "off" } : {}),
    };
    const options = {
      cwd: temporary,
      env: environment,
      stdio: "pipe",
      maxBuffer: 16 * 1024 * 1024,
    };
    // 回归走上游真实登录编排和内存钥匙串，验证外部 gh 哨兵保持原值。
    execFileSync(
      "go",
      [
        "test",
        "-mod=readonly",
        "./internal/config",
        "-run",
        "^TestNeuxAllyLoginKeepsExternalKeyringUntouched$",
        "-count=1",
      ],
      options,
    );
    const [goos, goarch] = target.split("-");
    const output = path.join(temporary, goos === "windows" ? "gh.exe" : "gh");
    execFileSync(
      "go",
      [
        "build",
        "-mod=readonly",
        "-trimpath",
        "-buildvcs=false",
        `-ldflags=-s -w -X github.com/cli/cli/v2/internal/build.Version=${provider.build.version}`,
        "-o",
        output,
        "./cmd/gh",
      ],
      {
        ...options,
        env: { ...environment, GOOS: goos, GOARCH: goarch },
      },
    );
    return {
      binary: await readFile(output),
      license: await readFile(path.join(temporary, "LICENSE")),
      build: provider.build,
    };
  } finally {
    await rm(temporary, { recursive: true, force: true });
  }
}
