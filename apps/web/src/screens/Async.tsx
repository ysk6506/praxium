import type { ReactNode } from "react";
import { type AsyncResult, needsToken, routeHash } from "../data";
import { Button, ButtonLink, ErrorState, LoadingState } from "../ui";

export interface AsyncProps<T> {
  readonly result: AsyncResult<T>;
  readonly loadingLabel?: string;
  readonly children: (data: T) => ReactNode;
}

/** Draw a read: the spinner while there is nothing yet, the error with what to do, or the data. */
export function Async<T>({ result, loadingLabel, children }: AsyncProps<T>) {
  const { state } = result;
  if (state.status === "error") {
    const { error } = state;
    const details: [string, string][] = [];
    if (error.code) details.push(["코드", error.code]);
    if (error.status) details.push(["HTTP", String(error.status)]);
    if (error.traceId) details.push(["추적 ID", error.traceId]);
    return (
      <ErrorState
        title={error.title}
        message={error.message}
        hint={error.hint}
        details={details}
        action={
          needsToken(error) ? (
            <ButtonLink variant="primary" href={routeHash({ page: "settings" })}>
              접속 설정으로 이동
            </ButtonLink>
          ) : (
            <Button onClick={result.reload}>다시 시도</Button>
          )
        }
      />
    );
  }
  if (state.data === undefined) return <LoadingState label={loadingLabel} />;
  return <>{children(state.data)}</>;
}
