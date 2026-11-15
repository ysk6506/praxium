import { describe, expect, test } from "vitest";
import { miniRepo } from "../test-support/mini-repo";
import { workflowPinViolations } from "./workflow-pins";

const HASH = "11d5960a326750d5838078e36cf38b85af677262";
const workflow = (uses: string) => `name: t\njobs:\n  a:\n    steps:\n      - ${uses}\n`;
const check = (files: Record<string, string>) => workflowPinViolations(miniRepo(files, {}));

describe("outside actions are pinned to a commit [REQ-471]", () => {
  test("a tag, a branch and no version are WFL001, with the file and the line", () => {
    const found = check({
      ".github/workflows/ci.yml": workflow("uses: actions/checkout@v4"),
      ".github/workflows/b.yaml": workflow("uses: actions/checkout@main"),
      ".github/workflows/c.yml": workflow("uses: actions/checkout"),
    });
    expect(found.map((v) => v.code)).toEqual(["WFL001", "WFL001", "WFL001"]);
    expect(found.map((v) => v.path).sort()).toEqual([
      ".github/workflows/b.yaml",
      ".github/workflows/c.yml",
      ".github/workflows/ci.yml",
    ]);
    expect(found[0]?.line).toBe(5);
    expect(found[0]?.fix).toContain("git ls-remote");
  });

  test("a full commit hash passes, with or without a version comment", () => {
    expect(
      check({
        ".github/workflows/ci.yml": workflow(`uses: actions/checkout@${HASH} # v4.4.0`),
        ".github/workflows/b.yml": workflow(`uses: actions/checkout@${HASH}`),
        ".github/workflows/c.yml": workflow(`uses: "actions/checkout@${HASH}" # anything`),
      }),
    ).toEqual([]);
  });

  test("a short hash, an upper-case hash and a comment that names a hash do not count", () => {
    const found = check({
      ".github/workflows/a.yml": workflow("uses: actions/checkout@11d5960"),
      ".github/workflows/b.yml": workflow(`uses: actions/checkout@${HASH.toUpperCase()}`),
      ".github/workflows/c.yml": workflow(`uses: actions/checkout@v4 # ${HASH}`),
    });
    expect(found).toHaveLength(3);
  });

  test("an action of this repository passes", () => {
    expect(
      check({ ".github/workflows/ci.yml": workflow("uses: ./.github/actions/setup") }),
    ).toEqual([]);
  });

  test("a composite action is read too, and a reusable workflow by tag fails", () => {
    const found = check({
      ".github/actions/setup/action.yml":
        "runs:\n  using: composite\n  steps:\n    - uses: pnpm/action-setup@v4\n",
      ".github/workflows/ci.yml": "jobs:\n  a:\n    uses: o/r/.github/workflows/w.yml@v1\n",
    });
    expect(found.map((v) => v.path).sort()).toEqual([
      ".github/actions/setup/action.yml",
      ".github/workflows/ci.yml",
    ]);
  });

  test("a container image needs its digest", () => {
    const digest = `sha256:${"a".repeat(64)}`;
    expect(
      check({ ".github/workflows/a.yml": workflow(`uses: docker://alpine@${digest}`) }),
    ).toEqual([]);
    expect(
      check({ ".github/workflows/a.yml": workflow("uses: docker://alpine:3.20") }),
    ).toHaveLength(1);
  });

  test("a commented-out line and a repository with no workflows pass", () => {
    expect(check({ ".github/workflows/a.yml": "# uses: actions/checkout@v4\n" })).toEqual([]);
    expect(check({})).toEqual([]);
  });
});
