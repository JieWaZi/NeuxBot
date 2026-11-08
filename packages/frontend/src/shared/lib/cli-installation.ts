export type InstallationPlatform = "windows" | "macos" | "linux" | "unknown";

/** 优先使用浏览器平台提示；无法识别的平台只给官方入口，不猜测可执行命令。 */
export function installationPlatform(
  browser: {
    userAgent: string;
    platform?: string;
    userAgentData?: { platform: string };
  } = navigator,
): InstallationPlatform {
  if (/Android|iPhone|iPad|iPod/i.test(browser.userAgent)) return "unknown";
  const platform =
    browser.userAgentData?.platform || browser.platform || browser.userAgent;
  if (/Windows|^Win32$|^Win64$/i.test(platform)) return "windows";
  if (/Mac|Darwin/i.test(platform)) return "macos";
  if (/Linux/i.test(platform)) return "linux";
  return "unknown";
}

/** 安装命令仅用于展示和复制；来源为对应产品官方安装页，Qoder 来源核查于 2026-10-24，其余核查于 2026-10-16。 */
const installationGuides: Record<
  string,
  {
    url: string;
    command?: string;
    windows?: string;
    node?: boolean;
    beta?: boolean;
  }
> = {
  opencode: {
    url: "https://opencode.ai/v2/docs/",
    command: "curl -fsSL https://opencode.ai/v2/install | bash",
  },
  claude: {
    url: "https://code.claude.com/docs/en/setup",
    command: "curl -fsSL https://claude.ai/install.sh | bash",
    windows: "irm https://claude.ai/install.ps1 | iex",
  },
  codex: {
    url: "https://github.com/openai/codex#installing-and-running-codex-cli",
    command: "npm install -g @openai/codex",
    windows: "npm install -g @openai/codex",
    node: true,
  },
  kimi: {
    url: "https://github.com/MoonshotAI/kimi-code#install",
    command: "curl -fsSL https://code.kimi.com/kimi-code/install.sh | bash",
    windows: "irm https://code.kimi.com/kimi-code/install.ps1 | iex",
  },
  gemini: {
    url: "https://geminicli.com/docs/get-started/installation/",
    command: "npm install -g @google/gemini-cli",
    windows: "npm install -g @google/gemini-cli",
    node: true,
  },
  cursor: {
    url: "https://cursor.com/docs/cli/installation",
    command: "curl https://cursor.com/install -fsS | bash",
    windows: "irm 'https://cursor.com/install?win32=true' | iex",
  },
  pi: {
    url: "https://pi.dev/",
    command: "curl -fsSL https://pi.dev/install.sh | sh",
    windows: 'powershell -c "irm https://pi.dev/install.ps1 | iex"',
  },
  grok: {
    url: "https://grok.com/build",
    command: "curl -fsSL https://x.ai/cli/install.sh | bash",
    windows: "irm https://x.ai/cli/install.ps1 | iex",
  },
  "qoder-cn": {
    url: "https://docs.qoder.cn/cli/installation",
    command:
      "curl -fsSL https://static.qoder.com.cn/qoder-cli-cn/install.sh | bash",
    windows: "irm https://static.qoder.com.cn/qoder-cli-cn/install.ps1 | iex",
  },
  qoder: {
    url: "https://docs.qoder.com/cli/installation",
    command: "curl -fsSL https://qoder.com/install | bash",
    windows: "irm https://qoder.com/install.ps1 | iex",
  },
  workbuddy: {
    url: "https://www.workbuddy.cn/docs/cli/installation",
    command: "curl -fsSL https://www.codebuddy.cn/cli/install.sh | bash",
    windows: "irm https://www.codebuddy.cn/cli/install.ps1 | iex",
    beta: true,
  },
};

export function cliInstallation(
  baseRuntimeId: string,
  platform: InstallationPlatform,
) {
  const guide = installationGuides[baseRuntimeId];
  if (!guide) return undefined;
  return {
    url: guide.url,
    command:
      platform === "windows"
        ? guide.windows
        : platform === "macos" || platform === "linux"
          ? guide.command
          : undefined,
    node: guide.node ?? false,
    beta: guide.beta ?? false,
  };
}
