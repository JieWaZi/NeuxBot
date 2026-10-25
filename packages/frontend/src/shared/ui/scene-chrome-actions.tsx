import { createContext, useContext, type ReactNode } from "react";

const SceneChromeActionsContext = createContext<ReactNode>(null);

export function SceneChromeActionsProvider({
  actions,
  children,
}: {
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <SceneChromeActionsContext.Provider value={actions ?? null}>
      {children}
    </SceneChromeActionsContext.Provider>
  );
}

export function useSceneChromeActions() {
  return useContext(SceneChromeActionsContext);
}
