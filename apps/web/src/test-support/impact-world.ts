/**
 * Made-up impact answers for tests and for looking at the parts (ADR-0051). Nothing here is a real system: the
 * objects, names and ids are generated from a number, so the same options give the same answer, and a large answer is
 * made by a function instead of being kept as a file. The answers have the shape of `queries:impact` in the contract.
 */

export interface SampleRef {
  object_id: string;
  object_type: string;
  display_name: string;
}

export interface SampleStep {
  object_id: string | null;
  object_type?: string;
  display_name?: string;
  hidden?: boolean;
  link?: { grade: string; link_type: string; delay_class?: string };
  via_group?: SampleRef;
}

export interface SampleItem {
  object_id: string;
  object_type: string;
  display_name: string;
  result: string;
  also: string[];
  lost_channels: string[];
  via: string | null;
  via_hidden?: boolean;
  hops: number;
  assumption_based: boolean;
  weakest_grade: string;
  path: SampleStep[];
}

export interface SampleUpstreamItem {
  object_id: string;
  object_type: string;
  display_name: string;
  hops: number;
  effects: Record<string, string>;
  single_point_of_failure: boolean;
  assumption_based: boolean;
  path: SampleStep[];
}

export interface SampleSection {
  counts_by_result: Record<string, number>;
  counts_by_type: { object_type: string; count: number }[];
  counts_by_parent: { object_id: string; display_name: string; count: number }[];
  groups_touched: Record<string, unknown>[];
  unfilled_channel_objects: number;
  items: SampleItem[];
}

export interface SampleComputation {
  complete: boolean;
  items_over_cap: boolean;
  max_hops: number;
  max_items: number;
  rounds: number;
  stale_inputs: number;
  stopped_by: string | null;
  time_budget_seconds: number;
  weak_links: number;
}

export interface SampleImpact {
  direction: "downstream" | "upstream";
  computation: SampleComputation;
  seeds?: SampleRef[];
  target?: SampleRef;
  facts_only?: SampleSection;
  with_assumptions?: SampleSection;
  upstream?: {
    total: number;
    single_points_of_failure: number;
    counts_by_type: { object_type: string; count: number }[];
    counts_by_effect: Record<string, number>;
    items: SampleUpstreamItem[];
  };
}

/** The layer of each type of the samples, as the type list of the API would give it. */
export const SAMPLE_LAYERS: Readonly<Record<string, string>> = {
  PowerFeed: "facility",
  NetworkSwitch: "network",
  PhysicalServer: "hardware",
  VirtualMachine: "virtualization",
  Pod: "container",
  Application: "platform",
  BusinessService: "service",
};

export const sampleLayerOf = (type: string): string => SAMPLE_LAYERS[type] ?? "other";

/** The type at each depth of a downstream answer; an upstream one runs the other way. */
const CHAIN = [
  "PowerFeed",
  "NetworkSwitch",
  "PhysicalServer",
  "VirtualMachine",
  "Pod",
  "Application",
  "BusinessService",
] as const;

