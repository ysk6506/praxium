/**
 * The numbers the server counted over the whole answer (ADR-0054): how many objects have each result, how many have no
 * supplier for a channel they need, how many are single points of failure. They come with the summary and are not the
 * number of items received, which can be fewer (the limit, a stop); the screen shows these first and says so when the
 * items are fewer.
 */
import type { ImpactData } from "../api";
import { gradeRank } from "./impact-grades";
import type { ImpactReading } from "./impact-input";
import type { GradeCount } from "./impact-stats";

const bySeverity = (a: GradeCount, b: GradeCount): number =>
  gradeRank(a.grade) - gradeRank(b.grade) || (a.grade < b.grade ? -1 : a.grade > b.grade ? 1 : 0);

/** The results of the answer with how many objects have each, the most severe first; those with none are left out. */
export function serverCounts(data: ImpactData, reading: ImpactReading): GradeCount[] {
  const counts =
    data.direction === "upstream"
      ? (data.upstream?.counts_by_effect ?? {})
      : (data[reading]?.counts_by_result ?? {});
  return Object.entries(counts)
    .filter(([, count]) => count > 0)
    .map(([grade, count]) => ({ grade, count }))
    .sort(bySeverity);
}

/** How many objects the answer has in all. */
export const totalCount = (data: ImpactData, reading: ImpactReading): number =>
  data.direction === "upstream"
    ? (data.upstream?.total ?? 0)
    : serverCounts(data, reading).reduce((n, c) => n + c.count, 0);

/** How many objects need a channel that nothing supplies; asked downstream only. */
export const unfilledCount = (data: ImpactData, reading: ImpactReading): number | undefined =>
  data.direction === "upstream" ? undefined : data[reading]?.unfilled_channel_objects;

/** How many dependencies the target does not survive losing alone; asked upstream only. */
export const singlePoints = (data: ImpactData): number | undefined =>
  data.direction === "upstream" ? (data.upstream?.single_points_of_failure ?? 0) : undefined;
