import type { ReactNode } from "react";
import { cx } from "./cx";

export type ResultTone = "success" | "error";

export interface Result {
  readonly tone: ResultTone;
  readonly title: string;
  readonly message?: ReactNode;
  /** What to do next, when there is something to do. */
  readonly hint?: ReactNode;
}

const LOOK: Readonly<Record<ResultTone, { frame: string; mark: string; word: string }>> = {
  success: { frame: "border-ok/60 bg-ok-soft", mark: "text-ok", word: "완료" },
  error: { frame: "border-danger/60 bg-danger-soft", mark: "text-danger", word: "오류" },
};

function Body({ result }: { readonly result: Result }) {
  const look = LOOK[result.tone];
  return (
    <div
      className={cx("rounded-md border px-3 py-2 text-sm forced-colors:border-current", look.frame)}
    >
      <p className="font-semibold">
        <span className={cx("mr-1.5", look.mark)}>{look.word}</span>
        {result.title}
      </p>
      {result.message ? <p className="mt-0.5 max-w-prose text-fg">{result.message}</p> : null}
      {result.hint ? <p className="mt-0.5 max-w-prose text-muted">{result.hint}</p> : null}
    </div>
  );
}

/**
 * Where the answer of a save shows, and is read out. Both places are in the page before anything is written into
 * them, which is what makes a screen reader speak when their content arrives: a success is polite (`role="status"`,
 * it waits for the sentence being read), a failure is an alert (it speaks at once). Put it near the submit button.
 * The failure's place can take focus (`data-form-result`), which a form falls back on when no field is to blame.
 */
export function ResultNotice({ result }: { readonly result: Result | undefined }) {
  return (
    <div className="flex flex-col gap-2">
      <output>{result?.tone === "success" ? <Body result={result} /> : null}</output>
      <div
        role="alert"
        tabIndex={-1}
        data-form-result={result?.tone === "error" ? "" : undefined}
        className="outline-none focus-visible:outline-2 focus-visible:outline-accent"
      >
        {result?.tone === "error" ? <Body result={result} /> : null}
      </div>
    </div>
  );
}
