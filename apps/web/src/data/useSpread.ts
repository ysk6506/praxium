/**
 * The clock of the spread (ADR-0051). The state and its transitions are pure (`impact-spread.ts`); this hook gives them
 * time, from the `requestAnimationFrame` timestamps, and follows the system's wish for less motion. It reads no clock
 * of its own: the time of a frame is what the browser hands in.
 */
import { useCallback, useEffect, useReducer, useRef, useSyncExternalStore } from "react";
import { initialSpread, type SpreadState, spreadReducer } from "./impact-spread";

const QUERY = "(prefers-reduced-motion: reduce)";

function subscribe(listener: () => void): () => void {
  const media = globalThis.matchMedia?.(QUERY);
  media?.addEventListener("change", listener);
  return () => media?.removeEventListener("change", listener);
}

/** Whether the person asked the system for less motion. Without a way to ask, motion is allowed. */
export function useReducedMotion(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => globalThis.matchMedia?.(QUERY).matches ?? false,
    () => false,
  );
}

export interface Spread {
  readonly state: SpreadState;
  play(): void;
  pause(): void;
  replay(): void;
  seek(step: number): void;
}

/**
 * The spread of an answer whose last step is `last`. It plays once when the screen opens (not with reduced motion) and
 * afterwards only when asked: a change of the reading or of the filters keeps the step where it is.
 */
export function useSpread(last: number): Spread {
  const reduced = useReducedMotion();
  const [state, dispatch] = useReducer(spreadReducer, undefined, () =>
    initialSpread({ last, reducedMotion: reduced, autoplay: true }),
  );
  const before = useRef(last);
  useEffect(() => {
    if (before.current !== last) {
      before.current = last;
      dispatch({ type: "plan", last });
    }
  }, [last]);
  // The time between frames is added up here and handed to the state only when a beat is full, so a play makes one
  // change (and one drawing of the picture) for each beat and not one for each frame.
  const latest = useRef(state);
  latest.current = state;
  useEffect(() => {
    if (!state.playing) return;
    let frame = 0;
    let previous: number | undefined;
    let waited = 0;
    const onFrame = (time: number) => {
      waited += previous === undefined ? 0 : time - previous;
      previous = time;
      const now = latest.current;
      if (waited + now.carry >= now.plan.beatMs) {
        dispatch({ type: "tick", elapsedMs: waited });
        waited = 0;
      }
      frame = globalThis.requestAnimationFrame(onFrame);
    };
    frame = globalThis.requestAnimationFrame(onFrame);
    return () => globalThis.cancelAnimationFrame(frame);
  }, [state.playing]);
  return {
    state,
    play: useCallback(() => dispatch({ type: "play" }), []),
    pause: useCallback(() => dispatch({ type: "pause" }), []),
    replay: useCallback(() => dispatch({ type: "replay" }), []),
    seek: useCallback((step: number) => dispatch({ type: "seek", step }), []),
  };
}
