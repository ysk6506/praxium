import { describe, expect, it } from "vitest";
import {
  cappedImpact,
  impactOfSize,
  type SampleImpact,
  sampleLayerOf,
  smallImpact,
  syntheticImpact,
} from "../test-support/impact-world";
import { impactSource } from "./impact-input";
import { LANE_WIDTH, layoutEdges, layoutLanes, ROW_HEIGHT } from "./impact-layout";
import { buildImpactTree, itemIds } from "./impact-tree";

const treeOf = (
  data: SampleImpact,
  reading: "facts_only" | "with_assumptions" = "with_assumptions",
) => buildImpactTree(impactSource(data, reading), sampleLayerOf);

const layoutOf = (data: SampleImpact) => {
  const tree = treeOf(data);
  const inputs = tree.order.map((id) => {
    const n = tree.nodes.get(id);
    return { id, layer: n?.layer ?? "", parent: n?.parent ?? null };
  });
  return { tree, inputs, layout: layoutLanes(inputs) };
};

describe("[REQ-350] the answer is a tree of causes", () => {
  it("has the objects that failed as roots and every item under the object just before it", () => {
    const data = syntheticImpact({ items: 80, seed: 2, seeds: 2 });
    const tree = treeOf(data);
    expect(tree.roots).toHaveLength(2);
    expect(tree.roots).toEqual([...tree.roots].sort());
    const items = data.with_assumptions?.items ?? [];
    for (const item of items) {
      const node = tree.nodes.get(item.object_id);
      expect(node?.kind).toBe("item");
      expect(node?.parent).toBe(item.via);
      expect(node?.grade).toBe(item.result);
      const parent = node?.parent ? tree.nodes.get(node.parent) : undefined;
      expect(parent?.children).toContain(item.object_id);
    }
    expect(itemIds(tree)).toHaveLength(items.length);
    expect(tree.order).toHaveLength(tree.nodes.size);
  });

  it("puts an object that is on a path but is not an item in the tree as a waypoint, and a hidden one as hidden", () => {
    const data = smallImpact();
    const tree = treeOf(data);
    const hidden = [...tree.nodes.values()].filter((n) => n.kind === "hidden");
    expect(hidden.length).toBeGreaterThan(0);
    for (const h of hidden) {
      expect(h.name).toBe("");
      expect(h.id.startsWith("hidden:")).toBe(true);
      expect(h.grade).toBeUndefined();
      expect(tree.nodes.get(h.parent ?? "")).toBeDefined();
      expect(h.layer).toBe(tree.nodes.get(h.parent ?? "")?.layer);
    }
    const waypoint = {
      direction: "downstream",
      computation: data.computation,
      seeds: [{ object_id: "s", object_type: "PowerFeed", display_name: "feed" }],
      facts_only: {
        counts_by_result: {},
        counts_by_type: [],
        counts_by_parent: [],
        groups_touched: [],
        unfilled_channel_objects: 0,
        items: [
          {
            object_id: "vm",
            object_type: "VirtualMachine",
            display_name: "vm-1",
            result: "down",
            also: [],
            lost_channels: [],
            via: "srv",
            hops: 2,
            assumption_based: false,
            weakest_grade: "fact",
            path: [
              { object_id: "s", object_type: "PowerFeed", display_name: "feed" },
              {
                object_id: "srv",
                object_type: "PhysicalServer",
                display_name: "srv-1",
                link: { grade: "assumed", link_type: "supplies", delay_class: "delayed" },
                via_group: { object_id: "g", object_type: "RedundancyGroup", display_name: "grp" },
              },
              {
                object_id: "vm",
                object_type: "VirtualMachine",
                display_name: "vm-1",
                link: { grade: "fact", link_type: "hosts" },
              },
            ],
          },
        ],
      },
    } satisfies SampleImpact;
    const t = treeOf(waypoint, "facts_only");
    expect(t.nodes.get("srv")).toMatchObject({
      kind: "waypoint",
      parent: "s",
      hops: 1,
      grade: undefined,
      name: "srv-1",
    });
    expect(t.nodes.get("srv")?.edge).toEqual({
      grade: "assumed",
      viaGroup: { id: "g", name: "grp" },
      delayed: true,
    });
    expect(t.nodes.get("vm")?.edge).toEqual({ grade: "fact", viaGroup: undefined, delayed: false });
    expect(t.order).toEqual(["s", "srv", "vm"]);
  });

  it("is the same tree and the same picture whatever the order of the items", () => {
    for (const items of [40, 300]) {
      for (const seed of [1, 2, 3]) {
        const plain = layoutOf(syntheticImpact({ items, seed, seeds: 2, hiddenEvery: 9 }));
        const mixed = layoutOf(
          syntheticImpact({ items, seed, seeds: 2, hiddenEvery: 9, shuffle: true }),
        );
        expect(mixed.tree.order).toEqual(plain.tree.order);
        expect([...mixed.layout.placed]).toEqual([...plain.layout.placed]);
      }
    }
  });

  it("reads the same picture upstream as downstream", () => {
    const down = syntheticImpact({ items: 120, seed: 4 });
    const items = down.with_assumptions?.items ?? [];
    const upstream = {
      direction: "upstream",
      computation: down.computation,
      target: down.seeds?.[0],
      upstream: {
        total: items.length,
        single_points_of_failure: 0,
        counts_by_type: [],
        counts_by_effect: {},
        items: items.map((it) => ({
          object_id: it.object_id,
          object_type: it.object_type,
          display_name: it.display_name,
          hops: it.hops,
          effects: { facts_only: it.result, with_assumptions: it.result },
          single_point_of_failure: false,
          assumption_based: it.assumption_based,
          // A dependency's path runs to the target; the link of a step is the one that carried the failure to it.
          path: it.path
            .map((step, i, all) => ({
              ...step,
              link: i === 0 ? undefined : undefined,
              via_group: undefined,
              ...(all[i + 1] && { link: all[i + 1]?.link, via_group: all[i + 1]?.via_group }),
            }))
            .reverse(),
        })),
      },
    } satisfies SampleImpact;
    const a = layoutOf(down);
    const b = layoutOf(upstream);
    expect(b.tree.direction).toBe("upstream");
    expect(b.tree.order).toEqual(a.tree.order);
    expect([...b.layout.placed]).toEqual([...a.layout.placed]);
    for (const [id, node] of a.tree.nodes) {
      expect(b.tree.nodes.get(id)?.parent).toBe(node.parent);
      expect(b.tree.nodes.get(id)?.edge).toEqual(node.edge);
    }
  });

  it("reads an upstream item by the effect of the reading asked for and keeps the single points of failure", () => {
    const data = syntheticImpact({ items: 20, seed: 6, upstream: true });
    const first = data.upstream?.items[0];
    if (first === undefined) throw new Error("no items");
    first.effects = { facts_only: "none", with_assumptions: "degraded" };
    const facts = treeOf(data, "facts_only");
    const assumed = treeOf(data, "with_assumptions");
    expect(facts.nodes.get(first.object_id)?.grade).toBe("none");
    expect(assumed.nodes.get(first.object_id)?.grade).toBe("degraded");
    expect([...facts.nodes.values()].some((n) => n.single)).toBe(true);
  });

  it("falls back to the other reading, and to unknown, for an effect it does not find", () => {
    const data = syntheticImpact({ items: 4, seed: 6, upstream: true });
    const [a, b] = data.upstream?.items ?? [];
    if (a === undefined || b === undefined) throw new Error("no items");
    a.effects = { with_assumptions: "down" };
    b.effects = {};
    const tree = treeOf(data, "facts_only");
    expect(tree.nodes.get(a.object_id)?.grade).toBe("down");
    expect(tree.nodes.get(b.object_id)?.grade).toBe("unknown");
  });

  it("reads an answer with nothing in it and a direction it does not know", () => {
    const data = syntheticImpact({ items: 0, seed: 1 });
    expect(treeOf({ ...data, direction: "sideways" as "downstream" }).direction).toBe("downstream");
    const none = treeOf({
      ...data,
      seeds: undefined,
      facts_only: undefined,
      with_assumptions: undefined,
    });
    expect(none.order).toEqual([]);
    const target = treeOf({ direction: "upstream", computation: data.computation });
    expect(target.order).toEqual([]);
  });

  it("keeps a forest when the causes go round in a circle, and an item whose cause is unknown is a root", () => {
    const base = syntheticImpact({ items: 3, seed: 8 });
    const items = [...(base.with_assumptions?.items ?? [])];
    const [a, b, c] = items;
    if (!a || !b || !c) throw new Error("no items");
    const loop = {
      ...base,
      seeds: [],
      with_assumptions: {
        ...base.with_assumptions,
        items: [
          { ...a, via: b.object_id, path: [] },
          { ...b, via: a.object_id, path: [] },
          { ...c, via: "obj_missing", path: [] },
        ],
      },
    } as SampleImpact;
    const tree = treeOf(loop);
    expect(tree.order).toHaveLength(3);
    expect(tree.roots).toHaveLength(2);
    expect(tree.roots).toContain(c.object_id);
    expect(tree.nodes.get(c.object_id)?.parent).toBeNull();
    const again = treeOf({
      ...loop,
      with_assumptions: {
        ...loop.with_assumptions,
        items: [...(loop.with_assumptions?.items ?? [])].reverse(),
      },
    } as SampleImpact);
    expect(again.order).toEqual(tree.order);
  });

  it("keeps the nearer, more severe item when an object is listed twice, and a seed that is also an item keeps its result", () => {
    const base = syntheticImpact({ items: 2, seed: 9 });
    const [a] = base.with_assumptions?.items ?? [];
    if (!a) throw new Error("no items");
    const far = { ...a, hops: a.hops + 2, result: "degraded" };
    const near = { ...a, result: "interrupted" };
    for (const list of [
      [far, near],
      [near, far],
    ]) {
      const tree = treeOf({
        ...base,
        with_assumptions: { ...base.with_assumptions, items: list },
      } as SampleImpact);
      expect(tree.nodes.get(a.object_id)?.grade).toBe("interrupted");
    }
    const seed = base.seeds?.[0];
    if (!seed) throw new Error("no seed");
    const both = treeOf({
      ...base,
      with_assumptions: {
        ...base.with_assumptions,
        items: [{ ...a, object_id: seed.object_id, result: "unknown" }],
      },
    } as SampleImpact);
    expect(both.nodes.get(seed.object_id)).toMatchObject({ kind: "root", grade: "unknown" });
  });

  it("does the work in one pass: the nodes and the path steps seen are bounded by the answer's size", () => {
    const data = impactOfSize(5000);
    const items = data.with_assumptions?.items ?? [];
    const tree = treeOf(data);
    const pathSteps = items.reduce((sum, it) => sum + it.path.length, 0);
    expect(tree.work.visited).toBe(tree.nodes.size);
    expect(tree.nodes.size).toBeLessThanOrEqual(items.length + 2);
    expect(tree.work.chainSteps).toBeLessThanOrEqual(pathSteps);
    expect(tree.work.chainSteps).toBeLessThanOrEqual(items.length * 7);
  });
});

