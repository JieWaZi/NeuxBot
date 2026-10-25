type MarkdownNode = {
  type: string;
  value?: string;
  url?: string;
  children?: MarkdownNode[];
};

/** 阅读器已有独立标题，沿固定上游规则只省略正文开头与其同名的一级标题。 */
export function stripDuplicateWikiTitle(
  content: string,
  title: string,
): string {
  if (!content || !title) return content;
  const lines = content.split("\n");
  let first = 0;
  while (first < lines.length && lines[first]?.trim() === "") first++;
  const heading = lines[first]?.match(/^#\s+(.+?)\s*$/)?.[1]?.trim();
  if (!heading || heading.toLowerCase() !== title.trim().toLowerCase())
    return content;
  first++;
  while (first < lines.length && lines[first]?.trim() === "") first++;
  return lines.slice(first).join("\n");
}

/** 只将正文文本中的 Wiki 引用转为链接，代码和已有链接保留原字节。 */
export function remarkWikiLinks() {
  return function transform(tree: MarkdownNode) {
    function visit(node: MarkdownNode) {
      if (
        ["code", "inlineCode", "html", "link", "definition"].includes(
          node.type,
        ) ||
        !node.children
      )
        return;
      const children: MarkdownNode[] = [];
      for (const child of node.children) {
        if (child.type !== "text" || !child.value) {
          visit(child);
          children.push(child);
          continue;
        }
        let offset = 0;
        for (const match of child.value.matchAll(
          /\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/g,
        )) {
          const slug = match[1]?.trim();
          if (!slug) continue;
          if (match.index > offset)
            children.push({
              type: "text",
              value: child.value.slice(offset, match.index),
            });
          children.push({
            type: "link",
            url: `./${slug.split("/").map(encodeURIComponent).join("/")}.md`,
            children: [{ type: "text", value: match[2] || slug }],
          });
          offset = match.index + match[0].length;
        }
        if (offset < child.value.length)
          children.push({ type: "text", value: child.value.slice(offset) });
      }
      node.children = children;
    }
    visit(tree);
  };
}

/** 索引专用换行适配，保持上游marked breaks:true；不影响普通Wiki正文和代码。 */
export function remarkWikiIndexBreaks() {
  return function transform(tree: MarkdownNode) {
    function visit(node: MarkdownNode) {
      if (["code", "inlineCode", "html"].includes(node.type) || !node.children)
        return;
      node.children = node.children.flatMap((child) => {
        if (child.type !== "text" || !child.value?.includes("\n")) {
          visit(child);
          return [child];
        }
        const lines = child.value.split("\n");
        return lines.flatMap((line, index): MarkdownNode[] => [
          ...(index ? [{ type: "break" }] : []),
          { type: "text", value: line },
        ]);
      });
    }
    visit(tree);
  };
}

/** 索引名称属于导航契约；只替换开头一级标题，保留概述、代码和目录正文。 */
export function normalizeWikiIndexIntro(content: string): string {
  const lines = content.replace(/[\r\n]+$/, "").split("\n");
  while (lines.length && !lines[0]?.trim()) lines.shift();
  if (!lines.length) return "# Wiki Index";
  if (/^ {0,3}#(?:[ \t]+|$)/.test(lines[0] ?? "")) {
    lines[0] = "# Wiki Index";
    return lines.join("\n");
  }
  if (
    lines.length > 1 &&
    !/^(?: {4}|\t)/.test(lines[0] ?? "") &&
    /^ {0,3}=+[ \t]*$/.test(lines[1] ?? "")
  )
    return ["# Wiki Index", ...lines.slice(2)].join("\n");
  return `# Wiki Index\n\n${lines.join("\n")}`;
}
