import { bindErrorNoticeRetry } from "@/shared/api/error-notices";
import { ApiErrorNotifications } from "./api-error-notifications";
import {
  QueryCache,
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import * as Tooltip from "@radix-ui/react-tooltip";
import { lazy, Suspense, type PropsWithChildren } from "react";

import {
  ChatContributionsProvider,
  type ChatContributionScopeProps,
  type WorkbenchContributionProps,
} from "@/modules/chat";
import { ContentLoadingState } from "@/shared/ui/content-loading-state";
import { useTranslation } from "react-i18next";
import { ThemeProvider } from "@/shared/theme";
import { ChatQueueProvider } from "@/modules/chat/runtime/chat-queue-store";
import { WorkbenchProvider } from "@/modules/chat/workbench/workbench-store";

const ResultScope = lazy(async () => ({
  default: (await import("@/modules/workflow")).WorkflowResultMessageScope,
}));
const ResultContent = lazy(async () => ({
  default: (await import("@/modules/workflow")).WorkflowResultContent,
}));
function ContributionLoading() {
  const { t } = useTranslation("workflow");
  return <ContentLoadingState label={t("loading")} />;
}
const chatContributions = {
  Scope: (props: ChatContributionScopeProps) => (
    <Suspense fallback={null}>
      <ResultScope {...props} />
    </Suspense>
  ),
  Content: (props: WorkbenchContributionProps) => (
    <Suspense fallback={<ContributionLoading />}>
      <ResultContent {...props} />
    </Suspense>
  ),
};

export const appQueryClient = new QueryClient({
  queryCache: new QueryCache({
    onError: (error, query) => bindErrorNoticeRetry(error, () => query.fetch()),
  }),
  defaultOptions: {
    queries: { retry: 2, staleTime: 10_000, refetchOnWindowFocus: false },
  },
});

export function AppProviders({ children }: PropsWithChildren) {
  return (
    <ThemeProvider>
      <ApiErrorNotifications />
      <QueryClientProvider client={appQueryClient}>
        <ChatQueueProvider>
          <WorkbenchProvider>
            <ChatContributionsProvider value={chatContributions}>
              <Tooltip.Provider delayDuration={400}>
                {children}
              </Tooltip.Provider>
            </ChatContributionsProvider>
          </WorkbenchProvider>
        </ChatQueueProvider>
      </QueryClientProvider>
    </ThemeProvider>
  );
}
