import { rmSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { findRoot } from "../common";
import { miniRepo } from "../test-support/mini-repo";
import { run } from "./architecture";
import { findCycles } from "./import-graph";

const codes = (root: string): string[] => [...new Set(run(root).map((v) => v.code))].sort();

describe("architecture check [REQ-021]", () => {
  test("a healthy mini repo passes", () => {
    expect(run(miniRepo())).toEqual([]);
  });

  test("the real repository passes", () => {
    expect(run(findRoot())).toEqual([]);
  });

  test("upward imports are rejected", () => {
    const root = miniRepo({
      "packages/core/src/kernel/a.ts": 'import { w } from "../world";\nexport const a = w;\n',
    });
    expect(codes(root)).toEqual(["ARCH010"]);
  });

  test("downward imports through the index are allowed, by path or alias", () => {
    const root = miniRepo({
      "packages/core/src/world/a.ts": 'import { k } from "../kernel";\nexport const a = k;\n',
      "apps/server/src/http/b.ts":
        'import { w } from "@praxium/core/world";\nexport const b = w;\n',
    });
    expect(run(root)).toEqual([]);
  });

  test("reaching inside another unit is rejected", () => {
    const root = miniRepo({
      "packages/core/src/kernel/ids.ts": "export const x = 1;\n",
      "packages/core/src/world/a.ts": 'import { x } from "../kernel/ids";\nexport const a = x;\n',
    });
    expect(codes(root)).toEqual(["ARCH011"]);
  });

  test("imports inside one unit are free", () => {
    const root = miniRepo({
      "packages/core/src/world/a.ts": 'import { b } from "./b";\nexport const a = b;\n',
      "packages/core/src/world/b.ts": "export const b = 1;\n",
    });
    expect(run(root)).toEqual([]);
  });

  test("sibling units must not know each other", () => {
    const root = miniRepo({
      "packages/core/src/sources/gen/a.ts": 'import { i } from "../imp";\nexport const a = i;\n',
    });
    expect(codes(root)).toEqual(["ARCH012"]);
  });

  test("a child unit may use its parent unit", () => {
    const root = miniRepo({
      "packages/core/src/sources/gen/a.ts": 'import { s } from "..";\nexport const a = s;\n',
    });
    expect(run(root)).toEqual([]);
  });

  test("production code must not import test support, tests may", () => {
    const files = {
      "packages/core/src/world/test-support/fake.ts": "export const fake = 1;\n",
      "packages/core/src/world/a.ts":
        'import { fake } from "./test-support/fake";\nexport const a = fake;\n',
      "packages/core/src/world/a.test.ts":
        'import { fake } from "./test-support/fake";\nexport const t = fake;\n',
    };
    const found = run(miniRepo(files));
    expect(found.map((v) => [v.code, v.path])).toEqual([
      ["ARCH013", "packages/core/src/world/a.ts"],
    ]);
  });

  test("unresolvable imports are reported", () => {
    const root = miniRepo({
      "packages/core/src/world/a.ts": 'import { x } from "./missing";\nexport const a = x;\n',
    });
    expect(codes(root)).toEqual(["ARCH014"]);
  });

  test("undeclared packages are rejected", () => {
    const root = miniRepo({
      "packages/core/src/world/a.ts": 'import lodash from "lodash";\nexport const a = lodash;\n',
    });
    expect(run(root)[0]?.message).toContain("not declared");
  });

  test("declared packages are confined to their units", () => {
    const root = miniRepo({
      "packages/core/src/world/a.ts": 'import Fastify from "fastify";\nexport const a = Fastify;\n',
      "apps/server/src/http/b.ts": 'import Fastify from "fastify";\nexport const b = Fastify;\n',
      "packages/core/src/world/c.ts": 'import { z } from "zod";\nexport const c = z;\n',
    });
    expect(run(root).map((v) => [v.code, v.path])).toEqual([
      ["ARCH020", "packages/core/src/world/a.ts"],
    ]);
  });

  test("scoped package names are recognised", () => {
    const root = miniRepo({
      "packages/core/src/world/a.ts": 'import x from "@scope/pkg/deep";\nexport const a = x;\n',
    });
    expect(run(root)[0]?.message).toContain("'@scope/pkg'");
  });

  test("node built-ins are rejected in browser-safe code and allowed on the server", () => {
    const root = miniRepo({
      "packages/core/src/world/a.ts": 'import fs from "node:fs";\nexport const a = fs;\n',
      "packages/core/src/world/b.ts": 'import path from "path";\nexport const b = path;\n',
      "apps/server/src/http/c.ts": 'import fs from "node:fs";\nexport const c = fs;\n',
    });
    expect(run(root).map((v) => [v.code, v.path])).toEqual([
      ["ARCH021", "packages/core/src/world/a.ts"],
      ["ARCH021", "packages/core/src/world/b.ts"],
    ]);
  });

  test("test-only packages are rejected outside tests", () => {
    const root = miniRepo({
      "packages/core/src/world/a.ts":
        'import { expect } from "vitest";\nexport const a = expect;\n',
      "packages/core/src/world/a.test.ts":
        'import { expect } from "vitest";\nexport const t = expect;\n',
    });
    expect(run(root).map((v) => [v.code, v.path])).toEqual([
      ["ARCH022", "packages/core/src/world/a.ts"],
    ]);
  });

  test("dynamic import() and require() are checked too", () => {
    const root = miniRepo({
      "packages/core/src/kernel/a.ts":
        'export const a = () => import("../world");\nexport const b = require("fastify");\n',
    });
    expect(codes(root)).toEqual(["ARCH010", "ARCH020"]);
  });

  test("type-only imports follow the same layer rules", () => {
    const root = miniRepo({
      "packages/core/src/kernel/a.ts":
        'import type { w } from "../world";\nexport type A = typeof w;\n',
    });
    expect(codes(root)).toEqual(["ARCH010"]);
  });

  test("import cycles are rejected, type-only cycles are not", () => {
    const cyclic = miniRepo({
      "packages/core/src/world/a.ts": 'import { b } from "./b";\nexport const a = 1 + b;\n',
      "packages/core/src/world/b.ts": 'import { a } from "./a";\nexport const b = 1 + a;\n',
    });
    expect(codes(cyclic)).toEqual(["ARCH030"]);
    const typeOnly = miniRepo({
      "packages/core/src/world/a.ts": 'import type { B } from "./b";\nexport type A = { b?: B };\n',
      "packages/core/src/world/b.ts": 'import type { A } from "./a";\nexport type B = { a?: A };\n',
    });
    expect(run(typeOnly)).toEqual([]);
  });

  test("files outside any unit are rejected unless ignored", () => {
    const root = miniRepo({
      "packages/core/src/stray.ts": "export const s = 1;\n",
      "apps/server/tool.config.ts": "export {};\n",
    });
    expect(run(root).map((v) => [v.code, v.path])).toEqual([
      ["ARCH001", "packages/core/src/stray.ts"],
    ]);
  });

  test("a unit without index.ts is rejected, a missing unit folder too", () => {
    const missingIndex = miniRepo({ "packages/core/src/world/index.ts": "" });
    rmSync(path.join(missingIndex, "packages/core/src/world/index.ts"));
    expect(codes(missingIndex)).toEqual(["ARCH040"]);
    const missingFolder = miniRepo();
    rmSync(path.join(missingFolder, "apps/server/src/http"), { recursive: true });
    expect(codes(missingFolder)).toEqual(["ARCH003"]);
  });

  test.each([
    ["eval", 'export const a = eval("1");\n', "ARCH050"],
    ["new Function", 'export const a = new Function("return 1");\n', "ARCH050"],
    ["debugger", "export function a() {\n  debugger;\n}\n", "ARCH050"],
    ["export star", 'export * from "./b";\n', "ARCH051"],
    ["ts-ignore", "// @ts-ignore\nexport const a: number = 'x';\n", "ARCH051"],
    ["Date.now", "export const a = Date.now();\n", "ARCH052"],
    ["new Date()", "export const a = new Date();\n", "ARCH052"],
    ["Math.random", "export const a = Math.random();\n", "ARCH052"],
    ["performance.now", "export const a = performance.now();\n", "ARCH052"],
    ["crypto.randomUUID", "export const a = crypto.randomUUID();\n", "ARCH052"],
  ])("banned construct: %s", (_name, source, code) => {
    const root = miniRepo({
      "packages/core/src/world/a.ts": source,
      "packages/core/src/world/b.ts": "export const b = 1;\n",
    });
    expect(codes(root)).toEqual([code]);
  });

  test("new Date(value) is deterministic and allowed", () => {
    expect(
      run(miniRepo({ "packages/core/src/world/a.ts": "export const a = new Date(0);\n" })),
    ).toEqual([]);
  });

  test("nondeterminism is allowed where configured", () => {
    const root = miniRepo({
      "packages/core/src/kernel/entropy.ts": "export const a = Date.now() + Math.random();\n",
      "apps/server/src/http/b.ts": "export const b = Date.now();\n",
    });
    expect(run(root)).toEqual([]);
  });

  test("every violation explains how to fix it", () => {
    const root = miniRepo({
      "packages/core/src/kernel/a.ts":
        'import { w } from "../world";\nexport const a = w + Math.random();\n',
    });
    const found = run(root);
    expect(found.length).toBeGreaterThan(1);
    for (const v of found) {
      expect(v.fix.length).toBeGreaterThan(10);
      expect(v.code).toMatch(/^ARCH\d{3}$/);
    }
  });
});

describe("import graph cycles [REQ-021]", () => {
  test("finds components with more than one member and self loops", () => {
    const graph = new Map<string, Set<string>>([
      ["a", new Set(["b"])],
      ["b", new Set(["a", "c"])],
      ["c", new Set(["missing"])],
      ["d", new Set(["d"])],
    ]);
    expect(findCycles(graph)).toEqual([["a", "b"], ["d"]]);
  });

  test("an acyclic graph has no cycles", () => {
    expect(
      findCycles(
        new Map([
          ["a", new Set(["b"])],
          ["b", new Set<string>()],
        ]),
      ),
    ).toEqual([]);
  });
});
