import { notificationPresentationProps } from "@/shared/api/notification-presentation";
import { useRef, type ReactNode } from "react";
import { useVisibleFailureNotice } from "./use-visible-failure-notice";

/** 无成果回执的失败正文；从普通导航或通知入口进入均走同一呈现与阅读契约。 */
export function FailureNoticeContent({
  runId,
  children,
}: {
  runId: string;
  children: ReactNode;
}) {
  const root = useRef<HTMLDivElement>(null);
  const noticeId = `automatic-run-failed:${runId}`;
  useVisibleFailureNotice({ root, noticeId, runId, presented: Boolean(runId) });
  return (
    <div
      ref={root}
      {...notificationPresentationProps({
        kind: "request",
        noticeId: noticeId,
      })}
    >
      {children}
    </div>
  );
}
