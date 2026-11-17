import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LoaderCircle, Pencil } from "lucide-react";
import { useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useLocation, useNavigate } from "react-router-dom";

import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { useQueryEvents } from "@/shared/api/use-query-events";
import {
  chatHref,
  wakerProfileOptimizationHref,
} from "@/shared/lib/chat-navigation";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { PageAction } from "@/shared/ui/resource-page";
import { Skeleton } from "@/shared/ui/skeleton";
import { StaticMarkdown } from "@/shared/ui/static-markdown";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import {
  getWakerDocumentOptions,
  getWakerQueryKey,
  startWakerOnboardingMutation,
} from "../api/waker-queries";
import type { Waker } from "../model/types";
import { ProfileDocumentEditorDialog } from "./profile-document-editor-dialog";

export function WakerProfileSection({
  waker,
  fillAvailableHeight = false,
}: {
  waker: Waker;
  fillAvailableHeight?: boolean;
}) {
  const { t } = useTranslation("waker");
  const navigate = useNavigate();
  const { state } = useLocation();
  const client = useQueryClient();
  const active = useRouteSceneActive();
  const canNavigate = useRef(false);
  useLayoutEffect(() => {
    canNavigate.current = active;
    return () => {
      canNavigate.current = false;
    };
  }, [active]);
  const wakerId = waker.id ?? "";
  const options = getWakerDocumentOptions(
    withLocalErrorSurface({ path: { wakerId, kind: "agents" } }),
  );
  const query = useQuery({ ...options, enabled: Boolean(wakerId) });
  const content = query.data?.content ?? "";
  const start = useMutation(
    withLocalErrorMutation(startWakerOnboardingMutation()),
  );
  const [editing, setEditing] = useState(false);
  const editButton = useRef<HTMLButtonElement>(null);
  const [startError, setStartError] = useState<string>();
  const initialError =
    typeof state?.onboardingStartError === "string"
      ? state.onboardingStartError
      : "";
  const launchError = start.error
    ? localApiErrorMessage(start.error, t("profile.startError"))
    : (startError ?? initialError);
  const pending = waker.onboardingStatus === "pending";

  useQueryEvents({
    topic: "resources",
    resource: { kind: "waker", id: wakerId },
    queryKeys: [options.queryKey],
    eventTypes: ["resources.changed"],
    enabled: Boolean(wakerId),
    active,
  });

  const continueOnboarding = async () => {
    setStartError("");
    try {
      const { chatId, startError } = await start.mutateAsync({
        path: { wakerId },
      });
      if (chatId) {
        void client.invalidateQueries({
          queryKey: getWakerQueryKey({ path: { wakerId } }),
        });
        if (!startError && canNavigate.current)
          navigate(chatHref(chatId, wakerId));
      }
      if (startError || !chatId)
        setStartError(startError || t("profile.startError"));
    } catch {
      // Mutation 在档案区显示启动错误。
    }
  };

  return (
    <section className="flex min-h-0 min-w-0 flex-1 flex-col">
      <header className="mb-3 flex shrink-0 flex-wrap items-center justify-between gap-3">
        <h2 className="type-label font-semibold">{t("profile.title")}</h2>
        <div className="flex flex-wrap items-center gap-2">
          {!pending && query.data?.editable ? (
            <PageAction
              intent="edit"
              label={t("profile.edit")}
              onClick={(event) => {
                editButton.current = event.currentTarget;
                setEditing(true);
              }}
            >
              <Pencil />
            </PageAction>
          ) : null}
          {pending && start.data?.chatId && !start.isPending ? (
            <Button asChild variant="secondary">
              <Link to={chatHref(start.data.chatId, wakerId)}>
                {t("profile.openChat")}
              </Link>
            </Button>
          ) : null}
          {pending ? (
            <Button
              type="button"
              disabled={start.isPending}
              onClick={() => void continueOnboarding()}
            >
              {start.isPending ? (
                <LoaderCircle className="animate-spin" />
              ) : null}
              {t("profile.continue")}
            </Button>
          ) : null}
        </div>
      </header>

      {pending && launchError ? (
        <p role="alert" className="text-destructive type-body mb-3">
          {launchError}
        </p>
      ) : null}
      {query.isPending ? (
        <div
          role="status"
          aria-busy="true"
          aria-label={t("profile.loading")}
          className="grid gap-3"
        >
          <Skeleton className="h-4 w-48 max-w-full" />
          <Skeleton className="h-3 w-full" />
          <Skeleton className="h-3 w-3/4" />
        </div>
      ) : null}
      {query.isError ? (
        <div role="alert" className="flex flex-wrap items-center gap-3">
          <p className="text-destructive type-body">{t("profile.loadError")}</p>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => void query.refetch()}
          >
            {t("profile.retry")}
          </Button>
        </div>
      ) : null}
      {content.trim() ? (
        <div
          className={cn(
            "max-h-96 min-h-0 min-w-0 overflow-hidden",
            fillAvailableHeight &&
              "flex-1 @[60rem]/resource-page:max-h-none @[60rem]/resource-page:[contain:size]",
          )}
        >
          <StaticMarkdown source={content} />
        </div>
      ) : query.isSuccess && !pending ? (
        <p className="text-muted-foreground type-body">{t("profile.empty")}</p>
      ) : null}

      {editing && !pending && query.data?.editable ? (
        <ProfileDocumentEditorDialog
          content={content}
          revision={query.data.revision}
          maxTokens={query.data.maxTokens}
          wakerId={wakerId}
          onClose={() => setEditing(false)}
          onOptimize={() => {
            setEditing(false);
            navigate(wakerProfileOptimizationHref(wakerId));
          }}
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            if (canNavigate.current) editButton.current?.focus();
          }}
        />
      ) : null}
    </section>
  );
}