describe("[REQ-350] the layers are columns and the rows follow the tree", () => {
  it("puts a node in the column of its layer, never above its parent and never on another node", () => {
    for (const data of [smallImpact(), impactOfSize(300, 3), cappedImpact("items")]) {
      const { tree, inputs, layout } = layoutOf(data);
      const used = new Set<string>();
      for (const node of inputs) {
        const p = layout.placed.get(node.id);
        if (p === undefined) throw new Error("not placed");
        expect(layout.lanes[p.lane]?.layer).toBe(node.layer);
        expect(p.x).toBe(p.lane * LANE_WIDTH + LANE_WIDTH / 2);
        expect(p.y).toBe(p.row * ROW_HEIGHT + ROW_HEIGHT / 2);
        const spot = `${p.lane}:${p.row}`;
        expect(used.has(spot)).toBe(false);
        used.add(spot);
        const parent = node.parent === null ? undefined : layout.placed.get(node.parent);
        if (parent) expect(p.row).toBeGreaterThanOrEqual(parent.row);
      }
      expect(layout.height).toBe(layout.rows * ROW_HEIGHT);
      expect(layout.width).toBe(layout.lanes.length * LANE_WIDTH);
      expect(layout.placed.size).toBe(tree.order.length);
    }
  });

  it("keeps the columns in the order of supply and puts an unknown layer last", () => {
    const layout = layoutLanes([
      { id: "a", layer: "service", parent: null },
      { id: "b", layer: "mystery", parent: "a" },
      { id: "c", layer: "facility", parent: "a" },
      { id: "d", layer: "network", parent: "c" },
    ]);
    expect(layout.lanes.map((l) => l.layer)).toEqual(["facility", "network", "service", "other"]);
    expect(layout.placed.get("b")?.lane).toBe(3);
  });

  it("draws a line from each parent to its child", () => {
    const { inputs, layout } = layoutOf(smallImpact());
    const edges = layoutEdges(inputs, layout);
    expect(edges).toHaveLength(inputs.filter((n) => n.parent !== null).length);
    for (const e of edges) {
      expect(e.x1).toBe(layout.placed.get(e.from)?.x);
      expect(e.y2).toBe(layout.placed.get(e.to)?.y);
    }
  });

  it("is empty for nothing to place", () => {
    const layout = layoutLanes([]);
    expect(layout).toMatchObject({ rows: 0, width: 0, height: 0 });
  });
});
