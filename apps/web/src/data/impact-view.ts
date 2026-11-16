/**
 * The question and the way of looking at it, kept in the address after `#` (ADR-0051): `#/kg/{kg}/impact?...`. The
 * question is the start (objects that failed, places that fail with everything in them, links that are cut) or the
 * target whose dependencies are asked; the rest is the view (reading, limits, list or graph, selection, step, sorting).
 * Pure functions between the pairs of the query and a view, and from a view to the two requests the screen sends. A
 * value that does not fit is read as the default, a view at its default writes no pair, and a pair written is a pair
 * read back as the same view (the address is the same after a round trip).
 */

import { isImpactGrade } from "./impact-grades";
import type { ImpactReading } from "./impact-input";

/** What the address holds of each id list at most; the API takes 1000, an address must stay short. */
export const MAX_ADDRESS_IDS = 20;
export const ITEM_LIMITS = [500, 2000, 5000, 20000] as const;
export const DEFAULT_ITEM_LIMIT = 2000;
/** The page size of the items the screen asks for. */
export const ITEMS_PER_REQUEST = 1000;

const OBJECT_ID = /^obj_[0-9a-f]{32}$/;
const LINK_ID = /^lnk_[0-9a-f]{32}$/;

export type ImpactSortKey = "name" | "type" | "hops" | "lost" | "via" | "basis";
const SORT_KEYS: readonly ImpactSortKey[] = ["name", "type", "hops", "lost", "via", "basis"];

export interface ImpactQuestion {
  /** Objects that have failed. */
  readonly dead: readonly string[];
  /** Objects that fail with everything contained in them. */
  readonly site: readonly string[];
  /** Links that are cut. */
  readonly cut: readonly string[];
  /** Asks what this object depends on; with it the three above are empty. */
  readonly target: string | undefined;
}

export interface ImpactView {
  readonly question: ImpactQuestion;
  readonly reading: ImpactReading;
  /** The most hops asked for; none means no limit. */
  readonly hops: number | undefined;
  /** How many items the screen asks for. */
  readonly max: number;
  readonly view: "graph" | "list";
  /** The key of the group the picture is zoomed into. */
  readonly focus: string | undefined;
  readonly selected: string | undefined;
  readonly step: number | undefined;
  /** Show every node instead of groups. */
  readonly all: boolean;
  readonly sort:
    | { readonly key: ImpactSortKey; readonly direction: "ascending" | "descending" }
    | undefined;
  /** The result code the list is narrowed to. */
  readonly grade: string | undefined;
}

export const EMPTY_QUESTION: ImpactQuestion = { dead: [], site: [], cut: [], target: undefined };

export const DEFAULT_IMPACT_VIEW: ImpactView = {
  question: EMPTY_QUESTION,
  reading: "facts_only",
  hops: undefined,
  max: DEFAULT_ITEM_LIMIT,
  view: "graph",
  focus: undefined,
  selected: undefined,
  step: undefined,
  all: false,
  sort: undefined,
  grade: undefined,
};

const wholeNumber = (text: string | null, least: number): number | undefined => {
  if (text === null || !/^\d{1,9}$/.test(text)) return undefined;
  const n = Number(text);
  return n >= least ? n : undefined;
};

/** The ids of a comma-separated list that are of the form, each once, in the order given, up to the limit. */
function idList(text: string | null, form: RegExp): string[] {
  if (text === null) return [];
  const seen = new Set<string>();
  for (const part of text.split(",")) {
    if (form.test(part)) seen.add(part);
    if (seen.size >= MAX_ADDRESS_IDS) break;
  }
  return [...seen];
}

function readQuestion(params: URLSearchParams): ImpactQuestion {
  const target = idList(params.get("target"), OBJECT_ID)[0];
  if (target !== undefined) return { ...EMPTY_QUESTION, target };
  return {
    dead: idList(params.get("dead"), OBJECT_ID),
    site: idList(params.get("site"), OBJECT_ID),
    cut: idList(params.get("cut"), LINK_ID),
    target: undefined,
  };
}

/** The focus of the address, when it is there and short enough to be one. */
function readFocus(params: URLSearchParams): string | undefined {
  const focus = params.get("focus");
  return focus === null || focus === "" || focus.length > 500 ? undefined : focus;
}

function readSort(params: URLSearchParams): ImpactView["sort"] {
  const key = SORT_KEYS.find((k) => k === params.get("sort"));
  if (key === undefined) return undefined;
  return { key, direction: params.get("dir") === "desc" ? "descending" : "ascending" };
}

