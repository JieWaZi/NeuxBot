import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { MessageSquarePlus, RotateCw } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
  forkChatMutation,
  getChatForkTargetsOptions,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import { Button } from "@/shared/ui/button";
import { Spinner } from "@/shared/ui/spinner";
import type { WorkbenchTab } from "./workbench-model";

const EmbeddedChat = lazy(async () => ({
  default: (await import("../pages/chat-page")).ChatPageContent,
}));

// React StrictMode and a hidden/reshown tab may both try to create the same
// child. The request identity lives on the tab, and the in-flight operation is
// shared in this renderer. NeuxBot Core remains the authority for idempotency.
const inFlightForks = new Map<string, Promise<string>>();

type SideChatWorkbenchViewProps = {
  tab: WorkbenchTab;
  onChatCreated: (chatId: string) => void;
};

export function SideChatWorkbenchView({
  tab,
  onChatCreated,
}: SideChatWorkbenchViewProps) {
  const { t } = useTranslation("chat");
  const started = useRef(false);
  const [error, setError] = useState("");
  const targets = useQuery({
    ...getChatForkTargetsOptions({ path: { chatId: tab.parentChatId ?? "" } }),
    enabled: Boolean(tab.parentChatId && !tab.chatId),
  });
  const fork = useMutation(forkChatMutation());
  const target =
    targets.data?.find(
      (candidate) =>
        candidate.latest && candidate.available && candidate.executionId,
    ) ??
    targets.data?.findLast(
      (candidate) => candidate.available && candidate.executionId,
    );

  const start = useCallback(async () => {
    if (!tab.parentChatId || !tab.createRequestId || !target?.executionId)
      return;
    setError("");
    let pending = inFlightForks.get(tab.createRequestId);
    if (!pending) {
      pending = fork
        .mutateAsync({
          path: { chatId: tab.parentChatId },
          body: {
            clientRequestId: tab.createRequestId,
            executionId: target.executionId,
          },
        })
        .then((result) => {
          if (!result.chatId)
            throw new Error(t("workbench.sideChatCreateFailed"));
          return result.chatId;
        });
      inFlightForks.set(tab.createRequestId, pending);
      void pending
        .finally(() => {
          if (inFlightForks.get(tab.createRequestId ?? "") === pending)
            inFlightForks.delete(tab.createRequestId ?? "");
        })
        .catch(() => {});
    }
    try {
      const chatId = await pending;
      onChatCreated(chatId);
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : t("workbench.sideChatCreateFailed"),
      );
    }
  }, [fork, onChatCreated, t, tab.createRequestId, tab.parentChatId, target]);

  useEffect(() => {
    if (tab.chatId || !target || started.current) return;
    started.current = true;
    void start();
  }, [start, tab.chatId, target]);

  if (tab.chatId) {
    return (
      <Suspense
        fallback={
          <div className="grid h-full place-items-center">
            <Spinner className="size-icon-lg" />
          </div>
        }
      >
        <EmbeddedChat chatId={tab.chatId} embedded />
      </Suspense>
    );
  }

  if (targets.isPending || fork.isPending) {
    return (
      <div
        className="text-muted-foreground type-body flex h-full flex-col items-center justify-center gap-3 px-4"
        role="status"
      >
        <Spinner className="size-icon-lg" />
        {t("workbench.sideChatCreating")}
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-5 text-center">
      <MessageSquarePlus
        className="text-muted-foreground size-icon-xl"
        strokeWidth={1.5}
      />
      <p className="type-title text-foreground">{t("workbench.sideChat")}</p>
      <p className="type-body text-muted-foreground">
        {error ||
          (targets.isError
            ? t("workbench.sideChatLoadFailed")
            : t("workbench.sideChatUnavailable"))}
      </p>
      {target ? (
        <Button
          size="md"
          variant="secondary"
          onClick={() => {
            started.current = true;
            void start();
          }}
        >
          <RotateCw />
          {t("workspace.retry")}
        </Button>
      ) : targets.isError ? (
        <Button
          size="md"
          variant="secondary"
          onClick={() => void targets.refetch()}
        >
          <RotateCw />
          {t("workspace.retry")}
        </Button>
      ) : null}
    </div>
  );
}
