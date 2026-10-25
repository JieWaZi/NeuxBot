import type { Manifest, WorkflowInstance } from "./types";

export type InstanceBranch = {
  key: string;
  segment: string;
  kind: "phase" | "group" | "item" | "iteration";
  ordinal?: number;
  children: InstanceBranch[];
  instances: WorkflowInstance[];
};

export function instanceTree(
  instances: WorkflowInstance[],
  manifest?: Manifest,
): InstanceBranch[] {
  const phases = new Map<string, InstanceBranch>();
  for (const instance of instances) {
    const phaseId = instance.phaseId ?? "";
    let phase = phases.get(phaseId);
    if (!phase) {
      phase = {
        key: phaseId,
        segment: phaseId,
        kind: "phase",
        children: [],
        instances: [],
      };
      phases.set(phaseId, phase);
    }
    let parent = phase;
    for (const segment of (instance.path ?? "root").split("/").slice(1)) {
      if (!segment) continue;
      const match = /^(.*):(\d+)$/.exec(segment);
      const kind = match
        ? match[1] === "item"
          ? "item"
          : "iteration"
        : "group";
      const key = `${parent.key}/${segment}`;
      let branch = parent.children.find((item) => item.key === key);
      if (!branch) {
        branch = {
          key,
          segment: match?.[1] ?? segment,
          kind,
          ordinal: match?.[2] ? Number(match[2]) : undefined,
          children: [],
          instances: [],
        };
        parent.children.push(branch);
      }
      parent = branch;
    }
    parent.instances.push(instance);
  }
  const phaseOrder = new Map(
    manifest?.meta?.phases?.map((phase, index) => [phase.id, index]),
  );
  return Array.from(phases.values()).sort(
    (a, b) =>
      (phaseOrder.get(a.segment) ?? Number.MAX_SAFE_INTEGER) -
      (phaseOrder.get(b.segment) ?? Number.MAX_SAFE_INTEGER),
  );
}
