import { describe, expect, it } from "vitest";
import { parseReference } from "@/shared/workspace/file-reference";
import { remarkWorkspaceLinks } from "./chat-link";

describe("Markdown 文件引用传递", () => {
  it("链接和引用定义传递不透明引用，代码及原始路径百分号不被二次改写", () => {
    const tree = {
      type: "root",
      children: [
        { type: "link", url: "./docs/a%20b.txt" },
        { type: "definition", url: "file:///tmp/a%2520.txt" },
        { type: "code", value: "file:///tmp/a%20.txt" },
        { type: "link", url: "https://example.com" },
      ],
    };
    remarkWorkspaceLinks()(tree);
    expect(parseReference(tree.children[0]?.url ?? "")).toMatchObject({
      kind: "file",
      path: "./docs/a b.txt",
    });
    expect(parseReference(tree.children[1]?.url ?? "")).toMatchObject({
      kind: "file",
      path: "/tmp/a%20.txt",
    });
    expect(tree.children[2]?.value).toBe("file:///tmp/a%20.txt");
    expect(tree.children[3]?.url).toBe("https://example.com");
  });
});
