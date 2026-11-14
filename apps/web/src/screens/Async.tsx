import type { ReactNode } from "react";
import {
  type AsyncResult,
  type ErrorView,
  errorDetails,
  errorFacts,
  type Names,
  needsToken,
  routeHash,
  useEntryMode,
  withAccessMode,
  withNames,
} from "../data";
import { Button, ButtonLink, ErrorState, LoadingState } from "../ui";

export interface AsyncProps<T> {
  readonly result: AsyncResult<T>;
  readonly loadingLabel?: string;
  /** What the screen asked for, so "not found" can say what was not found. */
  readonly names?: Names;
  /** Where to go when what was asked for does not exist. */
  readonly back?: { readonly label: string; readonly href: string };
  /** A way out of an error that "try again" does not fix; nothing means the usual action. */
  readonly recover?: (error: ErrorView) => ReactNode;
  readonly children: (data: T) => ReactNode;
}

/** Draw a read: the spinner while there is nothing yet, the error with its cause and what to do, or the data. */
export function Async<T>({ result, loadingLabel, names, back, recover, children }: AsyncProps<T>) {
  const mode = useEntryMode();
  const { state } = result;
  if (state.status === "error") {
    const error = withNames(withAccessMode(state.error, mode), names ?? {});
    const own = recover?.(error);
    let action: ReactNode;
    if (own) action = own;
    else if (needsToken(error)) {
      action = (
        <ButtonLink variant="primary" href={routeHash({ page: "settings" })}>
          접속 설정으로 이동
        </ButtonLink>
      );
    } else if (error.kind === "notFound" && back) {
      action = (
        <ButtonLink variant="primary" href={back.href}>
          {back.label}
        </ButtonLink>
      );
    } else action = <Button onClick={result.reload}>다시 시도</Button>;
    return (
      <ErrorState
        title={error.title}
        message={error.message}
        hint={error.hint}
        facts={errorFacts(error)}
        details={errorDetails(error)}
        action={action}
      />
    );
  }
  if (state.data === undefined) return <LoadingState label={loadingLabel} />;
  return <>{children(state.data)}</>;
}
