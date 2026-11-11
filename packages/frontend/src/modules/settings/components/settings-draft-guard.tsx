import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { UNSAFE_DataRouterContext, useBlocker } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";

type NavigationAction = () => unknown;
type DraftState = { dirty: boolean; pending?: boolean };
type Guard = {
  register: (id: string, state: DraftState | undefined) => void;
  navigate: (action: NavigationAction) => void;
};
const DraftGuardContext = createContext<Guard | undefined>(undefined);
const immediate = (action: NavigationAction) => action();

function RouteDraftBlocker({
  protectedDraft,
  pending,
  bypass,
  request,
}: {
  protectedDraft: boolean;
  pending: boolean;
  bypass: { current: boolean };
  request: (proceed: NavigationAction, reset?: () => void) => void;
}) {
  const blocker = useBlocker(({ currentLocation, nextLocation }) => {
    if (!protectedDraft || bypass.current) return false;
    if (currentLocation.pathname !== nextLocation.pathname) return true;
    const current = new URLSearchParams(currentLocation.search);
    const next = new URLSearchParams(nextLocation.search);
    // 对象定位决定编辑生命周期；分页、搜索等同页读取条件不丢弃配置草稿。
    return [
      "tab",
      "detail",
      "platform",
      "connection",
      "binding",
      "chat",
      "agent",
    ].some((key) => current.get(key) !== next.get(key));
  });
  useEffect(() => {
    if (blocker.state !== "blocked") return;
    if (pending) blocker.reset();
    else request(blocker.proceed, blocker.reset);
  }, [blocker, pending, request]);
  return null;
}

/** 同一入口保护URL导航和Scene副作用；确认之前不切换或卸载设置。 */
export function SettingsNavigationGuardProvider({
  children,
}: {
  children: ReactNode;
}) {
  const { t } = useTranslation("im");
  const dataRouter = useContext(UNSAFE_DataRouterContext);
  const drafts = useRef(new Map<string, DraftState>());
  const [state, setState] = useState<DraftState>({
    dirty: false,
    pending: false,
  });
  const [request, setRequest] = useState<{
    proceed: NavigationAction;
    reset?: () => void;
  }>();
  const bypass = useRef(false);
  const register = useCallback((id: string, value: DraftState | undefined) => {
    if (value) drafts.current.set(id, value);
    else drafts.current.delete(id);
    const values = [...drafts.current.values()];
    const next = {
      dirty: values.some((item) => item.dirty),
      pending: values.some((item) => item.pending),
    };
    setState((previous) =>
      previous.dirty === next.dirty && previous.pending === next.pending
        ? previous
        : next,
    );
  }, []);
  const ask = useCallback(
    (proceed: NavigationAction, reset?: () => void) =>
      setRequest({ proceed, reset }),
    [],
  );
  const navigate = useCallback(
    (action: NavigationAction) => {
      if (state.pending) return;
      if (state.dirty) ask(action);
      else action();
    },
    [state.dirty, state.pending, ask],
  );
  const reset = () => {
    request?.reset?.();
    setRequest(undefined);
  };
  const proceed = () => {
    if (state.pending || !request) return;
    bypass.current = true;
    setRequest(undefined);
    const result = request.proceed();
    const resetBypass = () => {
      bypass.current = false;
    };
    if (result instanceof Promise) void result.then(resetBypass, resetBypass);
    else queueMicrotask(resetBypass);
  };
  return (
    <DraftGuardContext.Provider value={{ register, navigate }}>
      {dataRouter ? (
        <RouteDraftBlocker
          protectedDraft={state.dirty || Boolean(state.pending)}
          pending={Boolean(state.pending)}
          bypass={bypass}
          request={ask}
        />
      ) : null}
      {children}
      {request ? (
        <Dialog
          open
          onOpenChange={(open) => {
            if (!open) reset();
          }}
        >
          <DialogContent
            title={t("unsavedTitle")}
            description={t("unsavedDescription")}
          >
            <DialogFooter>
              <Button variant="secondary" onClick={reset}>
                {t("continueEditing")}
              </Button>
              <Button
                variant="danger-quiet"
                onClick={proceed}
                disabled={state.pending}
              >
                {t("discardChanges")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      ) : null}
    </DraftGuardContext.Provider>
  );
}

export function useSettingsNavigationGuard() {
  return useContext(DraftGuardContext)?.navigate ?? immediate;
}

/** 草稿仍由领域表单拥有；这里只登记离开保护与关闭窗口保护。 */
export function useSettingsDraftGuard({ dirty, pending = false }: DraftState) {
  const guard = useContext(DraftGuardContext);
  const id = useId();
  const register = guard?.register;
  useEffect(() => {
    register?.(id, { dirty, pending });
    return () => register?.(id, undefined);
  }, [register, id, dirty, pending]);
  useEffect(() => {
    if (!dirty && !pending) return;
    const beforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", beforeUnload);
    return () => window.removeEventListener("beforeunload", beforeUnload);
  }, [dirty, pending]);
}
