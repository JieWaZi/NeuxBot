import { afterEach, expect, it, vi } from "vitest";
import { createCoreUploads } from "./core-uploads";

afterEach(() => vi.unstubAllGlobals());

it("stages original desktop bytes in Core and discards only the returned upload reference", async () => {
  const received: Request[] = [];
  vi.stubGlobal("fetch", async (url: URL, options: RequestInit) => {
    const request = new Request(url, options);
    received.push(request);
    return request.method === "POST"
      ? Response.json({ sourcePath: "upload:fixture/image.png" })
      : new Response(null, { status: 204 });
  });
  const client = createCoreUploads(async () => ({
    baseUrl: "http://127.0.0.1:8432",
    token: "test-token",
  }));
  const ref = await client.stage_attachment({
    name: "image.png",
    bytes: new Uint8Array([0, 255]),
  });
  await client.discard_attachment({ sourcePath: "/workspace/image.png" });
  await client.discard_attachment({ sourcePath: ref });

  expect(received).toHaveLength(2);
  const [stage, discard] = received;
  if (!stage || !discard) throw new Error("Expected stage and discard requests");
  expect(stage.method).toBe("POST");
  expect(stage.headers.get("Authorization")).toBe("Bearer test-token");
  expect(stage.headers.get("Content-Type")).toBe("application/octet-stream");
  expect(new Uint8Array(await stage.arrayBuffer())).toEqual(
    new Uint8Array([0, 255]),
  );
  expect(discard.method).toBe("DELETE");
  expect(new URL(discard.url).searchParams.get("sourcePath")).toBe(
    "upload:fixture/image.png",
  );
});
