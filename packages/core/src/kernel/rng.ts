import { hash53 } from "./hash";

/** The random operations the simulator needs. */
export interface Rng {
  random(): number;
  uniform(low: number, high: number): number;
  gauss(mu: number, sigma: number): number;
}

/**
 * Deterministic Rng (sfc32). Same seed, same sequence, in Node and in browsers.
 * `fork(label)` derives an independent stream from a label, so results do not depend on the
 * order in which unrelated parts of the simulator draw numbers.
 */
export class SeededRng implements Rng {
  readonly seed: number;
  private a: number;
  private b: number;
  private c: number;
  private d: number;
  private spare: number | undefined;

  constructor(seed: number) {
    this.seed = Math.floor(Math.abs(seed)) % 2 ** 53;
    const text = String(this.seed);
    this.a = hash53(text, 11) >>> 0;
    this.b = hash53(text, 23) >>> 0;
    this.c = hash53(text, 37) >>> 0;
    this.d = 1;
    for (let i = 0; i < 12; i++) this.random();
  }

  random(): number {
    const t = (((this.a + this.b) | 0) + this.d) | 0;
    this.d = (this.d + 1) | 0;
    this.a = this.b ^ (this.b >>> 9);
    this.b = (this.c + (this.c << 3)) | 0;
    this.c = (this.c << 21) | (this.c >>> 11);
    this.c = (this.c + t) | 0;
    return (t >>> 0) / 4294967296;
  }

  uniform(low: number, high: number): number {
    return low + (high - low) * this.random();
  }

  /** Normal distribution (Box-Muller, caching the second value). */
  gauss(mu: number, sigma: number): number {
    if (this.spare !== undefined) {
      const value = this.spare;
      this.spare = undefined;
      return mu + sigma * value;
    }
    const u = Math.max(this.random(), Number.EPSILON);
    const v = this.random();
    const radius = Math.sqrt(-2 * Math.log(u));
    this.spare = radius * Math.sin(2 * Math.PI * v);
    return mu + sigma * radius * Math.cos(2 * Math.PI * v);
  }

  int(low: number, highInclusive: number): number {
    return low + Math.floor(this.random() * (highInclusive - low + 1));
  }

  pick<T>(items: readonly T[]): T {
    if (items.length === 0) throw new RangeError("pick from an empty list");
    return items[Math.floor(this.random() * items.length)] as T;
  }

  fork(label: string): SeededRng {
    return new SeededRng(hash53(`${this.seed}:${label}`));
  }
}
