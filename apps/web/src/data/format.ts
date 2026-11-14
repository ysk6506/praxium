/** How values of the API are written on a screen. Pure functions; the words are the glossary's. */

const GRADE_LABELS: Readonly<Record<string, string>> = {
  fact: "사실",
  assumed: "가정",
  unknown: "알 수 없음",
};

const BASIS_LABELS: Readonly<Record<string, string>> = {
  observed: "관측",
  declared: "기재",
  derived: "파생",
  configured: "설정",
};

const DIRECTION_LABELS: Readonly<Record<string, string>> = {
  out: "나가는 링크",
  in: "들어오는 링크",
  both: "양방향",
};

/** The value-less cell. */
export const EMPTY_CELL = "—";

/** The evidence grade as the glossary words it: 사실, 가정, 알 수 없음. Unknown grades are shown as they come. */
export const gradeLabel = (grade: string | null | undefined): string =>
  grade == null ? EMPTY_CELL : (GRADE_LABELS[grade] ?? grade);

export const basisLabel = (basis: string | null | undefined): string =>
  basis == null ? EMPTY_CELL : (BASIS_LABELS[basis] ?? basis);

export const directionLabel = (direction: string): string => DIRECTION_LABELS[direction] ?? direction;

/** Which tone a grade gets in a badge. */
export function gradeTone(grade: string | null | undefined): "ok" | "warn" | "neutral" {
  if (grade === "fact") return "ok";
  if (grade === "assumed") return "warn";
  return "neutral";
}

/** An attribute value on one line: text as it is, numbers and booleans plain, lists and maps as JSON. */
export function formatValue(value: unknown): string {
  if (value === null || value === undefined) return EMPTY_CELL;
  if (typeof value === "string") return value === "" ? EMPTY_CELL : value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return JSON.stringify(value);
}

/** A value with its unit, such as `3141.8 W`. */
export function formatQuantity(value: unknown, unit: string | undefined): string {
  const text = formatValue(value);
  return unit && text !== EMPTY_CELL ? `${text} ${unit}` : text;
}

const TIMESTAMP = /^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2}:\d{2})(?:\.\d+)?Z$/;

/** `2026-11-14T03:42:02Z` as `2026-11-14 03:42:02 UTC`. Anything else is shown as it came. */
export function formatTimestamp(value: string | null | undefined): string {
  if (!value) return EMPTY_CELL;
  const match = TIMESTAMP.exec(value);
  return match ? `${match[1]} ${match[2]} UTC` : value;
}

/** `obj_1f6bdfd7bf636df9364f3bfaba59e7f0` as `obj_1f6bdfd7…`, for places where the whole id would crowd. */
export function shortId(id: string, keep = 12): string {
  return id.length <= keep + 1 ? id : `${id.slice(0, keep)}…`;
}

/** A count with the thousands separated: `12,345`. */
export const formatCount = (count: number): string => count.toLocaleString("en-US");
