/** One read of the API as a state a screen can draw: loading, ready or failed. */
import { useCallback, useEffect, useRef, useState } from "react";
import { createClient, type Requester } from "../api";
import { useToken } from "./connection";
import { describeError, type ErrorView } from "./errors";

export type AsyncState<T> =
  | { readonly status: "loading"; readonly data: T | undefined; readonly attempt: number }
  | { readonly status: "ready"; readonly data: T }
  | { readonly status: "error"; readonly error: ErrorView; readonly data: T | undefined };

export interface AsyncResult<T> {
  readonly state: AsyncState<T>;
  /** Read again, keeping what is shown until the answer comes. */
  readonly reload: () => void;
}

const isAbort = (error: unknown): boolean =>
  error instanceof DOMException && error.name === "AbortError";

/**
 * Run `load` when it changes (and the token), and again on `reload()`. Callers give `load` its identity with
 * `useCallback`, so it changes exactly when its inputs do. An answer that comes after that, or after the screen left,
 * is dropped. `load` gets a client that carries the token and the abort signal. What was shown stays while a read
 * repeats for the same token, and goes when the token changes: it was read with another token (another server run, a
 * pasted address), and showing it under the new one would mislead.
 */
export function useAsync<T>(
  load: (client: Requester, signal: AbortSignal) => Promise<T>,
  enabled = true,
): AsyncResult<T> {
  const token = useToken();
  const [state, setState] = useState<AsyncState<T>>({
    status: "loading",
    data: undefined,
    attempt: 0,
  });
  const [round, setRound] = useState(0);
  const readWith = useRef(token);

  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    const sameToken = readWith.current === token;
    readWith.current = token;
    setState((previous) => ({
      status: "loading",
      data: sameToken ? previous.data : undefined,
      attempt: round,
    }));
    load(createClient({ token, signal: controller.signal }), controller.signal)
      .then((data) => {
        if (!controller.signal.aborted) setState({ status: "ready", data });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted || isAbort(error)) return;
        setState((previous) => ({
          status: "error",
          error: describeError(error),
          data: previous.data,
        }));
      });
    return () => controller.abort();
  }, [load, token, enabled, round]);

  const reload = useCallback(() => setRound((n) => n + 1), []);
  return { state, reload };
}
