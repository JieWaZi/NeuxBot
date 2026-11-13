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
  type AppSceneTab,
  type ChatSceneOwner,
} from "@/shared/workspace/app-scene-model";
import {
  routeSceneForLocation,
  type RouteSceneLabels,
  type RouteSceneLocation,
} from "./app-route-scene-model";
import {
  chatOwnerForScene,
  sameChatOwner,
  unsentChatSceneForOwner,
} from "../layout/entity-navigation-history";
import { useRouteSceneNotFound } from "./route-scene-not-found";

export function AppSceneWorkspaceProvider({
  children,
  location,
  navigate,
  navigationType = "PUSH",
}: {
  children: ReactNode;
  location: RouteSceneLocation;
  navigationType?: "POP" | "PUSH" | "REPLACE";
  navigate: (href: string, navigationState?: unknown) => void;
}) {
  const { t } = useTranslation("common");
  const {
    pathname,
    search,
    state: navigationState,
    key: navigationKey,
  } = location;
  const labels = useMemo<RouteSceneLabels>(
    () => ({
      knowledge: t("knowledge:knowledge"),
      resources: t("plugin:title"),
      workBoard: t("workBoard:title"),
      usage: t("usage:title"),
      im: t("im:title"),
      automaticTask: t("automaticTask:title"),
      workflow: t("workflow:title"),
      chat: t("sceneTabs.chat"),
      detail: t("sceneTabs.detail"),
      newChat: t("sceneTabs.newChat"),
      newWaker: t("partnerNavigation.createWaker"),
      page: t("sceneTabs.page"),
      partner: t("sceneTabs.partner"),
      settings: t("sceneTabs.settings"),
      team: t("sceneTabs.team"),
      teamChat: t("sceneTabs.teamChat"),
    }),
    [t],
  );
  const routeScene = useMemo(
    () =>
      routeSceneForLocation(
        { pathname, search, state: navigationState },
        labels,
      ),
    [labels, navigationState, pathname, search],
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

  const previousNavigation = useRef({ pathname, search, navigationKey });
  useLayoutEffect(() => {
    const previous = previousNavigation.current;
    const returningToSameRoute =
      previous.pathname === pathname &&
      previous.search === search &&
      previous.navigationKey !== navigationKey;
    previousNavigation.current = { pathname, search, navigationKey };
    // 同一 URL 的显式导航也代表返回主会话，不能继续停留在工作台子页签。
    if (returningToSameRoute) {
      const contribution = workspaceContributionsRef.current.get(routeScene.id);
      contribution?.onSelect(contribution.primaryWorkspaceTabId);
    }
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setSnapshot((current) =>
      appSceneReducer(current, {
        type: "open",
        tab: routeScene,
        // 只有首发接纳或入口解析的 replace 导航迁移草稿；普通切换保留未发送内容。
        replaceTemporary: navigationType === "REPLACE",
      }),
    );
  }, [navigationKey, navigationType, pathname, routeScene, search]);

  const navigateToScene = useCallback(
    (target: AppSceneTab | undefined, tabs: readonly AppSceneTab[]) => {
      if (!target?.href) return;
      const owner =
        target.kind === "workspace"
          ? tabs.find((tab) => tab.id === target.ownerTabId)
          : target;
      const state = owner?.navigationState;
      if (
        target.href === `${pathname}${search}` &&
        Object.is(state, navigationState)
      )
        return;
      if (state === undefined) navigate(target.href);
      else navigate(target.href, state);
    },
    [navigate, navigationState, pathname, search],
  );

  const dispatch = useCallback(
    (command: AppSceneCommand) => {
      const current = snapshot;
      const selectedBeforeUpdate =
        command.type === "select" || command.type === "close"
          ? current.tabs.find((tab) => tab.id === command.tabId)
          : undefined;
      const next = appSceneReducer(current, command);
      setSnapshot(next);

      if (command.type === "close-others") {
        for (const tab of current.tabs) {
          if (
            tab.kind !== "workspace" ||
            !tab.workspaceTabId ||
            next.tabs.some((retained) => retained.id === tab.id)
          )
            continue;
          // 宿主保留时通过原有关闭入口释放子页签；宿主移除时由卸载清理。
          if (
            tab.ownerTabId &&
            next.tabs.some((retained) => retained.id === tab.ownerTabId)
          ) {
            workspaceContributionsRef.current
              .get(tab.ownerTabId)
              ?.onClose(tab.workspaceTabId);
          }
        }
        const target = next.tabs.find((tab) => tab.id === next.activeTabId);
        if (
          target?.kind === "workspace" &&
          target.ownerTabId &&
          target.workspaceTabId
        ) {
          workspaceContributionsRef.current
            .get(target.ownerTabId)
            ?.onSelect(target.workspaceTabId);
        } else if (target?.kind === "route") {
          const contribution = workspaceContributionsRef.current.get(target.id);
          if (contribution)
            contribution.onSelect(contribution.primaryWorkspaceTabId);
        }
        navigateToScene(target, next.tabs);
        return;
      }

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
        navigateToScene(target, next.tabs);
        return;
      }

      if (
        command.type === "close" &&
        current.activeTabId &&
        !next.tabs.some((tab) => tab.id === current.activeTabId)
      ) {
        const target = next.tabs.find((tab) => tab.id === next.activeTabId);
        // 关闭 owner 也可能移除活跃子项；回退沿用目标 pane 的选择协议。
        if (
          target?.kind === "workspace" &&
          target.ownerTabId &&
          target.workspaceTabId
        ) {
          workspaceContributionsRef.current
            .get(target.ownerTabId)
            ?.onSelect(target.workspaceTabId);
        } else if (target?.kind === "route") {
          const contribution = workspaceContributionsRef.current.get(target.id);
          if (contribution)
            contribution.onSelect(contribution.primaryWorkspaceTabId);
        }
        navigateToScene(target, next.tabs);
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
    [navigateToScene, snapshot],
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
          chatOwner: contribution.chatOwner,
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

  const openNewChat = useCallback(
    (requestedOwner?: ChatSceneOwner) => {
      const contribution = workspaceContributionsRef.current.get(routeScene.id);
      const current = snapshot.tabs.find((tab) => tab.id === routeScene.id);
      const owner =
        requestedOwner ?? contribution?.chatOwner ?? chatOwnerForScene(current);
      const draft = unsentChatSceneForOwner(snapshot.tabs, owner);
      if (draft) {
        dispatch({ type: "select", tabId: draft.id });
        return true;
      }
      if (
        requestedOwner &&
        !sameChatOwner(chatOwnerForScene(current), requestedOwner)
      )
        return false;
      if (!contribution?.onNewChat) return false;
      contribution.onNewChat();
      return true;
    },
    [dispatch, routeScene.id, snapshot.tabs],
  );
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
