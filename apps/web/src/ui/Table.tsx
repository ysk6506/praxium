import { type ComponentProps, useCallback, useEffect, useRef, useState } from "react";
import { cx } from "./cx";

type Overflow = { readonly start: boolean; readonly end: boolean };

/** Which sides of a scrolling frame have more to show: the content is wider than the frame, and the frame is not at that edge. */
function overflowOf(frame: HTMLElement): Overflow {
  const hidden = frame.scrollWidth - frame.clientWidth;
  if (hidden <= 1) return { start: false, end: false };
  return { start: frame.scrollLeft > 1, end: frame.scrollLeft < hidden - 1 };
}

/**
 * A dense table inside a frame that scrolls sideways when the columns do not fit. When it does, a fade on the edge
 * that has more and a line under the table say so: a scroll bar that appears only on hover does not.
 */
export function Table({ className, children, ...rest }: ComponentProps<"table">) {
  const frame = useRef<HTMLDivElement>(null);
  const [overflow, setOverflow] = useState<Overflow>({ start: false, end: false });
  const measure = useCallback(() => {
    const element = frame.current;
    if (!element) return;
    const next = overflowOf(element);
    setOverflow((now) => (now.start === next.start && now.end === next.end ? now : next));
  }, []);
  useEffect(() => {
    measure();
    const element = frame.current;
    if (!element || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    if (element.firstElementChild) observer.observe(element.firstElementChild);
    return () => observer.disconnect();
  }, [measure]);
  return (
    <div>
      <div className="relative rounded-lg border border-line bg-surface">
        <div
          ref={frame}
          onScroll={measure}
          className="overflow-x-auto rounded-lg"
          // Where the columns do not fit, the frame scrolls: a keyboard must be able to reach it (WCAG 2.1.1).
          {...(overflow.start || overflow.end
            ? { tabIndex: 0, role: "region", "aria-label": rest["aria-label"] }
            : {})}
        >
          <table className={cx("w-full border-collapse text-left text-sm", className)} {...rest}>
            {children}
          </table>
        </div>
        {overflow.start ? (
          <span
            aria-hidden
            className="pointer-events-none absolute inset-y-0 left-0 w-8 rounded-l-lg bg-linear-to-r from-surface to-transparent"
          />
        ) : null}
        {overflow.end ? (
          <span
            aria-hidden
            className="pointer-events-none absolute inset-y-0 right-0 w-8 rounded-r-lg bg-linear-to-l from-surface to-transparent"
          />
        ) : null}
      </div>
      {overflow.start || overflow.end ? (
        <p className="mt-1 text-xs text-muted">표가 넓어 가로로 스크롤할 수 있습니다.</p>
      ) : null}
    </div>
  );
}

export function THead(props: ComponentProps<"thead">) {
  return <thead className="bg-sunken" {...props} />;
}

export function TBody(props: ComponentProps<"tbody">) {
  return <tbody className="divide-y divide-line" {...props} />;
}

export function Tr({ className, ...rest }: ComponentProps<"tr">) {
  return <tr className={cx("hover:bg-sunken/60", className)} {...rest} />;
}

export function Th({ className, ...rest }: ComponentProps<"th">) {
  return (
    <th
      scope="col"
      className={cx(
        "whitespace-nowrap border-b border-line px-3 py-2 text-xs font-semibold text-muted",
        className,
      )}
      {...rest}
    />
  );
}

export interface SortThProps {
  readonly sort: "ascending" | "descending" | undefined;
  readonly onSort: () => void;
  readonly className?: string;
  readonly children: string;
}

/** A column header that sorts the table when it is pressed. The arrow says which way, and a screen reader hears it too. */
export function SortTh({ sort, onSort, className, children }: SortThProps) {
  return (
    <Th aria-sort={sort ?? "none"} className={className}>
      <button
        type="button"
        onClick={onSort}
        className="inline-flex cursor-pointer items-center gap-1 rounded-sm font-semibold hover:text-fg"
      >
        {children}
        <span aria-hidden className={sort ? "text-accent" : "text-subtle"}>
          {sort === "ascending" ? "▲" : sort === "descending" ? "▼" : "↕"}
        </span>
      </button>
    </Th>
  );
}

export function Td({ className, ...rest }: ComponentProps<"td">) {
  return <td className={cx("px-3 py-1.5 align-top", className)} {...rest} />;
}
