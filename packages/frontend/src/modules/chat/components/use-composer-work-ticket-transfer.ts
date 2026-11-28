import { useQueryClient } from "@tanstack/react-query";
import { useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  listChannels,
  listWakers,
  listWorkTickets,
  transferWorkTicket,
} from "@/shared/api/generated/sdk.gen";
import type { TransferWorkTicketData } from "@/shared/api/generated/types.gen";
import { listWorkTicketsQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import { ApiError } from "@/shared/api/errors";
import {
  apiErrorNoticePresentation,
  dismissErrorNotice,
  reportErrorNotice,
} from "@/shared/api/error-notices";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import type { WorkTicketOrigin } from "../api/work-tickets";
import type { ChatReplyReference } from "../runtime/chat-projection";

export type ComposerWorkTicketTransfer = {
  origin: WorkTicketOrigin;
  replyHasTicket: boolean;
  refresh: () => Promise<unknown>;
  onAccepted: () => void;
};

const transferCommand =
  /^\s*\/(?:移交|transfer)(?=\s|$)[^\S\r\n]*([^\r\n]*)(?:\r?\n([\s\S]*))?$/;
const targetPageSize = 100;
type TargetKind = "waker" | "team";
type Target = { id?: string; name?: string };
type TargetMatch = { kind: TargetKind; id: string };

export function isWorkTicketTransfer(text: string): boolean {
  return transferCommand.test(text);
}

async function listExactTargets(
  kind: TargetKind,
  name: string,
): Promise<TargetMatch[]> {
  const matches = new Map<string, TargetMatch>();
  const isID = /^[a-f0-9]{32}$/i.test(name);
  for (let pageNum = 1; ; pageNum++) {
    const request = withLocalErrorSurface({
      query: {
        pageNum,
        pageSize: targetPageSize,
        query: isID ? undefined : name,
      },
      throwOnError: true as const,
    });
    let targets: Array<Target | undefined>;
    let totalSize: number | undefined;
    if (kind === "waker") {
      const { data } = await listWakers(request);
      targets = data.resources ?? [];
      ({ totalSize } = data);
    } else {
      const { data } = await listChannels(request);
      targets = (data.resources ?? []).map(({ channel }) => channel);
      ({ totalSize } = data);
    }
    for (const target of targets) {
      if (target?.id && (target.id === name || target.name === name)) {
        matches.set(target.id, { kind, id: target.id });
      }
    }
    if (
      targets.length < targetPageSize ||
      (totalSize !== undefined && pageNum * targetPageSize >= totalSize)
    )
      return [...matches.values()];
  }
}

export function useComposerWorkTicketTransfer(
  config: ComposerWorkTicketTransfer | undefined,
  reply: ChatReplyReference | undefined,
  errorScope: string,
) {
  const client = useQueryClient();
  const { t } = useTranslation("chat");
  const [pending, setPending] = useState(false);
  const busy = useRef(false);
  const retry = useRef<
    | { key: string; request: Pick<TransferWorkTicketData, "path" | "body"> }
    | undefined
  >(undefined);
  const currentReply = useRef(reply?.messageId);
  useLayoutEffect(() => {
    currentReply.current = reply?.messageId;
  }, [reply?.messageId]);
  const notice = `work-ticket-transfer:${errorScope}`;

  return {
    pending,
    submit: async (
      text: string,
      hasAttachments: boolean,
      stillEditing: () => boolean,
    ) => {
      if (!config || busy.current) return;
      const command = transferCommand.exec(text);
      if (!command) return;
      busy.current = true;
      setPending(true);
      dismissErrorNotice(notice);
      const key = JSON.stringify([config.origin, reply?.messageId, text]);
      try {
        if (!reply?.messageId)
          throw new ApiError(t("workTicket.transferRequiresReply"));
        if (hasAttachments)
          throw new ApiError(t("workTicket.transferTextOnly"));
        if (retry.current?.key !== key) {
          const destination = command[1]?.trim() ?? "";
          const qualified = /^(waker|team):(.+)$/.exec(destination);
          const kinds: TargetKind[] = qualified
            ? [qualified[1] as TargetKind]
            : ["waker", "team"];
          const name = (qualified?.[2] ?? destination).trim();
          if (!name) throw new ApiError(t("workTicket.transferUsage"));
          const [matches, { data }] = await Promise.all([
            Promise.all(kinds.map((kind) => listExactTargets(kind, name))).then(
              (rows) => rows.flat(),
            ),
            listWorkTickets(
              withLocalErrorSurface({
                query: { ...config.origin, messageId: [reply.messageId] },
                throwOnError: true,
              }),
            ),
          ]);
          const ticket = data.find(
            ({ messageId }) => messageId === reply.messageId,
          )?.ticket;
          if (!ticket?.id || !ticket.generation)
            throw new ApiError(t("workTicket.transferRequiresReply"));
          const target = matches[0];
          if (!target)
            throw new ApiError(t("workTicket.transferTargetMissing"));
          if (matches.length !== 1)
            throw new ApiError(t("workTicket.transferTargetAmbiguous"));
          const content = command[2]?.trim() || reply.preview?.trim();
          if (!content) throw new ApiError(t("workTicket.transferNeedsNote"));
          retry.current = {
            key,
            request: {
              path: { ticketId: ticket.id },
              body: {
                ...config.origin,
                targetKind: target.kind,
                targetId: target.id,
                expectedGeneration: ticket.generation,
                requestId: crypto.randomUUID(),
                content,
              },
            },
          };
        }
        const { path, body } = retry.current.request;
        await transferWorkTicket(
          withLocalErrorSurface({ path, body, throwOnError: true }),
        );
        retry.current = undefined;
        if (currentReply.current === reply.messageId && stillEditing())
          config.onAccepted();
        void config.refresh().catch(() => {});
        const [ticketKey] = listWorkTicketsQueryKey({
          query: { ...config.origin, messageId: [reply.messageId] },
        });
        void client.invalidateQueries({
          queryKey: [{ ...ticketKey, query: config.origin }],
        });
      } catch (error) {
        if (
          error instanceof ApiError &&
          error.status &&
          error.status >= 400 &&
          error.status < 500 &&
          error.status !== 408 &&
          error.status !== 429
        )
          retry.current = undefined;
        reportErrorNotice({
          id: notice,
          scope: errorScope,
          lifetime: "session",
          ...apiErrorNoticePresentation(error, t("workTicket.transferFailed")),
        });
      } finally {
        busy.current = false;
        setPending(false);
      }
    },
  };
}
