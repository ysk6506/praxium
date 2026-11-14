/**
 * The grades of an impact answer (kg-queries.md 5): the seven results of an affected object, in the order of their
 * severity, and what each looks like. The look is a shape, a word and a tone together, so no grade is told apart by
 * color alone (WCAG 1.4.1). This file is data, not drawing: `ui` turns a shape name into a figure, and the tone names
 * are the color tokens of `tokens.css`. The names are the glossary's words; "이중화 상실" is how a person says that an
 * object lost its redundancy (ADR-0051).
 */

export const IMPACT_GRADES = [
  "down",
  "interrupted",
  "unknown",
  "exposed_direct",
  "exposed_inherited",
  "degraded",
  "unmanageable",
] as const;

export type ImpactGrade = (typeof IMPACT_GRADES)[number];

/** The figures a grade can be drawn as. `ui` knows how to draw each one. */
export type GradeShape =
  | "double-circle"
  | "square-solid"
  | "square-dot"
  | "circle-dashed"
  | "triangle-solid"
  | "triangle-open"
  | "diamond"
  | "square-bar"
  | "circle-open";

/** The color tokens a grade may use. */
export type GradeTone = "danger" | "warn" | "accent" | "muted";

export interface GradeStyle {
  readonly name: string;
  readonly shape: GradeShape;
  readonly tone: GradeTone;
  /** A grade that is not known for sure is drawn with a dashed line. */
  readonly dashed: boolean;
}

export const GRADE_STYLES: Readonly<Record<ImpactGrade, GradeStyle>> = {
  down: { name: "중단", shape: "square-solid", tone: "danger", dashed: false },
  interrupted: { name: "일시 중단(페일오버)", shape: "square-dot", tone: "danger", dashed: false },
  unknown: { name: "알 수 없음", shape: "circle-dashed", tone: "muted", dashed: true },
  exposed_direct: {
    name: "이중화 상실(직접)",
    shape: "triangle-solid",
    tone: "warn",
    dashed: false,
  },
  exposed_inherited: {
    name: "이중화 상실(상속)",
    shape: "triangle-open",
    tone: "warn",
    dashed: false,
  },
  degraded: { name: "저하", shape: "diamond", tone: "warn", dashed: false },
  unmanageable: { name: "관리 불가", shape: "square-bar", tone: "accent", dashed: false },
};

/** The start of the question: the objects that failed. Not a result, so not in the list of grades. */
export const START_STYLE: GradeStyle = {
  name: "시작",
  shape: "double-circle",
  tone: "accent",
  dashed: false,
};

/** An upstream answer says "none" for a dependency whose failure does not reach the target. */
const NO_EFFECT_STYLE: GradeStyle = {
  name: "영향 없음",
  shape: "circle-open",
  tone: "muted",
  dashed: false,
};

/** Where a path passes through an object that is not an item of the answer. */
export const WAYPOINT_STYLE: GradeStyle = {
  name: "경유",
  shape: "circle-open",
  tone: "muted",
  dashed: false,
};

export const isImpactGrade = (code: string): code is ImpactGrade =>
  (IMPACT_GRADES as readonly string[]).includes(code);

/** The rank of a grade: 0 is the most severe. What is not a grade comes after all of them. */
export function gradeRank(code: string | undefined): number {
  if (code === undefined) return IMPACT_GRADES.length + 1;
  const at = (IMPACT_GRADES as readonly string[]).indexOf(code);
  return at < 0 ? IMPACT_GRADES.length : at;
}

/** The look of a result code. A code this version does not know is shown as it came, as an unknown one. */
export function gradeStyle(code: string | undefined): GradeStyle {
  if (code === undefined) return WAYPOINT_STYLE;
  if (isImpactGrade(code)) return GRADE_STYLES[code];
  if (code === "none") return NO_EFFECT_STYLE;
  return { name: code, shape: "circle-dashed", tone: "muted", dashed: true };
}

export const impactGradeName = (code: string | undefined): string => gradeStyle(code).name;

/** The grades of the answer that are drawn, most severe first, each once. */
export function sortedGrades(codes: Iterable<string>): string[] {
  return [...new Set(codes)].sort(
    (a, b) => gradeRank(a) - gradeRank(b) || (a < b ? -1 : a > b ? 1 : 0),
  );
}
