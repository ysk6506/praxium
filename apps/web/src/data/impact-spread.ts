/**
 * The spread of an impact answer over time (ADR-0051): the picture shows the answer one step at a time, from the
 * objects that failed outward. The state is one whole number, the step; everything else in the picture follows from it.
 * The transitions are pure functions. The clock is given in: the caller passes the time that went by since the last
 * tick (from `requestAnimationFrame`), because reading the time is not the data layer's job.
 *
 * Time. A beat takes between 250 and 700 ms; the whole spread takes 4 seconds at most. A long spread would need more
 * beats than 4 seconds hold at 250 ms, so each beat then shows several steps at once.
 */

export const SPREAD_TOTAL_MS = 4000;
export const BEAT_MIN_MS = 250;
export const BEAT_MAX_MS = 700;

export interface SpreadPlan {
  /** The last step: the most hops of any node. */
  readonly last: number;
  /** How many steps one beat shows. */
  readonly stride: number;
  readonly beatMs: number;
}

export function spreadPlan(last: number): SpreadPlan {
  const steps = Math.max(1, Math.floor(last));
  const stride = Math.max(1, Math.ceil((steps * BEAT_MIN_MS) / SPREAD_TOTAL_MS));
  const beats = Math.ceil(steps / stride);
  // Whole milliseconds, rounded down, so that the beats never add up to more than the total.
  const beatMs = Math.min(BEAT_MAX_MS, Math.max(BEAT_MIN_MS, Math.floor(SPREAD_TOTAL_MS / beats)));
  return { last: Math.max(0, Math.floor(last)), stride, beatMs };
}

/** The time the spread takes from the first step to the last, by the plan. */
export const spreadDuration = (plan: SpreadPlan): number =>
  plan.last === 0 ? 0 : Math.ceil(plan.last / plan.stride) * plan.beatMs;

export interface SpreadState {
  readonly plan: SpreadPlan;
  /** The steps shown: a node of step `s` is shown when `s <= step`. */
  readonly step: number;
  readonly playing: boolean;
  /** The time already spent on the beat that is going. */
  readonly carry: number;
  /** Moving the picture is off (the person asked the system for less motion): a beat is shown at once. */
  readonly reducedMotion: boolean;
}

export type SpreadAction =
  | { readonly type: "play" }
  | { readonly type: "pause" }
  | { readonly type: "replay" }
  | { readonly type: "seek"; readonly step: number }
  | { readonly type: "tick"; readonly elapsedMs: number }
  | { readonly type: "plan"; readonly last: number };

export interface SpreadStart {
  readonly last: number;
  readonly reducedMotion: boolean;
  /** The first time the screen opens it plays once. Never with reduced motion: that opens at the last step. */
  readonly autoplay: boolean;
}

export function initialSpread({ last, reducedMotion, autoplay }: SpreadStart): SpreadState {
  const plan = spreadPlan(last);
  const play = autoplay && !reducedMotion && plan.last > 0;
  return {
    plan,
    step: play ? 0 : plan.last,
    playing: play,
    carry: 0,
    reducedMotion,
  };
}

const clampStep = (plan: SpreadPlan, step: number): number =>
  Math.min(plan.last, Math.max(0, Math.round(Number.isFinite(step) ? step : 0)));

function tick(state: SpreadState, elapsedMs: number): SpreadState {
  if (!state.playing) return state;
  let carry = state.carry + Math.max(0, elapsedMs);
  let step = state.step;
  while (carry >= state.plan.beatMs && step < state.plan.last) {
    carry -= state.plan.beatMs;
    step = Math.min(state.plan.last, step + state.plan.stride);
  }
  if (step >= state.plan.last) return { ...state, step: state.plan.last, playing: false, carry: 0 };
  return { ...state, step, carry };
}

/** Pressing play at the end plays again from the start. */
function play(state: SpreadState): SpreadState {
  if (state.plan.last === 0) return state;
  const step = state.step >= state.plan.last ? 0 : state.step;
  return { ...state, step, playing: true, carry: 0 };
}

export function spreadReducer(state: SpreadState, action: SpreadAction): SpreadState {
  switch (action.type) {
    case "play":
      return play(state);
    case "pause":
      return { ...state, playing: false, carry: 0 };
    case "replay":
      return play({ ...state, step: 0 });
    case "seek":
      return { ...state, step: clampStep(state.plan, action.step), playing: false, carry: 0 };
    case "tick":
      return tick(state, action.elapsedMs);
    case "plan": {
      const plan = spreadPlan(action.last);
      return { ...state, plan, step: Math.min(state.step, plan.last), carry: 0 };
    }
  }
}
