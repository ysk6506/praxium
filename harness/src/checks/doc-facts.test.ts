import { describe, expect, test } from "vitest";
import { findRoot } from "../common";
import { docsRepo, front, TODAY } from "../test-support/docs-repo";
import { run } from "./docs";

const ROUTES =
  'export const R = {\n  a: {\n    method: "GET",\n    path: "/api/a",\n  },\n  b: {\n    method: "POST",\n    path: "/api/b",\n  },\n};\n';
const fact = (name: string, value: number): string => `<!-- fact:${name} -->${value}<!-- /fact -->`;
const repo = (guide: string): string =>
  docsRepo({
    "docs/guide.md": `${front()}# G\n${guide}\n`,
    "packages/core/src/contract/routes.ts": ROUTES,
    "harness/src/rules.ts": 'export const A = "ARCH010";\nexport const B = "DOC010";\n',
  });
const found = (root: string) => run(root, TODAY).filter((v) => v.code === "DOC095");

describe("counted facts [REQ-020]", () => {
  test("a written number equal to the count passes", () => {
    expect(
      found(repo(`${fact("contract_routes", 2)} routes, ${fact("check_codes", 2)} codes`)),
    ).toEqual([]);
  });

  test("a different number fails and says which number is true", () => {
    const [violation] = found(repo(`${fact("contract_routes", 7)} routes`));
    expect(violation?.message).toContain("says 7");
    expect(violation?.message).toContain("has 2");
    expect(violation?.fix).toContain("reverify does not fix");
  });

  test("an unknown fact name fails", () => {
    expect(found(repo(fact("nonsense", 1))).map((v) => v.message)).toEqual([
      "unknown fact 'nonsense'",
    ]);
  });

  test("the real repository's counts come from the running core code", () => {
    const root = findRoot();
    expect(run(root).filter((v) => v.code === "DOC095")).toEqual([]);
  });
});
