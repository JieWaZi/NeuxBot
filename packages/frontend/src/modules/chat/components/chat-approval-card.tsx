import { notificationPresentationProps } from "@/shared/api/notification-presentation";
import {
  Brain,
  ChevronDown,
  FilePenLine,
  FileSearch,
  FileText,
  FileX2,
  Globe,
  Loader2,
  MoveRight,
  RefreshCw,
  ShieldCheck,
  SquareTerminal,
  Wrench,
  type LucideIcon,
} from "lucide-react";
import type { TFunction } from "i18next";
import { useCallback, useEffect } from "react";
import { useTranslation } from "react-i18next";

import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsPendingApproval as PendingApproval } from "@/shared/api/generated/types.gen";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";

export type ApprovalSubmissionState = "idle" | "submitting" | "confirming";

export function nextApprovalSeenIds(
  pending: readonly { approvalId?: string }[],
  seenIds: readonly string[],
): string[] {
  if (pending.length === 0) return [];
  const pendingIds = pending.flatMap((item) =>
    item.approvalId ? [item.approvalId] : [],
  );
  const related = pendingIds.some((id) => seenIds.includes(id));
  const next = related ? [...seenIds] : [];
  const seen = new Set(next);
  for (const id of pendingIds) {
    if (seen.has(id)) continue;
    seen.add(id);
    next.push(id);
  }
  return next;
}

export function approvalQueuePosition(
  approvalId: string | undefined,
  seenIds: readonly string[],
): { current: number; total: number } {
  const index = approvalId ? seenIds.indexOf(approvalId) : -1;
  return {
    current: index >= 0 ? index + 1 : 1,
    total: Math.max(seenIds.length, 1),
  };
}

type ChatApprovalCardProps = {
  approval: PendingApproval;
  current: number;
  total: number;
  submissionState: ApprovalSubmissionState;
  onResolve: (approvalId: string, optionId: string) => void;
};

const approvalKindIcons: Record<string, LucideIcon> = {
  read: FileText,
  edit: FilePenLine,
  delete: FileX2,
  move: MoveRight,
  search: FileSearch,
  execute: SquareTerminal,
  command: SquareTerminal,
  think: Brain,
  fetch: Globe,
  network: Globe,
  switch_mode: RefreshCw,
  other: Wrench,
};

const allowCommandPrefixPattern = /^Allow Commands Starting With `(.+)`$/;
const allowFuturePattern = /^Allow (.+) in the Future$/;
const blockFuturePattern = /^Block (.+) in the Future$/;

const approvalOptionNameKeys = {
  "Allow for Session": "allowSession",
  "Allow Root for Session": "allowRootSession",
  "Allow Host for Session": "allowHostSession",
  "Allow and Remember Command Pattern": "allowCommandPattern",
  "Always allow": "allowAlways",
  "Allow always": "allowAlways",
  "Always reject": "rejectAlways",
  "Reject always": "rejectAlways",
} as const;

const approvalOptionLabelKeys = {
  allowSession: "approval.option.allowSession",
  allowRootSession: "approval.option.allowRootSession",
  allowHostSession: "approval.option.allowHostSession",
  allowCommandPattern: "approval.option.allowCommandPattern",
  allowAlways: "approval.option.allowAlways",
  rejectAlways: "approval.option.rejectAlways",
} as const;

const approvalResultLabelKeys = {
  allowCommandPrefix: "toolApproval.allowedCommandPrefix",
  allowFuture: "toolApproval.allowedFuture",
  blockFuture: "toolApproval.blockedFuture",
  allowSession: "toolApproval.allowedSession",
  allowRootSession: "toolApproval.allowedRootSession",
  allowHostSession: "toolApproval.allowedHostSession",
  allowCommandPattern: "toolApproval.allowedCommandPattern",
  allowAlways: "toolApproval.allowedAlways",
  rejectAlways: "toolApproval.rejectedAlways",
} as const;

type ApprovalOptionNameKey =
  (typeof approvalOptionNameKeys)[keyof typeof approvalOptionNameKeys];

type ApprovalOptionMatch =
  | { type: "allowCommandPrefix"; command: string }
  | { type: "allowFuture"; target: string }
  | { type: "blockFuture"; target: string }
  | { type: "named"; key: ApprovalOptionNameKey }
  | { type: "custom"; name: string };

