import { useQuery } from "@tanstack/react-query";
import type { RefObject } from "react";
import { useVisibleScene } from "./chat-reading";
import {
  markRunViewed,
  runViewingOptions,
  type RunReadingKind,
} from "./reading-queries";
import { usePresentationReading } from "./presentation-reading";
import { useQueryEvents } from "./use-query-events";

export function useRunViewing({
  ownerKind,
  ownerId,
  root,
  selector,
  rendered,
  automaticTaskId,
}: {
  ownerKind: RunReadingKind;
  ownerId: string;
  root: RefObject<HTMLElement | null>;
  selector?: string;
  rendered: boolean;
  automaticTaskId?: string;
}) {
  const visible = useVisibleScene();
  const active = visible && rendered && Boolean(ownerId);
  const options = runViewingOptions(ownerKind, ownerId);
  const query = useQuery({ ...options, enabled: active });
  useQueryEvents({
    topic: "reading",
    resource: { kind: ownerKind, id: ownerId },
    queryKeys: [options.queryKey],
    eventTypes: ["reading.changed"],
    authority: true,
    active,
    enabled: active,
  });
  const data =
    !automaticTaskId || query.data?.automaticTaskId === automaticTaskId
      ? query.data
      : undefined;
  usePresentationReading({
    root,
    selector,
    identity:
      active && data?.complete && data.viewable && !data.viewed
        ? `${ownerKind}:${ownerId}`
        : undefined,
    revision: query.dataUpdatedAt,
    read: () => markRunViewed(ownerKind, ownerId),
  });
  return { ...query, data };
}
