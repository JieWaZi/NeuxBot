import { useEffect, useMemo, type RefObject } from "react";
import { isElementVisible } from "@/shared/lib/element-visibility";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsNotificationItem as Notice } from "./generated/types.gen";

export type ChatResultOwner = { kind: "chat" | "team_thread"; id: string };
export type ChatResultSource = { chatId: string; owner: ChatResultOwner };

export const notificationPresentationSelector =
  "[data-notification-id], [data-notification-workflow-run], [data-work-result-body], [data-work-result-output]";

type PresentationTarget =
  | { kind: "request"; noticeId: string }
  | { kind: "workflow_run"; runId: string }
  | {
      kind: "result";
      ownerKind: string;
      ownerId: string;
      version: number;
      chatId?: string;
    };

/** 业务组件只声明呈现身份，不自行实现观察器、提示或阅读逻辑。 */
export function notificationPresentationProps(
  target: PresentationTarget,
): Record<string, string> {
  switch (target.kind) {
    case "request":
      return { "data-notification-id": target.noticeId };
    case "workflow_run":
      return { "data-notification-workflow-run": target.runId };
    case "result":
      return {
        "data-work-result-owner-kind": target.ownerKind,
        "data-work-result-owner-id": target.ownerId,
        "data-work-result-version": String(target.version),
        "data-work-result-chat-id":
          target.chatId ?? (target.ownerKind === "chat" ? target.ownerId : ""),
      };
  }
}

/** 在已有 DOM 根上登记同一呈现契约；不写阅读或业务完成事实。 */
export function useNotificationPresentation({
  root,
  target,
  active,
}: {
  root: RefObject<HTMLElement | null>;
  target?: PresentationTarget;
  active: boolean;
}) {
  const kind = target?.kind;
  const noticeId = target?.kind === "request" ? target.noticeId : "";
  const runId = target?.kind === "workflow_run" ? target.runId : "";
  const ownerKind = target?.kind === "result" ? target.ownerKind : "";
  const ownerId = target?.kind === "result" ? target.ownerId : "";
  const version = target?.kind === "result" ? target.version : 0;
  const chatId = target?.kind === "result" ? target.chatId : undefined;
  const attributes = useMemo(() => {
    if (kind === "request")
      return notificationPresentationProps({ kind, noticeId });
    if (kind === "workflow_run")
      return notificationPresentationProps({ kind, runId });
    if (kind === "result")
      return notificationPresentationProps({
        kind,
        ownerKind,
        ownerId,
        version,
        chatId,
      });
    return {};
  }, [kind, noticeId, runId, ownerKind, ownerId, version, chatId]);
  useEffect(() => {
    const element = root.current;
    if (!active || !element) return;
    for (const [name, value] of Object.entries(attributes))
      element.setAttribute(name, value);
    return () => {
      for (const [name, value] of Object.entries(attributes))
        if (element.getAttribute(name) === value) element.removeAttribute(name);
    };
  }, [active, root, attributes]);
}

/** 所有通知类型共用呈现资格；请求身份、成果归属及版本必须准确匹配。 */
export function notificationIsPresented(
  notice: Pick<Notice, "id" | "kind" | "target" | "version">,
): boolean {
  if (notice.kind !== "result")
    return [
      ...document.querySelectorAll<HTMLElement>("[data-notification-id]"),
    ].some(
      (element) =>
        element.dataset.notificationId === notice.id &&
        isElementVisible(element),
    );
  const target = notice.target;
  if (!target) return false;
  if (
    target.ownerKind === "workflow_run" &&
    target.ownerId &&
    target.reference?.workflowRunId === target.ownerId &&
    [
      ...document.querySelectorAll<HTMLElement>(
        "[data-notification-workflow-run]",
      ),
    ].some(
      (element) =>
        element.dataset.notificationWorkflowRun === target.ownerId &&
        isElementVisible(element),
    )
  )
    return true;
  const messageId = target.reference?.messageId || target.messageId;
  const chatId = target.reference?.chatId || target.chatId;
  return [
    ...document.querySelectorAll<HTMLElement>("[data-work-result-owner-kind]"),
  ].some((root) => {
    if (
      root.dataset.workResultOwnerKind !== target.ownerKind ||
      root.dataset.workResultOwnerId !== target.ownerId ||
      root.dataset.workResultVersion !== String(notice.version)
    )
      return false;
    if (target.ownerKind === "workflow_run")
      return (
        target.reference?.workflowRunId === target.ownerId &&
        [
          ...root.querySelectorAll<HTMLElement>("[data-work-result-output]"),
        ].some(isElementVisible)
      );
    if (!chatId || !messageId || root.dataset.workResultChatId !== chatId)
      return false;
    return [...root.querySelectorAll<HTMLElement>("[data-message-id]")].some(
      (message) =>
        message.dataset.messageId === messageId &&
        [
          ...message.querySelectorAll<HTMLElement>("[data-work-result-body]"),
        ].some(isElementVisible),
    );
  });
}
