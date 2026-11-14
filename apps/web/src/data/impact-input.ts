/**
 * The one place that knows the shape of the impact answer (ADR-0051). The answer of `queries:impact` is read here and
 * turned into `ImpactSource`: records with the chain of objects from the start of the question to each affected
 * object. Everything else in the impact screen (tree, layout, groups, lists) works on `ImpactSource` only, so when the
 * engine gives a graph shape (`shape=graph`) this file is the one that changes.
 *
 * The input types are the least that is read from the contract, written here so that the pure code does not wait for
 * the generated types. The generated `ImpactData` is assignable to `ImpactDataInput` (checked by the compiler in
 * `impact-input.test.ts`).
 */

export interface ImpactRefInput {
  readonly object_id: string;
  readonly object_type: string;
  readonly display_name: string;
}

export interface ImpactLinkInput {
  /** The evidence grade of the link: fact, assumed or unknown. */
  readonly grade: string;
  readonly delay_class?: string;
}

export interface ImpactStepInput {
  readonly object_id: string | null;
  readonly object_type?: string;
  readonly display_name?: string;
  readonly hidden?: boolean;
  readonly link?: ImpactLinkInput;
  readonly via_group?: ImpactRefInput;
}

export interface ImpactItemInput {
  readonly object_id: string;
  readonly object_type: string;
  readonly display_name: string;
  readonly result: string;
  readonly hops: number;
  readonly via?: string | null;
  readonly assumption_based: boolean;
  readonly lost_channels?: readonly string[];
  readonly delay_class?: string;
  readonly path: readonly ImpactStepInput[];
}

export interface UpstreamItemInput {
  readonly object_id: string;
  readonly object_type: string;
  readonly display_name: string;
  readonly hops: number;
  readonly effects: Readonly<Record<string, string>>;
  readonly single_point_of_failure: boolean;
  readonly assumption_based: boolean;
  readonly path: readonly ImpactStepInput[];
}

export interface ImpactSectionInput {
  readonly items?: readonly ImpactItemInput[];
}

export interface ImpactDataInput {
  readonly direction: string;
  readonly seeds?: readonly ImpactRefInput[];
  readonly target?: ImpactRefInput;
  readonly facts_only?: ImpactSectionInput;
  readonly with_assumptions?: ImpactSectionInput;
  readonly upstream?: { readonly items?: readonly UpstreamItemInput[] };
}

/** Which reading of the evidence is shown: the facts only, or the facts and the assumptions. */
export type ImpactReading = "facts_only" | "with_assumptions";

export type ImpactDirection = "downstream" | "upstream";

/** One object on a chain. The first of a chain is the start of the question, the last is the record itself. */
export interface ChainStep {
  /** Null when the caller may not read the object. */
  readonly id: string | null;
  readonly type: string;
  readonly name: string;
  readonly hidden: boolean;
  /** The link that carried the failure between this step and the one before it (nearer the start). */
  readonly link: ImpactLinkInput | undefined;
  readonly viaGroup: ImpactRefInput | undefined;
}

/** One affected object (or, upstream, one dependency) with how the question reached it. */
export interface ImpactRecord {
  readonly id: string;
  readonly type: string;
  readonly name: string;
  /** The result code. Upstream it is the effect on the target when this object fails alone. */
  readonly grade: string;
  readonly hops: number;
  readonly assumed: boolean;
  readonly delayed: boolean;
  /** Upstream: the target does not survive the failure of this object alone. */
  readonly single: boolean;
  readonly lost: readonly string[];
  /** The id of the object just before this one, when the chain does not say. */
  readonly via: string | null;
  readonly chain: readonly ChainStep[];
}

export interface ImpactSource {
  readonly direction: ImpactDirection;
  readonly roots: readonly ImpactRefInput[];
  readonly records: readonly ImpactRecord[];
}

const EMPTY: readonly string[] = [];

function stepOf(step: ImpactStepInput, link: ImpactLinkInput | undefined): ChainStep {
  const hidden = step.hidden === true || step.object_id === null;
  return {
    id: step.object_id,
    type: step.object_type ?? "",
    name: step.display_name ?? "",
    hidden,
    link,
    viaGroup: step.via_group,
  };
}

/** A downstream path runs from the failed object to the item; each step holds the link into it. */
function downstreamChain(path: readonly ImpactStepInput[]): ChainStep[] {
  return path.map((step, i) => stepOf(step, i === 0 ? undefined : step.link));
}

/**
 * An upstream path runs from the dependency to the target; the link that carried the failure on is on the step it
 * reached, so the chain, turned to start at the target, takes each link from the step before it in the path.
 */
function upstreamChain(path: readonly ImpactStepInput[]): ChainStep[] {
  const n = path.length;
  const chain: ChainStep[] = [];
  for (let j = 0; j < n; j++) {
    const step = path[n - 1 - j];
    if (step === undefined) continue;
    const carrier = j === 0 ? undefined : path[n - j];
    chain.push({ ...stepOf(step, carrier?.link), viaGroup: carrier?.via_group });
  }
  return chain;
}

function downstreamRecord(item: ImpactItemInput): ImpactRecord {
  return {
    id: item.object_id,
    type: item.object_type,
    name: item.display_name,
    grade: item.result,
    hops: item.hops,
    assumed: item.assumption_based,
    delayed: item.delay_class === "delayed",
    single: false,
    lost: item.lost_channels ?? EMPTY,
    via: item.via ?? null,
    chain: downstreamChain(item.path),
  };
}

/** The effect an upstream item has on the target, from the reading asked for, else from the other one. */
function effectOf(item: UpstreamItemInput, reading: ImpactReading): string {
  const other = reading === "facts_only" ? "with_assumptions" : "facts_only";
  return item.effects[reading] ?? item.effects[other] ?? "unknown";
}

function upstreamRecord(item: UpstreamItemInput, reading: ImpactReading): ImpactRecord {
  const chain = upstreamChain(item.path);
  const parent = chain.length >= 2 ? chain[chain.length - 2] : undefined;
  return {
    id: item.object_id,
    type: item.object_type,
    name: item.display_name,
    grade: effectOf(item, reading),
    hops: item.hops,
    assumed: item.assumption_based,
    delayed: chain.some((s) => s.link?.delay_class === "delayed"),
    single: item.single_point_of_failure,
    lost: EMPTY,
    via: parent?.id ?? null,
    chain,
  };
}

/** The answer of impact as records. A direction this version does not know is read as downstream. */
export function impactSource(data: ImpactDataInput, reading: ImpactReading): ImpactSource {
  if (data.direction === "upstream") {
    const items = data.upstream?.items ?? [];
    return {
      direction: "upstream",
      roots: data.target ? [data.target] : [],
      records: items.map((item) => upstreamRecord(item, reading)),
    };
  }
  const section = reading === "with_assumptions" ? data.with_assumptions : data.facts_only;
  return {
    direction: "downstream",
    roots: data.seeds ?? [],
    records: (section?.items ?? []).map(downstreamRecord),
  };
}
