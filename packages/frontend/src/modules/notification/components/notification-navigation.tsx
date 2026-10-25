import { createContext, useContext, type ReactNode } from "react";
import type { NotificationItem } from "../model/notification";

const Navigation = createContext<(notice: NotificationItem) => string>(
  () => "/work-board",
);
export function NotificationNavigationProvider({
  resolveHref,
  children,
}: {
  resolveHref: (notice: NotificationItem) => string;
  children: ReactNode;
}) {
  return (
    <Navigation.Provider value={resolveHref}>{children}</Navigation.Provider>
  );
}
export function useNotificationHref() {
  return useContext(Navigation);
}
