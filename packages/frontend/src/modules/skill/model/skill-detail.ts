import type { SkillResource } from "./types";

export type SkillResourceEntry = {
  path?: string;
  directory?: boolean;
  size?: number;
  digest?: string;
  executable?: boolean;
};

export function buildSkillResourceTree(
  entries: SkillResourceEntry[],
): SkillResource[] {
  const roots: SkillResource[] = [];
  const directories = new Map<string, SkillResource>();
  for (const entry of entries) {
    const path = entry.path?.trim();
    if (!path) continue;
    const parts = path.split("/").filter(Boolean);
    let children = roots;
    let currentPath = "";
    for (let index = 0; index < parts.length; index += 1) {
      const name = parts[index];
      if (!name) continue;
      currentPath = currentPath ? `${currentPath}/${name}` : name;
      let resource = directories.get(currentPath);
      if (!resource) {
        const directory = index < parts.length - 1 || Boolean(entry.directory);
        resource = {
          name,
          path: currentPath,
          type: directory ? "directory" : "file",
          ...(index === parts.length - 1
            ? {
                size: entry.size,
                digest: entry.digest,
                ...(entry.executable ? { executable: true } : {}),
              }
            : {}),
        };
        children.push(resource);
        if (directory) {
          resource.children = [];
          directories.set(currentPath, resource);
        }
      }
      children = resource.children ?? children;
    }
  }
  return roots;
}

export function findSkillResourceByPath(
  resources: SkillResource[],
  path: string,
): SkillResource | undefined {
  for (const resource of resources) {
    if (resource.path === path) return resource;
    const child = findSkillResourceByPath(resource.children ?? [], path);
    if (child) return child;
  }
  return undefined;
}

export function countSkillResourceFiles(resources: SkillResource[]): number {
  return resources.reduce(
    (count, resource) =>
      count +
      (resource.type === "file"
        ? 1
        : countSkillResourceFiles(resource.children ?? [])),
    0,
  );
}

export function suggestNextPatchVersion(version: string): string {
  const parsed = parseVersion(version);
  if (!parsed) return "1.0.0";
  return `${parsed[0]}.${parsed[1]}.${parsed[2] + 1n}`;
}

export function isVersionGreater(candidate: string, current: string): boolean {
  const next = parseVersion(candidate);
  const base = parseVersion(current);
  if (!next || !base) return false;
  return (
    next[0] > base[0] ||
    (next[0] === base[0] && next[1] > base[1]) ||
    (next[0] === base[0] && next[1] === base[1] && next[2] > base[2])
  );
}

function parseVersion(value: string): [bigint, bigint, bigint] | null {
  const match = /^(\d+)\.(\d+)\.(\d+)$/.exec(value.trim());
  if (!match) return null;
  const major = match[1];
  const minor = match[2];
  const patch = match[3];
  if (major === undefined || minor === undefined || patch === undefined) {
    return null;
  }
  return [BigInt(major), BigInt(minor), BigInt(patch)];
}
