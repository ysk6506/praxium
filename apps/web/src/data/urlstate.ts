/**
 * The state of the type and object screens in the query of the address (ADR-0042): what a person would expect the back
 * button, a reload and a shared link to keep. Pure functions from the pairs to a view and back; a view at its default
 * writes no pair, so the plain address is the default screen. A value that does not fit (a page size the screen does
 * not offer, a page number that is not a number) is read as the default instead of failing.
 */

import type { TypeEntry } from "./catalog";
import type { StorageLike } from "./connection";
import { layerName } from "./display";
import { PAGE_SIZES } from "./pager";

export interface ObjectsView {
  readonly size: number;
  /** The 1-based number of the page. */
  readonly page: number;
  /** Where the page starts: the API's cursor, absent on the first page. */
  readonly cursor: string | undefined;
  /** The attribute columns picked; absent means the default columns. */
  readonly columns: readonly string[] | undefined;
}

const positive = (text: string | null): number | undefined => {
  if (text === null || !/^\d+$/.test(text)) return undefined;
  const n = Number(text);
  return n >= 1 ? n : undefined;
};

export function readObjectsView(params: URLSearchParams): ObjectsView {
  const size = positive(params.get("size"));
  const cursor = params.get("cursor") || undefined;
  const page = positive(params.get("page")) ?? 1;
  const cols = params.get("cols");
  // A page after the first is reached by its cursor; a number without one is the first page.
  const later = cursor !== undefined && page > 1;
  return {
    size: PAGE_SIZES.find((s) => s === size) ?? PAGE_SIZES[0],
    page: later ? page : 1,
    cursor: later ? cursor : undefined,
    columns: cols === null ? undefined : cols.split(",").filter((c) => c !== ""),
  };
}

export function objectsViewParams(view: ObjectsView): Record<string, string | undefined> {
  return {
    size: view.size === PAGE_SIZES[0] ? undefined : String(view.size),
    page: view.page > 1 ? String(view.page) : undefined,
    cursor: view.page > 1 ? view.cursor : undefined,
    cols: view.columns === undefined ? undefined : view.columns.join(","),
  };
}

export type TypeSortKey = "name" | "layer" | "count";
export type SortDirection = "ascending" | "descending";

export interface TypesView {
  readonly text: string;
  readonly onlyFilled: boolean;
  readonly sort: { readonly key: TypeSortKey; readonly direction: SortDirection } | undefined;
}

const SORT_KEYS: readonly TypeSortKey[] = ["name", "layer", "count"];

export function readTypesView(params: URLSearchParams): TypesView {
  const key = SORT_KEYS.find((k) => k === params.get("sort"));
  const direction: SortDirection = params.get("dir") === "desc" ? "descending" : "ascending";
  return {
    text: params.get("q") ?? "",
    onlyFilled: params.get("all") !== "1",
    sort: key ? { key, direction } : undefined,
  };
}

export function typesViewParams(view: TypesView): Record<string, string | undefined> {
  return {
    q: view.text === "" ? undefined : view.text,
    all: view.onlyFilled ? undefined : "1",
    sort: view.sort?.key,
    dir: view.sort?.direction === "descending" ? "desc" : undefined,
  };
}

/** The first click on a column sorts it (the counts from the largest), the second reverses, the third is the default. */
export function nextSort(current: TypesView["sort"], key: TypeSortKey): TypesView["sort"] {
  const first: SortDirection = key === "count" ? "descending" : "ascending";
  if (current?.key !== key) return { key, direction: first };
  if (current.direction === first) {
    return { key, direction: first === "ascending" ? "descending" : "ascending" };
  }
  return undefined;
}

const KO = "ko";

/** The types in the order of the sort; without one, the order they came in (by layer, then the largest first). */
export function sortTypes(types: readonly TypeEntry[], sort: TypesView["sort"]): TypeEntry[] {
  if (!sort) return [...types];
  const sign = sort.direction === "ascending" ? 1 : -1;
  const compare = (a: TypeEntry, b: TypeEntry): number => {
    if (sort.key === "count") return a.count - b.count;
    if (sort.key === "layer") return layerName(a.layer).localeCompare(layerName(b.layer), KO);
    return a.name.localeCompare(b.name, "en");
  };
  return [...types].sort((a, b) => sign * compare(a, b) || a.name.localeCompare(b.name, "en"));
}

/**
 * The cursors of the pages already seen, by page number, kept for the tab: a cursor only goes forward, so "previous"
 * needs the cursor of the page before, and after a reload the address holds the current page only.
 */
export interface Trail {
  get(page: number): string | undefined;
  set(page: number, cursor: string): void;
}

export function createTrail(storage: StorageLike | undefined, key: string): Trail {
  const read = (): Record<string, string> => {
    try {
      const parsed: unknown = JSON.parse(storage?.getItem(key) ?? "{}");
      return typeof parsed === "object" && parsed !== null
        ? (parsed as Record<string, string>)
        : {};
    } catch {
      return {};
    }
  };
  let memory = read();
  return {
    get: (page) => memory[String(page)],
    set(page, cursor) {
      memory = { ...memory, [String(page)]: cursor };
      try {
        storage?.setItem(key, JSON.stringify(memory));
      } catch {
        // The trail stays in memory for this page.
      }
    },
  };
}

/** The storage of the tab, or nothing when the browser does not give it out. */
export function tabStorage(): StorageLike | undefined {
  try {
    return globalThis.sessionStorage;
  } catch {
    return undefined;
  }
}
