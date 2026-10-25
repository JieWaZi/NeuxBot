import {
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";

import {
  AppSceneWorkspaceContextProvider,
  type AppWorkspaceContribution,
} from "@/shared/workspace/app-scene-workspace";
import {
  appSceneReducer,
  initialAppSceneSnapshot,
  type AppSceneCommand,
} from "@/shared/workspace/app-scene-model";
import {
  routeSceneForLocation,
  type RouteSceneLabels,
  type RouteSceneLocation,
} from "./app-route-scene-model";
import { useRouteSceneNotFound } from "./route-scene-not-found";

export function AppSceneWorkspaceProvider({
  children,
  location,
  navigate,
}: {
  children: ReactNode;
  location: RouteSceneLocation;
  navigate: (href: string) => void;
}) {
  const { t } = useTranslation("common");
  const { pathname, search } = location;
  const labels = useMemo<RouteSceneLabels>(
    () => ({
      knowledge: t("knowledge:knowledge"),
      resources: t("plugin:title"),
      workBoard: t("workBoard:title"),
      im: t("im:title"),
      automaticTask: t("automaticTask:title"),
      workflow: t("workflow:title"),
      chat: t("sceneTabs.chat"),
      detail: t("sceneTabs.detail"),
      newChat: t("sceneTabs.newChat"),
      page: t("sceneTabs.page"),
      partner: t("sceneTabs.partner"),
      settings: t("sceneTabs.settings"),
      team: t("sceneTabs.team"),
      teamChat: t("sceneTabs.teamChat"),
    }),
    [t],
  );
  const routeScene = useMemo(
    () => routeSceneForLocation({ pathname, search }, labels),
    [labels, pathname, search],
  );
  const [snapshot, setSnapshot] = useState(() =>
    initialAppSceneSnapshot(routeScene),
  );
  const notFoundFlags = useRouteSceneNotFound(snapshot.tabs);
  const visibleSnapshot = useMemo(
    () => ({
      ...snapshot,
      tabs: snapshot.tabs.map((tab, index) => ({
        ...tab,
        notFound: notFoundFlags[index] === "1",
      })),
    }),
    [notFoundFlags, snapshot],
  );
  const workspaceContributionsRef = useRef(
    new Map<string, AppWorkspaceContribution>(),
  );

  useLayoutEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setSnapshot((current) =>
      appSceneReducer(current, { type: "open", tab: routeScene }),
    );
  }, [routeScene]);

  const dispatch = useCallback(
    (command: AppSceneCommand) => {
      const current = snapshot;
      const selectedBeforeUpdate =
        command.type === "select" || command.type === "close"
          ? current.tabs.find((tab) => tab.id === command.tabId)
          : undefined;
      const next = appSceneReducer(current, command);
      setSnapshot(next);

      if (command.type === "select") {
        const target = next.tabs.find((tab) => tab.id === command.tabId);
        if (target?.kind === "workspace") {
          const contribution = target.ownerTabId
            ? workspaceContributionsRef.current.get(target.ownerTabId)
            : undefined;
          if (target.workspaceTabId) {
            contribution?.onSelect(target.workspaceTabId);
          }
        } else if (target?.kind === "route") {
          const contribution = workspaceContributionsRef.current.get(target.id);
          if (contribution) {
            contribution.onSelect(contribution.primaryWorkspaceTabId);
          }
        }
        if (target?.href && target.href !== `${pathname}${search}`) {
          navigate(target.href);
        }
        return;
      }

      if (command.type === "close" && current.activeTabId === command.tabId) {
        const target = next.tabs.find((tab) => tab.id === next.activeTabId);
        if (target?.href && target.href !== `${pathname}${search}`) {
          navigate(target.href);
        }
      }

      if (
        command.type === "close" &&
        selectedBeforeUpdate?.kind === "workspace" &&
        selectedBeforeUpdate.workspaceTabId
      ) {
        const contribution = selectedBeforeUpdate.ownerTabId
          ? workspaceContributionsRef.current.get(
              selectedBeforeUpdate.ownerTabId,
            )
          : undefined;
        contribution?.onClose(selectedBeforeUpdate.workspaceTabId);
      }
    },
    [navigate, pathname, search, snapshot],
  );

  const syncWorkspace = useCallback(
    (contribution: AppWorkspaceContribution) => {
      workspaceContributionsRef.current.set(
        contribution.ownerTabId,
        contribution,
      );
      setSnapshot((current) =>
        appSceneReducer(current, {
          type: "sync-workspace",
          activeWorkspaceTabId: contribution.activeWorkspaceTabId,
          ownerHref: contribution.ownerHref,
          ownerTabId: contribution.ownerTabId,
          primaryWorkspaceTabId: contribution.primaryWorkspaceTabId,
          tabs: contribution.tabs,
        }),
      );
    },
    [],
  );

  const openNewChat = useCallback(() => {
    const contribution = workspaceContributionsRef.current.get(routeScene.id);
    if (!contribution?.onNewChat) return false;
    contribution.onNewChat();
    return true;
  }, [routeScene.id]);
  const value = useMemo(
    () => ({
      dispatch,
      routeSceneId: routeScene.id,
      snapshot: visibleSnapshot,
      syncWorkspace,
      openNewChat,
    }),
    [dispatch, routeScene.id, visibleSnapshot, syncWorkspace, openNewChat],
  );
  return (
    <AppSceneWorkspaceContextProvider value={value}>
      {children}
    </AppSceneWorkspaceContextProvider>
  );
}
