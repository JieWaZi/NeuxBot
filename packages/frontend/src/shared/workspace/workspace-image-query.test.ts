import { QueryClient } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import {
  WorkspaceImageError,
  workspaceDataImageOptions,
} from "./workspace-image-query";

// 缩小共享字节预算，覆盖相同边界算法而不为验证分配数十 MiB。
vi.mock("@neuxbot/platform-contracts/workspace-byte-limits", () => ({
  workspaceSingleFileBytesLimit: 8,
}));
const clients: QueryClient[] = [];
function decode(src: string) {
  const client = new QueryClient();
  clients.push(client);
  return client.fetchQuery(workspaceDataImageOptions(src));
}
afterEach(() => {
  for (const client of clients.splice(0)) client.clear();
});

it("accepts an exact-limit raster payload and percent-encoded binary bytes", async () => {
  await expect(
    decode("data:image/png;base64,AQIDBAUGBwg="),
  ).resolves.toMatchObject({ size: 8, type: "image/png" });
  await expect(decode("data:image/png,%89PNG")).resolves.toMatchObject({
    size: 4,
    type: "image/png",
  });
});

it.each([
  "data:image/png;base64,AQIDBAUGBwgJ",
  "data:image/png;base64,AQIDBAUGBwgJCgsMDQ4P",
  "data:image/png,%01%02%03%04%05%06%07%08%09",
])("enforces encoded and decoded byte budgets for %s", async (source) => {
  await expect(decode(source)).rejects.toMatchObject(
    new WorkspaceImageError("tooLarge"),
  );
});

it.each([
  "data:image/svg+xml;base64,PHN2Zz4=",
  "data:text/html;base64,PHN2Zz4=",
])("does not open unsupported media %s", async (source) => {
  await expect(decode(source)).rejects.toMatchObject(
    new WorkspaceImageError("unsupported"),
  );
});

it.each([
  "data:image/png;base64,%%%",
  "data:image/png,%xx",
  "data:image/png,中文",
])("rejects malformed binary data %s", async (source) => {
  await expect(decode(source)).rejects.toMatchObject(
    new WorkspaceImageError("failed"),
  );
});
