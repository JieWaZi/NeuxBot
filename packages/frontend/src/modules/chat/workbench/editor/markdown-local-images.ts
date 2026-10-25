/**
 * Local image
 * references become inert preview markers; the React image component resolves
 * each marker through NeuxBot's Chat-scoped file bridge before creating a Blob URL.
 */
const markerBase = "https://ally.invalid/__workbench_markdown_image__";

function localImageMarker(destination: string): string {
  const value = destination.trim();
  if (!value || value.startsWith("#")) return destination;
  if (/^(?:https?:|data:)/i.test(value) || value.startsWith("//"))
    return destination;
  if (/^[a-z][a-z\d+.-]*:/i.test(value) && !/^[a-z]:[\\/]/i.test(value))
    return destination;
  const url = new URL(markerBase);
  url.searchParams.set("path", value);
  return url.href;
}

export function localImagePath(marker: string): string | null {
  try {
    const url = new URL(marker);
    if (`${url.origin}${url.pathname}` !== markerBase) return null;
    return url.searchParams.get("path");
  } catch {
    return null;
  }
}

/** Rewrite inline and referenced images, preserving code examples and links. */
export function rewriteLocalImageUrls(source: string): string {
  const masks: string[] = [];
  const mask = (value: string) => {
    masks.push(value);
    return `\u0000${masks.length - 1}\u0000`;
  };
  const masked = source
    .replace(/```[\s\S]*?```/g, mask)
    .replace(/`[^`\n]*`/g, mask);
  const inline = masked.replace(
    /!\[([^\]]*)\]\((<[^>]+>|[^)\s]+)(?:\s+"[^"]*")?\)/g,
    (_match, alt: string, raw: string) => {
      const angled = raw.startsWith("<") && raw.endsWith(">");
      const destination = angled ? raw.slice(1, -1) : raw;
      return `![${alt}](${localImageMarker(destination)})`;
    },
  );
  const imageLabels = new Set<string>();
  const labels = /!\[([^\]]*)\](?:\[([^\]]*)\])?/g;
  let match: RegExpExecArray | null;
  while ((match = labels.exec(inline))) {
    if (inline[labels.lastIndex] === "(") continue;
    const label = match[2] || match[1];
    if (label) imageLabels.add(label.trim().toLowerCase());
  }
  const referenced = inline.replace(
    /^(\s{0,3}\[([^\]]+)\]:\s*)(<[^>]+>|[^\s]+)/gm,
    (whole, head: string, label: string, raw: string) => {
      if (!imageLabels.has(label.trim().toLowerCase())) return whole;
      const destination =
        raw.startsWith("<") && raw.endsWith(">") ? raw.slice(1, -1) : raw;
      return `${head}${localImageMarker(destination)}`;
    },
  );
  return referenced.replace(
    // eslint-disable-next-line no-control-regex -- NUL is a temporary source mask.
    /\u0000(\d+)\u0000/g,
    (_whole, index: string) => masks[Number(index)] ?? "",
  );
}