type ApprovalOptionLabelSource = {
  name?: string;
  kind?: string;
};

type ApprovalResultLabelSource = ApprovalOptionLabelSource & {
  result?: string;
};

function patternTarget(pattern: RegExp, value: string): string | undefined {
  return pattern.exec(value)?.[1];
}

export function matchApprovalOptionName(name: string): ApprovalOptionMatch {
  const trimmed = name.trim();
  const command = patternTarget(allowCommandPrefixPattern, trimmed);
  if (command) return { type: "allowCommandPrefix", command };
  const allowedTarget = patternTarget(allowFuturePattern, trimmed);
  if (allowedTarget) return { type: "allowFuture", target: allowedTarget };
  const blockedTarget = patternTarget(blockFuturePattern, trimmed);
  if (blockedTarget) return { type: "blockFuture", target: blockedTarget };
  const key =
    approvalOptionNameKeys[trimmed as keyof typeof approvalOptionNameKeys];
  if (key) return { type: "named", key };
  return { type: "custom", name: trimmed };
}

export function approvalHotkeysAllowed(event: KeyboardEvent): boolean {
  if (event.defaultPrevented) return false;
  if (
    document.querySelector(
      "[role='dialog'][data-state='open'], [role='alertdialog'][data-state='open']",
    )
  ) {
    return false;
  }
  const target = event.target;
  if (
    target instanceof Element &&
    target.closest("[role='dialog'], [role='alertdialog']")
  ) {
    return false;
  }
  if (
    target instanceof HTMLElement &&
    target.closest("input, textarea, select, [contenteditable='true']") &&
    !target.closest("[data-chat-approval-card]")
  ) {
    return false;
  }
  return true;
}

export function approvalOptionLabel(
  option: ApprovalOptionLabelSource,
  t: TFunction<"chat">,
): string {
  const match = matchApprovalOptionName(option.name ?? "");
  switch (match.type) {
    case "allowCommandPrefix":
      return t("approval.option.allowCommandPrefix", {
        command: match.command,
      });
    case "allowFuture":
      return t("approval.option.allowFuture", { target: match.target });
    case "blockFuture":
      return t("approval.option.blockFuture", { target: match.target });
    case "named":
      return t(approvalOptionLabelKeys[match.key]);
    case "custom":
      return match.name;
  }
}

export function approvalResultLabel(
  result: ApprovalResultLabelSource,
  t: TFunction<"chat">,
): string {
  if (result.result === "invalidated") return t("toolApproval.invalidated");
  if (result.result === "cancelled") return t("toolApproval.cancelled");

  const match = matchApprovalOptionName(result.name ?? "");
  switch (match.type) {
    case "allowCommandPrefix":
      return t(approvalResultLabelKeys.allowCommandPrefix, {
        command: match.command,
      });
    case "allowFuture":
      return t(approvalResultLabelKeys.allowFuture, { target: match.target });
    case "blockFuture":
      return t(approvalResultLabelKeys.blockFuture, { target: match.target });
    case "named":
      return t(approvalResultLabelKeys[match.key]);
    case "custom":
      break;
  }
  if (result.kind === "reject_always") {
    return t("toolApproval.rejectedAlways");
  }
  if (result.kind?.startsWith("reject")) {
    return t("toolApproval.rejected");
  }
  if (result.kind === "allow_always") {
    return t("toolApproval.allowedAlways");
  }
  if (result.kind === "allow_once") {
    return t("toolApproval.allowedOnce");
  }
  return match.name || t("toolApproval.handled");
}

