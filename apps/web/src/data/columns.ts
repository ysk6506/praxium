/** Which attributes a table of one type shows as columns. */
import type { Property } from "../api";

/** Attributes that say little in a column: the name is the first column already, the rest are long or nested. */
const SKIPPED = new Set(["name", "display_name", "description", "labels", "tags"]);

export const DEFAULT_COLUMN_COUNT = 5;

/** The properties a person can pick as columns: those that have a plain value. */
export function columnCandidates(properties: readonly Property[]): Property[] {
  return properties.filter((p) => !SKIPPED.has(p.name) && p.status !== "retired");
}

/** The first columns shown: required properties first, then the others in the order of the model. */
export function defaultColumns(
  properties: readonly Property[],
  count: number = DEFAULT_COLUMN_COUNT,
): string[] {
  const candidates = columnCandidates(properties);
  const required = candidates.filter((p) => p.required);
  const rest = candidates.filter((p) => !p.required);
  return [...required, ...rest].slice(0, count).map((p) => p.name);
}

/**
 * The candidates in the order the table shows its columns and the dialog lists them: the required properties first,
 * then the others in the order of the model. One order for both, so the dialog is the table's header rotated.
 */
export function orderedColumns(properties: readonly Property[]): Property[] {
  const candidates = columnCandidates(properties);
  return [...candidates.filter((p) => p.required), ...candidates.filter((p) => !p.required)];
}
