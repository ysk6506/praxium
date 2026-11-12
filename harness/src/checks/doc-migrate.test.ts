import { readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import YAML from "yaml";
import { splitFrontMatter } from "../common";
import { ADR_DIR, adr, docsRepo, TODAY } from "../test-support/docs-repo";
import { auditQueue } from "./doc-audit";
import { legacyKeysOf } from "./doc-freshness";
import { migrateAll, stripKeys } from "./doc-legacy";
import { coversFingerprint, followsFingerprint } from "./doc-print";
import { readDocRecords } from "./doc-records";
import { run } from "./docs";

const read = (root: string, file: string): string => readFileSync(path.join(root, file), "utf8");

/** Every file under `dir` with its text, so two states of the tree can be compared. */
function snapshot(root: string, dir = ""): Record<string, string> {
  const out: Record<string, string> = {};
  for (const entry of readdirSync(path.join(root, dir), { withFileTypes: true })) {
    const rel = path.posix.join(dir, entry.name);
    if (entry.isDirectory()) Object.assign(out, snapshot(root, rel));
    else out[rel] = read(root, rel);
  }
  return out;
}

const legacyText = (fields: Record<string, unknown>, body = "# Doc\n"): string =>
  `---\n${YAML.stringify({ title: "T", kind: "guide", status: "active", ...fields })}---\n${body}`;

/** The documents of the old format, each in a state the old check judged one way or the other. */
function oldFormatRepo(): { root: string; stale: string[]; fresh: string[] } {
  const root = docsRepo({
    "src/a.ts": "export const a = 1;\n",
    "src/b.ts": "export const b = 1;\n",
    "docs/spec.md": "# Spec\nv1\n",
    [`${ADR_DIR}/0001-x.md`]: adr({ last_verified: TODAY }),
  });
  rmSync(path.join(root, "docs/verified"), { recursive: true });
  const covers = (globs: string[]) => ({
    covers: globs,
    covers_hash: coversFingerprint(root, globs),
  });
  const follows = (globs: string[]) => ({
    follows: globs,
    follows_hash: followsFingerprint(root, globs),
  });
  const stamp = {
    last_verified: TODAY,
    verification_log: [`${TODAY}: read it`],
    verified_body: "b1",
  };
  const docs: Record<string, Record<string, unknown>> = {
    guide: { ...follows(["package.json"]), ...stamp },
    "fresh-covers": { ...covers(["src/a.ts"]), ...stamp },
    "fresh-glob": { ...covers(["src/*.ts"]), ...stamp },
    "fresh-dir": { ...covers(["src/"]), ...stamp },
    "fresh-follows": { ...follows(["docs/spec.md"]), ...stamp },
    "fresh-both": { ...covers(["src/a.ts"]), ...follows(["docs/spec.md"]), ...stamp },
    "stale-covers": { covers: ["src/b.ts"], covers_hash: "0000000000000000", ...stamp },
    "stale-follows": { follows: ["docs/spec.md"], follows_hash: "0000000000000000", ...stamp },
    "stale-one-of-two": {
      ...covers(["src/a.ts"]),
      follows: ["docs/spec.md"],
      follows_hash: "x",
      ...stamp,
    },
    "never-verified": { covers: ["src/a.ts"], last_verified: TODAY },
    "old-but-fresh": { ...covers(["src/a.ts"]), last_verified: "2026-09-01" },
    audited: {
      ...follows(["docs/spec.md"]),
      ...stamp,
      unchanged_reverifies: 2,
      last_audited: "2026-11-01",
    },
  };
  for (const [name, fields] of Object.entries(docs))
    writeFileSync(path.join(root, `docs/${name}.md`), legacyText(fields));
  writeFileSync(
    path.join(root, "docs/spec.md"),
    legacyText({ ...follows(["package.json"]), ...stamp }, "# Spec\nv1\n"),
  );
  writeFileSync(
    path.join(root, "docs/index.md"),
    `${legacyText({ ...follows(["docs/spec.md"]), ...stamp }, `# Index\n${["spec", ...Object.keys(docs)].map((n) => `[${n}](${n}.md)`).join(" ")}\n`)}`,
  );
  const names = [...Object.keys(docs), "spec"];
  return {
    root,
    stale: names.filter((n) => n.startsWith("stale") || n === "never-verified"),
    fresh: names.filter((n) => !n.startsWith("stale") && n !== "never-verified"),
  };
}

const staleDocs = (root: string): string[] =>
  run(root, TODAY)
    .filter((v) => v.code === "DOC010" || v.code === "DOC011")
    .map((v) => path.basename(v.path, ".md"))
    .sort();

describe("migrating the old format [REQ-020]", () => {
  test("a document that was fresh is fresh after, one that was stale is stale after", () => {
    const { root, stale, fresh } = oldFormatRepo();
    const before = (name: string): boolean => {
      const { data } = splitFrontMatter(read(root, `docs/${name}.md`));
      const coversOk =
        !data?.covers || data.covers_hash === coversFingerprint(root, data.covers as string[]);
      const followsOk =
        !data?.follows || data.follows_hash === followsFingerprint(root, data.follows as string[]);
      return coversOk && followsOk;
    };
    expect(fresh.every(before)).toBe(true);
    expect(stale.some(before)).toBe(false);
    migrateAll(root);
    expect(staleDocs(root)).toEqual([...stale].sort());
    for (const name of fresh)
      expect(readDocRecords(root, `docs/${name}.md`).records.size).toBeGreaterThan(0);
  });

  test("nothing but verification state leaves the front matter, and the age and audit state carry over", () => {
    const { root } = oldFormatRepo();
    const before = splitFrontMatter(read(root, "docs/fresh-covers.md")).data;
    migrateAll(root);
    const after = splitFrontMatter(read(root, "docs/fresh-covers.md")).data;
    expect(legacyKeysOf(after)).toEqual([]);
    expect(after).toEqual({ title: "T", kind: "guide", status: "active", covers: before?.covers });
    expect(legacyKeysOf(splitFrontMatter(read(root, `${ADR_DIR}/0001-x.md`)).data)).toEqual([]);
    // "old-but-fresh" was verified 71 days before TODAY: still too old after the move.
    expect(
      run(root, TODAY)
        .filter((v) => v.code === "DOC020")
        .map((v) => path.basename(v.path)),
    ).toEqual(["old-but-fresh.md"]);
    expect(auditQueue(root, ["docs/audited.md"]).map((e) => [e.file, e.lastAudited])).toEqual([
      ["docs/audited.md", "2026-11-01"],
    ]);
  });

  test("it is idempotent: a second run changes nothing", () => {
    const { root } = oldFormatRepo();
    expect(migrateAll(root).converted.length).toBeGreaterThan(10);
    const once = snapshot(root);
    const again = migrateAll(root);
    expect(again).toEqual({ converted: [], records: 0 });
    expect(snapshot(root)).toEqual(once);
  });

  test("a document still in the old format fails once, with the command that converts it", () => {
    const { root } = oldFormatRepo();
    const found = run(root, TODAY).filter((v) => v.code === "DOC100");
    expect(found).toHaveLength(1);
    expect(found[0]?.message).toMatch(/\d+ document\(s\) still keep verification state/);
    expect(found[0]?.fix).toContain("pnpm tsx harness/src/checks/docs.ts migrate");
    expect(run(root, TODAY).some((v) => v.code === "DOC010" || v.code === "DOC011")).toBe(false);
    migrateAll(root);
    expect(run(root, TODAY).some((v) => v.code === "DOC100")).toBe(false);
  });

  test("stripKeys removes a key with its list and leaves every other line alone", () => {
    const text =
      '---\ntitle: "T"\nlast_verified: 2026-01-01\ncovers:\n  - a.ts\nverification_log:\n  - "x: y"\n  - z\nkind: guide\n---\n# Body\nlast_verified: stays\n';
    expect(stripKeys(text, ["last_verified", "verification_log"])).toBe(
      '---\ntitle: "T"\ncovers:\n  - a.ts\nkind: guide\n---\n# Body\nlast_verified: stays\n',
    );
  });
});
