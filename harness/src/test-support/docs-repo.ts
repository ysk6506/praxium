/** Throwaway repositories for the document checks that follow decisions (ADR-0180, ADR-0194). */
import { readFileSync } from "node:fs";
import path from "node:path";
import YAML from "yaml";
import { reverify } from "../checks/doc-audit";
import { discoverDocs, loadDocsConfig } from "../checks/doc-config";
import { docState } from "../checks/doc-freshness";
import { DocContext } from "../checks/doc-targets";
import { findRoot, splitFrontMatter } from "../common";
import { miniRepo } from "./mini-repo";

export const TODAY = "2026-11-11";
export const ADR_DIR = "docs/design-docs/adr";
const realConfig = readFileSync(path.join(findRoot(), "harness/config/docs.yaml"), "utf8");

/** Front matter that satisfies every document rule unless `fields` says otherwise (no verification state: records). */
export function front(fields: Record<string, unknown> = {}): string {
  const data = {
    title: "T",
    kind: "guide",
    status: "active",
    follows: ["package.json"],
    ...fields,
  };
  return `---\n${YAML.stringify(data)}---\n`;
}

/** A decision record with the keys the ADR rules need (unsealed). */
export const adr = (fields: Record<string, unknown> = {}, body = "# D\nWe do X.\n"): string =>
  `${front({ kind: "decision", enforced_by: ["ARCH010"], follows: undefined, ...fields })}${body}`;

/**
 * Verify, as of `now`, every document that only follows package.json (the default fixture document) and has no current
 * record yet. A document that is already verified keeps its record: a note is not written twice (ADR-0028).
 */
export function verifyDefaults(root: string, now: string = TODAY): void {
  const config = loadDocsConfig(root);
  for (const file of discoverDocs(root, config)) {
    const { data } = splitFrontMatter(readFileSync(path.join(root, file), "utf8"));
    const follows = Array.isArray(data?.follows) ? data.follows : [];
    const wanted =
      data && data.kind !== "decision" && !data.covers && follows.join() === "package.json";
    if (!wanted) continue;
    const ctx = new DocContext(root);
    const state = docState(ctx, file, data);
    if (state.records.size === 0 || state.stale.length > 0)
      reverify(root, file, `fixture ${ctx.files.hash("package.json")}`, now);
  }
}

export function docsRepo(files: Record<string, string> = {}): string {
  const root = miniRepo(
    {
      "harness/config/docs.yaml": realConfig,
      "harness/src/rules.ts": 'export const RULE = "ARCH010";\n',
      "docs/index.md": `${front()}# Index\n[guide](guide.md)\n`,
      "docs/guide.md": `${front()}# Guide\n`,
      "apps/x/package.json": '{"scripts":{"check":"x"}}\n',
      ...files,
    },
    { "package.json": "{}\n" },
  );
  verifyDefaults(root);
  return root;
}
