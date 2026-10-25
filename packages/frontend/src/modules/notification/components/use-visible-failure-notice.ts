import { useMutation, useQuery } from "@tanstack/react-query";
import { type RefObject } from "react";
import { viewNotificationMutation } from "@/shared/api/generated/@tanstack/react-query.gen";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import { useVisibleScene } from "@/shared/api/work-result-reading";
import { useContentVisibility } from "@/shared/lib/content-presentation";
import { usePresentationReading } from "@/shared/api/presentation-reading";
import { notificationItemOptions } from "../api/notification-queries";

/** 原运行失败记录实际进入前台可见区域后，才确认对应不可变通知。 */
export function useVisibleFailureNotice({
  noticeId,
  runId,
  root,
  presented,
}: {
  noticeId: string;
  runId: string;
  root: RefObject<HTMLElement | null>;
  presented: boolean;
}) {
  const active = useVisibleScene();
  const visible = useContentVisibility(root, active && presented);
  const query = useQuery({
    ...notificationItemOptions(noticeId),
    enabled: visible && Boolean(noticeId && runId),
    retry: false,
  });
  const mark = useMutation(withLocalErrorMutation(viewNotificationMutation()));
  const notice = query.data;
  const readable =
    active &&
    presented &&
    notice?.kind === "automatic_failure" &&
    notice.id === noticeId &&
    notice.target.automaticRunId === runId;
  usePresentationReading({
    root,
    identity: readable ? `${notice.id}:${notice.version}` : undefined,
    revision: query.dataUpdatedAt,
    read: () =>
      mark.mutateAsync({
        path: { noticeId },
        body: { version: notice?.version ?? 0, target: notice?.target },
      }),
  });
}
