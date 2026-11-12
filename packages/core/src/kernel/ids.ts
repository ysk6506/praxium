import { InvalidValueError } from "./errors";

/** Entity identifiers: short, stable, URL-safe strings. */
export type EntityId = string;

const ID_RE = /^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$/;

/** Validate and return `raw`. Ids appear in URLs, so slashes and spaces are rejected. */
export function entityId(raw: string): EntityId {
  if (typeof raw !== "string" || !ID_RE.test(raw)) {
    throw new InvalidValueError(
      `invalid id ${JSON.stringify(raw)}: use 1-128 chars of letters, digits and _ . : - (starting with a letter or digit)`,
    );
  }
  return raw;
}

export function isEntityId(raw: unknown): raw is EntityId {
  return typeof raw === "string" && ID_RE.test(raw);
}

/** Turn arbitrary text (a hostname, a Korean room name, ...) into a valid id fragment. */
export function slug(text: string): string {
  const cleaned = text
    .trim()
    .replace(/[^A-Za-z0-9_.-]+/g, "-")
    .replace(/^[-._]+|[-._]+$/g, "");
  return cleaned.slice(0, 100).replace(/[-._]+$/, "") || "x";
}

/** Compose a child id such as `r01-rack03` from parts. */
export function joinId(...parts: string[]): EntityId {
  return entityId(parts.map(slug).join("-"));
}

/** Source of new ids. Deterministic implementations keep runs reproducible. */
export interface IdGenerator {
  next(prefix: string): string;
}

/** `prefix-0001`, `prefix-0002`, ... counted per prefix. */
export class SequentialIds implements IdGenerator {
  private readonly counters = new Map<string, number>();

  next(prefix: string): string {
    const value = (this.counters.get(prefix) ?? 0) + 1;
    this.counters.set(prefix, value);
    return entityId(`${slug(prefix)}-${String(value).padStart(4, "0")}`);
  }
}
