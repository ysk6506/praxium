import { afterEach, describe, expect, it, vi } from "vitest";
import { newIdempotencyKey } from "./idempotency";
import { isIdempotencyKey } from "./useMutation";

afterEach(() => vi.unstubAllGlobals());

describe("[REQ-408] the key of a submission", () => {
  it("is a UUID the server takes, and differs every time", () => {
    const keys = new Set(Array.from({ length: 50 }, () => newIdempotencyKey()));
    expect(keys.size).toBe(50);
    for (const key of keys) {
      expect(key).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
      expect(isIdempotencyKey(key)).toBe(true);
    }
  });

  it("is made from getRandomValues where there is no randomUUID", () => {
    let seed = 0;
    vi.stubGlobal("crypto", {
      getRandomValues: (bytes: Uint8Array) => {
        for (let i = 0; i < bytes.length; i++) {
          seed += 37;
          bytes[i] = seed & 255;
        }
        return bytes;
      },
    });
    const first = newIdempotencyKey();
    expect(first).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    expect(isIdempotencyKey(first)).toBe(true);
    expect(newIdempotencyKey()).not.toBe(first);
  });

  it("throws where the browser has no cryptographic randomness, and does not fall back to a weak one", () => {
    vi.stubGlobal("crypto", {});
    expect(() => newIdempotencyKey()).toThrow(/Idempotency-Key/);
    vi.stubGlobal("crypto", undefined);
    expect(() => newIdempotencyKey()).toThrow(/Idempotency-Key/);
  });
});
