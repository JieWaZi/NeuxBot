import { StrictMode } from "react";
import { createRoot, type Root } from "react-dom/client";

import { StartupScreen, type StartupStage } from "@/app/startup-screen";
import { prepareWelcomeAssets } from "@/app/welcome/welcome-brand";
import { logger } from "@/shared/diagnostics/logger";
import { initializeApi } from "@/shared/api/runtime";
import { i18n } from "@/shared/i18n";
import { initializeTheme, ThemeProvider } from "@/shared/theme";

let activeRoot: Root | undefined;

export function startApplication(): void {
  initializeTheme();
  const startupStarted = performance.now();
  const startupTimings: Record<string, number> = {};
  const noteStartup = (phase: string) => {
    startupTimings[phase] = Math.round(performance.now() - startupStarted);
    performance.mark?.(`ally.startup.${phase}`);
  };
  noteStartup("entry");
  const container = document.getElementById("root");
  if (!container) throw new Error("缺少应用挂载节点");
  const root = createRoot(container);
  activeRoot = root;

  let attempt = 0;
  let welcomeCompleted = false;
  let completeWelcome: () => void;
  const welcomeFinished = new Promise<void>((resolve) => {
    completeWelcome = () => {
      welcomeCompleted = true;
      noteStartup("welcomeComplete");
      resolve();
    };
  });

  async function start() {
    const attemptId = ++attempt;
    let stage: StartupStage = "connection";
    const showStartup = (error?: string) =>
      root.render(
        <StrictMode>
          <ThemeProvider>
            <StartupScreen
              stage={stage}
              intro={!welcomeCompleted}
              onIntroComplete={completeWelcome}
              error={error}
              onRetry={() => {
                void start();
              }}
            />
          </ThemeProvider>
        </StrictMode>,
      );
    showStartup();
    // 品牌代码先准备，随后在欢迎播放和服务连接期间加载工作区；预先接住模块失败。
    const workspace = prepareWelcomeAssets()
      .then(() => {
        if (attemptId === attempt) noteStartup("brandAssetsReady");
        return import("@/app/workspace");
      })
      .then(
        (module) => {
          if (attemptId === attempt) noteStartup("workspaceModuleReady");
          return { ok: true as const, module };
        },
        (error: unknown) => ({ ok: false as const, error }),
      );
    try {
      await initializeApi();
      if (attemptId !== attempt) return;
      noteStartup("coreReady");
      stage = "workspace";
      showStartup();
      const result = await workspace;
      if (attemptId !== attempt) return;
      await Promise.all([
        welcomeFinished,
        result.ok
          ? result.module.prepareWorkspace().then(() => {
              if (attemptId === attempt) noteStartup("runtimesReady");
            })
          : Promise.resolve(),
      ]);
      if (attemptId !== attempt) return;
      if (!result.ok) throw result.error;
      for (const phase of ["characterReady", "logoReady"]) {
        const mark = performance
          .getEntriesByName?.(`ally.startup.${phase}`, "mark")
          .at(-1);
        if (mark)
          startupTimings[phase] = Math.round(mark.startTime - startupStarted);
      }
      noteStartup("handoffRequested");
      logger.info("startup renderer phases", {
        attempt: attemptId,
        milliseconds: { ...startupTimings },
      });
      result.module.renderWorkspace(root);
    } catch (error) {
      if (attemptId !== attempt) return;
      showStartup(
        error instanceof Error
          ? error.message
          : typeof error === "string"
            ? error
            : i18n.t("startup.unknownError"),
      );
    }
  }

  void start();
}

/** 登录失效或退出时释放界面、请求和事件，保留 Core 已接纳工作。 */
export async function resetApplicationSession(): Promise<void> {
  activeRoot?.unmount();
  activeRoot = undefined;
  const [{ appQueryClient }, { resetGlobalEvents }, { resetApiConnection }] =
    await Promise.all([
      import("@/app/providers"),
      import("@/shared/api/global-events"),
      import("@/shared/api/runtime"),
    ]);
  await appQueryClient.cancelQueries();
  appQueryClient.clear();
  await resetGlobalEvents();
  resetApiConnection();
}
