import fc from "fast-check";
import { describe, expect, test } from "vitest";
import {
  approach,
  asPraxiumError,
  ConflictError,
  clamp,
  entityId,
  freshSeed,
  hash53,
  hashHex,
  hashValue,
  InvalidValueError,
  isEntityId,
  isoNow,
  joinId,
  ManualClock,
  NotFoundError,
  PraxiumError,
  ReadOnlyViolationError,
  randomToken,
  round,
  SeededRng,
  SequentialIds,
  slug,
  stableStringify,
  UnsupportedError,
  wallClockMs,
} from ".";

describe("ids [REQ-001]", () => {
  test.each(["a", "rack-01", "srv.r1:u12", "A_b-9", "x".repeat(128)])(
    "valid id %s is accepted",
    (id) => {
      expect(entityId(id)).toBe(id);
      expect(isEntityId(id)).toBe(true);
    },
  );

  test.each(["", " ", "-lead", "has space", "a/b", "한글", "x".repeat(129), "a#b"])(
    "invalid id %j is rejected",
    (id) => {
      expect(() => entityId(id)).toThrow(/invalid id/);
      expect(isEntityId(id)).toBe(false);
    },
  );

  test.each([
    ["Server Room 1", "Server-Room-1"],
    ["  --x-- ", "x"],
    ["DSR동_C타워", "DSR-_C"],
    ["", "x"],
    [".", "x"],
    ["a".repeat(300), "a".repeat(100)],
  ])("slug(%j) = %j", (text, expected) => {
    expect(slug(text)).toBe(expected);
  });

  test("slug output is always a valid id fragment", () => {
    fc.assert(
      fc.property(fc.string({ maxLength: 80 }), (text) => {
        expect(isEntityId(slug(text))).toBe(true);
      }),
    );
  });

  test("joinId composes parts", () => {
    expect(joinId("dc1", "Row 2", "rack03")).toBe("dc1-Row-2-rack03");
  });

  test("sequential ids count per prefix", () => {
    const ids = new SequentialIds();
    expect([ids.next("run"), ids.next("run"), ids.next("exp"), ids.next("run")]).toEqual([
      "run-0001",
      "run-0002",
      "exp-0001",
      "run-0003",
    ]);
  });
});

describe("errors [REQ-001]", () => {
  test.each([
    [new InvalidValueError("x"), "invalid_value", 400],
    [new NotFoundError("x"), "not_found", 404],
    [new ConflictError("x"), "conflict", 409],
    [new UnsupportedError("x"), "unsupported", 422],
    [new ReadOnlyViolationError("x"), "read_only_violation", 403],
    [new PraxiumError("x"), "praxium_error", 500],
  ])("%s has a stable code and status", (error, code, status) => {
    expect(error.code).toBe(code);
    expect(error.status).toBe(status);
    expect(error.message).toBe("x");
    expect(error).toBeInstanceOf(PraxiumError);
  });

  test("anything thrown can be normalized", () => {
    const known = new NotFoundError("gone");
    expect(asPraxiumError(known)).toBe(known);
    expect(asPraxiumError(new Error("boom")).message).toBe("boom");
    expect(asPraxiumError("plain").message).toBe("plain");
  });
});

describe("clock [REQ-001]", () => {
  test("a manual clock only moves when advanced", () => {
    const clock = new ManualClock();
    expect(clock.now()).toBe(0);
    expect(clock.advance(1.5)).toBe(1.5);
    clock.set(10);
    expect(clock.now()).toBe(10);
  });

  test.each([
    ["advance(-1)", (c: ManualClock) => c.advance(-1)],
    ["advance(NaN)", (c: ManualClock) => c.advance(Number.NaN)],
    ["set(-1)", (c: ManualClock) => c.set(-1)],
  ])("a clock cannot run backwards: %s", (_name, call) => {
    expect(() => call(new ManualClock())).toThrow(InvalidValueError);
  });

  test("a clock cannot start negative", () => {
    expect(() => new ManualClock(-1)).toThrow(InvalidValueError);
  });
});

