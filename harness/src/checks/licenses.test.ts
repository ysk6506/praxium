import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { describe, expect, test } from "vitest";
import YAML from "yaml";
import { writeFiles } from "../test-support/mini-repo";
import { evaluate, main, report, run, write } from "./licenses";
import { splitCopyright } from "./licenses-render";
import { decide, parseExpr, show } from "./licenses-spdx";

const CONFIG = {
  notices_file: "THIRD_PARTY_NOTICES.md",
  assets_manifest: "harness/config/third-party-assets.yaml",
  distributed_workspaces: ["apps/server"],
  allowed: ["MIT", "Apache-2.0", "ISC", "BSD-3-Clause", "OFL-1.1"],
  allowed_unmodified: ["MPL-2.0"],
  exceptions: [] as unknown[],
};

const MIT_TEXT =
  "MIT License\n\nCopyright (c) 2020 Someone\n\nPermission is hereby granted, free of charge.\n";

function dep(
  name: string,
  fields: Record<string, unknown> = { license: "MIT" },
  extra: Record<string, string> = {},
): Record<string, string> {
  return {
    [`node_modules/${name}/package.json`]: JSON.stringify({ name, version: "1.0.0", ...fields }),
    ...Object.fromEntries(
      Object.entries(extra).map(([file, text]) => [`node_modules/${name}/${file}`, text]),
    ),
  };
}

function repo(
  deps: string[],
  files: Record<string, string>,
  config: Record<string, unknown> = {},
  rootPackage: Record<string, unknown> = {},
): string {
  const root = mkdtempSync(path.join(tmpdir(), "praxium-lic-"));
  writeFiles(root, {
    "package.json": JSON.stringify(rootPackage),
    "harness/config/licenses.yaml": YAML.stringify({ ...CONFIG, ...config }),
    "apps/server/package.json": JSON.stringify({
      name: "@t/server",
      dependencies: Object.fromEntries(deps.map((d) => [d, "1.0.0"])),
      devDependencies: { "dev-only": "1.0.0" },
    }),
    ...dep("dev-only", { license: "GPL-3.0-only" }),
    ...files,
  });
  return root;
}

const codes = (root: string): string[] => run(root).map((v) => v.code);

describe("license expressions", () => {
  test("OR takes the first allowed alternative in the order written", () => {
    const expr = parseExpr("(GPL-3.0-only OR Apache-2.0 OR MIT)");
    const table = new Map([
      ["mit", "MIT"],
      ["apache-2.0", "Apache-2.0"],
    ]);
    expect(decide(expr as never, (id) => table.get(id.toLowerCase()))).toEqual({
      ok: true,
      applies: ["Apache-2.0"],
    });
  });

  test("AND needs every part; WITH stays part of the identifier", () => {
    const only = (id: string) => (id === "MIT" ? id : undefined);
    expect(decide(parseExpr("MIT AND ISC") as never, only).ok).toBe(false);
    expect(show(parseExpr("GPL-2.0 WITH Classpath-exception-2.0") as never)).toBe(
      "GPL-2.0 WITH Classpath-exception-2.0",
    );
  });

  test("text that is not an expression is not parsed", () => {
    expect(parseExpr("SEE LICENSE IN LICENSE.md")).toBeUndefined();
    expect(parseExpr("(MIT")).toBeUndefined();
    expect(parseExpr("")).toBeUndefined();
  });
});

describe("copyright lines", () => {
  test("the copyright paragraph is taken out of the shared text", () => {
    const { copyright, body } = splitCopyright(MIT_TEXT);
    expect(copyright).toEqual(["Copyright (c) 2020 Someone"]);
    expect(body).not.toContain("Someone");
  });

  test("sentences that start with the word are not copyright lines", () => {
    expect(splitCopyright("Copyright Holder means the owner.\n").copyright).toEqual([]);
    expect(splitCopyright("## Copyright\n\nCopyright 2021 A\n").copyright).toEqual([
      "Copyright 2021 A",
    ]);
  });
});

