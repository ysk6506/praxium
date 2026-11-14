import { cx } from "./cx";
import { Glyph, type GlyphShape, type GlyphTone } from "./Glyph";

export interface GradeMarkProps {
  readonly shape: GlyphShape;
  readonly tone: GlyphTone;
  readonly dashed?: boolean;
  /** The word for the grade, written beside the figure. A figure with no word beside it is not a mark. */
  readonly name: string;
  /** What the mark says when it has more to tell than the name does (such as the code the name was made from). */
  readonly title?: string;
  /** How many objects have this grade, written after the name. */
  readonly count?: number;
  readonly className?: string;
}

/**
 * A grade as a shape, a word and a color together (WCAG 1.4.1). The same mark stands in the legend, the list and the
 * graph, so a person learns it once. The figure is decoration; the word beside it is what a screen reader hears.
 */
export function GradeMark({ shape, tone, dashed, name, title, count, className }: GradeMarkProps) {
  return (
    <span
      title={title}
      className={cx("inline-flex items-center gap-1.5 whitespace-nowrap", className)}
    >
      <svg aria-hidden="true" width={16} height={16} viewBox="0 0 16 16" className="shrink-0">
        <Glyph shape={shape} tone={tone} dashed={dashed} x={8} y={8} r={5.5} />
      </svg>
      <span>{name}</span>
      {count === undefined ? null : <span className="tabular-nums text-muted">{count}</span>}
    </span>
  );
}
