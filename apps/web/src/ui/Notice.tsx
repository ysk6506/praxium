import type { ReactNode } from "react";
import { cx } from "./cx";

export type NoticeTone = "warn" | "info";

const TONES: Readonly<
  Record<NoticeTone, { frame: string; label: string; mark: string; text: string }>
> = {
  warn: { frame: "border-warn/60 bg-warn-soft", label: "주의", mark: "text-warn", text: "text-fg" },
  info: {
    frame: "border-line-strong bg-sunken",
    label: "참고",
    mark: "text-muted",
    text: "text-muted",
  },
};

export interface NoticeProps {
  readonly tone: NoticeTone;
  readonly title: string;
  readonly children?: ReactNode;
  /** What a person can do about it, such as asking again with a higher limit. */
  readonly action?: ReactNode;
}

/**
 * A note beside an answer: what limits it or what it rests on. The kind is written ("주의", "참고") as well as shown by
 * the color, and the frame has a border that stays when a forced color mode takes the background away.
 */
export function Notice({ tone, title, children, action }: NoticeProps) {
  const look = TONES[tone];
  return (
    <section
      aria-label={`${look.label}: ${title}`}
      className={cx("rounded-md border px-3 py-2 text-sm forced-colors:border-current", look.frame)}
    >
      <p className="font-semibold">
        <span className={cx("mr-1.5", look.mark)}>{look.label}</span>
        {title}
      </p>
      {children ? <p className={cx("mt-0.5 max-w-prose", look.text)}>{children}</p> : null}
      {action ? <div className="mt-2">{action}</div> : null}
    </section>
  );
}
