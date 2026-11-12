/**
 * Stable, platform-independent hashing (no Node crypto, so it also runs in browsers).
 * Not for security: for keys that must be equal when content is equal (benchmark sets, seeds).
 */

/** cyrb53: a fast 53-bit string hash with good distribution. */
export function hash53(text: string, seed = 0): number {
  let h1 = 0xdeadbeef ^ seed;
  let h2 = 0x41c6ce57 ^ seed;
  for (let i = 0; i < text.length; i++) {
    const ch = text.charCodeAt(i);
    h1 = Math.imul(h1 ^ ch, 2654435761);
    h2 = Math.imul(h2 ^ ch, 1597334677);
  }
  h1 = Math.imul(h1 ^ (h1 >>> 16), 2246822507) ^ Math.imul(h2 ^ (h2 >>> 13), 3266489909);
  h2 = Math.imul(h2 ^ (h2 >>> 16), 2246822507) ^ Math.imul(h1 ^ (h1 >>> 13), 3266489909);
  return 4294967296 * (2097151 & h2) + (h1 >>> 0);
}

/** 16 hex chars from two independent 53-bit hashes. */
export function hashHex(text: string): string {
  const a = hash53(text, 1).toString(16).padStart(14, "0");
  const b = hash53(text, 2).toString(16).padStart(14, "0");
  return (a + b).slice(0, 16);
}

/** JSON with object keys sorted at every level, so equal values always serialize the same. */
export function stableStringify(value: unknown): string {
  if (value === null || typeof value !== "object") {
    return JSON.stringify(value === undefined ? null : value);
  }
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(",")}]`;
  const entries = Object.entries(value as Record<string, unknown>)
    .filter(([, v]) => v !== undefined)
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
  return `{${entries.map(([k, v]) => `${JSON.stringify(k)}:${stableStringify(v)}`).join(",")}}`;
}

export function hashValue(value: unknown): string {
  return hashHex(stableStringify(value));
}