export function ChatApprovalCard({
  approval,
  current,
  total,
  submissionState,
  onResolve,
}: ChatApprovalCardProps) {
  const { t } = useTranslation("chat");
  const options = approval.options ?? [];
  const allowOnce = options.find((option) => option.kind === "allow_once");
  const rejectOnce = options.find((option) => option.kind === "reject_once");
  const extras = options.filter(
    (option) => option.id !== allowOnce?.id && option.id !== rejectOnce?.id,
  );
  const pending = submissionState !== "idle";
  const ApprovalIcon = approvalKindIcons[approval.kind ?? ""] ?? Wrench;
  const resolve = useCallback(
    (optionId?: string) => {
      if (pending || !approval.approvalId || !optionId) return;
      onResolve(approval.approvalId, optionId);
    },
    [approval.approvalId, onResolve, pending],
  );

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (pending || !approvalHotkeysAllowed(event)) return;
      const target = event.target;
      if (
        event.key === "Enter" &&
        target instanceof HTMLElement &&
        target.closest("button,[role=menuitem]")
      ) {
        return;
      }
      if (event.key === "Escape" && rejectOnce?.id) {
        event.preventDefault();
        resolve(rejectOnce.id);
      }
      if (event.key === "Enter" && allowOnce?.id) {
        event.preventDefault();
        resolve(allowOnce.id);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [allowOnce?.id, pending, rejectOnce?.id, resolve]);

  return (
    <section
      data-chat-approval-card
      {...notificationPresentationProps({
        kind: "request",
        noticeId: `approval:${approval.approvalId}`,
      })}
      className="grid min-h-32 gap-3 px-4 py-3"
      aria-label={t("approval.aria")}
    >
      <div className="flex items-start gap-3">
        <span className="bg-muted grid size-8 shrink-0 place-items-center rounded-md">
          <ApprovalIcon className="text-muted-foreground size-icon-md" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex items-center justify-between gap-3">
            <p className="text-muted-foreground type-caption">
              {total > 1
                ? t("approval.position", { current, total })
                : t("waitingApproval")}
            </p>
            {approval.kind ? (
              <span className="bg-muted text-muted-foreground type-caption rounded-xs px-2 py-0.5">
                {t(`approval.kind.${approval.kind}`, {
                  defaultValue: approval.kind,
                })}
              </span>
            ) : null}
          </div>
          <h3 className="type-title mt-1 font-medium">
            {approval.title || t("approval.fallbackTitle")}
          </h3>
          {approval.summary && approval.summary !== approval.title ? (
            <p className="text-muted-foreground type-body mt-1 whitespace-pre-wrap">
              {approval.summary}
            </p>
          ) : null}
          {approval.command && approval.command !== approval.title ? (
            <code
              className="bg-muted text-foreground/75 type-ui mt-2 block max-w-full truncate rounded-xs px-2 py-1 font-mono"
              title={approval.command}
            >
              {approval.command}
            </code>
          ) : null}
          {(approval.locations ?? []).length ? (
            <div className="text-muted-foreground type-caption mt-2 grid gap-0.5 font-mono">
              {approval.locations?.map((location) => (
                <span key={`${location.path}:${location.line ?? 0}`}>
                  {location.path}
                  {location.line !== undefined ? `:${location.line}` : ""}
                </span>
              ))}
            </div>
          ) : null}
        </div>
      </div>

      <div className="flex flex-wrap items-center justify-end gap-2">
        {submissionState === "submitting" ? (
          <span className="text-muted-foreground type-caption mr-auto inline-flex items-center gap-1.5">
            <Loader2 className="size-icon-sm animate-spin" />
            {t("approval.submitting")}
          </span>
        ) : submissionState === "confirming" ? (
          <span className="text-warning type-caption mr-auto inline-flex items-center gap-1.5">
            <Loader2 className="size-icon-sm animate-spin" />
            {t("approval.confirming")}
          </span>
        ) : null}
        <Button
          type="button"
          size="sm"
          variant="secondary"
          disabled={pending || !rejectOnce?.id}
          onClick={() => resolve(rejectOnce?.id)}
        >
          {t("approval.reject")}
          <kbd className="text-muted-foreground type-caption">Esc</kbd>
        </Button>
        <div className="inline-flex">
          <Button
            type="button"
            size="sm"
            variant="success"
            attached={extras.length ? "start" : undefined}
            disabled={pending || !allowOnce?.id}
            onClick={() => resolve(allowOnce?.id)}
          >
            <ShieldCheck />
            {t("approval.allowOnce")}
            <kbd className="text-primary-foreground/70 type-caption">↵</kbd>
          </Button>
          {extras.length ? (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  type="button"
                  size="icon-sm"
                  variant="success"
                  attached="end"
                  disabled={pending}
                  aria-label={t("approval.moreOptions")}
                >
                  <ChevronDown />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-80">
                {extras.map((option) => (
                  <DropdownMenuItem
                    key={option.id}
                    onSelect={() => resolve(option.id)}
                  >
                    {approvalOptionLabel(option, t)}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
        </div>
      </div>
    </section>
  );
}
