import {
  createContext,
  useCallback,
  useContext,
  useLayoutEffect,
  useState,
  type ReactNode,
} from "react";

export type FloatingChatLauncherEntry = {
  ariaLabel: string;
  label: string;
  onActivate: () => void;
  state: "idle" | "running" | "waiting";
};

type Registration = { id: symbol; entry: FloatingChatLauncherEntry };

type FloatingChatLauncherContextValue = {
  current: FloatingChatLauncherEntry | null;
  register: (entry: FloatingChatLauncherEntry) => () => void;
};

const FloatingChatLauncherContext =
  createContext<FloatingChatLauncherContextValue | null>(null);

export function FloatingChatLauncherProvider({
  children,
}: {
  children: ReactNode;
}) {
  const [registration, setRegistration] = useState<Registration | null>(null);
  const register = useCallback((entry: FloatingChatLauncherEntry) => {
    const id = Symbol("floating-chat-launcher");
    setRegistration({ id, entry });
    return () =>
      setRegistration((current) => (current?.id === id ? null : current));
  }, []);

  return (
    <FloatingChatLauncherContext
      value={{ current: registration?.entry ?? null, register }}
    >
      {children}
    </FloatingChatLauncherContext>
  );
}

export function useFloatingChatLauncher() {
  return useContext(FloatingChatLauncherContext)?.current ?? null;
}

export function useRegisterFloatingChatLauncher(
  entry: FloatingChatLauncherEntry | null,
) {
  const register = useContext(FloatingChatLauncherContext)?.register;
  useLayoutEffect(() => {
    if (!register || !entry) return;
    return register(entry);
  }, [entry, register]);
}
