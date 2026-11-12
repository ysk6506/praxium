/** Fingerprints that let a document prove it is current (ADR-0002). */
import { readFileSync } from "node:fs";
import path from "node:path";
import { splitFrontMatter } from "../common";
import { expandGlobs } from "../files";
import { normalized, sha16 } from "../ts-ast";

/** TS/TSX: structure without comments or formatting. Anything else: raw bytes. */
export function fileFingerprint(root: string, file: string): string {
  const text = readFileSync(path.join(root, file), "utf8");
  if (/\.(ts|tsx)$/.test(file)) return sha16(normalized(file, text));
  return sha16(text);
}

export function coversFingerprint(root: string, globs: readonly string[]): string {
  const lines = expandGlobs(root, globs).map((file) => `${file}:${fileFingerprint(root, file)}`);
  return sha16(lines.join("\n"));
}

/** Body fingerprint for sealed ADRs: trailing whitespace and surrounding blank lines ignored. */
export function bodyFingerprint(body: string): string {
  return sha16(
    body
      .trim()
      .split("\n")
      .map((line) => line.trimEnd())
      .join("\n"),
  );
}

/**
 * Fingerprint of the documents (or files) a doc follows (ADR-0180). A Markdown file counts by its body only:
 * its own `last_verified`, hashes and log change when it is re-verified and must not make followers stale.
 */
export function followsFingerprint(root: string, globs: readonly string[]): string {
  const lines = expandGlobs(root, globs).map((file) => {
    if (!file.endsWith(".md")) return `${file}:${fileFingerprint(root, file)}`;
    const { body } = splitFrontMatter(readFileSync(path.join(root, file), "utf8"));
    return `${file}:${bodyFingerprint(body)}`;
  });
  return sha16(lines.join("\n"));
}
