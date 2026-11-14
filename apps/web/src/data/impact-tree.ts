/**
 * The cause tree of an impact answer (ADR-0051). Every affected object has one object just before it on its path (its
 * cause), so the answer is a forest whose roots are the objects the question started from. Upstream the same tree is
 * read the other way: the root is the target and a child is something the parent depends on. A path can pass through
 * an object that is not an item of the answer (a waypoint) or one the caller may not read (a hidden object); both get
 * a node, so that every item has a parent to hang on.
 *
 * The tree does not depend on the order of the items: where two paths disagree about an object, the smaller id wins,
 * and the children and the roots are sorted. The same answer always gives the same tree, and so the same picture.
 */

import { gradeRank } from "./impact-grades";
import type { ChainStep, ImpactDirection, ImpactRecord, ImpactSource } from "./impact-input";

export type ImpactNodeKind = "root" | "item" | "waypoint" | "hidden";

/** How the failure passed from a node to its parent's side: the evidence of the link, a group on the way, a delay. */
export interface ImpactEdge {
  /** fact, assumed or unknown. */
  readonly grade: string;
  readonly viaGroup: { readonly id: string; readonly name: string } | undefined;
  readonly delayed: boolean;
}

export interface ImpactNode {
  readonly id: string;
  readonly kind: ImpactNodeKind;
  readonly type: string;
  readonly name: string;
  readonly layer: string;
  /** The result code. Undefined for a node that is not an item (a waypoint, a hidden object, a root that failed). */
  readonly grade: string | undefined;
  /** How many links from the start, as the answer says. */
  readonly hops: number;
  /** The step of the spread at which the node appears: its hops, but never before its parent. */
  readonly step: number;
  readonly parent: string | null;
  readonly children: readonly string[];
  readonly assumed: boolean;
  /** The weakest evidence grade on the path of the item (fact, assumed, unknown), when the answer says. */
  readonly weakest: string | undefined;
  /** The path of the item went through an object the caller may not read. */
  readonly viaHidden: boolean;
  /** Other objects the failure also came through (carried, not shown yet). */
  readonly also: readonly string[];
  readonly delayed: boolean;
  readonly single: boolean;
  readonly lost: readonly string[];
  readonly edge: ImpactEdge | undefined;
}

export interface ImpactTree {
  readonly direction: ImpactDirection;
  readonly nodes: ReadonlyMap<string, ImpactNode>;
  /** The ids in depth-first order, children in the order of the sort: the order the layout reads. */
  readonly order: readonly string[];
  readonly roots: readonly string[];
  /** What the work counted, so a test can bound it without a clock. */
  readonly work: { readonly visited: number; readonly chainSteps: number };
}

type Mutable<T> = { -readonly [K in keyof T]: T[K] };
type Draft = Mutable<ImpactNode> & { childIds: string[] };

/** Orders two texts by code unit, the same on every machine (no locale). The one comparator of the impact files. */
export const compareText = (a: string, b: string): number => (a < b ? -1 : a > b ? 1 : 0);

const edgeOf = (step: ChainStep | undefined): ImpactEdge | undefined =>
  step?.link === undefined
    ? undefined
    : {
        grade: step.link.grade,
        viaGroup: step.viaGroup && {
          id: step.viaGroup.object_id,
          name: step.viaGroup.display_name,
        },
        delayed: step.link.delay_class === "delayed",
      };

/** The id of a step of a chain; a step the caller may not read gets an id that belongs to its chain alone. */
const stepId = (owner: string, chain: readonly ChainStep[], at: number): string | null => {
  const step = chain[at];
  if (step === undefined) return null;
  return step.id ?? `hidden:${owner}:${at}`;
};

function draft(id: string, kind: ImpactNodeKind, fields: Partial<Draft>): Draft {
  return {
    id,
    kind,
    type: "",
    name: "",
    layer: "",
    grade: undefined,
    hops: 0,
    step: 0,
    parent: null,
    children: [],
    childIds: [],
    assumed: false,
    weakest: undefined,
    viaHidden: false,
    also: [],
    delayed: false,
    single: false,
    lost: [],
    edge: undefined,
    ...fields,
  };
}

/** Whether an item is better kept than the node already there for the same object: the nearer, then the more severe. */
const betterThan = (rec: ImpactRecord, node: Draft): boolean =>
  rec.hops !== node.hops ? rec.hops < node.hops : gradeRank(rec.grade) < gradeRank(node.grade);

function addItem(nodes: Map<string, Draft>, rec: ImpactRecord, roots: ReadonlySet<string>): void {
  const known = nodes.get(rec.id);
  if (known?.kind === "root") {
    known.grade = rec.grade;
    known.assumed = rec.assumed;
    known.weakest = rec.weakest;
    known.viaHidden = rec.viaHidden;
    known.also = rec.also;
    known.lost = rec.lost;
    return;
  }
  if (known !== undefined && !betterThan(rec, known)) return;
  const chain = rec.chain;
  const k = chain.length;
  const parentId = k >= 2 ? stepId(rec.id, chain, k - 2) : rec.via;
  nodes.set(
    rec.id,
    draft(rec.id, roots.has(rec.id) ? "root" : "item", {
      type: rec.type,
      name: rec.name,
      grade: rec.grade,
      hops: rec.hops,
      parent: parentId,
      assumed: rec.assumed,
      weakest: rec.weakest,
      viaHidden: rec.viaHidden,
      also: rec.also,
      delayed: rec.delayed,
      single: rec.single,
      lost: rec.lost,
      edge: edgeOf(chain[k - 1]),
    }),
  );
}

