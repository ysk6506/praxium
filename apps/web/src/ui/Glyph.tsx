import { cx } from "./cx";

/** The figures a mark can be drawn as. A figure says something by its shape, so it does not depend on its color. */
export type GlyphShape =
  | "double-circle"
  | "square-solid"
  | "square-dot"
  | "circle-dashed"
  | "triangle-solid"
  | "triangle-open"
  | "diamond"
  | "square-bar"
  | "circle-open";

/** The colors a mark may take: the tokens of the theme and no other. */
export type GlyphTone = "danger" | "warn" | "accent" | "muted";

const FILL: Readonly<Record<GlyphTone, string>> = {
  danger: "fill-danger",
  warn: "fill-warn",
  accent: "fill-accent",
  muted: "fill-muted",
};

const STROKE: Readonly<Record<GlyphTone, string>> = {
  danger: "stroke-danger",
  warn: "stroke-warn",
  accent: "stroke-accent",
  muted: "stroke-muted",
};

export interface GlyphProps {
  readonly shape: GlyphShape;
  readonly tone: GlyphTone;
  readonly dashed?: boolean;
  /** The center. */
  readonly x: number;
  readonly y: number;
  /** Half the width of the figure. */
  readonly r: number;
}

/** One figure inside an SVG, centered at a point. Decoration: the name of what it stands for is written beside it. */
export function Glyph({ shape, tone, dashed = false, x, y, r }: GlyphProps) {
  const line = cx(STROKE[tone], "stroke-[1.5]");
  const solid = cx(FILL[tone], line);
  const open = cx("fill-surface", line);
  const dash = dashed ? "2.5 2" : undefined;
  const top = `${x},${y - r}`;
  const triangle = `${top} ${x + r},${y + r - 1} ${x - r},${y + r - 1}`;
  switch (shape) {
    case "double-circle":
      return (
        <>
          <circle cx={x} cy={y} r={r} className={open} />
          <circle cx={x} cy={y} r={r * 0.5} className={solid} />
        </>
      );
    case "square-solid":
      return <rect x={x - r} y={y - r} width={r * 2} height={r * 2} className={solid} />;
    case "square-dot":
      return (
        <>
          <rect x={x - r} y={y - r} width={r * 2} height={r * 2} className={open} />
          <circle cx={x} cy={y} r={r * 0.35} className={solid} />
        </>
      );
    case "circle-dashed":
      return (
        <>
          <circle cx={x} cy={y} r={r} strokeDasharray={dash} className={open} />
          <text
            x={x}
            y={y + 3.5}
            textAnchor="middle"
            className={cx(FILL[tone], "stroke-0 text-[10px] font-bold")}
          >
            ?
          </text>
        </>
      );
    case "triangle-solid":
      return <polygon points={triangle} className={solid} />;
    case "triangle-open":
      return <polygon points={triangle} className={open} />;
    case "diamond":
      return (
        <polygon points={`${top} ${x + r},${y} ${x},${y + r} ${x - r},${y}`} className={solid} />
      );
    case "square-bar":
      return (
        <>
          <rect x={x - r} y={y - r} width={r * 2} height={r * 2} rx={3} className={open} />
          <line x1={x - r * 0.6} y1={y} x2={x + r * 0.6} y2={y} className={line} />
        </>
      );
    case "circle-open":
      return <circle cx={x} cy={y} r={r} strokeDasharray={dash} className={open} />;
  }
}
