import { bindErrorNoticeRetry } from "@/shared/api/error-notices";
import { ApiErrorNotifications } from "./api-error-notifications";
import {
  QueryCache,
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import * as Tooltip from "@radix-ui/react-tooltip";
import { type PropsWithChildren } from "react";

import { ThemeProvider } from "@/shared/theme";
import { ChatQueueProvider } from "@/modules/chat/runtime/chat-queue-store";
import { WorkbenchProvider } from "@/modules/chat/workbench/workbench-store";
import { WorkflowAnswerDraftProvider } from "@/modules/workflow/model/human-answer-draft";

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
            <WorkflowAnswerDraftProvider>
              <Tooltip.Provider delayDuration={400}>
                {children}
              </Tooltip.Provider>
            </WorkflowAnswerDraftProvider>
          </WorkbenchProvider>
        </ChatQueueProvider>
      </QueryClientProvider>
    </ThemeProvider>
  );
}
