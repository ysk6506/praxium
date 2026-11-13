import { execFileSync } from "node:child_process";
import { writeFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, test } from "vitest";
import { API_CONFIG, apiRepo, healthyContract } from "../test-support/api-fixture";
import { commitAll, miniRepo } from "../test-support/mini-repo";
import type { BaseReader } from "./api-breaking";
import { type ApiIo, defaultIo, run } from "./api-contract";
import { asObject } from "./api-json";
import { bodyFingerprint } from "./doc-print";

const SERVED = [
  { method: "GET", path: "/v1/things", roles: ["read"] },
  { method: "GET", path: "/v1/health/live", roles: ["anonymous"] },
  { method: "PATCH", path: "/v1/things/{thing_id}", roles: ["read"] },
  { method: "POST", path: "/v1/things/{thing_id}:approve", roles: ["read"] },
  { method: "POST", path: "/v1/kgs/{kg}/questions/{question_name}", roles: ["read"] },
];

function io(over: Partial<ApiIo> = {}): ApiIo {
  const text = JSON.stringify(healthyContract(), null, 2);
  return {
    assemble: () => ({ text: `${text}\n` }),
    listRoutes: () => ({ routes: SERVED }),
    base: null,
    ...over,
  };
}

const at = (files: Record<string, string>): BaseReader => ({
  read: (p) => files[p] ?? null,
  has: (p) => p in files,
});

describe("the API checks as one [REQ-143]", () => {
  test("a repository without the configuration is not checked", () => {
    expect(run(miniRepo())).toEqual([]);
  });

  test("a healthy repository passes", () => {
    const root = apiRepo();
    expect(run(root, undefined, io())).toEqual([]);
  });

  test("the problems of every part are reported together", () => {
    const root = apiRepo();
    const found = run(
      root,
      undefined,
      io({ assemble: () => ({ text: "{}\n" }), listRoutes: () => ({ error: "x" }) }),
    );
    expect(found.map((v) => v.code)).toEqual(expect.arrayContaining(["API020", "API051"]));
  });

  test("a contract that is not JSON or is missing is reported once", () => {
    const bad = apiRepo({ [API_CONFIG.openapi]: "nope" });
    expect(run(bad, undefined, io()).map((v) => v.code)).toContain("API001");
    const gone = apiRepo();
    execFileSync("rm", [path.join(gone, API_CONFIG.openapi)]);
    expect(run(gone, undefined, io()).map((v) => v.code)).toEqual(["API022"]);
  });

  test("a break against the base needs a new sealed ADR that declares it", () => {
    const baseDoc = healthyContract();
    const baseText = `${JSON.stringify(baseDoc, null, 2)}\n`;
    // The base had one more response than the change.
    const before = structuredClone(baseDoc);
    const live = asObject(asObject(asObject(before.paths)["/v1/health/live"]).get);
    asObject(live.responses)["404"] = { $ref: "#/components/responses/NotFound" };
    const base = at({
      [API_CONFIG.openapi]: JSON.stringify(before),
      "docs/design-docs/adr/0001-first.md": "x",
    });
    const root = apiRepo();
    const found = run(root, "origin/main", io({ base, assemble: () => ({ text: baseText }) }));
    expect(found.map((v) => v.code)).toEqual(["API030"]);

    const key = "GET /v1/health/live#404";
    const body = "Body\n";
    const adr = `---\ntitle: t\ndecision_hash: ${bodyFingerprint(body)}\nbreaks_contract:\n  - "${key}"\ncontract_break_reason: "${"x".repeat(45)}"\n---\n${body}`;
    const declared = apiRepo({ "docs/design-docs/adr/0002-break.md": adr });
    expect(
      run(declared, "origin/main", io({ base, assemble: () => ({ text: baseText }) })),
    ).toEqual([]);
    const sameAtBase = at({
      [API_CONFIG.openapi]: JSON.stringify(before),
      "docs/design-docs/adr/0002-break.md": adr,
    });
    expect(
      run(
        declared,
        "origin/main",
        io({ base: sameAtBase, assemble: () => ({ text: baseText }) }),
      ).map((v) => v.code),
    ).toEqual(["API030"]);
  });

  test("a repository without an ADR folder has no declarations", () => {
    const root = apiRepo();
    const found = run(
      root,
      "origin/main",
      io({ base: at({ [API_CONFIG.openapi]: JSON.stringify(healthyContract()) }) }),
    );
    expect(found).toEqual([]);
    const noAdrs = apiRepo();
    execFileSync("rm", ["-r", path.join(noAdrs, API_CONFIG.adr_dir)]);
    expect(run(noAdrs, "origin/main", io({ base: at({}) })).map((v) => v.code)).not.toContain(
      "API031",
    );
  });
});

describe("the default io runs the program once and reads git [REQ-143]", () => {
  test("a repository with no Go module reports the build failure for both commands", () => {
    const root = apiRepo();
    const made = defaultIo(root, API_CONFIG, undefined);
    try {
      const assembled = made.assemble();
      const listed = made.listRoutes();
      expect("error" in assembled).toBe(true);
      expect("error" in listed).toBe(true);
      expect(made.base).toBeNull();
    } finally {
      made.dispose?.();
    }
  });

  test("the base ref is read through git, and an unknown ref means no base", () => {
    const root = apiRepo();
    commitAll(root);
    writeFileSync(path.join(root, "later.txt"), "x");
    const made = defaultIo(root, API_CONFIG, "HEAD");
    try {
      expect(made.base?.has(API_CONFIG.openapi)).toBe(true);
      expect(made.base?.has("later.txt")).toBe(false);
      expect(made.base?.read(API_CONFIG.feature_routes)).toContain("/v1/things");
      expect(made.base?.read("later.txt")).toBeNull();
      expect(defaultIo(root, API_CONFIG, "no-such-ref").base).toBeNull();
    } finally {
      made.dispose?.();
    }
  });
});
