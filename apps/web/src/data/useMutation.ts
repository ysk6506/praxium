/**
 * One write to the API as a state a form can draw: idle, pending, succeeded or failed.
 *
 * - **No double submit.** While a write is in flight, `submit` sends nothing new and gives back the answer of the one
 *   in flight. A double click or a second Enter makes one request.
 * - **One key per submission.** Each submission carries an `Idempotency-Key` (see `rest-api.md`). Submitting the same
 *   variables again after a failure (the answer may have been lost on the way) sends the same key, so the server
 *   replays its first answer instead of doing the work twice. Changed variables are another submission and get a new
 *   key, and so does the next submission after a success. A key the server says it cannot take again
 *   (`conflict.idempotency_key_reused`) is dropped.
 * - **Failures in words.** A failure becomes an `ErrorView` (Korean sentences), and the fields the server named in
 *   `errors[]` come as `issues`, to be tied to the fields of the form with `attachIssues`.
 * - **Reading again.** After a success the topics in `invalidates` are invalidated, so the reads that depend on them
 *   read again (`invalidation.ts`).
 *
 * The key is made by `newKey`. By default it is `newIdempotencyKey` (`idempotency.ts`), the one file of `apps/web` that
 * may use the browser's randomness (ARCH052). The tests give a counter, so they stay deterministic.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import { createClient, type Requester } from "../api";
import { useToken } from "./connection";
import { describeError, type ErrorView } from "./errors";
import { newIdempotencyKey } from "./idempotency";
import { invalidate } from "./invalidation";
import type { Issue } from "./json-doc";

/** What the server accepts as an `Idempotency-Key`: 8 to 128 characters (`idem.go`); here also printable ASCII only. */
export const IDEMPOTENCY_KEY_LENGTH = { min: 8, max: 128 } as const;

/** Whether this text can be sent as an `Idempotency-Key`. */
export const isIdempotencyKey = (key: string): boolean =>
  key.length >= IDEMPOTENCY_KEY_LENGTH.min &&
  key.length <= IDEMPOTENCY_KEY_LENGTH.max &&
  /^[\x21-\x7e]+$/.test(key);

export interface MutationContext {
  /** Send this as the `Idempotency-Key` of the call. */
  readonly idempotencyKey: string;
}

export interface MutationOptions<V, R> {
  /** The call. `client` carries the token; `variables` are what the form gave to `submit`. */
  readonly run: (client: Requester, variables: V, context: MutationContext) => Promise<R>;
  /** Make a new key. Called once per submission. Defaults to a random UUID (`newIdempotencyKey`). */
  readonly newKey?: () => string;
  /** The topics a success changes: the reads that depend on them read again. */
  readonly invalidates?: readonly string[];
  /** Called after a success, once the reads have been told. */
  readonly onSuccess?: (result: R, variables: V) => void;
}

export type MutationState<R> =
  | { readonly status: "idle" }
  | { readonly status: "pending" }
  | { readonly status: "success"; readonly result: R }
  | {
      readonly status: "error";
      readonly error: ErrorView;
      /** The fields the server named, as pointers into the body and codes. Empty when it named none. */
      readonly issues: readonly Issue[];
    };

export interface Mutation<V, R> {
  readonly state: MutationState<R>;
  readonly isPending: boolean;
  /** Send the write. Gives `true` when it succeeded. A call while one is in flight gives that one's answer. */
  readonly submit: (variables: V) => Promise<boolean>;
  /** Forget the last outcome (a success message that was read, an error that was dismissed). */
  readonly reset: () => void;
}

function fingerprint(variables: unknown): string | undefined {
  try {
    return JSON.stringify(variables) ?? "";
  } catch {
    return undefined;
  }
}

export function useMutation<V, R>(options: MutationOptions<V, R>): Mutation<V, R> {
  const token = useToken();
  const [state, setState] = useState<MutationState<R>>({ status: "idle" });
  const latest = useRef({ options, token });
  latest.current = { options, token };
  const inFlight = useRef<Promise<boolean> | undefined>(undefined);
  const attempt = useRef<{ key: string; variables: string } | undefined>(undefined);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const send = useCallback(async (variables: V): Promise<boolean> => {
    const { options: current, token: bearer } = latest.current;
    let result: R;
    try {
      const sameness = fingerprint(variables);
      let key = attempt.current?.key;
      if (key === undefined || sameness === undefined || attempt.current?.variables !== sameness) {
        key = (current.newKey ?? newIdempotencyKey)();
        if (!isIdempotencyKey(key)) {
          throw new Error(
            `An Idempotency-Key is ${IDEMPOTENCY_KEY_LENGTH.min} to ${IDEMPOTENCY_KEY_LENGTH.max} printable ASCII characters.`,
          );
        }
        attempt.current = sameness === undefined ? undefined : { key, variables: sameness };
      }
      result = await current.run(createClient({ token: bearer }), variables, {
        idempotencyKey: key,
      });
    } catch (error) {
      const view = describeError(error);
      if (view.code === "conflict.idempotency_key_reused") attempt.current = undefined;
      if (mounted.current) {
        setState({ status: "error", error: view, issues: view.fieldErrors ?? [] });
      }
      return false;
    }
    attempt.current = undefined;
    if (mounted.current) setState({ status: "success", result });
    if (current.invalidates) invalidate(...current.invalidates);
    current.onSuccess?.(result, variables);
    return true;
  }, []);

  const submit = useCallback(
    (variables: V): Promise<boolean> => {
      if (inFlight.current) return inFlight.current;
      setState({ status: "pending" });
      const running = send(variables).finally(() => {
        inFlight.current = undefined;
      });
      inFlight.current = running;
      return running;
    },
    [send],
  );

  const reset = useCallback(() => {
    if (!inFlight.current) setState({ status: "idle" });
  }, []);

  return { state, isPending: state.status === "pending", submit, reset };
}
