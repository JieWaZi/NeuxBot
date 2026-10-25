import { expect, it } from "vitest";
import { instanceTree } from "./instance-tree";

it("keeps actual item, iteration and repeated instances under the frozen phase order", () => {
  const tree = instanceTree(
    [
      { id: "verify", phaseId: "verify", path: "root" },
      { id: "first", phaseId: "build", path: "root/collect/item:0/rework:1" },
      { id: "second", phaseId: "build", path: "root/collect/item:0/rework:1" },
      { id: "next", phaseId: "build", path: "root/collect/item:0/rework:2" },
      { id: "other", phaseId: "build", path: "root/collect/item:1" },
    ],
    { meta: { phases: [{ id: "build" }, { id: "verify" }] } },
  );
  expect(tree.map((phase) => phase.segment)).toEqual(["build", "verify"]);
  const group = tree[0]?.children[0];
  expect(group?.segment).toBe("collect");
  expect(group?.children.map((item) => item.ordinal)).toEqual([0, 1]);
  expect(group?.children[0]?.children.map((loop) => loop.ordinal)).toEqual([
    1, 2,
  ]);
  expect(
    group?.children[0]?.children[0]?.instances.map((item) => item.id),
  ).toEqual(["first", "second"]);
  expect(tree[1]?.instances.map((item) => item.id)).toEqual(["verify"]);
});
