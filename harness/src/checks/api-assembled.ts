/**
 * The committed contract is what the parts assemble to (ADR-0024).
 *
 * API020 the committed document differs from the assembled one   API021 the parts cannot be assembled
 * API022 the committed document is missing                        API091 the schema keywords of the harness and of the
 *                                                                 Go validator differ
 */
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { type Violation, violation } from "../common";
import type { ApiConfig } from "./api-config";

/** Runs the assemble command and returns the assembled text, or an error text. */
export type Assembler = () => { readonly text: string } | { readonly error: string };

export function checkAssembled(root: string, config: ApiConfig, assemble: Assembler): Violation[] {
  const file = path.join(root, config.openapi);
  if (!existsSync(file))
    return [
      violation(
        "API022",
        config.openapi,
        0,
        "the assembled contract is missing",
        "Run `praxium contract assemble` from backend/ (go run ./cmd/praxium contract assemble) and write the output to this file.",
      ),
    ];
  const result = assemble();
  if ("error" in result)
    return [
      violation(
        "API021",
        config.parts,
        0,
        `the parts cannot be assembled: ${result.error}`,
        "Fix the part named above: every part is a JSON object and no key may have two owners.",
      ),
    ];
  if (result.text === readFileSync(file, "utf8")) return [];
  return [
    violation(
      "API020",
      config.openapi,
      0,
      "the committed contract is not what the parts assemble to",
      "Never edit the assembled file. Change the parts, then run `go run ./cmd/praxium contract assemble > internal/contract/openapi/openapi.json` in backend/.",
    ),
  ];
}

/** The words returned by SchemaKeywords() in the Go source: the string literals of that function. */
export function goKeywords(source: string): string[] | null {
  const start = source.indexOf("func SchemaKeywords()");
  if (start < 0) return null;
  const end = source.indexOf("\n}\n", start);
  const body = source.slice(start, end < 0 ? undefined : end);
  return [...body.matchAll(/"([^"]+)"/g)].map((m) => m[1] ?? "");
}

export function checkKeywords(root: string, config: ApiConfig): Violation[] {
  const file = path.join(root, config.schema_keywords_source);
  const found = existsSync(file) ? goKeywords(readFileSync(file, "utf8")) : null;
  if (found === null)
    return [
      violation(
        "API091",
        config.schema_keywords_source,
        0,
        "SchemaKeywords() is not in the Go validator",
        "Keep func SchemaKeywords() in the validator: it is the list the lint compares with.",
      ),
    ];
  const mine = [...config.schema_keywords].sort().join(",");
  if (mine === [...found].sort().join(",")) return [];
  return [
    violation(
      "API091",
      config.schema_keywords_source,
      0,
      `the schema keywords differ: harness has ${mine}; Go has ${[...found].sort().join(",")}`,
      "Change schema_keywords in harness/config/api-contract.yaml and SchemaKeywords() together, with an ADR.",
    ),
  ];
}
