import { createCoreWorkspaceServices } from "@neuxbot/frontend/core-workspace";
import { createCoreTerminalService } from "@neuxbot/frontend/core-terminal";
import { createRoot } from "react-dom/client";
import { configurePlatform } from "@neuxbot/frontend/platform";
import { startApplication } from "@neuxbot/frontend/start";
import { initializeTheme, ThemeProvider } from "@/shared/theme";
import "@neuxbot/frontend/shared/styles/index.css";
import { webHost } from "./platform/host";
import {
  requestSession,
  SessionError,
  type WebSession,
} from "./platform/session";
import { SessionActions } from "./shell/session-actions";
import { PairingScreen } from "./shell/pairing-screen";

initializeTheme();
window.addEventListener(
  "ally:web-session-expired",
  () => window.location.reload(),
  { once: true },
);
function connect(session: WebSession) {
  const getConnection = async () => ({
    baseUrl: window.location.origin,
    credentials: "same-origin" as const,
    csrfToken: session.csrfToken,
  });
  const services = createCoreWorkspaceServices(webHost);
  configurePlatform({
    kind: "web",
    host: webHost,
    workspace: session.capabilities.workspace ? services.workspace : undefined,
    git: session.capabilities.git ? services.git : undefined,
    terminal: session.capabilities.terminal
      ? createCoreTerminalService(getConnection)
      : undefined,
    SessionActions: () => <SessionActions session={session} />,
    getConnection,
  });
  // 到期只重载当前页面，Core 中已接纳的工作继续运行。
  window.setTimeout(
    () => window.location.reload(),
    Math.max(0, session.expiresAt - Date.now()),
  );
  startApplication();
}
try {
  connect(await requestSession());
} catch (error) {
  const container = document.getElementById("root");
  if (!container) throw new Error("缺少应用挂载节点");
  const root = createRoot(container);
  root.render(
    <ThemeProvider>
      <PairingScreen
        initialOffline={
          !(error instanceof SessionError && error.status === 401)
        }
        onConnected={(session) => {
          root.unmount();
          connect(session);
        }}
      />
    </ThemeProvider>,
  );
}
