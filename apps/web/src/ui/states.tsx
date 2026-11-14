import type { ReactNode } from "react";
import { cx } from "./cx";

function Frame({ children, tone }: { readonly children: ReactNode; readonly tone?: "danger" }) {
  return (
    <div
      className={cx(
        "flex flex-col items-center gap-1 rounded-lg border border-dashed px-6 py-10 text-center",
        tone === "danger" ? "border-danger/50 bg-danger-soft" : "border-line-strong bg-surface",
      )}
    >
      {children}
    </div>
  );
}

/** Something is being read. `role="status"` makes a screen reader say it. */
export function LoadingState({ label = "불러오는 중" }: { readonly label?: string }) {
  return (
    <output className="flex items-center justify-center gap-2 py-10 text-sm text-muted">
      <span
        aria-hidden
        className="size-4 animate-spin rounded-full border-2 border-line-strong border-t-accent"
      />
      {label}
    </output>
  );
}

export interface EmptyStateProps {
  readonly title: string;
  readonly children?: ReactNode;
  readonly action?: ReactNode;
}

/** There is nothing to show, and that is an answer, not a failure. */
export function EmptyState({ title, children, action }: EmptyStateProps) {
  return (
    <Frame>
      <p className="font-semibold text-fg">{title}</p>
      {children ? <p className="max-w-prose text-sm text-muted">{children}</p> : null}
      {action ? <div className="mt-3">{action}</div> : null}
    </Frame>
  );
}

export interface ErrorStateProps {
  readonly title: string;
  readonly message: string;
  readonly hint?: string;
  /** What the server said about the cause, to be read at once: the field, its message. */
  readonly facts?: readonly (readonly [label: string, value: string])[];
  /** Facts for whoever has to look into it (code, status, trace id), folded under "자세히". */
  readonly details?: readonly (readonly [label: string, value: string])[];
  readonly action?: ReactNode;
}

/** The read failed. The title says what, the hint says what to do, the details are for the person who reports it. */
export function ErrorState({ title, message, hint, facts, details, action }: ErrorStateProps) {
  return (
    <div role="alert">
      <Frame tone="danger">
        <p className="font-semibold text-danger">{title}</p>
        <p className="max-w-prose text-sm text-fg">{message}</p>
        {hint ? <p className="max-w-prose text-sm text-muted">{hint}</p> : null}
        {facts && facts.length > 0 ? (
          <dl className="mt-1 flex max-w-prose flex-col gap-0.5 text-sm">
            {facts.map(([label, value]) => (
              <div key={label} className="flex flex-wrap justify-center gap-x-2">
                <dt className="text-muted">{label}</dt>
                <dd className="font-mono break-all">{value}</dd>
              </div>
            ))}
          </dl>
        ) : null}
        {details && details.length > 0 ? (
          <details className="mt-2 max-w-full text-xs text-muted">
            <summary className="cursor-pointer select-none">자세히</summary>
            <dl className="mt-1 flex flex-col gap-0.5">
              {details.map(([label, value]) => (
                <div key={`${label}=${value}`} className="flex flex-wrap justify-center gap-x-2">
                  <dt>{label}</dt>
                  <dd className="font-mono break-all">{value}</dd>
                </div>
              ))}
            </dl>
          </details>
        ) : null}
        {action ? <div className="mt-3">{action}</div> : null}
      </Frame>
    </div>
  );
}
