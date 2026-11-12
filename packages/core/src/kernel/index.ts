/** core.kernel: ids, errors, Clock/Rng/IdGenerator ports, stable hashing, the only entropy entry point. */
export { type Clock, ManualClock } from "./clock";
export { freshSeed, isoNow, randomToken, wallClockMs } from "./entropy";
export {
  asPraxiumError,
  ConflictError,
  InvalidValueError,
  NotFoundError,
  PraxiumError,
  ReadOnlyViolationError,
  UnsupportedError,
} from "./errors";
export { hash53, hashHex, hashValue, stableStringify } from "./hash";
export {
  type EntityId,
  entityId,
  type IdGenerator,
  isEntityId,
  joinId,
  SequentialIds,
  slug,
} from "./ids";
export { type Rng, SeededRng } from "./rng";
export { formatDuration, josa, type Particle, withJosa } from "./text";
export { approach, clamp, round } from "./units";
