import type { ImpactNotice, NoticeAction } from "../../data";
import { Button, Notice } from "../../ui";

export interface NoticesProps {
  readonly notices: readonly ImpactNotice[];
  readonly onAction: (action: NoticeAction) => void;
  /** What the calculation does not see, as the server wrote it. */
  readonly limits: readonly string[];
}

/** What limits the answer and what it rests on, each with the button that asks again where there is one. */
export function Notices({ notices, onAction, limits }: NoticesProps) {
  return (
    <div className="flex flex-col gap-2">
      {notices.map((n) => (
        <Notice
          key={n.id}
          tone={n.tone}
          title={n.title}
          action={
            n.action ? (
              <Button onClick={() => n.action && onAction(n.action)}>{n.action.label}</Button>
            ) : undefined
          }
        >
          {n.text}
        </Notice>
      ))}
      {limits.length > 0 ? (
        <details className="text-sm">
          <summary className="cursor-pointer text-muted">이 계산이 보지 않는 것</summary>
          <ul className="mt-1 flex list-disc flex-col gap-1 pl-5 text-muted">
            {limits.map((text) => (
              <li key={text}>{text}</li>
            ))}
          </ul>
        </details>
      ) : null}
    </div>
  );
}
