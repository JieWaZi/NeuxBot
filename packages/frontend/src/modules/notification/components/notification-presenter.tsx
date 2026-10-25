import { logger } from "@/shared/diagnostics/logger";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { z } from "zod";
import { dismissNotificationMutation } from "@/shared/api/generated/@tanstack/react-query.gen";
import { getNotification } from "@/shared/api/generated/sdk.gen";
import {
  subscribeGlobalEvents,
  type GlobalEventResource,
} from "@/shared/api/global-events";
import { toApiError } from "@/shared/api/errors";
import {
  notificationIsPresented,
  notificationPresentationSelector,
} from "@/shared/api/notification-presentation";
import { observeContentPresentation } from "@/shared/lib/content-presentation";
import {
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { toast } from "@/shared/ui/sonner";
import {
  getNotificationSnapshotQueryKey,
  listNotificationsQueryKey,
  notificationSnapshotOptions,
  notificationItemQueryKey,
} from "../api/notification-queries";
import {
  notificationSnapshotSchema,
  notificationUpdateSchema,
  newerNotificationSnapshot,
  notificationItemSchema,
  type NotificationItem,
  type NotificationSnapshot,
} from "../model/notification";
import { useNotificationHref } from "./notification-navigation";

const snapshotFrame = z.object({ snapshot: notificationSnapshotSchema });
const maximumShown = 128;
type ShownNotice = Pick<
  NotificationItem,
  "id" | "kind" | "target" | "executor" | "version"
>;

/** 只检查当前提示所在原资源，不能用五条预览缺席推断通知失效。 */
function affectsNotice(notice: ShownNotice, resource: GlobalEventResource) {
  if (resource.kind === "global") return true;
  const target = notice.target;
  switch (resource.kind) {
    case "chat":
      return (
        resource.id === target.chatId ||
        resource.id === target.reference?.chatId ||
        (target.ownerKind === "chat" && resource.id === target.ownerId)
      );
    case "thread":
    case "team_thread":
      return (
        resource.id === target.threadId ||
        (target.ownerKind === "team_thread" && resource.id === target.ownerId)
      );
    case "workflow_run":
      return (
        resource.id === target.runId ||
        resource.id === target.reference?.workflowRunId ||
        (target.ownerKind === "workflow_run" && resource.id === target.ownerId)
      );
    case "workflow":
      return resource.id === target.definitionId;
    case "task":
    case "automatic_task":
      return resource.id === target.taskId;
    case "waker":
    case "channel":
      return (
        resource.id === notice.executor.id ||
        resource.id === target.wakerId ||
        resource.id === target.channelId
      );
    default:
      return false;
  }
}

/** 唯一全局提醒消费者；快照负责恢复，新事件才展示提示。 */
export function NotificationPresenter({
  onConnectionChange,
}: {
  onConnectionChange: (connected: boolean) => void;
}) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const { t } = useTranslation("notification");
  const href = useNotificationHref();
  const snapshot = useQuery(notificationSnapshotOptions());
  const [transportOpen, setTransportOpen] = useState(false);
  const [recovering, setRecovering] = useState(false);
  const connected =
    transportOpen && !recovering && snapshot.data?.synced === true;
  useEffect(() => {
    onConnectionChange(connected);
  }, [connected, onConnectionChange]);
  const dismiss = useMutation(
    withLocalErrorMutation(dismissNotificationMutation()),
  );
  const current = useRef({
    dismiss: dismiss.mutate,
    href,
    navigate,
    t,
  });
  const markDismissed = dismiss.mutate;
  useEffect(() => {
    current.current = {
      dismiss: markDismissed,
      href,
      navigate,
      t,
    };
  }, [markDismissed, href, navigate, t]);
  const seen = useRef(new Set<string>());
  // 仅拥有本组件当前 Toast 的定位引用；通知权威数据仍只有 Query Cache 一份。
  const shown = useRef(new Map<string, ShownNotice>());
  useEffect(() => {
    let alive = true;
    let listRevision = 0;
    const noticeChecks = new Map<string, AbortController>();
    const pending = new Map<
      string,
      { notice: NotificationItem; presentation: ShownNotice }
    >();
    let paint: number | undefined;
    let baseline = 0;
    let initialized = false;
    const key = getNotificationSnapshotQueryKey();
    const remember = (id: string) => {
      seen.current.add(id);
      if (seen.current.size > 4096) {
        const oldest = seen.current.values().next().value;
        if (oldest) seen.current.delete(oldest);
      }
    };
    const closeShown = () => {
      if (paint !== undefined) cancelAnimationFrame(paint);
      paint = undefined;
      pending.clear();
      for (const controller of noticeChecks.values()) controller.abort();
      noticeChecks.clear();
      const ids = [...shown.current.keys()];
      shown.current.clear();
      for (const id of ids) toast.dismiss(id);
    };
    const closeNotice = (id: string) => {
      pending.delete(id);
      if (!pending.size && paint !== undefined) {
        cancelAnimationFrame(paint);
        paint = undefined;
      }
      noticeChecks.get(id)?.abort();
      noticeChecks.delete(id);
      shown.current.delete(id);
      toast.dismiss(id);
    };
    const presentPending = () => {
      paint = undefined;
      const items = [...pending.values()];
      pending.clear();
      let arrived = false;
      for (const { notice, presentation } of items) {
        if (!alive || shown.current.get(notice.id) !== presentation) continue;
        if (notificationIsPresented(notice)) {
          noticeChecks.get(notice.id)?.abort();
          noticeChecks.delete(notice.id);
          shown.current.delete(notice.id);
          continue;
        }
        arrived = true;
        const markDismissed = () => {
          if (!alive || shown.current.get(notice.id) !== presentation) return;
          shown.current.delete(notice.id);
          current.current.dismiss({ path: { noticeId: notice.id } });
        };
        const options = {
          id: notice.id,
          description: notice.description,
          duration: notice.bucket === "action" ? 8000 : 4000,
          onDismiss: markDismissed,
          onAutoClose: markDismissed,
          action: {
            label: current.current.t(
              notice.bucket === "action" ? "handle" : "view",
            ),
            onClick: () => {
              if (!alive || shown.current.get(notice.id) !== presentation)
                return;
              markDismissed();
              toast.dismiss(notice.id);
              current.current.navigate(current.current.href(notice));
            },
          },
        };
        if (notice.kind === "automatic_failure")
          toast.error(notice.title, options);
        else if (notice.bucket === "action")
          toast.warning(notice.title, options);
        else toast.info(notice.title, options);
      }
      if (arrived) window.dispatchEvent(new Event("ally:notification-arrived"));
    };
    const stopPresentation = observeContentPresentation({
      root: document.body,
      selector: notificationPresentationSelector,
      onChange: () => {
        for (const [id, notice] of shown.current)
          if (notificationIsPresented(notice)) closeNotice(id);
      },
    });
    const flushBackground = () => {
      if (
        pending.size &&
        (document.visibilityState !== "visible" || !document.hasFocus())
      ) {
        if (paint !== undefined) cancelAnimationFrame(paint);
        presentPending();
      }
    };
    document.addEventListener("visibilitychange", flushBackground);
    window.addEventListener("blur", flushBackground);
    const reconcileLists = () => {
      const revision = ++listRevision;
      // 无数据的首读不会被 invalidate 自动替换，必须先取消旧观察窗口。
      void client
        .cancelQueries({ queryKey: listNotificationsQueryKey() })
        .then(() => {
          if (!alive || revision !== listRevision) return;
          return client.invalidateQueries(
            { queryKey: listNotificationsQueryKey() },
            { cancelRefetch: false },
          );
        });
    };
    const reconcileDetails = (ids?: readonly string[]) => {
      const keys = ids
        ? [...new Set(ids)].map((id) => notificationItemQueryKey(id))
        : [notificationItemQueryKey()];
      for (const queryKey of keys) {
        void client.cancelQueries({ queryKey }).then(() => {
          if (alive) return client.invalidateQueries({ queryKey });
        });
      }
    };
    const reconcileShown = (resources: GlobalEventResource[]) => {
      if (!resources.length || !shown.current.size) return;
      for (const [id, notice] of shown.current) {
        if (!resources.some((resource) => affectsNotice(notice, resource)))
          continue;
        noticeChecks.get(id)?.abort();
        const controller = new AbortController();
        noticeChecks.set(id, controller);
        void getNotification(
          withLocalErrorSurface({
            path: { noticeId: id },
            signal: controller.signal,
          }),
        )
          .then((response) => {
            if (
              !alive ||
              controller.signal.aborted ||
              noticeChecks.get(id) !== controller ||
              shown.current.get(id) !== notice
            )
              return;
            if (response.error) {
              // 该有效格式 ID 的定位空结果使用稳定 BadRequest，真实故障不作失效。
              const error = toApiError(
                response.error,
                response.response?.status,
              );
              if (error.code === "BadRequest" && error.status === 400)
                closeNotice(id);
              return;
            }
            const current = notificationItemSchema.safeParse(response.data);
            if (
              current.success &&
              current.data.id === id &&
              current.data.dismissed
            )
              closeNotice(id);
          })
          .catch(() => {
            // 传输、读取故障或取消保留原提示，后续真实变化再对账，不周期重试。
          })
          .finally(() => {
            if (noticeChecks.get(id) === controller) noticeChecks.delete(id);
          });
      }
    };
    const apply = (next: NotificationSnapshot) => {
      let accepted = false;
      client.setQueryData<NotificationSnapshot>(key, (previous) => {
        const value = newerNotificationSnapshot(previous, next);
        accepted = value === next;
        return value;
      });
      return accepted;
    };
    const close = subscribeGlobalEvents(
      "notifications",
      { kind: "global" },
      {
        onState: (state) => setTransportOpen(state === "open"),
        onResync: () => {
          setRecovering(true);
          initialized = false;
          closeShown();
          reconcileLists();
          reconcileDetails();
        },
        onEvent: (frame) => {
          if (frame.type === "notifications.snapshot") {
            const parsed = snapshotFrame.safeParse(frame.payload);
            if (!parsed.success) {
              logger.warn("Invalid notification snapshot", {
                eventType: frame.type,
              });
              return;
            }
            apply(parsed.data.snapshot);
            baseline = parsed.data.snapshot.generatedAt;
            initialized = parsed.data.snapshot.synced;
            setRecovering(false);
            for (const notice of [
              ...parsed.data.snapshot.actionPreview,
              ...parsed.data.snapshot.unreadPreview,
            ])
              remember(notice.id);
            closeShown();
            reconcileLists();
            reconcileDetails();
            return;
          }
          if (frame.type !== "notifications.updated") return;
          const parsed = notificationUpdateSchema.safeParse(frame.payload);
          if (!parsed.success) {
            logger.warn("Invalid notification update", {
              eventType: frame.type,
            });
            return;
          }
          const wasSynced =
            client.getQueryData<NotificationSnapshot>(key)?.synced === true;
          if (!apply(parsed.data.snapshot)) return;
          reconcileLists();
          reconcileDetails([
            ...parsed.data.upserts.map((notice) => notice.id),
            ...parsed.data.removals,
          ]);
          for (const id of parsed.data.removals) {
            closeNotice(id);
          }
          if (parsed.data.snapshot.synced)
            reconcileShown(parsed.data.affectedResources);
          if (!parsed.data.snapshot.synced || !wasSynced) {
            baseline = parsed.data.snapshot.generatedAt;
            initialized = parsed.data.snapshot.synced;
            for (const notice of parsed.data.upserts) remember(notice.id);
            return;
          }
          if (!initialized) return;
          for (const notice of parsed.data.upserts) {
            if (notice.dismissed) {
              closeNotice(notice.id);
              continue;
            }
            if (seen.current.has(notice.id) || notice.createdAt <= baseline)
              continue;
            remember(notice.id);
            if (shown.current.size >= maximumShown) {
              const oldest = shown.current.keys().next().value;
              if (oldest) closeNotice(oldest);
            }
            const presentation = {
              id: notice.id,
              kind: notice.kind,
              version: notice.version,
              target: notice.target,
              executor: notice.executor,
            };
            shown.current.set(notice.id, presentation);
            pending.set(notice.id, { notice, presentation });
          }
          if (pending.size) {
            if (document.visibilityState === "visible" && document.hasFocus())
              paint ??= requestAnimationFrame(presentPending);
            else flushBackground();
          }
        },
      },
    );
    return () => {
      alive = false;
      listRevision++;
      stopPresentation();
      document.removeEventListener("visibilitychange", flushBackground);
      window.removeEventListener("blur", flushBackground);
      close();
      closeShown();
    };
  }, [client]);
  return null;
}
