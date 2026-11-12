import { InvalidValueError } from "./errors";

/** Source of simulated time, in seconds since the start of the scenario. */
export interface Clock {
  now(): number;
}

/** A clock that only moves when told to, so runs are reproducible. */
export class ManualClock implements Clock {
  private value: number;

  constructor(start = 0) {
    if (start < 0) throw new InvalidValueError("clock start must be >= 0");
    this.value = start;
  }

  now(): number {
    return this.value;
  }

  advance(seconds: number): number {
    if (seconds < 0 || !Number.isFinite(seconds))
      throw new InvalidValueError("a clock cannot run backwards");
    this.value += seconds;
    return this.value;
  }

  /** Jump to `value` (used when restoring a snapshot). */
  set(value: number): void {
    if (value < 0 || !Number.isFinite(value))
      throw new InvalidValueError("clock value must be >= 0");
    this.value = value;
  }
}