/** A small, fast, seeded source of numbers in [0, 1). */
export function seededRandom(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const hex = (word: number): string => (word >>> 0).toString(16).padStart(8, "0");

/** An object id: `obj_` and 32 hex digits, different for every number, with no order that follows the tree. */
export function sampleId(salt: number, n: number): string {
  const rnd = seededRandom(Math.imul(salt + 1, 2654435761) ^ Math.imul(n + 1, 40503));
  return `obj_${hex(rnd() * 4294967296)}${hex(rnd() * 4294967296)}${hex(rnd() * 4294967296)}${hex(rnd() * 4294967296)}`;
}

export interface SampleOptions {
  /** How many items the answer has. */
  readonly items: number;
  readonly seed?: number;
  readonly seeds?: number;
  /** Ask what the first seed depends on, instead of what its failure reaches. */
  readonly upstream?: boolean;
  /** Every nth object may not be read by the caller (0 for none). */
  readonly hiddenEvery?: number;
  /** The answer stopped at its limits. */
  readonly capped?: "items" | "time_budget" | "max_hops";
  /** Mix the order of the items. */
  readonly shuffle?: boolean;
  /** Keep the tree at most this deep (at most 6). */
  readonly depth?: number;
}

interface Made {
  index: number;
  parent: number;
  depth: number;
  hidden: boolean;
  assumed: boolean;
  grade: string;
  type: string;
  ref: SampleRef;
}

const WEIGHTS: readonly (readonly [string, number])[] = [
  ["down", 0.5],
  ["interrupted", 0.05],
  ["unknown", 0.05],
  ["exposed_direct", 0.1],
  ["exposed_inherited", 0.1],
  ["degraded", 0.15],
  ["unmanageable", 0.05],
];

function pickGrade(roll: number): string {
  let sum = 0;
  for (const [grade, weight] of WEIGHTS) {
    sum += weight;
    if (roll < sum) return grade;
  }
  return "down";
}

function makeTree(o: SampleOptions): Made[] {
  const rnd = seededRandom(o.seed ?? 1);
  const salt = o.seed ?? 1;
  const deepest = Math.min(o.depth ?? 6, 6);
  const types = o.upstream ? [...CHAIN].reverse() : [...CHAIN];
  const made: Made[] = [];
  const add = (parent: number, depth: number): void => {
    const index = made.length;
    const type = types[depth] ?? "BusinessService";
    const up = made[parent];
    const hidden = (o.hiddenEvery ?? 0) > 0 && depth > 0 && index % (o.hiddenEvery ?? 1) === 0;
    const label = `${type.toLowerCase()}-${index}`;
    made.push({
      index,
      parent,
      depth,
      hidden,
      assumed: depth > 0 && ((up?.assumed ?? false) || rnd() < 0.04),
      grade: pickGrade(rnd()),
      type,
      ref: { object_id: sampleId(salt, index), object_type: type, display_name: label },
    });
  };
  for (let s = 0; s < (o.seeds ?? 1); s++) add(-1, 0);
  const eligible = made.map((m) => m.index);
  for (let i = 0; i < o.items; i++) {
    const parent = eligible[Math.floor(rnd() * eligible.length)] ?? 0;
    const depth = (made[parent]?.depth ?? 0) + 1;
    add(parent, depth);
    if (depth < deepest) eligible.push(made.length - 1);
  }
  return made;
}

function stepOf(m: Made, link: SampleStep["link"], group: SampleRef | undefined): SampleStep {
  if (m.hidden) return { object_id: null, hidden: true, link, via_group: group };
  return {
    object_id: m.ref.object_id,
    object_type: m.type,
    display_name: m.ref.display_name,
    link,
    via_group: group,
  };
}

function linkOf(m: Made): SampleStep["link"] {
  return {
    grade: m.assumed ? "assumed" : "fact",
    link_type: m.depth % 2 === 0 ? "depends_on" : "supplies",
    ...(m.index % 11 === 0 && { delay_class: "delayed" }),
  };
}

/** The objects from the start to `m`, as the steps of a path. */
function ancestors(made: readonly Made[], m: Made, groups: Map<number, SampleRef>): Made[] {
  const chain: Made[] = [];
  for (let at: Made | undefined = m; at !== undefined; at = made[at.parent]) chain.unshift(at);
  for (const c of chain) {
    if (c.grade.startsWith("exposed_") && c.index % 3 === 0 && !groups.has(c.index)) {
      groups.set(c.index, {
        object_id: sampleId(9000, c.index),
        object_type: "RedundancyGroup",
        display_name: `group-${c.index}`,
      });
    }
  }
  return chain;
}

function downstreamItem(
  made: readonly Made[],
  m: Made,
  groups: Map<number, SampleRef>,
): SampleItem {
  const chain = ancestors(made, m, groups);
  const parent = made[m.parent];
  return {
    object_id: m.ref.object_id,
    object_type: m.type,
    display_name: m.ref.display_name,
    result: m.grade,
    also: [],
    lost_channels: [m.depth % 2 === 0 ? "power" : "network"],
    via: parent === undefined || parent.hidden ? null : parent.ref.object_id,
    ...(parent?.hidden && { via_hidden: true }),
    hops: m.depth,
    assumption_based: m.assumed,
    weakest_grade: m.assumed ? "assumed" : "fact",
    path: chain.map((c, i) => stepOf(c, i === 0 ? undefined : linkOf(c), groups.get(c.index))),
  };
}

function upstreamItem(
  made: readonly Made[],
  m: Made,
  groups: Map<number, SampleRef>,
): SampleUpstreamItem {
  const chain = ancestors(made, m, groups).reverse();
  return {
    object_id: m.ref.object_id,
    object_type: m.type,
    display_name: m.ref.display_name,
    hops: m.depth,
    effects: { facts_only: m.grade, with_assumptions: m.grade },
    single_point_of_failure: m.grade === "down",
    assumption_based: m.assumed,
    path: chain.map((c, i) => {
      // The link and the group of a step are those of the edge the failure took to reach it: from the step before.
      const before = chain[i - 1];
      return before === undefined
        ? stepOf(c, undefined, undefined)
        : stepOf(c, linkOf(before), groups.get(before.index));
    }),
  };
}

function sectionOf(items: SampleItem[], groups: Map<number, SampleRef>): SampleSection {
  const byResult: Record<string, number> = {};
  const byType = new Map<string, number>();
  for (const it of items) {
    byResult[it.result] = (byResult[it.result] ?? 0) + 1;
    byType.set(it.object_type, (byType.get(it.object_type) ?? 0) + 1);
  }
  return {
    counts_by_result: byResult,
    counts_by_type: [...byType].map(([object_type, count]) => ({ object_type, count })),
    counts_by_parent: [],
    groups_touched: [...groups.values()].map((g) => ({
      group_id: g.object_id,
      group_type: g.object_type,
      display_name: g.display_name,
      mode: "active_active",
      value: "1/2",
      alive: 1,
      unknown: 0,
      n: 2,
      min_alive: 1,
      slack: 0,
    })),
    unfilled_channel_objects: 0,
    items,
  };
}

function computationOf(o: SampleOptions): SampleComputation {
  const stopped = o.capped === "time_budget" || o.capped === "max_hops" ? o.capped : null;
  return {
    complete: o.capped === undefined,
    items_over_cap: o.capped === "items",
    max_hops: o.capped === "max_hops" ? 3 : 0,
    max_items: o.capped === "items" ? o.items : 5000,
    rounds: 3,
    stale_inputs: 0,
    stopped_by: stopped,
    time_budget_seconds: 30,
    weak_links: 0,
  };
}

function shuffled<T>(list: T[], seed: number): T[] {
  const rnd = seededRandom(seed * 31 + 7);
  const out = [...list];
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(rnd() * (i + 1));
    [out[i], out[j]] = [out[j] as T, out[i] as T];
  }
  return out;
}

