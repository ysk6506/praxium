/**
 * Breaking-change detection against the base (rest-api.md 5-2, ADR-0024).
 *
 * API030 a change breaks the contract and no sealed new ADR declares it
 * API031 an ADR declares breaks_contract without a sealed ADR, a list of keys or a real reason
 * API032 a declared break is not a break (the declaration is stale)
 *
 * A break is declared like a test amendment: a new ADR (one that is not at the base) lists the keys in
 * `breaks_contract` and gives `contract_break_reason` (at least 40 characters), and is sealed.
 */
import { splitFrontMatter, type Violation, violation } from "../common";
import type { ApiConfig } from "./api-config";
import type { JsonObject } from "./api-json";
import { type Fact, type Surface, surfaceOf } from "./api-surface";
import { bodyFingerprint } from "./doc-print";

export interface Break {
  readonly key: string;
  readonly what: string;
}

function missing(fact: Fact): string {
  return `${fact.kind === "field" || fact.kind === "bodyfield" ? "the field" : `the ${fact.kind}`} was removed`;
}

function removedValues(before: Fact, after: Fact): string[] {
  const kept = new Set(after.enum ?? []);
  return (before.enum ?? []).filter((v) => !kept.has(v));
}

function changed(kind: string, before: Fact, after: Fact): string[] {
  const out: string[] = [];
  if (before.type !== after.type)
    out.push(`the type changed from ${before.type || "any"} to ${after.type || "any"}`);
  const gone = removedValues(before, after);
  if (gone.length > 0) out.push(`the values ${gone.join(", ")} were removed`);
  if (kind === "param" || kind === "bodyfield") {
    if (!before.required && after.required) out.push("it became required");
  } else if (kind === "field" && before.required && !after.required) {
    out.push("it is no longer always there");
  }
  return out;
}

function compareFact(key: string, before: Fact, after: Fact): Break[] {
  if (before.kind === "roles") {
    const kept = new Set(after.values);
    const lost = (before.values ?? []).filter((r) => !kept.has(r));
    return lost.length > 0
      ? [{ key, what: `callers with ${lost.join(", ")} are no longer accepted` }]
      : [];
  }
  if (before.kind === "code") {
    return before.values?.join("/") === after.values?.join("/")
      ? []
      : [
          {
            key,
            what: `the group or status changed from ${before.values?.join("/")} to ${after.values?.join("/")}`,
          },
        ];
  }
  return changed(before.kind, before, after).map((what) => ({ key, what }));
}

/** True when a break is already explained by a removed operation or response that is its parent. */
function explainedBy(key: string, removed: readonly string[]): boolean {
  return removed.some(
    (p) => key !== p && key.startsWith(p) && (key[p.length] === " " || key[p.length] === "#"),
  );
}

/** Everything in `after` that a client built on `before` cannot live with. */
export function breaksBetween(before: Surface, after: Surface): Break[] {
  const all: Break[] = [];
  const removed: string[] = [];
  for (const [key, was] of before) {
    const now = after.get(key);
    if (now === undefined) {
      all.push({ key, what: missing(was) });
      if (was.kind === "op" || was.kind === "response") removed.push(key);
    } else all.push(...compareFact(key, was, now));
  }
  const out = all.filter((b) => !explainedBy(b.key, removed));
  for (const [key, now] of after)
    if (
      !before.has(key) &&
      now.required &&
      (now.kind === "param" || now.kind === "bodyfield") &&
      !isNewOperation(before, key)
    )
      out.push({ key, what: `a new ${now.kind === "param" ? "parameter" : "field"} is required` });
  return out;
}

/** True when the operation of a key did not exist before: everything of a new operation is new. */
function isNewOperation(before: Surface, key: string): boolean {
  const [method, path] = key.split(" ");
  return !before.has(`${method} ${path}`);
}

export interface BaseReader {
  /** The text of a file at the base, or null when it is not there. */
  read(path: string): string | null;
  /** True when the file exists at the base. */
  has(path: string): boolean;
}

export interface Declarations {
  readonly keys: Set<string>;
  readonly violations: Violation[];
}

/** The ADR files of the change that are new (not at the base), with their text. */
export interface NewAdr {
  readonly file: string;
  readonly text: string;
}

export function readDeclarations(config: ApiConfig, adrs: readonly NewAdr[]): Declarations {
  const keys = new Set<string>();
  const violations: Violation[] = [];
  for (const adr of adrs) {
    const { data, body } = splitFrontMatter(adr.text);
    const declared = data?.breaks_contract;
    if (declared === undefined) continue;
    const path = `${config.adr_dir}/${adr.file}`;
    const problem = declarationProblem(data ?? {}, body, declared);
    if (problem) {
      violations.push(
        violation(
          "API031",
          path,
          0,
          problem,
          "List the broken keys in breaks_contract, give a contract_break_reason of at least 40 characters and seal the ADR (pnpm tsx harness/src/checks/docs.ts seal-adr <adr>).",
        ),
      );
      continue;
    }
    for (const key of declared as unknown[]) keys.add(String(key));
  }
  return { keys, violations };
}

function declarationProblem(
  data: Record<string, unknown>,
  body: string,
  declared: unknown,
): string | null {
  if (
    !Array.isArray(declared) ||
    declared.length === 0 ||
    declared.some((k) => typeof k !== "string" || !k)
  )
    return "breaks_contract must be a list of keys";
  if (data.decision_hash !== bodyFingerprint(body))
    return "the ADR is not sealed (or was edited after sealing)";
  if (String(data.contract_break_reason ?? "").trim().length < 40)
    return "contract_break_reason is missing or shorter than 40 characters";
  return null;
}

/** The violations of a change: breaks not declared, and declarations that break nothing. */
export function checkBreaking(
  config: ApiConfig,
  current: JsonObject,
  baseText: string | null,
  declarations: Declarations,
): Violation[] {
  const out = [...declarations.violations];
  if (baseText === null) return out;
  let baseDoc: JsonObject;
  try {
    baseDoc = JSON.parse(baseText) as JsonObject;
  } catch {
    return out;
  }
  const breaks = breaksBetween(surfaceOf(baseDoc), surfaceOf(current));
  const real = new Set(breaks.map((b) => b.key));
  for (const b of breaks)
    if (!declarations.keys.has(b.key))
      out.push(
        violation(
          "API030",
          config.openapi,
          0,
          `breaking change [${b.key}]: ${b.what}`,
          "Keep the contract compatible inside /v1 (add, never remove or change). If the break is intended, write a new ADR with breaks_contract listing this key and a contract_break_reason, and seal it.",
        ),
      );
  for (const key of declarations.keys)
    if (!real.has(key))
      out.push(
        violation(
          "API032",
          config.openapi,
          0,
          `breaks_contract declares [${key}], which is not a break of this change`,
          "Remove the key from the ADR's breaks_contract (an ADR that is already sealed cannot be edited: write the declaration in the right ADR).",
        ),
      );
  return out;
}
