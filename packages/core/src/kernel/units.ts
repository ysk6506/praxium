import { InvalidValueError } from "./errors";

export function clamp(value: number, low: number, high: number): number {
  if (low > high) throw new InvalidValueError(`clamp bounds are reversed: ${low} > ${high}`);
  return Math.min(high, Math.max(low, value));
}

/** Round to `digits` decimals (for display and stable test values). */
export function round(value: number, digits = 2): number {
  const factor = 10 ** digits;
  return Math.round(value * factor) / factor;
}

/** First-order lag: move `current` toward `target` over `dt` with time constant `tau`. */
export function approach(current: number, target: number, dt: number, tau: number): number {
  if (tau <= 0) return target;
  return target + (current - target) * Math.exp(-dt / tau);
}
