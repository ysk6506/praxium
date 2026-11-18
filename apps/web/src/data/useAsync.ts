/** One read of the API as a state a screen can draw: loading, ready or failed. */
import { useCallback, useEffect, useRef, useState } from "react";
import { createClient, type Requester } from "../api";
import { useToken } from "./connection";
import { describeError, type ErrorView } from "./errors";
import { onInvalidate } from "./invalidation";

export type AsyncState<T> =
  | { readonly status: "loading"; readonly data: T | undefined; readonly attempt: number }
  | { readonly status: "ready"; readonly data: T }
  | { readonly status: "error"; readonly error: ErrorView; readonly data: T | undefined };

export interface AsyncResult<T> {
  readonly state: AsyncState<T>;
  /** Read again, keeping what is shown until the answer comes. The state is `loading` meanwhile. */
  readonly reload: () => void;
}

/** What `useAsync` gives: the state and the two ways to read again. */
export interface RefreshableResult<T> extends AsyncResult<T> {
  /** Read again without showing `loading`: a `ready` state stays `ready` until the new answer replaces its data. */
  readonly refresh: () => void;
}

/** Read again and again while the answer says the work is not done (a run that has not finished). */
export interface PollOptions<T> {
  /** How long to wait after an answer before reading again. */
  readonly intervalMs: number;
  /** Stop reading when this says so about the latest answer. Without it the reading goes on. */
  readonly until?: (data: T) => boolean;
}

export interface AsyncOptions<T> {
  /** Read again on a timer, until the answer says it is done. */
  readonly poll?: PollOptions<T>;
  /** The topics this read depends on: a write that invalidates one of them makes this read `refresh()` (see `invalidation.ts`). */
  readonly refreshOn?: readonly string[];
}

const isAbort = (error: unknown): boolean =>
  error instanceof DOMException && error.name === "AbortError";

/**
 * Run `load` when it changes (and the token), and again on `reload()`. Callers give `load` its identity with
 * `useCallback`, so it changes exactly when its inputs do. An answer that comes after that, or after the screen left,
 * is dropped. `load` gets a client that carries the token and the abort signal. What was shown stays while a read
 * repeats for the same token, and goes when the token changes: it was read with another token (another server run, a
 * pasted address), and showing it under the new one would mislead.
 *
 * `reload()` shows `loading` while it reads; `refresh()` does not, for a read that repeats under the person's eyes.
 * With `poll`, a `ready` answer is followed by a `refresh()` after `poll.intervalMs`, until `poll.until` is true for the
 * answer. A hidden tab does not read: the wait ends when the tab shows again. A failed read stops the repeating
 * (the screen shows the failure and its own retry), and so does `enabled` going false. With `refreshOn`, a write that
 * invalidates one of the topics makes the read `refresh()`.
 */
export function useAsync<T>(
  load: (client: Requester, signal: AbortSignal) => Promise<T>,
  enabled = true,
  options: AsyncOptions<T> = {},
): RefreshableResult<T> {
  const { poll, refreshOn } = options;
  const token = useToken();
  const [state, setState] = useState<AsyncState<T>>({
    status: "loading",
    data: undefined,
    attempt: 0,
  });
  const [round, setRound] = useState(0);
  const readWith = useRef(token);
  const readLoad = useRef(load);
  const quiet = useRef(false);

  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    const sameToken = readWith.current === token;
    readWith.current = token;
    // A quiet read keeps `ready` only for the same question and the same token; anything else shows `loading`.
    const silent = quiet.current && sameToken && readLoad.current === load;
    quiet.current = false;
    readLoad.current = load;
    setState((previous) =>
      silent && previous.status === "ready"
        ? previous
        : { status: "loading", data: sameToken ? previous.data : undefined, attempt: round },
    );
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
  const refresh = useCallback(() => {
    quiet.current = true;
    setRound((n) => n + 1);
  }, []);

  usePolling(poll, enabled, state, refresh);
  useRefreshOn(refreshOn, enabled, refresh);

  return { state, reload, refresh };
}

/** Read again on a timer while the answer is ready and not done; a hidden tab waits until it shows again. */
function usePolling<T>(
  poll: PollOptions<T> | undefined,
  enabled: boolean,
  state: AsyncState<T>,
  refresh: () => void,
): void {
  const until = useRef(poll?.until);
  until.current = poll?.until;
  const intervalMs = poll?.intervalMs;
  useEffect(() => {
    if (intervalMs === undefined || !enabled || state.status !== "ready") return;
    if (until.current?.(state.data)) return;
    let waiting = false;
    const hidden = (): boolean => document.visibilityState === "hidden";
    const timer = setTimeout(() => {
      if (hidden()) waiting = true;
      else refresh();
    }, intervalMs);
    const onVisibility = (): void => {
      if (waiting && !hidden()) {
        waiting = false;
        refresh();
      }
    };
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [state, intervalMs, enabled, refresh]);
}

/** Read again when a write invalidates one of the topics. */
function useRefreshOn(
  refreshOn: readonly string[] | undefined,
  enabled: boolean,
  refresh: () => void,
): void {
  const topics = refreshOn?.join("\n") ?? "";
  useEffect(() => {
    if (!enabled || topics === "") return;
    const wanted = new Set(topics.split("\n"));
    return onInvalidate((topic) => {
      if (wanted.has(topic)) refresh();
    });
  }, [topics, enabled, refresh]);
}
