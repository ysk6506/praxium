import { describe, expect, it } from "vitest";
import {
  BEAT_MAX_MS,
  BEAT_MIN_MS,
  initialSpread,
  SPREAD_TOTAL_MS,
  type SpreadAction,
  type SpreadState,
  spreadDuration,
  spreadPlan,
  spreadReducer,
} from "./impact-spread";

const run = (state: SpreadState, ...actions: SpreadAction[]): SpreadState =>
  actions.reduce(spreadReducer, state);

describe("[REQ-352] the spread is paced by beats and never takes more than 4 seconds", () => {
  it("makes a beat between 250 and 700 ms, whatever the length of the spread", () => {
    for (let last = 1; last <= 300; last++) {
      const plan = spreadPlan(last);
      expect(plan.beatMs).toBeGreaterThanOrEqual(BEAT_MIN_MS);
      expect(plan.beatMs).toBeLessThanOrEqual(BEAT_MAX_MS);
      expect(spreadDuration(plan)).toBeLessThanOrEqual(SPREAD_TOTAL_MS);
      expect(plan.stride).toBeGreaterThanOrEqual(1);
    }
  });

  it("takes about 4 seconds for a spread of several steps, and shows more steps in a beat when it is long", () => {
    expect(spreadPlan(7).beatMs).toBe(571);
    expect(spreadPlan(7).stride).toBe(1);
    expect(spreadPlan(2).beatMs).toBe(BEAT_MAX_MS);
    expect(spreadPlan(16).stride).toBe(1);
    expect(spreadPlan(17).stride).toBe(2);
    expect(spreadPlan(64).stride).toBe(4);
    expect(spreadDuration(spreadPlan(0))).toBe(0);
  });

  it("plays one beat at a time from the first step to the last and then stops", () => {
    const start = initialSpread({ last: 4, reducedMotion: false, autoplay: true });
    expect(start).toMatchObject({ step: 0, playing: true });
    const beat = start.plan.beatMs;
    const half = run(start, { type: "tick", elapsedMs: beat / 2 });
    expect(half.step).toBe(0);
    const one = run(half, { type: "tick", elapsedMs: beat / 2 });
    expect(one.step).toBe(1);
    const end = run(one, { type: "tick", elapsedMs: beat * 10 });
    expect(end).toMatchObject({ step: 4, playing: false });
    expect(run(end, { type: "tick", elapsedMs: 5000 })).toBe(end);
  });

  it("shows several steps in a beat when the plan strides, and never past the last", () => {
    const start = initialSpread({ last: 40, reducedMotion: false, autoplay: true });
    const next = run(start, { type: "tick", elapsedMs: start.plan.beatMs });
    expect(next.step).toBe(start.plan.stride);
    const total = spreadDuration(start.plan);
    expect(run(start, { type: "tick", elapsedMs: total }).step).toBe(40);
  });

  it("pauses where it is and goes on from there", () => {
    const start = initialSpread({ last: 5, reducedMotion: false, autoplay: true });
    const two = run(start, { type: "tick", elapsedMs: start.plan.beatMs * 2 });
    expect(two.step).toBe(2);
    const paused = run(two, { type: "pause" });
    expect(paused).toMatchObject({ step: 2, playing: false });
    expect(run(paused, { type: "tick", elapsedMs: 9999 }).step).toBe(2);
    expect(run(paused, { type: "play" })).toMatchObject({ step: 2, playing: true });
  });

  it("plays again from the start when play is pressed at the end, and replays on request", () => {
    const end = initialSpread({ last: 3, reducedMotion: false, autoplay: false });
    expect(end).toMatchObject({ step: 3, playing: false });
    expect(run(end, { type: "play" })).toMatchObject({ step: 0, playing: true });
    expect(run(end, { type: "replay" })).toMatchObject({ step: 0, playing: true });
  });

  it("moves to a step by the slider, stopping the play, and keeps the step inside the spread", () => {
    const start = initialSpread({ last: 6, reducedMotion: false, autoplay: true });
    expect(run(start, { type: "seek", step: 4 })).toMatchObject({ step: 4, playing: false });
    expect(run(start, { type: "seek", step: 99 }).step).toBe(6);
    expect(run(start, { type: "seek", step: -3 }).step).toBe(0);
    expect(run(start, { type: "seek", step: Number.NaN }).step).toBe(0);
    expect(run(start, { type: "seek", step: 2.6 }).step).toBe(3);
  });

  it("has nothing to play for a spread with no steps, and follows a longer or shorter spread", () => {
    const none = initialSpread({ last: 0, reducedMotion: false, autoplay: true });
    expect(none).toMatchObject({ step: 0, playing: false });
    expect(run(none, { type: "play" })).toBe(none);
    const some = initialSpread({ last: 5, reducedMotion: false, autoplay: false });
    expect(run(some, { type: "plan", last: 3 }).step).toBe(3);
    expect(run(some, { type: "plan", last: 9 })).toMatchObject({ step: 5 });
    expect(run(some, { type: "tick", elapsedMs: -5 })).toBe(some);
  });
});

describe("[REQ-356] with reduced motion the picture opens at the end and the controls still work", () => {
  it("opens at the last step and does not play by itself", () => {
    const state = initialSpread({ last: 6, reducedMotion: true, autoplay: true });
    expect(state).toMatchObject({ step: 6, playing: false, reducedMotion: true });
  });

  it("steps by the slider and plays beat by beat when asked", () => {
    const state = initialSpread({ last: 6, reducedMotion: true, autoplay: true });
    expect(run(state, { type: "seek", step: 2 }).step).toBe(2);
    const played = run(state, { type: "replay" });
    expect(played).toMatchObject({ step: 0, playing: true });
    expect(run(played, { type: "tick", elapsedMs: played.plan.beatMs }).step).toBe(1);
  });
});