/** Adds the objects on the path of an item that are not items themselves, from the item up to a known object. */
function addWaypoints(
  nodes: Map<string, Draft>,
  items: ReadonlySet<string>,
  rec: ImpactRecord,
): number {
  const chain = rec.chain;
  let steps = 0;
  for (let i = chain.length - 2; i >= 0; i--) {
    steps++;
    const step = chain[i];
    const id = stepId(rec.id, chain, i);
    if (step === undefined || id === null || items.has(id)) break;
    const parent = i === 0 ? null : stepId(rec.id, chain, i - 1);
    const known = nodes.get(id);
    if (known === undefined || (known.kind === "waypoint" && smallerParent(parent, known.parent))) {
      nodes.set(
        id,
        draft(id, step.hidden ? "hidden" : "waypoint", {
          type: step.type,
          name: step.name,
          hops: i,
          parent,
          edge: edgeOf(step),
        }),
      );
    }
  }
  return steps;
}

/** A parent is smaller than another one when it has an id and the other has none, or its id sorts first. */
const smallerParent = (a: string | null, b: string | null): boolean =>
  a !== null && (b === null || compareText(a, b) < 0);

/** Cuts a parent link that leads back to a node already on the way up, so the nodes always form a forest. */
function cutCycles(nodes: Map<string, Draft>): void {
  const state = new Map<string, 1 | 2>();
  for (const start of [...nodes.keys()].sort(compareText)) {
    const path: string[] = [];
    let at: string | null = start;
    while (at !== null && !state.has(at)) {
      state.set(at, 1);
      path.push(at);
      at = nodes.get(at)?.parent ?? null;
    }
    if (at !== null && state.get(at) === 1) {
      const last = nodes.get(path[path.length - 1] ?? "");
      if (last) last.parent = null;
    }
    for (const id of path) state.set(id, 2);
  }
}

function compareChildren(nodes: ReadonlyMap<string, Draft>) {
  return (a: string, b: string): number => {
    const x = nodes.get(a);
    const y = nodes.get(b);
    if (x === undefined || y === undefined) return compareText(a, b);
    return (
      compareText(x.type, y.type) || gradeRank(x.grade) - gradeRank(y.grade) || compareText(a, b)
    );
  };
}

/** Links the children to their parents. A parent that is not a node leaves the child a root of its own. */
function link(nodes: Map<string, Draft>): string[] {
  const roots: string[] = [];
  for (const node of nodes.values()) {
    const parent = node.parent === null ? undefined : nodes.get(node.parent);
    if (parent === undefined) {
      node.parent = null;
      roots.push(node.id);
    } else parent.childIds.push(node.id);
  }
  const compare = compareChildren(nodes);
  for (const node of nodes.values()) node.childIds.sort(compare);
  return roots.sort(compareText);
}

/** Depth-first, children in order; fills the layer, the step and the children of each node. */
function walk(
  nodes: Map<string, Draft>,
  roots: readonly string[],
  layerOf: (type: string) => string,
): string[] {
  const order: string[] = [];
  const stack = [...roots].reverse();
  for (let id = stack.pop(); id !== undefined; id = stack.pop()) {
    const node = nodes.get(id);
    if (node === undefined) continue;
    const parent = node.parent === null ? undefined : nodes.get(node.parent);
    node.layer = node.kind === "hidden" ? (parent?.layer ?? "other") : layerOf(node.type);
    node.step = Math.max(node.hops, parent?.step ?? 0);
    node.children = node.childIds;
    order.push(id);
    for (let i = node.childIds.length - 1; i >= 0; i--) stack.push(node.childIds[i] as string);
  }
  return order;
}

/**
 * Builds the cause tree. `layerOf` says which layer a type belongs to (the types of the model carry it); a type it
 * does not know is placed after the known layers by the layout.
 */
export function buildImpactTree(
  source: ImpactSource,
  layerOf: (type: string) => string,
): ImpactTree {
  const roots = new Set(source.roots.map((r) => r.object_id));
  const nodes = new Map<string, Draft>();
  for (const root of source.roots) {
    nodes.set(
      root.object_id,
      draft(root.object_id, "root", { type: root.object_type, name: root.display_name }),
    );
  }
  for (const rec of source.records) addItem(nodes, rec, roots);
  const items = new Set(nodes.keys());
  let chainSteps = 0;
  for (const rec of source.records) chainSteps += addWaypoints(nodes, items, rec);
  cutCycles(nodes);
  const rootIds = link(nodes);
  const order = walk(nodes, rootIds, layerOf);
  return {
    direction: source.direction,
    nodes,
    order,
    roots: rootIds,
    work: { visited: order.length, chainSteps },
  };
}

/** The ids of the items of the tree: the nodes that carry a result, in the order of the tree. */
export const itemIds = (tree: ImpactTree): string[] =>
  tree.order.filter((id) => tree.nodes.get(id)?.grade !== undefined);
