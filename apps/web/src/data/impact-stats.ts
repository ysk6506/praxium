/**
 * The numbers of an impact tree (ADR-0051): how many objects each result has, how many appear at each step of the
 * spread, which column a step reaches first. Counted from the items that were received, so they are the numbers of the
 * picture and the list; the totals of the whole answer are the server's (`counts_by_result`), shown beside them.
 */

import { gradeRank, impactGradeName } from "./impact-grades";
import type { ImpactTree } from "./impact-tree";

export interface GradeCount {
  readonly grade: string;
  readonly count: number;
}

const bySeverity = (a: GradeCount, b: GradeCount): number =>
  gradeRank(a.grade) - gradeRank(b.grade) || (a.grade < b.grade ? -1 : a.grade > b.grade ? 1 : 0);

const toList = (counts: ReadonlyMap<string, number>): GradeCount[] =>
  [...counts].map(([grade, count]) => ({ grade, count })).sort(bySeverity);

/** The items of the tree by result, the most severe first. */
export function countByGrade(tree: ImpactTree): GradeCount[] {
  const counts = new Map<string, number>();
  for (const node of tree.nodes.values()) {
    if (node.grade !== undefined) counts.set(node.grade, (counts.get(node.grade) ?? 0) + 1);
  }
  return toList(counts);
}

export interface StepCount {
  readonly step: number;
  /** How many nodes appear at this step, of any kind. */
  readonly nodes: number;
  /** The items among them by result. */
  readonly grades: readonly GradeCount[];
}

/** One entry for each step from 0 to the last, also the steps in which nothing new appears. */
export function countByStep(tree: ImpactTree): StepCount[] {
  let last = 0;
  for (const node of tree.nodes.values()) last = Math.max(last, node.step);
  const nodes: number[] = new Array(last + 1).fill(0);
  const grades: Map<string, number>[] = Array.from({ length: last + 1 }, () => new Map());
  for (const node of tree.nodes.values()) {
    nodes[node.step] = (nodes[node.step] ?? 0) + 1;
    const at = grades[node.step];
    if (at && node.grade !== undefined) at.set(node.grade, (at.get(node.grade) ?? 0) + 1);
  }
  return nodes.map((count, step) => ({
    step,
    nodes: count,
    grades: toList(grades[step] ?? new Map()),
  }));
}

/** The last step of the spread. */
export function lastStep(tree: ImpactTree): number {
  let last = 0;
  for (const node of tree.nodes.values()) last = Math.max(last, node.step);
  return last;
}

/** The step at which the spread first reaches each column: the column's head is lit then. */
export function laneFirstStep(tree: ImpactTree): ReadonlyMap<string, number> {
  const first = new Map<string, number>();
  for (const node of tree.nodes.values()) {
    const seen = first.get(node.layer);
    if (seen === undefined || node.step < seen) first.set(node.layer, node.step);
  }
  return first;
}

/** What a screen reader hears of a step: "단계 3 / 7: 새로 영향받음 중단 12, 이중화 상실(직접) 4". */
export function describeStep(counts: readonly StepCount[], step: number): string {
  const last = Math.max(0, counts.length - 1);
  const at = counts[Math.min(Math.max(0, step), last)];
  const head = `단계 ${at?.step ?? 0} / ${last}`;
  const grades = at?.grades ?? [];
  if (grades.length === 0) return `${head}: 새로 영향받은 객체 없음`;
  const parts = grades.map((g) => `${impactGradeName(g.grade)} ${g.count}`);
  return `${head}: 새로 영향받음 ${parts.join(", ")}`;
}
