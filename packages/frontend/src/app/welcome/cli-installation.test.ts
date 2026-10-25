import { expect, it } from "vitest";
import { cliInstallation, installationPlatform } from "./cli-installation";

it.each([
  [{ userAgent: "Windows NT 10.0" }, "windows"],
  [{ userAgent: "", platform: "Win32" }, "windows"],
  [{ userAgent: "", platform: "MacIntel" }, "macos"],
  [{ userAgent: "Mozilla/5.0 (darwin)" }, "macos"],
  [{ userAgent: "X11; Linux x86_64" }, "linux"],
  [
    {
      userAgent: "",
      platform: "MacIntel",
      userAgentData: { platform: "Windows" },
    },
    "windows",
  ],
  [{ userAgent: "Android; Linux" }, "unknown"],
  [{ userAgent: "iPad; like Mac OS X" }, "unknown"],
  [{ userAgent: "unrecognized" }, "unknown"],
] as const)("根据平台证据选择系统：%o", (browser, expected) => {
  expect(installationPlatform(browser)).toBe(expected);
});

it.each([
  ["pi", "https://pi.dev/install"],
  ["grok", "https://x.ai/cli/install"],
  ["workbuddy", "https://www.codebuddy.cn/cli/install"],
])("%s 提供官方 Unix 与 Windows 安装器，未知系统只给官方入口", (id, source) => {
  for (const platform of ["macos", "linux"] as const) {
    expect(cliInstallation(id, platform)?.command).toContain(`${source}.sh`);
  }
  expect(cliInstallation(id, "windows")?.command).toContain(`${source}.ps1`);
  expect(cliInstallation(id, "unknown")?.command).toBeUndefined();
  expect(cliInstallation(id, "unknown")?.url).toMatch(/^https:\/\//);
});

it("未登记的智能体不会误用其他产品的安装方式", () => {
  expect(cliInstallation("custom-agent", "windows")).toBeUndefined();
});

it("Qoder 提供官方原生安装器，未知系统只展示安装文档", () => {
  expect(cliInstallation("qoder", "macos")?.command).toBe(
    "curl -fsSL https://qoder.com/install | bash",
  );
  expect(cliInstallation("qoder", "linux")?.command).toBe(
    cliInstallation("qoder", "macos")?.command,
  );
  expect(cliInstallation("qoder", "windows")?.command).toBe(
    "irm https://qoder.com/install.ps1 | iex",
  );
  expect(cliInstallation("qoder", "unknown")?.command).toBeUndefined();
  expect(cliInstallation("qoder", "unknown")?.url).toBe(
    "https://docs.qoder.com/cli/installation",
  );
  expect(cliInstallation("qoder", "macos")?.node).toBe(false);
});

it("Qoder CN 使用中国版官方安装器和文档", () => {
  expect(cliInstallation("qoder-cn", "macos")?.command).toBe(
    "curl -fsSL https://static.qoder.com.cn/qoder-cli-cn/install.sh | bash",
  );
  expect(cliInstallation("qoder-cn", "linux")?.command).toBe(
    cliInstallation("qoder-cn", "macos")?.command,
  );
  expect(cliInstallation("qoder-cn", "windows")?.command).toBe(
    "irm https://static.qoder.com.cn/qoder-cli-cn/install.ps1 | iex",
  );
  expect(cliInstallation("qoder-cn", "unknown")?.command).toBeUndefined();
  expect(cliInstallation("qoder-cn", "unknown")?.url).toBe(
    "https://docs.qoder.cn/cli/installation",
  );
});