describe("seeded randomness [REQ-001]", () => {
  test("the same seed gives the same sequence", () => {
    const a = new SeededRng(42);
    const b = new SeededRng(42);
    expect(Array.from({ length: 5 }, () => a.random())).toEqual(
      Array.from({ length: 5 }, () => b.random()),
    );
    expect(a.gauss(0, 1)).toBe(b.gauss(0, 1));
    expect(a.gauss(0, 1)).toBe(b.gauss(0, 1));
  });

  test("the sequence is pinned so recorded benchmark runs stay comparable", () => {
    const rng = new SeededRng(7);
    expect([rng.random(), rng.random(), rng.random()].map((v) => v.toFixed(6))).toEqual([
      "0.447100",
      "0.602045",
      "0.818213",
    ]);
  });

  test("different seeds differ, forks depend on the label and not on call order", () => {
    expect(new SeededRng(1).random()).not.toBe(new SeededRng(2).random());
    const root = new SeededRng(5);
    const first = root.fork("thermal").random();
    root.random();
    expect(root.fork("thermal").random()).toBe(first);
    expect(root.fork("power").random()).not.toBe(first);
  });

  test("values stay in range", () => {
    fc.assert(
      fc.property(fc.integer({ min: 0, max: 2 ** 40 }), (seed) => {
        const rng = new SeededRng(seed);
        const u = rng.random();
        const v = rng.uniform(-2, 3);
        const i = rng.int(1, 6);
        expect(u >= 0 && u < 1).toBe(true);
        expect(v >= -2 && v <= 3).toBe(true);
        expect(i >= 1 && i <= 6 && Number.isInteger(i)).toBe(true);
      }),
    );
  });

  test("gauss is roughly standard normal", () => {
    const rng = new SeededRng(3);
    const samples = Array.from({ length: 4000 }, () => rng.gauss(10, 2));
    const mean = samples.reduce((s, v) => s + v, 0) / samples.length;
    const sd = Math.sqrt(samples.reduce((s, v) => s + (v - mean) ** 2, 0) / samples.length);
    expect(mean).toBeCloseTo(10, 0);
    expect(sd).toBeCloseTo(2, 0);
  });

  test("pick chooses from the list and refuses an empty one", () => {
    expect(["a", "b", "c"]).toContain(new SeededRng(9).pick(["a", "b", "c"]));
    expect(() => new SeededRng(9).pick([])).toThrow(RangeError);
  });

  test("negative and fractional seeds are normalized", () => {
    expect(new SeededRng(-7.9).seed).toBe(7);
  });
});

describe("stable hashing [REQ-001]", () => {
  test("hashes are pinned and differ for different text", () => {
    expect(hashHex("praxium")).toBe("08e93f064bf4ce03");
    expect(hashHex("praxium!")).not.toBe(hashHex("praxium"));
    expect(hash53("a", 1)).not.toBe(hash53("a", 2));
  });

  test("stableStringify ignores key order and undefined values", () => {
    expect(stableStringify({ b: 1, a: [2, { d: 3, c: undefined }] })).toBe(
      '{"a":[2,{"d":3}],"b":1}',
    );
    expect(stableStringify(undefined)).toBe("null");
    expect(hashValue({ x: 1, y: 2 })).toBe(hashValue({ y: 2, x: 1 }));
  });
});

describe("entropy and units [REQ-001]", () => {
  test("entropy sources produce plausible values", () => {
    expect(wallClockMs()).toBeGreaterThan(1_600_000_000_000);
    expect(isoNow()).toMatch(/^\d{4}-\d{2}-\d{2}T/);
    expect(freshSeed()).not.toBe(freshSeed());
    expect(randomToken()).toMatch(/^[0-9a-f]{36}$/);
  });

  test("numeric helpers", () => {
    expect(clamp(5, 0, 3)).toBe(3);
    expect(clamp(-1, 0, 3)).toBe(0);
    expect(() => clamp(1, 3, 0)).toThrow(InvalidValueError);
    expect(round(1.23456, 2)).toBe(1.23);
    expect(approach(20, 30, 1e9, 60)).toBeCloseTo(30, 6);
    expect(approach(20, 30, 0, 60)).toBe(20);
    expect(approach(20, 30, 5, 0)).toBe(30);
  });
});