describe("licenses check", () => {
  test("allowed licenses pass; dev-only dependencies are not looked at", () => {
    const root = repo(["a"], dep("a", { license: "MIT" }, { LICENSE: MIT_TEXT }));
    expect(codes(root)).toEqual([]);
  });

  test("LIC001: a license off the allowlist fails with the package, the license and the fix", () => {
    const root = repo(["a"], dep("a", { license: "GPL-3.0-only" }));
    const [v] = run(root);
    expect(v?.code).toBe("LIC001");
    expect(v?.message).toContain("a@1.0.0");
    expect(v?.message).toContain("GPL-3.0-only");
    expect(v?.fix).toContain("record an exception with a reason");
  });

  test("LIC001: UNLICENSED fails, and so does a transitive dependency", () => {
    const files = {
      ...dep("a", { license: "MIT", dependencies: { b: "1.0.0" } }),
      ...dep("b", { license: "UNLICENSED" }),
    };
    const out = run(repo(["a"], files));
    expect(out.map((v) => v.code)).toEqual(["LIC001"]);
    expect(out[0]?.message).toContain("b@1.0.0");
  });

  test("an OR expression with one allowed side passes", () => {
    expect(codes(repo(["a"], dep("a", { license: "(GPL-3.0-only OR MIT)" })))).toEqual([]);
  });

  test("LIC002: no license field, a non-SPDX field, or a package that is not installed", () => {
    expect(codes(repo(["a"], dep("a", {})))).toEqual(["LIC002"]);
    expect(codes(repo(["a"], dep("a", { license: "SEE LICENSE IN LICENSE" })))).toEqual(["LIC002"]);
    expect(codes(repo(["ghost"], {}))).toEqual(["LIC002"]);
  });

  test("an exception with a reason lets one package through and is printed every run", () => {
    const root = repo(["a"], dep("a", { license: "GPL-3.0-only" }), {
      exceptions: [
        { package: "a", license: "GPL-3.0-only", reason: "used only as a separate program" },
      ],
    });
    const { violations, notes } = report(root);
    expect(violations).toEqual([]);
    expect(notes.join("\n")).toContain("a@1.0.0: GPL-3.0-only (used only as a separate program)");
  });

  test("an exception for another version does not apply", () => {
    const root = repo(["a"], dep("a", { license: "GPL-3.0-only" }), {
      exceptions: [{ package: "a", version: "2.0.0", license: "GPL-3.0-only", reason: "x" }],
    });
    expect(codes(root)).toEqual(["LIC001"]);
  });

  test("MPL-2.0 passes unmodified and fails when the package is patched", () => {
    const files = dep("a", { license: "MPL-2.0" });
    expect(codes(repo(["a"], files))).toEqual([]);
    const patched = repo(
      ["a"],
      files,
      {},
      { pnpm: { patchedDependencies: { "a@1.0.0": "patches/a.patch" } } },
    );
    expect(codes(patched)).toEqual(["LIC001"]);
  });

  test("runtime roots outside the workspaces are checked too", () => {
    const root = repo([], dep("tsx", { license: "AGPL-3.0-only" }), {
      extra_runtime_roots: [{ package: "tsx", reason: "server runs on it" }],
    });
    expect(codes(root)).toEqual(["LIC001"]);
  });
});

describe("asset manifest", () => {
  const manifest = (asset: Record<string, unknown>): Record<string, string> => ({
    "harness/config/third-party-assets.yaml": YAML.stringify({ assets: [asset] }),
    "data/schemas.json": "{}",
    "data/LICENSE.md": MIT_TEXT,
  });
  const asset = {
    id: "schemas",
    name: "Some schemas",
    license: "BSD-3-Clause",
    source: "https://example.com/schemas",
    where: "tests",
    paths: ["data/schemas.json"],
    license_file: "data/LICENSE.md",
  };

  test("a good entry passes and appears in the notices", () => {
    const root = repo([], manifest(asset));
    expect(codes(root)).toEqual([]);
    expect(evaluate(root).components.map((c) => c.name)).toEqual(["Some schemas"]);
  });

  test("LIC001 for a denied asset license, LIC004 for a missing file or field", () => {
    expect(codes(repo([], manifest({ ...asset, license: "CC-BY-NC-4.0" })))).toEqual(["LIC001"]);
    expect(codes(repo([], manifest({ ...asset, paths: ["data/gone.bin"] })))).toEqual(["LIC004"]);
    expect(codes(repo([], manifest({ ...asset, source: undefined })))).toEqual(["LIC004"]);
  });
});

describe("references to outside material", () => {
  const files = (paths: string[]): Record<string, string> => ({
    "harness/config/third-party-assets.yaml": YAML.stringify({
      references: [
        { id: "presets", name: "Preset shapes", paths, text: "Drawn by hand.\n  Nothing copied." },
      ],
    }),
    "data/shapes.ts": "export {};",
  });

  test("a reference is written in its own section and is not a component", () => {
    const root = repo([], files(["data/shapes.ts"]));
    expect(codes(root)).toEqual([]);
    write(root);
    const text = readFileSync(path.join(root, "THIRD_PARTY_NOTICES.md"), "utf8");
    expect(text).toContain("## 바깥 자료를 참고한 데이터");
    expect(text).toContain("Drawn by hand. Nothing copied.");
    expect(evaluate(root).components).toEqual([]);
  });

  test("LIC004 when the file a reference names does not exist", () => {
    expect(codes(repo([], files(["data/gone.ts"])))).toEqual(["LIC004"]);
  });
});

describe("notices file", () => {
  const files = dep("a", { license: "MIT", repository: "github:me/a" }, { LICENSE: MIT_TEXT });

  test("LIC003: a missing or old notices file is a note, not a failure", () => {
    const root = repo(["a"], files);
    const out = report(root);
    expect(out.violations).toEqual([]);
    expect(out.notes.join("\n")).toContain("LIC003");
    write(root);
    expect(report(root).notes).toEqual([]);
    writeFiles(root, dep("b", { license: "MIT" }));
    writeFileSync(
      path.join(root, "apps/server/package.json"),
      JSON.stringify({ name: "@t/server", dependencies: { a: "1", b: "1" } }),
    );
    expect(report(root).notes.join("\n")).toContain("LIC003");
  });

  test("the written file names each component once and keeps the copyright line", () => {
    const root = repo(["a"], files);
    write(root);
    const text = readFileSync(path.join(root, "THIRD_PARTY_NOTICES.md"), "utf8");
    expect(text).toContain("### a@1.0.0");
    expect(text).toContain("https://github.com/me/a");
    expect(text).toContain("Copyright (c) 2020 Someone");
    expect(text.match(/Permission is hereby granted/g)).toHaveLength(1);
  });

  test("the command line prints OK, or the failures with a nonzero exit", () => {
    const lines: string[] = [];
    expect(main(["--root", repo(["a"], files)], (l) => lines.push(l))).toBe(0);
    expect(lines.join("\n")).toContain("OK   licenses");
    const bad = repo(["a"], dep("a", { license: "GPL-3.0-only" }));
    expect(main(["--root", bad], () => {})).toBe(1);
  });
});
