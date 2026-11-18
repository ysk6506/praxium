/**
 * The one door of randomness in `apps/web` (ADR-0072). A key for the `Idempotency-Key` header must not repeat across
 * page loads, tabs and people, so it needs the browser's cryptographic randomness. This file does that and nothing
 * else: it makes a key. It is the only file of `apps/web` that `nondeterminism_allowed` of the structure contract
 * names; everything else takes a key maker as a parameter (`useMutation`'s `newKey`), so tests stay deterministic.
 */

/** A version 4 UUID text from 16 random bytes (RFC 9562), for browsers that have no `randomUUID`. */
function uuidFromBytes(bytes: Uint8Array): string {
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x40;
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80;
  let text = "";
  bytes.forEach((byte, i) => {
    if (i === 4 || i === 6 || i === 8 || i === 10) text += "-";
    text += byte.toString(16).padStart(2, "0");
  });
  return text;
}

/**
 * A new key: a UUID of 36 printable characters, which the server takes (8 to 128). It uses `crypto.randomUUID`, then
 * `crypto.getRandomValues`. Where the browser has neither (an old browser on an insecure origin) it throws: a key from
 * weak randomness could repeat, and a repeated key makes the server replay another person's answer.
 */
export function newIdempotencyKey(): string {
  const source = globalThis.crypto;
  if (typeof source?.randomUUID === "function") return source.randomUUID();
  if (typeof source?.getRandomValues === "function") {
    return uuidFromBytes(source.getRandomValues(new Uint8Array(16)));
  }
  throw new Error("This browser cannot make a safe Idempotency-Key (no crypto.getRandomValues).");
}
