import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import { useTranslation } from "react-i18next";

import {
  importWakerNavigationGroupsMutation,
  listWakerNavigationGroupsOptions,
  listWakerNavigationGroupsQueryKey,
  moveWakerToNavigationGroupMutation,
  placeWakerInNavigationMutation,
} from "@/modules/waker";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { toast } from "@/shared/ui/sonner";

import type {
  NavigationDropPlacement,
  PartnerNavigationPreferences,
  WakerNavigationGroup,
} from "./partner-navigation-preferences";

const emptyGroups: WakerNavigationGroup[] = [];

type PendingPlacement = {
  wakerId: string;
  targetWakerId: string;
  placement: NavigationDropPlacement;
};

function previewPlacement(
  snapshot: { groups: WakerNavigationGroup[]; ungroupedWakerIds: string[] },
  { wakerId, targetWakerId, placement }: PendingPlacement,
) {
  const { groups, ungroupedWakerIds } = snapshot;
  if (
    !ungroupedWakerIds.includes(targetWakerId) &&
    !groups.some((group) => group.wakerIds.includes(targetWakerId))
  )
    return snapshot;
  const placeMembers = (ids: string[]) => {
    if (!ids.includes(wakerId) && !ids.includes(targetWakerId)) return ids;
    return ids.flatMap((id) => {
      if (id === wakerId) return [];
      if (id !== targetWakerId) return [id];
      return placement === "before" ? [wakerId, id] : [id, wakerId];
    });
  };
  return {
    groups: groups.map((group) => ({
      ...group,
      wakerIds: placeMembers(group.wakerIds),
    })),
    ungroupedWakerIds: placeMembers(ungroupedWakerIds),
  };
}

/** 侧栏从 Core 读取分组，旧本机布局仅作为首次导入的输入。 */
export function useWakerNavigationGroups(
  preferences: PartnerNavigationPreferences,
  onPreferencesChange: Dispatch<SetStateAction<PartnerNavigationPreferences>>,
) {
  const { t } = useTranslation("common", {
    keyPrefix: "partnerNavigation.wakerActions",
  });
  const client = useQueryClient();
  const [pendingPlacement, setPendingPlacement] =
    useState<PendingPlacement | null>(null);
  const placementInFlight = useRef(false);
  const groupsQuery = useQuery(listWakerNavigationGroupsOptions());
  useQueryEvents({
    topic: "entities",
    resource: { kind: "waker_navigation_groups", id: "navigation" },
    queryKeys: [listWakerNavigationGroupsQueryKey()],
  });
  const refresh = useCallback(
    () =>
      client.invalidateQueries({
        queryKey: listWakerNavigationGroupsQueryKey(),
      }),
    [client],
  );
  const move = useMutation({
    ...withLocalErrorMutation(moveWakerToNavigationGroupMutation()),
    onSuccess: refresh,
  });
  const place = useMutation({
    ...withLocalErrorMutation(placeWakerInNavigationMutation()),
    onSuccess: refresh,
  });
  const importStatus = useRef<"new" | "pending" | "attempted">("new");
  const { mutate: importGroups, isError: importFailed } = useMutation({
    ...withLocalErrorMutation(importWakerNavigationGroupsMutation()),
    // 导入结果需要跨 StrictMode 的订阅重建，才能清除本机旧布局。
    onSuccess: async () => {
      await refresh();
      onPreferencesChange((current) => ({
        ...current,
        wakerGroups: undefined,
        railOrder: current.railOrder.filter((key) => !key.startsWith("waker:")),
      }));
    },
    onSettled: () => {
      importStatus.current = "attempted";
    },
  });
  const legacyGroups = preferences.wakerGroups;
  const legacyRailOrder = preferences.railOrder;
  const hasLegacyNavigation = Boolean(
    legacyGroups?.length ||
    legacyRailOrder.some((key) => key.startsWith("waker:")),
  );
  const retryImport = useCallback(() => {
    if (!hasLegacyNavigation || importStatus.current === "pending") return;
    importStatus.current = "pending";
    const wakerOrder = legacyRailOrder
      .filter((key) => key.startsWith("waker:"))
      .map((key) => key.slice("waker:".length));
    importGroups({ body: { groups: legacyGroups ?? [], wakerOrder } });
  }, [hasLegacyNavigation, importGroups, legacyGroups, legacyRailOrder]);
  useEffect(() => {
    if (importStatus.current === "new") retryImport();
  }, [retryImport]);

  // 投影只覆盖本次拖放，推送和刷新仍可对账其他伙伴的实际位置。
  const snapshot =
    pendingPlacement && groupsQuery.data
      ? previewPlacement(groupsQuery.data, pendingPlacement)
      : groupsQuery.data;
  const groups = snapshot?.groups ?? emptyGroups;
  const canMove =
    Boolean(groupsQuery.data) &&
    !hasLegacyNavigation &&
    !pendingPlacement &&
    !move.isPending &&
    !place.isPending;

  async function submitPlacement(
    submit: () => Promise<unknown>,
    preview?: PendingPlacement,
  ): Promise<boolean> {
    if (!canMove || placementInFlight.current) return false;
    placementInFlight.current = true;
    if (preview) setPendingPlacement(preview);
    try {
      await submit();
      return true;
    } catch (error) {
      toast.error(localApiErrorMessage(error, t("groupMoveError")));
      return false;
    } finally {
      if (preview) setPendingPlacement(null);
      placementInFlight.current = false;
    }
  }

  return {
    canMove,
    migrationResolved: !hasLegacyNavigation,
    retryImport: hasLegacyNavigation && importFailed ? retryImport : undefined,
    groups,
    wakerOrder: [
      ...(snapshot?.ungroupedWakerIds ?? []),
      ...groups.flatMap((group) => group.wakerIds),
    ],
    refresh,
    move(wakerId: string, name: string) {
      return submitPlacement(() =>
        move.mutateAsync({ path: { wakerId }, body: { name } }),
      );
    },
    place(
      wakerId: string,
      targetWakerId: string,
      placement: NavigationDropPlacement,
    ) {
      return submitPlacement(
        () =>
          place.mutateAsync({
            path: { wakerId },
            body: { targetWakerId, placement },
          }),
        { wakerId, targetWakerId, placement },
      );
    },
  };
}