export function readImpactView(params: URLSearchParams): ImpactView {
  const grade = params.get("grade");
  return {
    question: readQuestion(params),
    reading: params.get("reading") === "assumed" ? "with_assumptions" : "facts_only",
    hops: wholeNumber(params.get("hops"), 1),
    max: ITEM_LIMITS.find((n) => String(n) === params.get("max")) ?? DEFAULT_ITEM_LIMIT,
    view: params.get("view") === "list" ? "list" : "graph",
    focus: readFocus(params),
    selected: idList(params.get("sel"), OBJECT_ID)[0],
    step: wholeNumber(params.get("step"), 0),
    all: params.get("all") === "1",
    sort: readSort(params),
    grade: grade !== null && isImpactGrade(grade) ? grade : undefined,
  };
}

const list = (ids: readonly string[]): string | undefined =>
  ids.length === 0 ? undefined : ids.slice(0, MAX_ADDRESS_IDS).join(",");

/** The pairs of the question: the target alone when there is one, else the three lists. */
function questionParams(q: ImpactQuestion): Record<string, string | undefined> {
  if (q.target !== undefined)
    return { target: q.target, dead: undefined, site: undefined, cut: undefined };
  return { target: undefined, dead: list(q.dead), site: list(q.site), cut: list(q.cut) };
}

/** The pairs of the query for a view; a pair at its default is `undefined`. The keys come in a fixed order. */
export function impactViewParams(view: ImpactView): Record<string, string | undefined> {
  return {
    ...questionParams(view.question),
    reading: view.reading === "with_assumptions" ? "assumed" : undefined,
    hops: view.hops === undefined ? undefined : String(view.hops),
    max: view.max === DEFAULT_ITEM_LIMIT ? undefined : String(view.max),
    view: view.view === "list" ? "list" : undefined,
    focus: view.focus,
    sel: view.selected,
    step: view.step === undefined ? undefined : String(view.step),
    all: view.all ? "1" : undefined,
    sort: view.sort?.key,
    dir: view.sort?.direction === "descending" ? "desc" : undefined,
    grade: view.grade,
  };
}

/** The address of the screen: `#/kg/{kg}/impact` and the query of the view. */
export function impactAddress(kg: string, view: ImpactView): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(impactViewParams(view))) {
    if (value !== undefined) query.set(key, value);
  }
  const text = query.toString();
  return `#/kg/${encodeURIComponent(kg)}/impact${text === "" ? "" : `?${text}`}`;
}

/** The KG and the view of an address, or nothing when the address is not this screen's. */
export function readImpactAddress(
  hash: string,
): { readonly kg: string; readonly view: ImpactView } | undefined {
  const [path = "", query = ""] = hash.replace(/^#/, "").split("?");
  const [head, kg, section, extra] = path.split("/").filter(Boolean);
  if (head !== "kg" || kg === undefined || section !== "impact" || extra !== undefined) {
    return undefined;
  }
  try {
    return { kg: decodeURIComponent(kg), view: readImpactView(new URLSearchParams(query)) };
  } catch {
    return undefined;
  }
}

/** Whether the question names a start; without one there is nothing to ask. */
export function isAnswerable(question: ImpactQuestion): boolean {
  return (
    question.target !== undefined ||
    question.dead.length > 0 ||
    question.site.length > 0 ||
    question.cut.length > 0
  );
}

/** The input of `queries:impact` as far as the screen sends it. */
export interface ImpactRequestBody {
  readonly dead?: readonly string[];
  readonly dead_links?: readonly string[];
  readonly site_failure?: readonly string[];
  readonly target_object_id?: string;
  readonly direction: "downstream" | "upstream";
  readonly mode: "both" | ImpactReading;
  readonly shape: "summary" | "items";
  readonly max_hops?: number;
  readonly max_items?: number;
  readonly limit?: number;
  readonly cursor?: string;
}

function questionBody(
  view: ImpactView,
): Pick<
  ImpactRequestBody,
  "dead" | "dead_links" | "site_failure" | "target_object_id" | "direction"
> {
  const q = view.question;
  if (q.target !== undefined) return { target_object_id: q.target, direction: "upstream" };
  return {
    direction: "downstream",
    ...(q.dead.length > 0 && { dead: q.dead }),
    ...(q.cut.length > 0 && { dead_links: q.cut }),
    ...(q.site.length > 0 && { site_failure: q.site }),
  };
}

/** The first request: the counts of both readings, without items. */
export function summaryRequest(view: ImpactView): ImpactRequestBody {
  return {
    ...questionBody(view),
    mode: "both",
    shape: "summary",
    ...(view.hops !== undefined && { max_hops: view.hops }),
    max_items: view.max,
  };
}

/** The request of one page of items of the reading chosen; the next page carries the cursor of the page before. */
export function itemsRequest(view: ImpactView, cursor?: string): ImpactRequestBody {
  return {
    ...questionBody(view),
    mode: view.reading,
    shape: "items",
    ...(view.hops !== undefined && { max_hops: view.hops }),
    max_items: view.max,
    limit: Math.min(ITEMS_PER_REQUEST, view.max),
    ...(cursor !== undefined && { cursor }),
  };
}
