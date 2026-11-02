export type MarkdownWorkload = {
  name: string;
  bytes: number;
  codeLines: number;
  code: string;
  text: string;
  chunks: string[];
  sha256: string;
  batches: number;
  inputIntervalMs: number;
};
export function markdownWorkload(name: string): MarkdownWorkload;