/** An impact answer with about `items` affected objects. The same options give the same answer. */
export function syntheticImpact(options: SampleOptions): SampleImpact {
  const made = makeTree(options);
  const groups = new Map<number, SampleRef>();
  const seeds = made.filter((m) => m.parent < 0).map((m) => m.ref);
  const rest = made.filter((m) => m.parent >= 0 && !m.hidden);
  const computation = computationOf(options);
  if (options.upstream) {
    const dependencies = rest.map((m) => upstreamItem(made, m, groups));
    const items = options.shuffle ? shuffled(dependencies, options.seed ?? 1) : dependencies;
    const counts: Record<string, number> = {};
    for (const it of items)
      counts[it.effects.facts_only ?? ""] = (counts[it.effects.facts_only ?? ""] ?? 0) + 1;
    return {
      direction: "upstream",
      computation,
      target: seeds[0],
      upstream: {
        total: items.length,
        single_points_of_failure: items.filter((i) => i.single_point_of_failure).length,
        counts_by_type: [],
        counts_by_effect: counts,
        items,
      },
    };
  }
  const all = rest.map((m) => downstreamItem(made, m, groups));
  const order = (list: SampleItem[]): SampleItem[] =>
    options.shuffle ? shuffled(list, options.seed ?? 1) : list;
  return {
    direction: "downstream",
    computation,
    seeds,
    facts_only: sectionOf(order(all.filter((i) => !i.assumption_based)), groups),
    with_assumptions: sectionOf(order(all), groups),
  };
}

/** About a dozen objects, with a hidden object, a group, an unknown result and a delayed link. */
export const smallImpact = (): SampleImpact =>
  syntheticImpact({ items: 14, seed: 3, hiddenEvery: 7, depth: 5 });

/** The sizes the screen has to take: the limit of the overview, the default, and the biggest it draws. */
export const impactOfSize = (items: 300 | 2000 | 5000, seed = 1): SampleImpact =>
  syntheticImpact({ items, seed, seeds: 2 });

/** An answer that stopped at a limit. */
export const cappedImpact = (by: "items" | "time_budget" | "max_hops"): SampleImpact =>
  syntheticImpact({ items: 120, seed: 5, capped: by, depth: by === "max_hops" ? 3 : 6 });
