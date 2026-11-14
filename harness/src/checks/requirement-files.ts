/**
 * Where the requirements live (ADR-0063): the entry document and the area documents next to it, all in
 * docs/product-specs. A requirement id is defined once across all of them.
 */
import { existsSync, readdirSync, readFileSync } from "node:fs";
import path from "node:path";

export const REQUIREMENTS = "docs/product-specs/requirements.md";
const DIR = "docs/product-specs";
const AREA = /^requirements-[a-z0-9-]+\.md$/;

/** The entry document first, then the area documents by name; only those that exist. */
export function requirementFiles(root: string): string[] {
  const dir = path.join(root, DIR);
  const areas = existsSync(dir)
    ? readdirSync(dir)
        .filter((n) => AREA.test(n))
        .sort()
    : [];
  return [REQUIREMENTS, ...areas.map((n) => `${DIR}/${n}`)].filter((f) =>
    existsSync(path.join(root, f)),
  );
}

/** The lines of every requirements document, with the file each comes from and its line number. */
export function requirementLines(
  root: string,
): Array<{ file: string; line: number; text: string }> {
  return requirementFiles(root).flatMap((file) =>
    readFileSync(path.join(root, file), "utf8")
      .split("\n")
      .map((text, index) => ({ file, line: index + 1, text })),
  );
}
