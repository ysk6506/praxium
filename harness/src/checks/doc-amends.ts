/**
 * `amends` / `amended_by` entries (ADR-0180, richer form in the ADR that follows it).
 *
 * An entry is either an ADR file name (the first form, still read) or `{ adr: "0165", what: "..." }`
 * where `what` is a short plain phrase for the part that changed.
 */

export interface AmendEntry {
  /** The ADR file name the entry points at (or, when no such file exists, what was written). */
  readonly name: string;
  /** The short phrase for what changed, when one was given. */
  readonly what: string | null;
  /** True when the entry was written as `{ adr, what }`. */
  readonly rich: boolean;
}

export const adrNumber = (name: string): string => /^(\d{4})/.exec(name)?.[1] ?? name;

/** The file name for an ADR number, among the known ADR file names. */
export function fileForNumber(adr: string, names: readonly string[]): string | null {
  return names.find((n) => n.startsWith(`${adr}-`)) ?? null;
}

/** Read an `amends` or `amended_by` value. `names` are the ADR file names that exist. */
export function amendEntries(value: unknown, names: readonly string[]): AmendEntry[] {
  if (!Array.isArray(value)) return [];
  return value.map((item): AmendEntry => {
    if (item !== null && typeof item === "object") {
      const { adr, what } = item as { adr?: unknown; what?: unknown };
      const number = String(adr ?? "");
      const text = typeof what === "string" && what.trim() ? what.trim() : null;
      return { name: fileForNumber(number, names) ?? number, what: text, rich: true };
    }
    return { name: String(item), what: null, rich: false };
  });
}

/** The entry `link-amends` writes into `amended_by` of the target: the rich form when there is a phrase. */
export function backEntry(
  selfName: string,
  what: string | null,
): string | { adr: string; what: string } {
  return what === null ? selfName : { adr: adrNumber(selfName), what };
}
