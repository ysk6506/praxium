import { describe, expect, it } from "vitest";
import {
  impactOfSize,
  sampleLayerOf,
  smallImpact,
  syntheticImpact,
} from "../test-support/impact-world";
import { buildOverview, needsOverview, OVERVIEW_THRESHOLD } from "./impact-group";
import { impactSource } from "./impact-input";
import { buildImpactTree } from "./impact-tree";

const treeOf = (data = impactOfSize(2000)) =>
  buildImpactTree(impactSource(data, "with_assumptions"), sampleLayerOf);

describe("[REQ-351] a big answer opens as groups of the same type under the same parent", () => {
  it("starts as an overview above 300 nodes and not at 300", () => {
    expect(OVERVIEW_THRESHOLD).toBe(300);
    expect(needsOverview(treeOf(smallImpact()))).toBe(false);
    const edge = treeOf(syntheticImpact({ items: 300, seed: 2, seeds: 1 }));
    expect(edge.nodes.size).toBe(301);
    expect(needsOverview(edge)).toBe(true);
    expect(needsOverview(treeOf(syntheticImpact({ items: 299, seed: 2, seeds: 1 })))).toBe(false);
  });

  it("puts every node in one group, so the counts add up", () => {
    for (const items of [300, 2000, 5000]) {
      const tree = treeOf(impactOfSize(items as 300 | 2000 | 5000));
      const overview = buildOverview(tree);
      let total = 0;
      const seen = new Set<string>();
      for (const group of overview.groups.values()) {
        total += group.count;
        expect(group.members).toHaveLength(group.count);
        for (const id of group.members) {
          expect(seen.has(id)).toBe(false);
          seen.add(id);
          expect(tree.nodes.get(id)?.type).toBe(group.type);
        }
        expect(Object.values(group.grades).reduce((a, b) => a + b, 0)).toBeLessThanOrEqual(
          group.count,
        );
      }
      expect(total).toBe(tree.nodes.size);
      expect(overview.order).toHaveLength(overview.groups.size);
      expect(overview.groups.size).toBeLessThan(200);
    }
  });

  it("makes a group from the nodes of one type below the members of one parent group", () => {
    const tree = treeOf(impactOfSize(2000));
    const overview = buildOverview(tree);
    for (const group of overview.groups.values()) {
      for (const id of group.members) {
        const node = tree.nodes.get(id);
        const parentGroup = [...overview.groups.values()].find(
          (g) => node?.parent && g.members.includes(node.parent),
        );
        expect(parentGroup?.key ?? null).toBe(group.parent);
      }
    }
  });

  it("keeps the members with the most severe result first, and says which result is the worst", () => {
    const overview = buildOverview(treeOf(impactOfSize(300)));
    for (const group of overview.groups.values()) {
      if (group.worst !== undefined) expect(group.grades[group.worst]).toBeGreaterThan(0);
    }
    const first = [...overview.groups.values()].find(
      (g) => (g.grades.down ?? 0) > 0 && Object.keys(g.grades).length > 2,
    );
    expect(first?.worst).toBe("down");
  });

  it("gives the same groups whatever the order of the items", () => {
    const a = buildOverview(treeOf(syntheticImpact({ items: 400, seed: 5, seeds: 2 })));
    const b = buildOverview(
      treeOf(syntheticImpact({ items: 400, seed: 5, seeds: 2, shuffle: true })),
    );
    expect(b.order).toEqual(a.order);
    expect([...b.groups.values()].map((g) => [g.key, g.count, g.grades, g.members])).toEqual(
      [...a.groups.values()].map((g) => [g.key, g.count, g.grades, g.members]),
    );
  });

  it("orders the groups depth first with the children sorted by type, and gives each its first step", () => {
    const tree = treeOf(impactOfSize(2000));
    const overview = buildOverview(tree);
    const index = new Map(overview.order.map((key, i) => [key, i]));
    for (const group of overview.groups.values()) {
      if (group.parent !== null)
        expect(index.get(group.parent) ?? -1).toBeLessThan(index.get(group.key) ?? 0);
      const types = group.children.map((k) => overview.groups.get(k)?.type ?? "");
      expect(types).toEqual([...types].sort());
      const steps = group.members.map((id) => tree.nodes.get(id)?.step ?? 0);
      expect(group.step).toBe(Math.min(...steps));
    }
    expect(overview.roots.every((k) => overview.groups.get(k)?.parent === null)).toBe(true);
  });
});
