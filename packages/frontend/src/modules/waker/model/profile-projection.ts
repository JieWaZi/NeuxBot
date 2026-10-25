import type { Expertise, WorkMethod } from "./types";

export type EditableProfileDocumentKind = "identity" | "persona" | "playbook";

export type ProfileProjection = {
  expertise: Expertise[];
  workStyle: string[];
  workMethods: WorkMethod[];
};

export function parseProfileProjection(
  documents: Partial<Record<EditableProfileDocumentKind, string>>,
): ProfileProjection {
  return {
    expertise: parseExpertise(documents.identity ?? ""),
    workStyle: parseListSection(documents.persona ?? "", "## 工作风格"),
    workMethods: parseWorkMethods(documents.playbook ?? ""),
  };
}

function parseExpertise(content: string): Expertise[] {
  const items: Expertise[] = [];
  let current: Expertise | undefined;
  for (const line of sectionLines(content, "## 擅长领域")) {
    if (line.startsWith("### ")) {
      const title = line.slice(4).trim();
      current = title ? { title, description: "" } : undefined;
      if (current) items.push(current);
      continue;
    }
    if (!current) continue;
    const description = line.trim();
    if (!description) continue;
    current.description = current.description
      ? `${current.description}\n${description}`
      : description;
  }
  return items;
}

function parseListSection(content: string, heading: string): string[] {
  const lines = sectionLines(content, heading);
  return lines.flatMap((line) => {
    const value = line.trim();
    const unordered = /^-\s+(.+)$/.exec(value);
    if (unordered?.[1]) return [unordered[1].trim()];
    const ordered = /^\d+\.\s+(.+)$/.exec(value);
    return ordered?.[1] ? [ordered[1].trim()] : [];
  });
}

function parseWorkMethods(content: string): WorkMethod[] {
  const methods: WorkMethod[] = [];
  let current: WorkMethod | undefined;
  for (const line of sectionLines(content, "## 工作方式")) {
    if (line.startsWith("### ")) {
      const name = line.slice(4).trim();
      current = name ? { name, steps: [] } : undefined;
      if (current) methods.push(current);
      continue;
    }
    if (!current) continue;
    const value = line.trim();
    const item = /^-\s+(.+)$/.exec(value) ?? /^\d+\.\s+(.+)$/.exec(value);
    if (item?.[1]) current.steps = [...(current.steps ?? []), item[1].trim()];
  }
  return methods;
}

function sectionLines(content: string, heading: string): string[] {
  const lines = content.replaceAll("\r\n", "\n").split("\n");
  const start = lines.indexOf(heading);
  if (start < 0) return [];
  const nextHeading = lines.findIndex(
    (line, index) =>
      index > start && line.startsWith("## ") && !line.startsWith("### "),
  );
  return lines.slice(start + 1, nextHeading < 0 ? undefined : nextHeading);
}
