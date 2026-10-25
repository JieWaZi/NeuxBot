import type { TerminalService } from "@neuxbot/platform-contracts/services";
import type {
  CoreConnection,
  TerminalOutputEvent,
  TerminalExitEvent,
  TerminalSnapshot,
} from "@neuxbot/platform-contracts/resources";

/** 只有用户创建/恢复终端时加载流客户端，首屏不等待终端模块。 */
export function createCoreTerminalService(
  connection: () => Promise<CoreConnection>,
): TerminalService {
  const outputs = new Set<(event: TerminalOutputEvent) => void>();
  const exits = new Set<(event: TerminalExitEvent) => void>();
  const snapshots = new Set<(event: TerminalSnapshot) => void>();
  let pending: Promise<TerminalService> | undefined;
  function load() {
    return (pending ??= import("./core-terminal")
      .then((module) => {
        const service = module.createCoreTerminalService(connection);
        service.onTerminalOutput((event) => {
          for (const listener of outputs) listener(event);
        });
        service.onTerminalExit((event) => {
          for (const listener of exits) listener(event);
        });
        service.onTerminalSnapshot?.((event) => {
          for (const listener of snapshots) listener(event);
        });
        return service;
      })
      .catch((error) => {
        pending = undefined;
        throw error;
      }));
  }
  return {
    terminal_start: (...args) =>
      load().then((service) => service.terminal_start(...args)),
    terminal_attach: (...args) =>
      load().then((service) => service.terminal_attach(...args)),
    terminal_detach: (...args) =>
      load().then((service) => service.terminal_detach(...args)),
    terminal_write: (...args) =>
      load().then((service) => service.terminal_write(...args)),
    terminal_resize: (...args) =>
      load().then((service) => service.terminal_resize(...args)),
    terminal_close: (...args) =>
      load().then((service) => service.terminal_close(...args)),
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
