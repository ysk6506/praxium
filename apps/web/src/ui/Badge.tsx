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
  children,
}: {
  readonly tone?: BadgeTone;
  readonly children: ReactNode;
}) {
  return (
    <span
      className={cx(
        "inline-flex items-center whitespace-nowrap rounded-sm border px-1.5 text-xs font-medium",
        TONES[tone],
      )}
    >
      {children}
    </span>
  );
}
