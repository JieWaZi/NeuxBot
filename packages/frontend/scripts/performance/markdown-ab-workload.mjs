import { createHash } from "node:crypto";

/** Identical ASCII bytes, chunk boundaries and code for both production components. */
export function markdownWorkload(name) {
  if (name === "plain-2kib") {
    const bytes = 2048;
    const prefix =
      "Here is an ordinary conversational answer with a few useful steps.\n\n" +
      "- Read the current requirement carefully.\n- Keep the existing behavior predictable.\n- Verify the result before continuing.\n\n";
    const ending = "\n\nFINAL_COMPONENT_AB_MARKER\n";
    const prose =
      "This is plain conversation text. It explains one idea clearly, then continues with another short paragraph.\n\n";
    const remaining = bytes - prefix.length - ending.length;
    const text =
      prefix +
      prose.repeat(Math.ceil(remaining / prose.length)).slice(0, remaining) +
      ending;
    const batches = 100;
    return {
      name,
      bytes,
      codeLines: 0,
      code: "",
      text,
      chunks: Array.from({ length: batches }, (_, index) =>
        text.slice(0, Math.floor(((index + 1) * text.length) / batches)),
      ),
      sha256: createHash("sha256").update(text).digest("hex"),
      batches,
      inputIntervalMs: 48,
    };
  }
  const large = name === "long-100kib";
  if (!large && name !== "short-2kib")
    throw new Error(`Unknown workload: ${name}`);
  const bytes = (large ? 100 : 2) * 1024;
  const codeLines = large ? 2000 : 24;
  const code = Array.from({ length: codeLines }, (_, index) => {
    const id = String(index).padStart(4, "0");
    return `const row${id} = "value${id}"; // benchmark ${id}`;
  }).join("\n");
  const prefix =
    "# Markdown component comparison\n\n" +
    "A **streamed** answer with `inlineValue`, a [reference](https://example.com/benchmark), and a real table.\n\n" +
    "| Metric | Value |\n| --- | ---: |\n| component | production |\n| workload | identical |\n\n" +
    `\`\`\`typescript\n${code}\n\`\`\`\n\n`;
  const ending = "\n\nFINAL_COMPONENT_AB_MARKER\n";
  const remaining = bytes - prefix.length - ending.length;
  if (remaining < 0) throw new Error("Code exceeds workload size");
  const prose =
    "A stable paragraph measures the production Markdown component under the same stream.\n\n";
  const text =
    prefix +
    prose.repeat(Math.ceil(remaining / prose.length)).slice(0, remaining) +
    ending;
  const batches = 100;
  const chunks = Array.from({ length: batches }, (_, index) =>
    text.slice(0, Math.floor(((index + 1) * text.length) / batches)),
  );
  if (Buffer.byteLength(text) !== bytes)
    throw new Error("Wrong workload byte size");
  return {
    name,
    bytes,
    codeLines,
    code,
    text,
    chunks,
    sha256: createHash("sha256").update(text).digest("hex"),
    batches,
    inputIntervalMs: 48,
  };
}
