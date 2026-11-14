import type { ReactNode } from "react";
import { cx } from "./cx";

export type BadgeTone = "neutral" | "ok" | "warn" | "danger" | "accent";

const TONES: Readonly<Record<BadgeTone, string>> = {
  neutral: "bg-sunken text-muted border-line",
  ok: "bg-ok-soft text-ok border-transparent",
  warn: "bg-warn-soft text-warn border-transparent",
  danger: "bg-danger-soft text-danger border-transparent",
  accent: "bg-accent-soft text-accent border-transparent",
};

export function Badge({
  tone = "neutral",
  title,
  children,
}: {
  readonly tone?: BadgeTone;
  /** What the badge stands for when the name is not enough, such as the code the name was made from. */
  readonly title?: string;
  readonly children: ReactNode;
}) {
  return (
    <span
      title={title}
      className={cx(
        "inline-flex items-center whitespace-nowrap rounded-sm border px-1.5 text-xs font-medium forced-colors:border-current",
        TONES[tone],
      )}
    >
      {children}
    </span>
  );
}
