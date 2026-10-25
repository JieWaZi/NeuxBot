import { z } from "zod";
import type {
  CoreConnection,
  TerminalSnapshot,
  TerminalOutputEvent,
  TerminalExitEvent,
} from "@neuxbot/platform-contracts/resources";
import type { TerminalService } from "@neuxbot/platform-contracts/services";
import { startTerminal, closeTerminal } from "@/shared/api/generated/sdk.gen";

const snapshotSchema = z.object({
  sessionId: z.string(),
  attachmentId: z.string(),
  shell: z.string(),
  screen: z.string(),
  sequence: z.number().int().nonnegative(),
  state: z.enum(["running", "exited"]),
  exitCode: z.number().optional(),
});
const outputSchema = z.object({
  sessionId: z.string(),
  sequence: z.number().int().positive(),
  data: z.string(),
});
const exitSchema = z.object({
  sessionId: z.string(),
  code: z.number().optional(),
});
type Attachment = {
  socket?: WebSocket;
  id: string;
  sequence: number;
  timer?: ReturnType<typeof setTimeout>;
  stopped: boolean;
  failures: number;
  exited: boolean;
};
/** Core 拥有 PTY；此适配器只管理视图连接、序号和屏幕恢复。 */
export function createCoreTerminalService(
  connection: () => Promise<CoreConnection>,
): TerminalService {
  const attachments = new Map<string, Attachment>();
  const outputs = new Set<(value: TerminalOutputEvent) => void>();
  const exits = new Set<(value: TerminalExitEvent) => void>();
  const snapshots = new Set<(value: TerminalSnapshot) => void>();
  function stop(sessionId: string) {
    const current = attachments.get(sessionId);
    if (!current) return;
    current.stopped = true;
    clearTimeout(current.timer);
    current.socket?.close();
    attachments.delete(sessionId);
  }
  function connect(
    sessionId: string,
    current: Attachment,
    recovery: boolean,
  ): Promise<TerminalSnapshot> {
    return new Promise((resolve, reject) => {
      let settled = false;
      let activeSocket: WebSocket | undefined;
      let deadline: ReturnType<typeof setTimeout> | undefined;
      const fail = () => {
        clearTimeout(deadline);
        if (
          current.stopped ||
          (activeSocket && current.socket !== activeSocket)
        )
          return;
        current.socket?.close();
        if (!settled) {
          settled = true;
          reject(new Error("终端连接已中断"));
        }
        if (current.exited || current.timer) return;
        if (++current.failures > 5) {
          stop(sessionId);
          for (const listener of exits) listener({ sessionId });
          return;
        }
        current.timer = setTimeout(
          () => {
            current.timer = undefined;
            void connect(sessionId, current, true).catch(() => undefined);
          },
          Math.min(1000 * 2 ** (current.failures - 1), 8000),
        );
      };
      void connection()
        .then((config) => {
          if (current.stopped) {
            reject(new Error("终端视图已释放"));
            return;
          }
          const url = new URL(
            `/api/v1/terminals/${encodeURIComponent(sessionId)}/stream`,
            config.baseUrl,
          );
          if (
            url.protocol !== "http:" ||
            !["127.0.0.1", "localhost", "[::1]"].includes(url.hostname)
          )
            throw new Error("终端服务地址无效");
          url.protocol = "ws:";
          const socket = new WebSocket(url, [
            "ally.terminal.v1",
            ...(config.token ? [`ally.auth.${config.token}`] : []),
          ]);
          activeSocket = socket;
          current.socket = socket;
          deadline = setTimeout(fail, 10000);
          socket.onclose = fail;
          socket.onerror = () => socket.close();
          socket.onmessage = (event) => {
            if (
              current.stopped ||
              current.socket !== socket ||
              typeof event.data !== "string"
            )
              return;
            try {
              if (event.data.length > 4 * 1024 * 1024)
                throw new Error("终端快照过大");
              const frame = JSON.parse(event.data) as {
                event: string;
                payload: unknown;
              };
              if (frame.event === "snapshot") {
                const snapshot = snapshotSchema.parse(frame.payload);
                if (snapshot.sessionId !== sessionId || settled)
                  throw new Error("终端身份无效");
                current.id = snapshot.attachmentId;
                current.sequence = snapshot.sequence;
                current.failures = 0;
                current.exited = snapshot.state === "exited";
                clearTimeout(deadline);
                settled = true;
                resolve(snapshot);
                if (recovery)
                  for (const listener of snapshots) listener(snapshot);
              } else if (frame.event === "terminal-output") {
                const output = outputSchema.parse(frame.payload);
                if (!settled || output.sessionId !== sessionId)
                  throw new Error("终端输出身份无效");
                if (output.sequence <= current.sequence) return;
                if (output.sequence !== current.sequence + 1) {
                  socket.close();
                  return;
                }
                current.sequence = output.sequence;
                for (const listener of outputs) listener(output);
              } else if (frame.event === "terminal-exit") {
                const exit = exitSchema.parse(frame.payload);
                if (exit.sessionId !== sessionId)
                  throw new Error("终端退出身份无效");
                current.exited = true;
                for (const listener of exits) listener(exit);
              }
            } catch {
              socket.close();
            }
          };
        })
        .catch(fail);
    });
  }
  function send(sessionId: string, attachmentId: string, value: unknown) {
    const current = attachments.get(sessionId);
    if (
      !current ||
      current.id !== attachmentId ||
      current.socket?.readyState !== WebSocket.OPEN
    )
      throw new Error("终端正在重新连接，请稍后重试");
    if (current.socket.bufferedAmount > 1024 * 1024)
      throw new Error("终端输入队列已满");
    current.socket.send(JSON.stringify(value));
  }
  return {
    async terminal_start(args) {
      const { data } = await startTerminal({ body: args, throwOnError: true });
      return z
        .object({ sessionId: z.string().min(1), shell: z.string() })
        .parse(data);
    },
    async terminal_close({ ownerKey }) {
      await closeTerminal({ query: { ownerKey }, throwOnError: true });
      return true;
    },
    terminal_attach({ sessionId }) {
      stop(sessionId);
      const current: Attachment = {
        id: "",
        sequence: 0,
        stopped: false,
        failures: 0,
        exited: false,
      };
      attachments.set(sessionId, current);
      return connect(sessionId, current, false);
    },
    async terminal_detach({ sessionId, attachmentId }) {
      if (!attachmentId || attachments.get(sessionId)?.id === attachmentId)
        stop(sessionId);
    },
    async terminal_write({ sessionId, attachmentId, data }) {
      if (new TextEncoder().encode(data).length > 65536)
        throw new Error("终端输入过大");
      send(sessionId, attachmentId, { type: "write", data });
    },
    async terminal_resize({ sessionId, attachmentId, size }) {
      send(sessionId, attachmentId, { type: "resize", ...size });
    },
    onTerminalOutput(handler) {
      outputs.add(handler);
      return () => {
        outputs.delete(handler);
      };
    },
    onTerminalExit(handler) {
      exits.add(handler);
      return () => {
        exits.delete(handler);
      };
    },
    onTerminalSnapshot(handler) {
      snapshots.add(handler);
      return () => {
        snapshots.delete(handler);
      };
    },
  };
}
