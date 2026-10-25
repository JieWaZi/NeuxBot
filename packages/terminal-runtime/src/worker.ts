import { createInterface } from "node:readline";
import { z } from "zod";
import { TerminalManager } from "./manager";

const session = { sessionId: z.string().min(1) };
const attachment = { ...session, attachmentId: z.string().min(1) };
const request = z.discriminatedUnion("method", [
  z.object({
    id: z.string(),
    method: z.literal("start"),
    args: z.object({
      root: z.string().min(1),
      ownerKey: z.string().min(1),
      createWhenMissing: z.boolean(),
    }),
  }),
  z.object({
    id: z.string(),
    method: z.literal("attach"),
    args: z.object(session),
  }),
  z.object({
    id: z.string(),
    method: z.literal("detach"),
    args: z.object(attachment),
  }),
  z.object({
    id: z.string(),
    method: z.literal("write"),
    args: z.object({ ...attachment, data: z.string().max(65536) }),
  }),
  z.object({
    id: z.string(),
    method: z.literal("resize"),
    args: z.object({
      ...attachment,
      size: z.object({
        cols: z.number().int().min(1).max(500),
        rows: z.number().int().min(1).max(300),
      }),
    }),
  }),
  z.object({
    id: z.string(),
    method: z.literal("close"),
    args: z.object({ ownerKey: z.string().min(1) }),
  }),
  z.object({
    id: z.string(),
    method: z.literal("dispose"),
    args: z.object({}),
  }),
]);
/** stdout 是唯一协议流；写入回调施加背压，不混入诊断正文。 */
function send(value: unknown): Promise<void> {
  return new Promise((resolve, reject) => {
    process.stdout.write(`${JSON.stringify(value)}\n`, (error) =>
      error ? reject(error) : resolve(),
    );
  });
}
const manager = new TerminalManager((event, payload) =>
  send({ event, payload }),
);
const input = createInterface({ input: process.stdin, crlfDelay: Infinity });
let stopping = false;
let pending = 0;
function stop() {
  if (stopping) return;
  stopping = true;
  input.close();
  manager.dispose();
  process.exitCode = 0;
}
async function handle(line: string): Promise<void> {
  let id = "";
  try {
    pending += 1;
    if (line.length > 1024 * 1024 || pending > 128) {
      stop();
      return;
    }
    const raw: unknown = JSON.parse(line);
    if (
      typeof raw === "object" &&
      raw !== null &&
      "id" in raw &&
      typeof raw.id === "string"
    )
      id = raw.id;
    const command = request.parse(raw);
    let result: unknown;
    switch (command.method) {
      case "start":
        result = await manager.start(
          command.args.root,
          command.args.ownerKey,
          command.args.createWhenMissing,
        );
        break;
      case "attach":
        result = await manager.attach(command.args.sessionId);
        break;
      case "detach":
        manager.detach(command.args.sessionId, command.args.attachmentId);
        result = true;
        break;
      case "write":
        manager.write(
          command.args.sessionId,
          command.args.attachmentId,
          command.args.data,
        );
        result = true;
        break;
      case "resize":
        manager.resize(
          command.args.sessionId,
          command.args.attachmentId,
          command.args.size,
        );
        result = true;
        break;
      case "close":
        result = await manager.close(command.args.ownerKey);
        break;
      case "dispose":
        stop();
        result = true;
        break;
    }
    await send({ id, result });
  } catch {
    await send({ id, error: "terminal operation failed" }).catch(stop);
  } finally {
    pending -= 1;
  }
}
input.on("line", (line) => {
  if (!stopping) void handle(line);
});
input.on("close", stop);
process.on("SIGTERM", stop);
process.on("SIGINT", stop);
process.stdout.on("error", stop);
void send({ event: "ready", payload: { protocolVersion: 1 } });
