/**
 * The overview of a big answer (ADR-0051). Above `OVERVIEW_THRESHOLD` nodes the picture shows groups: under one group,
 * the nodes that hang below its members and have the same type are one group, and so on down the tree. A group is "a
 * type, a count, and what grades make up the count". The groups are a tree of their own, with the same layers for
 * columns, so the layout reads them like nodes.
 */

import { gradeRank } from "./impact-grades";
import type { ImpactNode, ImpactTree } from "./impact-tree";

/** More nodes than this, and the picture starts as an overview. */
export const OVERVIEW_THRESHOLD = 300;

export interface ImpactGroup {
  /** The types from the root to this group, as `/Type/Type`. */
  readonly key: string;
  readonly parent: string | null;
  readonly type: string;
  readonly layer: string;
  readonly count: number;
  /** How many members have each result code. Members that carry none (waypoints, hidden objects) are not counted here. */
  readonly grades: Readonly<Record<string, number>>;
  /** The most severe result among the members. */
  readonly worst: string | undefined;
  /** The step of the spread at which the first member appears. */
  readonly step: number;
  /** The members, the most severe first, then by id. */
  readonly members: readonly string[];
  readonly children: readonly string[];
}

export interface ImpactOverview {
  readonly groups: ReadonlyMap<string, ImpactGroup>;
  /** The keys in depth-first order, the children of a group sorted by type. */
  readonly order: readonly string[];
  readonly roots: readonly string[];
}

export const needsOverview = (tree: ImpactTree): boolean => tree.nodes.size > OVERVIEW_THRESHOLD;

type Open = {
  key: string;
  parent: string | null;
  type: string;
  layer: string;
  members: ImpactNode[];
  children: Set<string>;
};

const compareText = (a: string, b: string): number => (a < b ? -1 : a > b ? 1 : 0);

const bySeverity = (a: ImpactNode, b: ImpactNode): number =>
  gradeRank(a.grade) - gradeRank(b.grade) || compareText(a.id, b.id);

/** Puts every node in the group of its type under the group of its parent. */
function collect(tree: ImpactTree): Map<string, Open> {
  const open = new Map<string, Open>();
  const keyOf = new Map<string, string>();
  for (const id of tree.order) {
    const node = tree.nodes.get(id);
    if (node === undefined) continue;
    const parentKey = node.parent === null ? null : (keyOf.get(node.parent) ?? null);
    const key = `${parentKey ?? ""}/${node.type}`;
    keyOf.set(id, key);
    let group = open.get(key);
    if (group === undefined) {
      group = {
        key,
        parent: parentKey,
        type: node.type,
        layer: node.layer,
        members: [],
        children: new Set(),
      };
      open.set(key, group);
      if (parentKey !== null) open.get(parentKey)?.children.add(key);
    }
    group.members.push(node);
  }
  return open;
}

function close(open: Open, children: readonly string[]): ImpactGroup {
  const members = [...open.members].sort(bySeverity);
  const grades: Record<string, number> = {};
  for (const m of members) if (m.grade !== undefined) grades[m.grade] = (grades[m.grade] ?? 0) + 1;
  return {
    key: open.key,
    parent: open.parent,
    type: open.type,
    layer: open.layer,
    count: members.length,
    grades,
    worst: members.find((m) => m.grade !== undefined)?.grade,
    step: members.reduce((least, m) => Math.min(least, m.step), Number.POSITIVE_INFINITY),
    members: members.map((m) => m.id),
    children,
  };
}

/** The groups of a tree. Every node is in exactly one group, so the counts add up to the nodes. */
export function buildOverview(tree: ImpactTree): ImpactOverview {
  const open = collect(tree);
  const sortKeys = (keys: Iterable<string>): string[] =>
    [...keys].sort(
      (a, b) => compareText(open.get(a)?.type ?? "", open.get(b)?.type ?? "") || compareText(a, b),
    );
  const groups = new Map<string, ImpactGroup>();
  for (const [key, g] of open) groups.set(key, close(g, sortKeys(g.children)));
  const roots = sortKeys([...open.values()].filter((g) => g.parent === null).map((g) => g.key));
  const order: string[] = [];
  const stack = [...roots].reverse();
  for (let key = stack.pop(); key !== undefined; key = stack.pop()) {
    order.push(key);
    const kids = groups.get(key)?.children ?? [];
    for (let i = kids.length - 1; i >= 0; i--) stack.push(kids[i] as string);
  }
  return { groups, order, roots };
}
