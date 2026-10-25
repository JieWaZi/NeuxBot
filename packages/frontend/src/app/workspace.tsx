import type { Root } from "react-dom/client";
import { StrictMode } from "react";
import { RouterProvider } from "react-router-dom";

import "@/app/workspace-i18n";
import { ErrorBoundary } from "@/app/error-boundary";
import { AppProviders, appQueryClient } from "@/app/providers";
import { router } from "@/app/router";
import { StartupGate } from "@/app/welcome/startup-gate";
import { listRuntimesOptions } from "@/shared/api/runtime-queries";

/** 欢迎播放期间读取准入快照，交接后直接复用同一个 Query Cache，避免再次展示中间欢迎页。 */
export async function prepareWorkspace() {
  await appQueryClient.prefetchQuery({
    ...listRuntimesOptions(),
    retry: false,
  });
}

export function renderWorkspace(root: Root) {
  root.render(
    <StrictMode>
      <ErrorBoundary>
        <AppProviders>
          <StartupGate>
            <RouterProvider router={router} />
          </StartupGate>
        </AppProviders>
      </ErrorBoundary>
    </StrictMode>,
  );
}
