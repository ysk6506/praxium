/**
 * The list of an impact answer (ADR-0051): the same items as the picture, as rows in sections by result. The list is
 * the answer; the picture is another way to look at it, so every item received is a row, also those the picture
 * leaves out. Pure functions from the tree to rows, sections, a sort and a page.
 */

import { gradeRank, sortedGrades } from "./impact-grades";
import type { ImpactTree } from "./impact-tree";
import type { ImpactSortKey } from "./impact-view";

export const ROWS_PER_PAGE = 50;

export interface ImpactRow {
  readonly id: string;
  readonly name: string;
  readonly type: string;
  readonly layer: string;
  readonly grade: string;
  readonly hops: number;
  /** What the object lost: power, cooling, network, storage, service or management. */
  readonly lost: readonly string[];
  /** The object just before this one: its name, or nothing for a start or an object the caller may not read. */
  readonly via: string | undefined;
  readonly viaId: string | null;
  readonly viaHidden: boolean;
  /** The reading stands on an assumption. */
  readonly assumed: boolean;
  readonly delayed: boolean;
  readonly single: boolean;
}

/** The rows of the items of the tree, in the order of the tree. */
export function impactRows(tree: ImpactTree): ImpactRow[] {
  const rows: ImpactRow[] = [];
  for (const id of tree.order) {
    const node = tree.nodes.get(id);
    if (node === undefined || node.grade === undefined) continue;
    const parent = node.parent === null ? undefined : tree.nodes.get(node.parent);
    rows.push({
      id,
      name: node.name,
      type: node.type,
      layer: node.layer,
      grade: node.grade,
      hops: node.hops,
      lost: node.lost,
      via: parent && parent.kind !== "hidden" ? parent.name : undefined,
      viaId: parent && parent.kind !== "hidden" ? parent.id : null,
      viaHidden: parent?.kind === "hidden",
      assumed: node.assumed,
      delayed: node.delayed,
      single: node.single,
    });
  }
  return rows;
}

export interface RowSection {
  readonly grade: string;
  readonly rows: readonly ImpactRow[];
}

/** The rows in sections, the most severe result first; a section for each result that has rows. */
export function sectionsByGrade(rows: readonly ImpactRow[]): RowSection[] {
  const by = new Map<string, ImpactRow[]>();
  for (const row of rows) {
    const at = by.get(row.grade);
    if (at) at.push(row);
    else by.set(row.grade, [row]);
  }
  return sortedGrades(by.keys()).map((grade) => ({ grade, rows: by.get(grade) ?? [] }));
}

const compareText = (a: string, b: string): number => (a < b ? -1 : a > b ? 1 : 0);

const basisOf = (row: ImpactRow): number => (row.assumed ? 1 : 0);

function compareBy(key: ImpactSortKey, a: ImpactRow, b: ImpactRow): number {
  switch (key) {
    case "name":
      return compareText(a.name, b.name);
    case "type":
      return compareText(a.type, b.type);
    case "hops":
      return a.hops - b.hops;
    case "lost":
      return compareText(a.lost.join(","), b.lost.join(","));
    case "via":
      return compareText(a.via ?? "", b.via ?? "");
    case "basis":
      return basisOf(a) - basisOf(b);
  }
}

/** The rows sorted; without a sort, the most severe first, then the nearest, then by name and id. */
export function sortRows(
  rows: readonly ImpactRow[],
  sort: { readonly key: ImpactSortKey; readonly direction: "ascending" | "descending" } | undefined,
): ImpactRow[] {
  const sign = sort?.direction === "descending" ? -1 : 1;
  return [...rows].sort((a, b) => {
    const picked = sort ? sign * compareBy(sort.key, a, b) : 0;
    return (
      picked ||
      gradeRank(a.grade) - gradeRank(b.grade) ||
      a.hops - b.hops ||
      compareText(a.name, b.name) ||
      compareText(a.id, b.id)
    );
  });
}

export interface RowPage {
  readonly rows: readonly ImpactRow[];
  /** The page shown, 1-based, kept inside the pages there are. */
  readonly page: number;
  readonly pages: number;
  readonly total: number;
}

export function pageOfRows(
  rows: readonly ImpactRow[],
  page: number,
  size = ROWS_PER_PAGE,
): RowPage {
  const pages = Math.max(1, Math.ceil(rows.length / size));
  const shown = Math.min(Math.max(1, Math.floor(page)), pages);
  return {
    rows: rows.slice((shown - 1) * size, shown * size),
    page: shown,
    pages,
    total: rows.length,
  };
}
