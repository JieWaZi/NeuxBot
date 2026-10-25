import {
  parseReference,
  referenceHref,
} from "@/shared/workspace/file-reference";

type MarkdownNode = {
  type: string;
  url?: string;
  children?: MarkdownNode[];
};

/** 在安全清洗前解析文件链接，只改写 Markdown 链接节点，保留代码原文。 */
export function remarkWorkspaceLinks() {
  return function transform(tree: MarkdownNode) {
    const visit = (node: MarkdownNode) => {
      if ((node.type === "link" || node.type === "definition") && node.url) {
        const target = parseReference(node.url, "link");
        if (target?.kind === "file") node.url = referenceHref(target);
      }
      node.children?.forEach(visit);
    };
    visit(tree);
  };
}
