/**
 * The only place in core that may read the outside world (wall clock, randomness).
 * Everything else receives a Clock, Rng or IdGenerator as an argument (ADR-0006, rule ARCH052).
 */

/** Milliseconds since the Unix epoch. For timestamps on records, never for simulation state. */
export function wallClockMs(): number {
  return Date.now();
}

export function isoNow(): string {
  return new Date(wallClockMs()).toISOString();
}

/** A new seed for runs that did not ask for a specific one. */
export function freshSeed(): number {
  return Math.floor(Math.random() * 2 ** 48);
}

/** Unguessable token (share links, per-run MCP endpoints). */
export function randomToken(bytes = 18): string {
  const buffer = new Uint8Array(bytes);
  crypto.getRandomValues(buffer);
  return Array.from(buffer, (b) => b.toString(16).padStart(2, "0")).join("");
}
